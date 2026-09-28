package download

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// ClientConfig is one configured Usenet server (a news server account from
// a provider such as Newshosting or Eweka): the built-in downloader connects
// straight to it, so no separate download client program is involved.
type ClientConfig struct {
	Host        string
	Port        int
	UseSSL      bool
	Username    string
	Password    string
	Connections int
}

// ReleaseError marks a failure that is the release's fault (a missing or
// undecodable article), as opposed to a connection or configuration problem
// on our side. Callers use it to decide whether to blocklist the release.
type ReleaseError struct{ Err error }

func (e *ReleaseError) Error() string { return e.Err.Error() }
func (e *ReleaseError) Unwrap() error { return e.Err }

// ProgressFunc is invoked as segments complete; bytesTotal is the NZB's
// declared total size (may be approximate — Usenet segment sizes are
// nominal).
type ProgressFunc func(bytesDone, bytesTotal int64)

// Result reports what a download managed to fetch. Articles that no server
// had are counted in MissingSegments rather than failing the download: like
// SABnzbd, the caller carries on to PAR2 repair, which can rebuild a file
// from its recovery blocks.
type Result struct {
	Paths           []string
	TotalSegments   int
	MissingSegments int
}

type job struct {
	fileIndex    int
	segmentIndex int
}

type failKind int

const (
	failNone     failKind = iota
	failNotFound          // the server does not have this article
	failDecode            // the article arrived but is not valid yEnc
	failConn              // the server could not be reached / logged in to
)

type outcome struct {
	kind failKind
	err  error
}

const (
	maxFetchAttempts = 2 // one retry on a fresh connection after a network error
	forwardBuffer    = 512
)

// Download fetches every file in nzb from a single server and fails if any
// article is missing. It is DownloadFromServers for the one-server case.
func Download(ctx context.Context, cfg ClientConfig, nzb *NZB, destDir string, onProgress ProgressFunc) ([]string, error) {
	res, err := DownloadFromServers(ctx, []ClientConfig{cfg}, nzb, destDir, onProgress)
	if err != nil {
		return nil, err
	}
	if res.MissingSegments > 0 {
		return nil, &ReleaseError{fmt.Errorf("%d of %d article(s) not found", res.MissingSegments, res.TotalSegments)}
	}
	return res.Paths, nil
}

// DownloadFromServers fetches every file in nzb into destDir (normally
// /downloads/incomplete/<job-id> — PRD §4.9). servers are in priority
// order: the first is tried for every article; an article it lacks (or any
// article, if the server is unreachable) falls through to the next server,
// and so on, exactly like SABnzbd's primary and backup servers. A server
// only opens connections once it is actually needed.
//
// It returns an error only for problems that are not the release's fault
// (nothing configured, a server down with no backup, disk errors).
func DownloadFromServers(ctx context.Context, servers []ClientConfig, nzb *NZB, destDir string, onProgress ProgressFunc) (Result, error) {
	if len(servers) == 0 {
		return Result{}, errors.New("no Usenet server configured")
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("create dest dir: %w", err)
	}

	paths := make([]string, len(nzb.Files))
	files := make([]*os.File, len(nzb.Files))
	totalSegments := 0
	for i, f := range nzb.Files {
		name := sanitizeFilename(articleFilename(f.Subject, i))
		path := filepath.Join(destDir, name)
		out, err := os.Create(path)
		if err != nil {
			closeAll(files)
			return Result{}, fmt.Errorf("create output file %s: %w", path, err)
		}
		files[i] = out
		paths[i] = path
		totalSegments += len(f.Segments)
	}
	defer closeAll(files)

	st := &dlState{files: files, total: nzb.TotalBytes(), onProgress: onProgress}

	chans := make([]chan job, len(servers))
	for i := range chans {
		size := forwardBuffer
		if i == 0 {
			size = 0
		}
		chans[i] = make(chan job, size)
	}

	go func() {
		defer close(chans[0])
		for fi, f := range nzb.Files {
			for si := range f.Segments {
				select {
				case chans[0] <- job{fileIndex: fi, segmentIndex: si}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	var all sync.WaitGroup
	for i, cfg := range servers {
		conns := cfg.Connections
		if conns < 1 {
			conns = 1
		}
		var tier sync.WaitGroup
		for w := 0; w < conns; w++ {
			tier.Add(1)
			all.Add(1)
			go func(i int, cfg ClientConfig) {
				defer tier.Done()
				defer all.Done()
				runWorker(ctx, st, nzb, cfg, chans[i], func(j job, o outcome) { forward(ctx, st, chans, i, j, o) })
			}(i, cfg)
		}
		go func(i int) {
			tier.Wait()
			if i+1 < len(chans) {
				close(chans[i+1])
			}
		}(i)
	}
	all.Wait()

	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if st.fatal != nil {
		return Result{}, st.fatal
	}
	return Result{Paths: paths, TotalSegments: totalSegments, MissingSegments: int(atomic.LoadInt64(&st.missing))}, nil
}

type dlState struct {
	files      []*os.File
	total      int64
	done       int64
	missing    int64
	onProgress ProgressFunc

	mu    sync.Mutex
	fatal error
}

func (s *dlState) recordFatal(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fatal == nil {
		s.fatal = err
	}
}

func (s *dlState) reportBytes(n int64) {
	newDone := atomic.AddInt64(&s.done, n)
	if s.onProgress != nil {
		s.onProgress(newDone, s.total)
	}
}

// forward hands a job that server i could not serve to the next server, or —
// past the last one — settles it as missing (or as a setup failure when the
// servers were unreachable rather than lacking the article).
func forward(ctx context.Context, st *dlState, chans []chan job, i int, j job, o outcome) {
	if i+1 < len(chans) {
		select {
		case chans[i+1] <- j:
		case <-ctx.Done():
		}
		return
	}
	switch o.kind {
	case failNotFound, failDecode:
		atomic.AddInt64(&st.missing, 1)
	default:
		st.recordFatal(o.err)
	}
}

// runWorker is one connection to one server. It connects lazily on its first
// job. A server that cannot be reached is marked dead for this worker: it
// keeps draining its queue, passing every job on to the next server.
func runWorker(ctx context.Context, st *dlState, nzb *NZB, cfg ClientConfig, jobs <-chan job, pass func(job, outcome)) {
	var (
		conn          *NNTPConn
		selectedGroup string
		dead          error
	)
	defer func() {
		if conn != nil {
			conn.Quit()
		}
	}()

	connect := func() error {
		c, err := DialNNTP(cfg.Host, cfg.Port, cfg.UseSSL, 30*time.Second)
		if err != nil {
			return err
		}
		if err := c.Authenticate(cfg.Username, cfg.Password); err != nil {
			c.Quit()
			return fmt.Errorf("nntp authenticate on %s: %w", cfg.Host, err)
		}
		conn, selectedGroup = c, ""
		return nil
	}
	drop := func() {
		if conn != nil {
			conn.Quit()
			conn = nil
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case j, ok := <-jobs:
			if !ok {
				return
			}
			if dead != nil {
				pass(j, outcome{kind: failConn, err: dead})
				continue
			}
			f := nzb.Files[j.fileIndex]
			seg := f.Segments[j.segmentIndex]

			var last outcome
			for attempt := 0; attempt < maxFetchAttempts; attempt++ {
				if conn == nil {
					if err := connect(); err != nil {
						dead = err
						last = outcome{kind: failConn, err: err}
						break
					}
				}
				if len(f.Groups) > 0 && f.Groups[0] != selectedGroup {
					// Some servers need a GROUP before BODY-by-id; a refusal is
					// not fatal (many serve BODY by message-id regardless).
					var ne *NNTPError
					if err := conn.SelectGroup(f.Groups[0]); err == nil {
						selectedGroup = f.Groups[0]
					} else if !errors.As(err, &ne) {
						drop()
						last = outcome{kind: failConn, err: fmt.Errorf("select group %s: %w", f.Groups[0], err)}
						continue
					}
				}

				body, err := conn.FetchBody(seg.MessageID)
				if err != nil {
					var ne *NNTPError
					if errors.As(err, &ne) && (ne.Code == 430 || ne.Code == 423) {
						last = outcome{kind: failNotFound, err: &ReleaseError{fmt.Errorf("fetch segment %s: %w", seg.MessageID, err)}}
						break
					}
					drop()
					last = outcome{kind: failConn, err: fmt.Errorf("fetch segment %s: %w", seg.MessageID, err)}
					continue
				}
				part, err := DecodeYenc(bytesReader(body))
				if err != nil {
					last = outcome{kind: failDecode, err: &ReleaseError{fmt.Errorf("decode segment %s: %w", seg.MessageID, err)}}
					break
				}
				offset := part.PartBegin - 1
				if offset < 0 {
					offset = 0
				}
				if _, err := st.files[j.fileIndex].WriteAt(part.Data, offset); err != nil {
					st.recordFatal(fmt.Errorf("write segment %s: %w", seg.MessageID, err))
					last = outcome{kind: failNone}
					break
				}
				st.reportBytes(int64(len(part.Data)))
				last = outcome{kind: failNone}
				break
			}
			if last.kind != failNone {
				pass(j, last)
			}
		}
	}
}

func closeAll(files []*os.File) {
	for _, f := range files {
		if f != nil {
			f.Close()
		}
	}
}
