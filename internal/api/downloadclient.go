package api

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/rdborg/mediarium/internal/books"
	"github.com/rdborg/mediarium/internal/netguard"
	"github.com/rdborg/mediarium/internal/organizer"
	"github.com/rdborg/mediarium/internal/plainerror"
	"github.com/rdborg/mediarium/internal/qbit"
	"github.com/rdborg/mediarium/internal/queue"
	"github.com/rdborg/mediarium/internal/settings"
	"github.com/rdborg/mediarium/internal/subtitles"
	"github.com/rdborg/mediarium/internal/torrentopts"
)

// Download client: torrent releases go to an external program that speaks
// the qBittorrent Web API (qBittorrent, RDT-Client, Decypharr) instead of the
// built-in torrent client or a debrid service. The client downloads into its
// own folder and may keep seeding there. Mediarium never moves, changes or
// deletes those files: once the torrent is done, the download's own folder
// (queue-N) gets a link to each of the torrent's files (a "view"), archives
// are unpacked into that folder, and imports read through the links (see
// importDownloaded). Cleaning up the download's folder removes the links,
// never what they point at.

// downloadClientPoll is how often the client is asked how a torrent is doing.
// It is atomic because tests shorten it while earlier downloads' goroutines
// may still be reading it.
var downloadClientPoll = func() *atomic.Int64 {
	v := new(atomic.Int64)
	v.Store(int64(15 * time.Second))
	return v
}()

// downloadClientPollEvery is the current time between two status checks.
func downloadClientPollEvery() time.Duration { return time.Duration(downloadClientPoll.Load()) }

const (
	// downloadClientStall is how long a torrent may sit in the client without
	// any progress before it's given up on (no one sharing it), unless the
	// setting says otherwise (torrentStall).
	downloadClientStall = 6 * time.Hour
	// downloadClientGone is how long a torrent may be missing from the
	// client's list before the download fails.
	downloadClientGone = time.Minute
	// clientHashFile remembers the torrent's hash, so a restart or a retry
	// picks it up instead of adding the torrent again. A second line "keep"
	// marks a torrent that was in the client already, which Mediarium then
	// never removes.
	clientHashFile = ".mediarium-client-hash"
	// clientContentFile holds where the torrent's files are, as Mediarium
	// sees them. Its presence marks the folder as a view of the client's
	// files.
	clientContentFile = ".mediarium-client-content"
	// defaultClientCategory is the category torrents are added with when
	// none is set.
	defaultClientCategory = "mediarium"
)

type downloadClientConfig struct {
	Kind              string
	URL               string
	Username          string
	Password          string
	Category          string
	KindCategories    map[string]string // optional category per kind of download (torrentopts.Kind*)
	RemotePath        string
	LocalPath         string
	RemoveAfterImport bool
	RemoveOnStop      bool
	LinkRoot          string
}

func (s *Server) downloadClientConfig() downloadClientConfig {
	get := func(key string) string {
		v, _ := s.Settings.Get(key)
		return v
	}
	remove, _ := s.Settings.GetBool(settings.KeyDownloadClientRemoveImport)
	return downloadClientConfig{
		Kind:     get(settings.KeyDownloadClientKind),
		URL:      get(settings.KeyDownloadClientURL),
		Username: get(settings.KeyDownloadClientUsername),
		Password: get(settings.KeyDownloadClientPassword),
		Category: get(settings.KeyDownloadClientCategory),
		KindCategories: map[string]string{
			torrentopts.KindMovie: get(settings.KeyDownloadClientCategoryMovies),
			torrentopts.KindTV:    get(settings.KeyDownloadClientCategoryTV),
			torrentopts.KindMusic: get(settings.KeyDownloadClientCategoryMusic),
			torrentopts.KindBook:  get(settings.KeyDownloadClientCategoryBooks),
		},
		RemotePath:        get(settings.KeyDownloadClientRemotePath),
		LocalPath:         get(settings.KeyDownloadClientLocalPath),
		RemoveAfterImport: remove,
		RemoveOnStop:      s.removeOnStopOn(),
		LinkRoot:          get(settings.KeyDownloadClientLinkRoot),
	}
}

// category is the main category. An empty one stays empty when the person gave
// some kinds a category of their own: a kind without one then gets no category
// at all. Only when nothing is set anywhere does the default apply.
func (c downloadClientConfig) category() string {
	if v := strings.TrimSpace(c.Category); v != "" {
		return v
	}
	for _, k := range torrentopts.Kinds {
		if strings.TrimSpace(c.KindCategories[k]) != "" {
			return ""
		}
	}
	return defaultClientCategory
}

// categoryFor is the category a download of this kind is added with: the one
// set for the kind, or the main category when that is empty (which may itself
// mean none, see category).
func (c downloadClientConfig) categoryFor(kind string) string {
	if v := strings.TrimSpace(c.KindCategories[kind]); v != "" {
		return v
	}
	return c.category()
}

// allCategories lists every category Mediarium may have used: the main one and
// those of the kinds. Torrents in any of them are Mediarium's.
func (c downloadClientConfig) allCategories() []string {
	var out []string
	if v := c.category(); v != "" {
		out = append(out, v)
	}
	for _, k := range torrentopts.Kinds {
		if v := strings.TrimSpace(c.KindCategories[k]); v != "" && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

// downloadClientOn reports whether torrent releases go to an external
// download client.
func (s *Server) downloadClientOn() bool {
	c := s.downloadClientConfig()
	return qbit.ValidKind(c.Kind) && c.URL != ""
}

// newDownloadClient connects to the client at url. The client is usually on
// the home network, so local addresses are fine (only link-local and cloud
// metadata addresses are refused).
func newDownloadClient(kind, url, username, password string) qbit.API {
	return qbit.NewAPI(kind, url, username, password, netguard.Client(60*time.Second))
}

func clientTag(queueID int64) string { return fmt.Sprintf("mediarium-q%d", queueID) }

// downloadViaClient is downloadTorrent's counterpart when a download client
// is set: the client's progress is the download's progress. When the torrent
// is done, incompleteDir becomes a view of its files (see buildClientView).
func (s *Server) downloadViaClient(ctx context.Context, queueID int64, downloadURL, incompleteDir string) error {
	cfg := s.downloadClientConfig()
	c := newDownloadClient(cfg.Kind, cfg.URL, cfg.Username, cfg.Password)
	label := qbit.Label(cfg.Kind)
	if err := os.MkdirAll(incompleteDir, 0o755); err != nil {
		return fmt.Errorf("create download folder: %w", err)
	}
	hashPath := filepath.Join(incompleteDir, clientHashFile)
	tag := clientTag(queueID)
	hash, keep := readClientHash(hashPath)
	if hash == "" {
		// Added before a restart that came before its hash was known?
		list, err := c.InfoByTag(ctx, tag)
		if err != nil {
			return s.downloadClientError(label, err)
		}
		if len(list) > 0 {
			hash = list[0].Hash
		} else if hash, keep, err = s.addToClient(ctx, c, cfg, label, queueID, downloadURL, incompleteDir); err != nil {
			return err
		}
		writeClientHash(hashPath, hash, keep)
	}

	progress := newProgressSaver(func(pct float64) error { return s.QueueRepo.SetProgress(queueID, pct) })
	defer progress.Flush()
	report := func(pct float64) { progress.Report(int64(pct*100), 10000) }
	lastProgress, lastMove := -1.0, time.Now()
	var gone time.Time
	var t qbit.Torrent
	var fails pollFailures
	// firstLook: a torrent found paused when this download starts was paused
	// by an earlier pause of the download here, so it is started again. A
	// torrent that is paused later was paused in the client by hand: it is left
	// alone and not mistaken for a stall. settle skips a few polls after a
	// start, while the client's state catches up.
	firstLook, settle, notedPause := true, 0, false
	for {
		var found bool
		var err error
		if hash != "" {
			t, found, err = c.Info(ctx, hash)
		} else {
			var list []qbit.Torrent
			if list, err = c.InfoByTag(ctx, tag); err == nil && len(list) > 0 {
				t, found = list[0], true
			}
		}
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			s.noteClientCheck(watchTorrentClient, label, err)
			if !fails.tolerate(err, qbit.ErrAuth) {
				return s.downloadClientError(label, err)
			}
			slog.Warn("download client: status check failed, trying again", "queueId", queueID, "err", err)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(downloadClientPollEvery()):
			}
			continue
		}
		fails.reset()
		s.noteClientCheck(watchTorrentClient, label, nil)
		if found {
			gone = time.Time{}
			if hash == "" && t.Hash != "" {
				hash = t.Hash
				writeClientHash(hashPath, hash, keep)
			}
		}
		switch {
		case !found:
			if gone.IsZero() {
				gone = time.Now()
			} else if time.Since(gone) > downloadClientGone {
				_ = os.Remove(hashPath)
				return fmt.Errorf("%s doesn't have this torrent any more. It may have been removed there. Try the download again to send it once more", label)
			}
		case t.Failed():
			// The client's own trouble (its disk is full or read-only, or its files
			// were deleted), not something wrong with the release: the release is
			// not blocklisted and the torrent stays in the client for the person to
			// look at.
			_ = os.Remove(hashPath)
			return fmt.Errorf("%s reports a problem with this torrent (it says %q). Check its free space and folder permissions, then try the download again", label, t.State)
		case t.Done():
			report(100)
			return s.clientDone(queueID, t, cfg, label, incompleteDir)
		default:
			if t.Paused() && settle == 0 {
				if firstLook {
					if err := c.Resume(ctx, t.Hash); err != nil {
						slog.Warn("download client: couldn't start a paused torrent again", "queueId", queueID, "err", err)
					} else {
						s.queueEvent(queueID, "download", queue.LevelInfo, fmt.Sprintf("This torrent was paused in %s, so it was started again.", label))
					}
					settle = 3
				} else {
					lastMove = time.Now() // waiting on a person, not stalled
					if !notedPause {
						notedPause = true
						s.queueEvent(queueID, "download", queue.LevelInfo, fmt.Sprintf("Paused in %s. It carries on when you start it there.", label))
					}
				}
			} else if !t.Paused() {
				notedPause = false
			}
			firstLook = false
			if settle > 0 {
				settle--
			}
			if t.Waiting() {
				lastMove = time.Now() // in line in the client: not stalled
			}
			if t.Progress != lastProgress {
				lastProgress, lastMove = t.Progress, time.Now()
				report(t.Progress * 100)
			} else if stall := s.torrentStall(true); stall > 0 && time.Since(lastMove) > stall {
				s.clientForget(c, hash, keep, hashPath)
				return badRelease(fmt.Errorf("%s couldn't download it: nothing moved for %s, so probably no one is sharing it", label, stall))
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err() // paused or stopped: the torrent stays in the client for later
		case <-time.After(downloadClientPollEvery()):
		}
	}
}

// pollGrace is how long the status checks of a download client or debrid
// service may keep failing (a network blip, the client restarting) before the
// download fails. A refused password or key fails it at once. A variable so
// tests can shorten it.
var pollGrace = 3 * time.Minute

// pollFailures follows the status checks that failed in a row.
type pollFailures struct{ since time.Time }

// tolerate reports whether a failed check may be passed over (ask again at the
// next poll). auth is the error that means the login was refused.
func (p *pollFailures) tolerate(err, auth error) bool {
	if errors.Is(err, auth) {
		return false
	}
	if p.since.IsZero() {
		p.since = time.Now()
	}
	return time.Since(p.since) < pollGrace
}

// reset is called after a check that worked.
func (p *pollFailures) reset() { p.since = time.Time{} }

// addToClient hands the release to the client. It returns the torrent's
// hash ("" when it can't be known up front) and whether the torrent was in
// the client already (then it's never removed from there).
func (s *Server) addToClient(ctx context.Context, c qbit.API, cfg downloadClientConfig, label string, queueID int64, downloadURL, incompleteDir string) (hash string, keep bool, err error) {
	magnet, torrentFile, err := s.torrentSource(ctx, downloadURL, incompleteDir)
	if err != nil {
		return "", false, fmt.Errorf("couldn't get the torrent: %w", err)
	}
	var data []byte
	if torrentFile != "" {
		if data, err = os.ReadFile(torrentFile); err != nil {
			return "", false, fmt.Errorf("read the torrent file: %w", err)
		}
		_ = os.Remove(torrentFile) // it's in the client now; the folder is for the view
	}
	hash = qbit.InfoHash(magnet, data)
	if hash != "" {
		if _, there, err := c.Info(ctx, hash); err == nil && there {
			s.queueEvent(queueID, "download", queue.LevelInfo, label+" has this torrent already; using that one")
			return hash, true, nil
		}
	}
	category := cfg.category()
	if item, gerr := s.QueueRepo.Get(queueID); gerr == nil {
		category = cfg.categoryFor(queueKind(item))
	}
	err = c.Add(ctx, qbit.AddOptions{Magnet: magnet, TorrentFile: data, Category: category, Tag: clientTag(queueID), Hash: hash, Share: s.clientShareLimits(queueID)})
	if err != nil {
		return "", false, s.downloadClientError(label, err)
	}
	s.queueEvent(queueID, "download", queue.LevelInfo, "Sent to "+label)
	return hash, false, nil
}

// clientShareLimits is the seeding goal of a queue item as the client should
// enforce it: the global limits or the indexer's own rule.
func (s *Server) clientShareLimits(queueID int64) qbit.ShareLimits {
	g := s.torrentSeedGoal(queueID)
	return qbit.ShareLimits{Ratio: g.Ratio, Time: g.Time, NoSeed: g.NoSeed}
}

// clientDone lays out the finished torrent's files in the download's folder.
func (s *Server) clientDone(queueID int64, t qbit.Torrent, cfg downloadClientConfig, label, incompleteDir string) error {
	reported := t.Content()
	content := mapClientPath(reported, cfg.RemotePath, cfg.LocalPath)
	if content == "" || !filepath.IsAbs(content) {
		return fmt.Errorf("%s didn't say where it saved the files (%q). If it runs on another computer or in another container, set the path mapping under Settings > Downloading > Torrents", label, reported)
	}
	if _, err := os.Lstat(content); err != nil {
		hint := "set the path mapping under Settings > Downloading > Torrents, or give Mediarium access to the client's download folder"
		if cfg.RemotePath != "" {
			hint = "check the path mapping under Settings > Downloading > Torrents"
		}
		return fmt.Errorf("Mediarium can't see the finished download at %s. If %s runs on another computer or in another container, %s", content, label, hint)
	}
	n, err := buildClientView(content, incompleteDir)
	if err != nil {
		return fmt.Errorf("couldn't read the finished download in %s: %w", content, err)
	}
	if n == 0 {
		return badRelease(fmt.Errorf("the finished download at %s has no files", content))
	}
	s.queueEvent(queueID, "download", queue.LevelInfo, fmt.Sprintf("%s has it ready; importing %s from its folder", label, plural(n, "file")))
	return nil
}

func readClientHash(path string) (hash string, keep bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	lines := strings.Fields(string(b))
	if len(lines) == 0 {
		return "", false
	}
	return lines[0], len(lines) > 1 && lines[1] == "keep"
}

func writeClientHash(path, hash string, keep bool) {
	if hash == "" {
		return
	}
	body := hash
	if keep {
		body += "\nkeep"
	}
	_ = os.WriteFile(path, []byte(body+"\n"), 0o600)
}

// clientForget removes a torrent that failed from the client, with its
// files, unless it was there before Mediarium asked for it.
func (s *Server) clientForget(c qbit.API, hash string, keep bool, hashPath string) {
	_ = os.Remove(hashPath)
	if hash == "" || keep {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := c.Delete(ctx, hash, true); err != nil {
		slog.Warn("download client: couldn't remove a failed torrent", "hash", hash, "err", err)
	}
}

func (s *Server) downloadClientError(label string, err error) error {
	if errors.Is(err, qbit.ErrAuth) {
		return fmt.Errorf("%s refused the user name or password. Check them under Settings > Downloading > Torrents", label)
	}
	return err
}

// mapClientPath turns a path as the client reports it into the same path as
// Mediarium sees it: a path inside remote gets local in its place. Without a
// mapping (or for a path outside remote) the path is used as it is.
func mapClientPath(p, remote, local string) string {
	p = strings.TrimSpace(p)
	if p == "" || remote == "" || local == "" {
		return filepath.Clean(filepath.FromSlash(p))
	}
	norm := func(v string) string { return strings.TrimRight(strings.ReplaceAll(v, `\`, "/"), "/") }
	r, q := norm(remote), strings.ReplaceAll(p, `\`, "/")
	// Windows paths don't care about case.
	windows := strings.Contains(remote, `\`) || (len(r) >= 2 && r[1] == ':')
	prefix, rest := q, ""
	if len(q) > len(r) {
		prefix, rest = q[:len(r)], q[len(r):]
	}
	same := prefix == r || (windows && strings.EqualFold(prefix, r))
	if !same || (rest != "" && rest[0] != '/') {
		return filepath.Clean(filepath.FromSlash(p))
	}
	return filepath.Join(local, filepath.FromSlash(rest))
}

// buildClientView lays out the torrent's files in viewDir as links to where
// they are in the client's folder (content: the torrent's folder, or the one
// file of a single-file torrent; then only that file, never others next to
// it). Folders are made for real; links already there from an earlier try
// are made again. It returns how many files it linked.
func buildClientView(content, viewDir string) (int, error) {
	info, err := os.Lstat(content)
	if err != nil {
		return 0, err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		if st, err := os.Stat(content); err == nil && st.IsDir() {
			// The torrent's folder is itself a link: walk the folder it leads to.
			if content, err = filepath.EvalSymlinks(content); err != nil {
				return 0, err
			}
			if info, err = os.Lstat(content); err != nil {
				return 0, err
			}
		}
	}
	if err := os.WriteFile(filepath.Join(viewDir, clientContentFile), []byte(content+"\n"), 0o600); err != nil {
		return 0, fmt.Errorf("write %s: %w", clientContentFile, err)
	}
	if !info.IsDir() {
		if !isFileOrLink(info.Mode()) {
			return 0, nil
		}
		return 1, viewLink(content, filepath.Join(viewDir, filepath.Base(content)))
	}
	n := 0
	err = filepath.WalkDir(content, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(content, p)
		if err != nil || rel == "." {
			return err
		}
		if rel == clientHashFile || rel == clientContentFile {
			return nil
		}
		at := filepath.Join(viewDir, rel)
		if d.IsDir() {
			return os.MkdirAll(at, 0o755)
		}
		if !isFileOrLink(d.Type()) {
			return nil // a device, a pipe: not a download
		}
		n++
		return viewLink(p, at)
	})
	return n, err
}

func isFileOrLink(m fs.FileMode) bool { return m.IsRegular() || m&fs.ModeSymlink != 0 }

// viewLink makes at a link to target, replacing a link made by an earlier
// try. A real file at that name (unpacked earlier) is left alone.
func viewLink(target, at string) error {
	if fi, err := os.Lstat(at); err == nil {
		if fi.Mode()&fs.ModeSymlink == 0 {
			return nil
		}
		if err := os.Remove(at); err != nil {
			return err
		}
	}
	return os.Symlink(target, at)
}

// isClientView reports whether dir holds links to an external client's
// files (see buildClientView).
func isClientView(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, clientContentFile))
	return err == nil
}

// clientFileBehind follows a link in a download's folder back to the
// client's file it was made for. A link Mediarium didn't make (anything not
// leading into the torrent's own files) is refused like any other link.
func (s *Server) clientFileBehind(link string) (string, error) {
	refuse := fmt.Errorf("%s: %w", filepath.Base(link), organizer.ErrNotRegularFile)
	work := filepath.Clean(s.downloadsIncompleteDir())
	link = filepath.Clean(link)
	if !isWithin(work, link) {
		return "", refuse
	}
	rel, _ := filepath.Rel(work, link)
	first, _, _ := strings.Cut(rel, string(filepath.Separator))
	if _, ok := queueDirID(first); !ok {
		return "", refuse
	}
	b, err := os.ReadFile(filepath.Join(work, first, clientContentFile))
	if err != nil {
		return "", refuse
	}
	content := filepath.Clean(strings.TrimSpace(string(b)))
	target, err := os.Readlink(link)
	if err != nil || !filepath.IsAbs(target) {
		return "", refuse
	}
	target = filepath.Clean(target)
	if target != content && !isWithin(content, target) {
		return "", refuse
	}
	return target, nil
}

// importDownloaded imports one file of a finished download. Files Mediarium
// downloaded or unpacked itself go through organizer.Import; a link to an
// external client's file goes through organizer.ImportClientFile, which
// leaves the client's file where it is.
func (s *Server) importDownloaded(src, dest string, policy organizer.ConflictPolicy, isBetter func() bool) (organizer.ImportResult, error) {
	if err := s.checkLibraryRoot(dest); err != nil {
		return organizer.ImportResult{}, err
	}
	if info, err := os.Lstat(src); err != nil || info.Mode()&fs.ModeSymlink == 0 {
		if err := s.checkRoomToMove(src, dest); err != nil {
			return organizer.ImportResult{}, err
		}
		return organizer.Import(src, dest, policy, isBetter)
	}
	clientFile, err := s.clientFileBehind(src)
	if err != nil {
		return organizer.ImportResult{}, err
	}
	root := s.downloadClientConfig().LinkRoot
	res, err := organizer.ImportClientFile(clientFile, dest, policy, isBetter, root)
	if errors.Is(err, organizer.ErrLinkNotAllowed) {
		slog.Warn("download client: refused a symlink", "path", clientFile, "err", err)
		name := filepath.Base(clientFile)
		if root == "" {
			return res, fmt.Errorf("%s is a symlink. Symlinks from your download client are only imported when you say where they may point. If your client makes them on purpose (Decypharr does), set \"Symlinks may point into\" under Settings > Downloading > Torrents to the folder they lead to", name)
		}
		return res, fmt.Errorf("%s is a symlink that leads outside %s, so it wasn't imported. Set \"Symlinks may point into\" under Settings > Downloading > Torrents to the folder your client's symlinks lead to", name, root)
	}
	return res, err
}

// clientFileReal is the plain file behind a link in a client view, checked
// the same way as an import: for copying small files (subtitles) that are
// read rather than linked.
func (s *Server) clientFileReal(link string) (string, bool) {
	clientFile, err := s.clientFileBehind(link)
	if err != nil {
		return "", false
	}
	info, err := os.Lstat(clientFile)
	if err != nil {
		return "", false
	}
	if info.Mode().IsRegular() {
		return clientFile, true
	}
	root := s.downloadClientConfig().LinkRoot
	if info.Mode()&fs.ModeSymlink == 0 || root == "" {
		return "", false
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", false
	}
	target, err := filepath.EvalSymlinks(clientFile)
	if err != nil || !isWithin(realRoot, target) {
		return "", false
	}
	if st, err := os.Stat(target); err != nil || !st.Mode().IsRegular() {
		return "", false
	}
	return target, true
}

// clientSidecars is sidecarsIn for a client view: the subtitles behind the
// links, each checked, by their real paths (subtitles are copied, and a copy
// never reads through a link).
func (s *Server) clientSidecars(dir string) []subtitles.Sidecar {
	scs, err := subtitles.FindSidecars(dir, subtitles.FollowLinks)
	if err != nil {
		slog.Warn("subtitles: look for subtitles in a download client's files", "err", err)
		return nil
	}
	out := scs[:0]
next:
	for _, sc := range scs {
		files := make([]string, 0, len(sc.Files))
		for _, f := range sc.Files {
			if info, err := os.Lstat(f); err == nil && info.Mode().IsRegular() {
				files = append(files, f)
				continue
			}
			real, ok := s.clientFileReal(f)
			if !ok {
				continue next
			}
			files = append(files, real)
		}
		sc.Files = files
		out = append(out, sc)
	}
	return out
}

// afterClientImport runs when a download from the client has been imported
// and its folder is about to go: with "remove from the client after import"
// on, the torrent is removed from the client. Its files go too, unless they
// are symlinks (Decypharr), which the library's new links still need, or the
// torrent was in the client before Mediarium asked for it.
func (s *Server) afterClientImport(dir string) {
	if id := readUsenetJob(dir); id != "" {
		s.afterUsenetImport(id)
		return
	}
	cfg := s.downloadClientConfig()
	if !cfg.RemoveAfterImport || !s.downloadClientOn() {
		return
	}
	hash, keep := readClientHash(filepath.Join(dir, clientHashFile))
	if hash == "" || keep {
		return
	}
	deleteFiles := !clientViewHasLinks(dir)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := newDownloadClient(cfg.Kind, cfg.URL, cfg.Username, cfg.Password)
	if err := c.Delete(ctx, hash, deleteFiles); err != nil {
		slog.Warn("download client: couldn't remove an imported torrent", "hash", hash, "err", err)
		return
	}
	slog.Info("download client: removed an imported torrent", "hash", hash, "withFiles", deleteFiles)
}

// clientViewHasLinks reports whether any of the client's files behind the
// view's links is itself a link.
func clientViewHasLinks(dir string) bool {
	found := false
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.Type()&fs.ModeSymlink == 0 {
			return nil
		}
		if target, err := os.Readlink(p); err == nil {
			if info, err := os.Lstat(target); err == nil && info.Mode()&fs.ModeSymlink != 0 {
				found = true
				return fs.SkipAll
			}
		}
		return nil
	})
	return found
}

type downloadClientPayload struct {
	Kind              string `json:"kind"` // "" (off), qbittorrent, rdtclient or decypharr
	URL               string `json:"url"`
	Username          string `json:"username"`
	HasPassword       bool   `json:"hasPassword"`
	Category          string `json:"category"`
	CategoryMovies    string `json:"categoryMovies"`
	CategoryTV        string `json:"categoryTv"`
	CategoryMusic     string `json:"categoryMusic"`
	CategoryBooks     string `json:"categoryBooks"`
	RemotePath        string `json:"remotePath"`
	LocalPath         string `json:"localPath"`
	RemoveAfterImport bool   `json:"removeAfterImport"`
	RemoveOnStop      bool   `json:"removeOnStop"`
	LinkRoot          string `json:"linkRoot"`
}

// GET /api/download-client: the download client settings, without the password.
func (s *Server) handleGetDownloadClient(w http.ResponseWriter, r *http.Request) {
	c := s.downloadClientConfig()
	writeJSON(w, http.StatusOK, downloadClientPayload{
		Kind: c.Kind, URL: c.URL, Username: c.Username, HasPassword: c.Password != "",
		Category: strings.TrimSpace(c.Category), CategoryMovies: c.KindCategories[torrentopts.KindMovie], CategoryTV: c.KindCategories[torrentopts.KindTV],
		CategoryMusic: c.KindCategories[torrentopts.KindMusic], CategoryBooks: c.KindCategories[torrentopts.KindBook], RemotePath: c.RemotePath, LocalPath: c.LocalPath,
		RemoveAfterImport: c.RemoveAfterImport, RemoveOnStop: c.RemoveOnStop, LinkRoot: c.LinkRoot,
	})
}

type downloadClientRequest struct {
	Kind              string `json:"kind"`
	URL               string `json:"url"`
	Username          string `json:"username"`
	Password          string `json:"password"` // blank keeps the saved one
	Category          string `json:"category"`
	CategoryMovies    string `json:"categoryMovies"`
	CategoryTV        string `json:"categoryTv"`
	CategoryMusic     string `json:"categoryMusic"`
	CategoryBooks     string `json:"categoryBooks"`
	RemotePath        string `json:"remotePath"`
	LocalPath         string `json:"localPath"`
	RemoveAfterImport bool   `json:"removeAfterImport"`
	RemoveOnStop      *bool  `json:"removeOnStop"` // absent keeps the saved choice (on unless switched off)
	LinkRoot          string `json:"linkRoot"`
}

func (req *downloadClientRequest) trim() {
	req.URL = strings.TrimSpace(req.URL)
	req.Username = strings.TrimSpace(req.Username)
	req.Category = strings.TrimSpace(req.Category)
	req.CategoryMovies = strings.TrimSpace(req.CategoryMovies)
	req.CategoryTV = strings.TrimSpace(req.CategoryTV)
	req.CategoryMusic = strings.TrimSpace(req.CategoryMusic)
	req.CategoryBooks = strings.TrimSpace(req.CategoryBooks)
	req.RemotePath = strings.TrimSpace(req.RemotePath)
	req.LocalPath = strings.TrimSpace(req.LocalPath)
	req.LinkRoot = strings.TrimSpace(req.LinkRoot)
}

func (req downloadClientRequest) check() string {
	if req.Kind != "" && !qbit.ValidKind(req.Kind) {
		return "Choose qBittorrent, Transmission, RDT-Client, Decypharr or none."
	}
	if req.Kind == "" {
		return ""
	}
	const example = "http://192.168.1.10:8080"
	msg := firstProblem(
		checkRequired(req.URL, "Add the address of your download client's web page, for example "+example+"."),
		checkHTTPURL(req.URL, example, true),
		checkMaxLen(req.URL, "The address", 500),
		checkMaxLen(req.Username, "The user name", 200),
		checkNoControl(req.Username, "The user name"),
		checkMaxLen(req.Password, "The password", 200),
		checkNoControl(req.Password, "The password"),
		checkMaxLen(req.Category, "The category", 100),
		checkNoControl(req.Category, "The category"),
		checkMaxLen(req.CategoryMovies, "The movies category", 100),
		checkNoControl(req.CategoryMovies, "The movies category"),
		checkMaxLen(req.CategoryTV, "The TV shows category", 100),
		checkNoControl(req.CategoryTV, "The TV shows category"),
		checkMaxLen(req.CategoryMusic, "The music category", 100),
		checkNoControl(req.CategoryMusic, "The music category"),
		checkMaxLen(req.CategoryBooks, "The books category", 100),
		checkNoControl(req.CategoryBooks, "The books category"),
	)
	if msg != "" {
		return msg
	}
	for _, c := range []struct{ msg, v string }{
		{`The category can't contain \ or //, or start or end with /.`, req.Category},
		{`The movies category can't contain \ or //, or start or end with /.`, req.CategoryMovies},
		{`The TV shows category can't contain \ or //, or start or end with /.`, req.CategoryTV},
		{`The music category can't contain \ or //, or start or end with /.`, req.CategoryMusic},
		{`The books category can't contain \ or //, or start or end with /.`, req.CategoryBooks},
	} {
		if strings.Contains(c.v, `\`) || strings.HasPrefix(c.v, "/") || strings.HasSuffix(c.v, "/") || strings.Contains(c.v, "//") {
			return c.msg
		}
	}
	if (req.RemotePath == "") != (req.LocalPath == "") {
		return "Fill in both folders of the path mapping, or leave both empty."
	}
	return firstProblem(
		checkAbsPath(req.RemotePath, "/downloads"),
		checkMaxLen(req.RemotePath, "The client's folder", 1000),
		checkAbsPath(req.LocalPath, "/data/torrents"),
		checkMaxLen(req.LocalPath, "The folder as Mediarium sees it", 1000),
		checkAbsPath(req.LinkRoot, "/mnt/remote"),
		checkMaxLen(req.LinkRoot, "The folder symlinks may point into", 1000),
	)
}

// PUT /api/download-client: save the download client. Saving one turns the
// debrid service off: torrent releases go to one place only.
func (s *Server) handlePutDownloadClient(w http.ResponseWriter, r *http.Request) {
	var req downloadClientRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that.")
		return
	}
	req.trim()
	if msg := req.check(); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	old := s.downloadClientConfig()
	type kv struct {
		key, value string
		encrypt    bool
	}
	remove := "0"
	if req.RemoveAfterImport {
		remove = "1"
	}
	removeOnStop := boolSetting(old.RemoveOnStop)
	if req.RemoveOnStop != nil {
		removeOnStop = boolSetting(*req.RemoveOnStop)
	}
	save := []kv{
		{settings.KeyDownloadClientURL, req.URL, false},
		{settings.KeyDownloadClientUsername, req.Username, false},
		{settings.KeyDownloadClientCategory, req.Category, false},
		{settings.KeyDownloadClientCategoryMovies, req.CategoryMovies, false},
		{settings.KeyDownloadClientCategoryTV, req.CategoryTV, false},
		{settings.KeyDownloadClientCategoryMusic, req.CategoryMusic, false},
		{settings.KeyDownloadClientCategoryBooks, req.CategoryBooks, false},
		{settings.KeyDownloadClientRemotePath, req.RemotePath, false},
		{settings.KeyDownloadClientLocalPath, req.LocalPath, false},
		{settings.KeyDownloadClientRemoveImport, remove, false},
		{settings.KeyDownloadClientRemoveOnStop, removeOnStop, false},
		{settings.KeyDownloadClientLinkRoot, req.LinkRoot, false},
	}
	switch {
	case req.Password != "":
		save = append(save, kv{settings.KeyDownloadClientPassword, req.Password, true})
	case req.Kind == "" || req.URL != old.URL:
		// Turning it off, or another address without a new password: the old
		// password belongs to the old client.
		save = append(save, kv{settings.KeyDownloadClientPassword, "", true})
	}
	save = append(save, kv{settings.KeyDownloadClientKind, req.Kind, false})
	if req.Kind != "" {
		if service, _ := s.debridConfig(); service != "" {
			save = append(save, kv{settings.KeyDebridService, "", false}, kv{settings.KeyDebridAPIKey, "", true})
			slog.Info("debrid service turned off: torrent releases now go to the download client")
		}
	}
	for _, e := range save {
		if err := s.Settings.Set(e.key, e.value, e.encrypt); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	slog.Info("download client saved", "kind", req.Kind)
	s.handleGetDownloadClient(w, r)
}

// POST /api/download-client/test {"kind", "url", "username", "password"}: a
// blank password tests the saved one when the address is the saved one.
func (s *Server) handleTestDownloadClient(w http.ResponseWriter, r *http.Request) {
	var req downloadClientRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that.")
		return
	}
	req.trim()
	if msg := req.check(); msg != "" || req.Kind == "" {
		if msg == "" {
			msg = "Choose qBittorrent, Transmission, RDT-Client or Decypharr."
		}
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	password := req.Password
	if password == "" {
		if saved := s.downloadClientConfig(); saved.URL == req.URL {
			password = saved.Password
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	version, err := newDownloadClient(req.Kind, req.URL, req.Username, password).Version(ctx)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": plainerror.Message(s.downloadClientError(qbit.Label(req.Kind), err))})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": version})
}

// findOptions makes the video finders follow the links of a client view.
func findOptions(dir string) []organizer.FindOption {
	if isClientView(dir) {
		return []organizer.FindOption{organizer.FollowLinks}
	}
	return nil
}

// bookFindOptions is findOptions for the book finders.
func bookFindOptions(dir string) []books.FindOption {
	if isClientView(dir) {
		return []books.FindOption{books.FollowLinks}
	}
	return nil
}
