package download

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rdborg/mediarium/internal/problems"
	"github.com/rdborg/mediarium/internal/speed"
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
		return nil, &ReleaseError{fmt.Errorf("%d of %d articles were not found on the server", res.MissingSegments, res.TotalSegments)}
	}
	return res.Paths, nil
}

// DownloadFromServers fetches every file in nzb into destDir (normally
// /downloads/incomplete/<job-id>). servers are in priority
// order: the first is tried for every article; an article it lacks (or any
// article, if the server is unreachable) falls through to the next server,
// and so on, exactly like SABnzbd's primary and backup servers. A server
// only opens connections once it is actually needed.
//
// A download that stops part way (paused, stopped, cut off by a restart or a
// failure) leaves a progress file in destDir (ResumeFile) listing the articles
// already saved. Calling this again for the same NZB and folder carries on
// with the rest; a folder with no progress file, or one written for another
// release, starts from nothing.
//
// It returns an error only for problems that are not the release's fault
// (nothing configured, a server down with no backup, disk errors).
func DownloadFromServers(ctx context.Context, servers []ClientConfig, nzb *NZB, destDir string, onProgress ProgressFunc) (Result, error) {
	if len(servers) == 0 {
		return Result{}, errors.New("no Usenet server has been added yet")
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("create dest dir: %w", err)
	}

	// A failure that dooms the whole download (the disk is full, say) stops
	// the workers at once instead of fetching the rest of the release only
	// to throw it away.
	parent := ctx
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	paths := make([]string, len(nzb.Files))
	files := make([]*os.File, len(nzb.Files))
	totalSegments := 0
	for i, name := range outputFileNames(nzb.Files) {
		paths[i] = filepath.Join(destDir, name)
		totalSegments += len(nzb.Files[i].Segments)
	}
	resume := loadResume(destDir, nzb, paths)
	for i, path := range paths {
		// A file that already holds saved articles is kept as it is; any
		// other starts empty.
		flags := os.O_RDWR | os.O_CREATE
		if !resume.has(i) {
			flags |= os.O_TRUNC
		}
		out, err := os.OpenFile(path, flags, 0o644)
		if err != nil {
			closeAll(files)
			return Result{}, fmt.Errorf("create output file %s: %w", path, err)
		}
		files[i] = out
	}
	defer closeAll(files)

	limits := make([]int64, len(nzb.Files))
	for i, f := range nzb.Files {
		limits[i] = fileSizeLimit(f)
	}
	st := &dlState{files: files, limits: limits, total: nzb.TotalBytes(), onProgress: onProgress, resume: resume, abort: cancel, nzb: nzb, repairable: par2Bytes(nzb)}
	st.done = resume.savedBytes()
	if st.done > 0 && onProgress != nil {
		onProgress(st.done, st.total)
	}

	// The progress file is refreshed every few seconds while articles arrive,
	// so a restart loses very little.
	stopSaver := make(chan struct{})
	saverDone := make(chan struct{})
	go func() {
		defer close(saverDone)
		t := time.NewTicker(resumeSaveEvery)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				resume.save()
			case <-stopSaver:
				return
			}
		}
	}()

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
				if resume.isDone(fi, si) {
					continue
				}
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
	close(stopSaver)
	<-saverDone

	if err := parent.Err(); err != nil {
		st.keepProgress()
		return Result{}, err
	}
	if st.fatal != nil {
		st.keepProgress()
		return Result{}, st.fatal
	}
	resume.remove()
	return Result{Paths: paths, TotalSegments: totalSegments, MissingSegments: int(atomic.LoadInt64(&st.missing))}, nil
}

// resumeSaveEvery is how often the progress file is refreshed during a
// download.
const resumeSaveEvery = 5 * time.Second

type dlState struct {
	files      []*os.File
	limits     []int64 // per file: the furthest byte an article may be written at
	total      int64
	done       int64
	missing    int64
	nzb        *NZB
	repairable int64 // bytes of articles PAR2 files in this release could rebuild, at most
	lostBytes  int64 // bytes of articles no server has
	onProgress ProgressFunc
	resume     *resumeState
	abort      context.CancelFunc // stops the download once a fatal error is recorded

	mu    sync.Mutex
	fatal error
}

// keepProgress is called when the download stops before it is complete: the
// files are flushed to disk first, so the progress file never claims more than
// the disk holds, and then it is written.
func (s *dlState) keepProgress() {
	for _, f := range s.files {
		if f != nil {
			_ = f.Sync()
		}
	}
	s.resume.save()
}

// segmentBytes is the declared size of the article job j fetches.
func (s *dlState) segmentBytes(j job) int64 {
	if s.nzb == nil || j.fileIndex >= len(s.nzb.Files) || j.segmentIndex >= len(s.nzb.Files[j.fileIndex].Segments) {
		return 0
	}
	return s.nzb.Files[j.fileIndex].Segments[j.segmentIndex].Bytes
}

// par2Bytes adds up the size of the PAR2 files in the release. Recovery data
// can rebuild about that much and no more, so a release that has lost more
// than this is beyond repair.
func par2Bytes(nzb *NZB) int64 {
	var n int64
	for i, name := range outputFileNames(nzb.Files) {
		if strings.HasSuffix(strings.ToLower(name), ".par2") {
			for _, seg := range nzb.Files[i].Segments {
				n += seg.Bytes
			}
		}
	}
	return n
}

func (s *dlState) recordFatal(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fatal == nil {
		s.fatal = err
	}
	if s.abort != nil {
		s.abort()
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
		n := atomic.AddInt64(&st.missing, 1)
		lost := atomic.AddInt64(&st.lostBytes, st.segmentBytes(j))
		if lost > st.repairable {
			// More is gone than the repair files could ever rebuild, so the
			// release can't be saved. Stop now instead of fetching the rest.
			reason := "and the release has no PAR2 files to repair it"
			if st.repairable > 0 {
				reason = "and the PAR2 files in the release can't repair that much"
			}
			st.recordFatal(&ReleaseError{fmt.Errorf("%d articles couldn't be found on your Usenet servers %s", n, reason)})
		}
	default:
		st.recordFatal(o.err)
	}
}

// dialAndAuth opens a connection to the server and signs in. The connection
// is closed again if signing in fails.
func dialAndAuth(cfg ClientConfig) (*NNTPConn, error) {
	c, err := DialNNTP(cfg.Host, cfg.Port, cfg.UseSSL, 30*time.Second)
	if err != nil {
		return nil, err
	}
	if err := c.Authenticate(cfg.Username, cfg.Password); err != nil {
		c.Quit()
		return nil, fmt.Errorf("nntp authenticate on %s: %w", cfg.Host, err)
	}
	return c, nil
}

// plainConnError turns a failed connection into the message that ends up in
// the download queue.
func plainConnError(cfg ClientConfig, err error) error {
	if errors.Is(err, errServerRemoved) {
		return err
	}
	noteConnProblem(cfg, err)
	if msg, ok := FriendlyError(err); ok {
		if IsTooManyConnections(err) {
			return errors.New(msg)
		}
		return fmt.Errorf("%s: %s", cfg.Host, msg)
	}
	return err
}

// runWorker is one connection to one server. It connects lazily on its first
// job, after taking a place from the server's shared gate (so all downloads
// together stay within the login's connection limit). A "too many
// connections" answer is not a failure: the worker waits a little and tries
// again, with fewer places, and only gives up after several minutes. A server
// that cannot be reached at all is marked dead for this worker: it keeps
// draining its queue, passing every job on to the next server. The connection
// is closed, and its place given back, when the worker ends for any reason:
// finished, failed, cancelled, or the server removed.
func runWorker(ctx context.Context, st *dlState, nzb *NZB, cfg ClientConfig, jobs <-chan job, pass func(job, outcome)) {
	var (
		conn          *NNTPConn
		release       func()
		stopWatch     func() bool
		selectedGroup string
		dead          error
	)
	gate := gateFor(cfg)

	// closeConn ends the connection, if there is one, and gives its place back.
	closeConn := func() {
		if conn != nil {
			if stopWatch != nil {
				stopWatch()
				stopWatch = nil
			}
			gate.untrack(conn)
			gate.noteClosed()
			conn.Quit()
			conn = nil
		}
		if release != nil {
			release()
			release = nil
		}
	}
	defer closeConn()

	connect := func() error {
		for {
			rel, err := gate.acquire(ctx)
			if err != nil {
				return err
			}
			c, err := dialAndAuth(cfg)
			if err == nil {
				conn, release, selectedGroup = c, rel, ""
				gate.track(c)
				gate.succeeded()
				// A cancelled download must not sit waiting on a slow read.
				stopWatch = context.AfterFunc(ctx, func() { c.Close() })
				return nil
			}
			rel()
			if !IsTooManyConnections(err) {
				return err
			}
			if err := sleepCtx(ctx, gate.tooMany()); err != nil {
				return err
			}
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
						if ctx.Err() != nil {
							return
						}
						dead = plainConnError(cfg, err)
						last = outcome{kind: failConn, err: dead}
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
						closeConn()
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
					closeConn()
					last = outcome{kind: failConn, err: fmt.Errorf("fetch segment %s: %w", seg.MessageID, err)}
					continue
				}
				part, err := DecodeYenc(bytesReader(body))
				if err != nil {
					last = outcome{kind: failDecode, err: &ReleaseError{fmt.Errorf("decode segment %s: %w", seg.MessageID, err)}}
					break
				}
				offset, err := segmentOffset(part, st.limits[j.fileIndex])
				if err != nil {
					last = outcome{kind: failDecode, err: &ReleaseError{fmt.Errorf("decode segment %s: %w", seg.MessageID, err)}}
					break
				}
				if _, err := st.files[j.fileIndex].WriteAt(part.Data, offset); err != nil {
					st.recordFatal(fmt.Errorf("write segment %s: %w", seg.MessageID, err))
					last = outcome{kind: failNone}
					break
				}
				st.resume.mark(j.fileIndex, j.segmentIndex, int64(len(part.Data)))
				st.reportBytes(int64(len(part.Data)))
				_ = speed.Wait(ctx, len(part.Data)) // the speed limit, when one is set
				last = outcome{kind: failNone}
				break
			}
			if last.kind != failNone {
				pass(j, last)
			}
			// The person lowered the connection count, or the provider said
			// too many: give this connection's place up.
			if conn != nil && gate.shedOne(conn) {
				release = nil // the gate has already taken the place back
				closeConn()
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

// noteConnProblem puts a server that could not be used in the problem log. A
// login that has too many connections open is noted where the provider says so
// (see serverGate.tooMany), so it is not counted twice here.
func noteConnProblem(cfg ClientConfig, err error) {
	if IsTooManyConnections(err) {
		return
	}
	code := problems.UsenetCode(err)
	message := fmt.Sprintf("Could not use %s.", cfg.Host)
	if code == problems.CodeUsenetAuthRefused {
		message = fmt.Sprintf("%s refused the username and password.", cfg.Host)
	}
	problems.Record(problems.Problem{Code: code, Subject: cfg.Host, Message: message, Err: err})
}
