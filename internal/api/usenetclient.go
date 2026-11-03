package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/download"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/netguard"
	"github.com/rdborg/mediarium/internal/plainerror"
	"github.com/rdborg/mediarium/internal/queue"
	"github.com/rdborg/mediarium/internal/settings"
	"github.com/rdborg/mediarium/internal/torrentopts"
	"github.com/rdborg/mediarium/internal/usenetclient"
)

// External Usenet downloader: Usenet releases go to SABnzbd or NZBGet instead
// of the built-in downloader, when the person chose that. It works like the
// torrent download client (downloadclient.go): the downloader fetches, repairs
// and unpacks into its own folder, Mediarium follows its progress, and once
// the job is finished the download's own folder (queue-N) gets a link to each
// file (a "view"), which imports read through. Mediarium never moves or
// deletes the downloader's files; only "Remove from the downloader after
// import" does, and only by asking the downloader to remove the job.

// usenetJobFile remembers the job's id in the downloader, so a restart or a
// retry picks the job up instead of sending the NZB again. It lives in the
// download's own folder, next to the view of the files.
const usenetJobFile = ".mediarium-usenet-job"

// usenetFailedJobFile remembers a job that failed for the downloader's own
// reasons (a full disk) and was left there to look at. When the download is
// tried again, that job is removed before the NZB is sent once more, so the
// downloader doesn't end up with the same release twice.
const usenetFailedJobFile = ".mediarium-usenet-failed-job"

type usenetClientConfig struct {
	Kind              string
	Use               bool
	URL               string
	APIKey            string
	Username          string
	Password          string
	Category          string
	KindCategories    map[string]string
	Priority          string
	SendURL           bool
	RemotePath        string
	LocalPath         string
	RemoveAfterImport bool
	RemoveOnStop      bool
}

func (s *Server) usenetClientConfig() usenetClientConfig {
	get := func(key string) string {
		v, _ := s.Settings.Get(key)
		return v
	}
	flag := func(key string) bool { return get(key) == "1" }
	return usenetClientConfig{
		Kind:     get(settings.KeyUsenetClientKind),
		Use:      flag(settings.KeyUsenetClientUse),
		URL:      get(settings.KeyUsenetClientURL),
		APIKey:   get(settings.KeyUsenetClientAPIKey),
		Username: get(settings.KeyUsenetClientUsername),
		Password: get(settings.KeyUsenetClientPassword),
		Category: get(settings.KeyUsenetClientCategory),
		KindCategories: map[string]string{
			torrentopts.KindMovie: get(settings.KeyUsenetClientCategoryMovies),
			torrentopts.KindTV:    get(settings.KeyUsenetClientCategoryTV),
			torrentopts.KindMusic: get(settings.KeyUsenetClientCategoryMusic),
			torrentopts.KindBook:  get(settings.KeyUsenetClientCategoryBooks),
		},
		Priority:          get(settings.KeyUsenetClientPriority),
		SendURL:           flag(settings.KeyUsenetClientSendURL),
		RemotePath:        get(settings.KeyUsenetClientRemotePath),
		LocalPath:         get(settings.KeyUsenetClientLocalPath),
		RemoveAfterImport: flag(settings.KeyUsenetClientRemoveImport),
		RemoveOnStop:      get(settings.KeyUsenetClientRemoveOnStop) != "0",
	}
}

// category is the main category, with the same rules as the torrent client's:
// empty stays empty when a kind has one of its own, and only when nothing is
// set anywhere does the default apply.
func (c usenetClientConfig) category() string {
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

func (c usenetClientConfig) categoryFor(kind string) string {
	if v := strings.TrimSpace(c.KindCategories[kind]); v != "" {
		return v
	}
	return c.category()
}

// allCategories lists every category Mediarium may have used.
func (c usenetClientConfig) allCategories() []string {
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

func (c usenetClientConfig) priority() usenetclient.Priority {
	return usenetclient.Priority(c.Priority)
}

// usenetClientSet reports whether a SABnzbd or NZBGet is set up, whether or
// not Usenet releases are sent to it right now. What is already in it can
// still be followed, stopped and removed.
func (s *Server) usenetClientSet() bool {
	c := s.usenetClientConfig()
	return usenetclient.ValidKind(c.Kind) && c.URL != ""
}

// usenetClientOn reports whether Usenet releases go to the external downloader.
func (s *Server) usenetClientOn() bool {
	return s.usenetClientSet() && s.usenetClientConfig().Use
}

// newUsenetClient connects to the downloader. It is usually on the home
// network, so local addresses are fine (only link-local and cloud metadata
// addresses are refused).
func newUsenetClient(c usenetClientConfig) usenetclient.API {
	return usenetclient.NewAPI(c.Kind, c.URL, c.APIKey, c.Username, c.Password, netguard.Client(60*time.Second))
}

func readUsenetJob(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, usenetJobFile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func writeUsenetJob(dir, id string) {
	if id == "" {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, usenetJobFile), []byte(id+"\n"), 0o600)
}

// usenetClientError puts an error from the downloader into words that say what
// to do about it.
func usenetClientError(kind string, err error) error {
	label := usenetclient.Label(kind)
	var cat *usenetclient.CategoryError
	var refused *usenetclient.RefusedError
	var timeout interface{ Timeout() bool }
	switch {
	case err == nil:
		return nil
	case errors.Is(err, usenetclient.ErrAuth) && kind == usenetclient.NZBGet:
		return fmt.Errorf("%s refused the user name or password. They are the ControlUsername and ControlPassword in its settings. Check them under Settings > Downloading > Usenet", label)
	case errors.Is(err, usenetclient.ErrAuth):
		return fmt.Errorf("%s refused the API key. Copy it again from Config > General in SABnzbd, and paste it under Settings > Downloading > Usenet", label)
	case errors.As(err, &cat) && kind == usenetclient.NZBGet:
		return fmt.Errorf("%s has no category called %q. Add it in NZBGet under Settings > Categories, or change the category under Settings > Downloading > Usenet", label, cat.Name)
	case errors.As(err, &cat):
		return fmt.Errorf("%s has no category called %q. Add it in SABnzbd under Config > Categories, or change the category under Settings > Downloading > Usenet", label, cat.Name)
	case errors.Is(err, usenetclient.ErrNotThisClient):
		return fmt.Errorf("That address answered, but not like %s. Check the address and the port under Settings > Downloading > Usenet", label)
	case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &timeout) && timeout.Timeout()):
		return fmt.Errorf("%s didn't answer in time. Check that it is running and that the address under Settings > Downloading > Usenet is right", label)
	case errors.Is(err, netguard.ErrBlocked):
		return errors.New("Mediarium may not connect to that address.")
	case errors.As(err, &refused):
		return fmt.Errorf("%s said no: %s", label, refused.Message)
	case errors.Is(err, context.Canceled):
		return err
	}
	return fmt.Errorf("Mediarium couldn't reach %s: %w", label, netguard.CleanError(err))
}

// fetchCheckedNZB gets a release's NZB file and checks it before anything is
// downloaded: that it is an NZB at all, that it holds no programs or scripts,
// and that it is not the same NZB as an earlier download of this title.
func (s *Server) fetchCheckedNZB(ctx context.Context, queueID int64, nzbURL string) (*download.NZB, []byte, error) {
	if isMagnetURI(nzbURL) {
		return nil, nil, errors.New("this is a torrent (a magnet link), but its indexer is set up as a Usenet indexer. Open Settings > Indexers & Search, remove it and add it again on the Torrent tab")
	}
	nzbBytes, err := s.fetchRelease(ctx, nzbURL)
	if err != nil {
		return nil, nil, fmt.Errorf("couldn't get the NZB file: %w", err)
	}
	if looksLikeTorrentFile(nzbBytes) {
		return nil, nil, errors.New("this is a .torrent file, but its indexer is set up as a Usenet indexer. Open Settings > Indexers & Search, remove it and add it again on the Torrent tab")
	}
	// An indexer at its daily limit, or showing a login or maintenance page,
	// sends something that is not an NZB: that says nothing about the release,
	// so it is not blocklisted.
	if err := notAnNZB(nzbBytes, nzbURL); err != nil {
		return nil, nil, err
	}
	nzb, err := download.ParseNZB(nzbBytes)
	if err != nil {
		return nil, nil, badRelease(fmt.Errorf("couldn't read the NZB file: %w", err))
	}
	// Before a single article is fetched: no programs or scripts in the file
	// list, and not the same NZB as an earlier download of this title.
	if err := s.checkNZBSafe(nzb); err != nil {
		return nil, nil, err
	}
	fingerprint := nzb.Fingerprint()
	if err := s.QueueRepo.SetNZBHash(queueID, fingerprint); err != nil {
		slog.Warn("api: save the NZB fingerprint", "queueId", queueID, "err", err)
	}
	if err := s.checkDuplicate(queueID, fingerprint); err != nil {
		return nil, nil, err
	}
	return nzb, nzbBytes, nil
}

// plainDownloadURL says whether the downloader could fetch the release itself
// from this address: an ordinary web link, not one only Mediarium can open (an
// indexer's signed-in session, an uploaded file).
func plainDownloadURL(u string) bool {
	if isUploadRef(u) || indexers.IsLinkRef(u) {
		return false
	}
	return strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")
}

// addToUsenetClient hands the release to the downloader and returns the job's id.
func (s *Server) addToUsenetClient(ctx context.Context, c usenetclient.API, cfg usenetClientConfig, queueID int64, nzbURL string) (string, error) {
	label := usenetclient.Label(cfg.Kind)
	opts := usenetclient.AddOptions{Priority: cfg.priority(), Category: cfg.category()}
	if item, err := s.QueueRepo.Get(queueID); err == nil {
		opts.Category = cfg.categoryFor(queueKind(item))
		opts.Name = item.ReleaseTitle
	}
	if cfg.SendURL && plainDownloadURL(nzbURL) {
		// The downloader fetches it itself, so Mediarium can't look inside first.
		opts.URL = nzbURL
	} else {
		_, data, err := s.fetchCheckedNZB(ctx, queueID, nzbURL)
		if err != nil {
			return "", err
		}
		opts.NZB = data
	}
	id, err := c.Add(ctx, opts)
	if err != nil {
		return "", usenetClientError(cfg.Kind, err)
	}
	s.queueEvent(queueID, "download", queue.LevelInfo, "Sent to "+label)
	return id, nil
}

// downloadViaUsenetClient is downloadUsenet's counterpart when an external
// downloader is used: its progress is the download's progress. When the job is
// done, incompleteDir becomes a view of its files (see buildClientView).
func (s *Server) downloadViaUsenetClient(ctx context.Context, queueID int64, nzbURL, incompleteDir string) error {
	cfg := s.usenetClientConfig()
	c := newUsenetClient(cfg)
	label := usenetclient.Label(cfg.Kind)
	if err := os.MkdirAll(incompleteDir, 0o755); err != nil {
		return fmt.Errorf("create download folder: %w", err)
	}
	id := readUsenetJob(incompleteDir)
	resumed := id != ""
	if id == "" {
		var err error
		if id, err = s.addToUsenetClient(ctx, c, cfg, queueID, nzbURL); err != nil {
			return err
		}
		writeUsenetJob(incompleteDir, id)
	}

	progress := newProgressSaver(func(pct float64) error { return s.QueueRepo.SetProgress(queueID, pct) })
	defer progress.Flush()
	var gone time.Time
	var fails pollFailures
	var last usenetclient.State
	for {
		j, found, err := c.Get(ctx, id)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			s.noteClientCheck(watchUsenetClient, label, err)
			if !fails.tolerate(err, usenetclient.ErrAuth) {
				return usenetClientError(cfg.Kind, err)
			}
			slog.Warn("usenet client: status check failed, trying again", "queueId", queueID, "err", err)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(downloadClientPollEvery()):
			}
			continue
		}
		fails.reset()
		s.noteClientCheck(watchUsenetClient, label, nil)
		switch {
		case !found:
			if gone.IsZero() {
				gone = time.Now()
			} else if time.Since(gone) > downloadClientGone {
				_ = os.Remove(filepath.Join(incompleteDir, usenetJobFile))
				return fmt.Errorf("%s doesn't have this download any more. It may have been removed there. Try the download again to send it once more", label)
			}
		case j.State == usenetclient.StateFailed && j.Removed:
			_ = os.Remove(filepath.Join(incompleteDir, usenetJobFile))
			return fmt.Errorf("%s says this download was deleted there. Try the download again to send it once more", label)
		case j.State == usenetclient.StateFailed:
			gone = time.Time{}
			_ = os.Remove(filepath.Join(incompleteDir, usenetJobFile))
			if !j.ReleaseFault {
				// The downloader's own trouble (disk full, a folder it can't write to),
				// not something wrong with the release: it is not blocklisted and the
				// job stays in the downloader for the person to look at.
				_ = os.WriteFile(filepath.Join(incompleteDir, usenetFailedJobFile), []byte(id+"\n"), 0o600)
				return fmt.Errorf("%s reports a problem with this download (it says: %s). Check its free space and folder permissions, then try the download again", label, failText(j))
			}
			s.usenetForget(c, cfg, id)
			return badRelease(fmt.Errorf("%s couldn't finish it: %s", label, failText(j)))
		case j.State == usenetclient.StateCompleted:
			progress.Report(100, 100)
			return s.usenetDone(queueID, j, cfg, label, incompleteDir)
		default:
			gone = time.Time{}
			if j.State == usenetclient.StatePaused && last == "" && resumed {
				// Left paused by an earlier pause of this download: it is running again.
				if err := c.Resume(ctx, id); err != nil {
					slog.Warn("usenet client: couldn't start a paused job again", "queueId", queueID, "err", err)
				}
			}
			progress.Report(int64(j.Progress*100), 10000)
			if j.State != last && j.State == usenetclient.StateProcessing {
				s.queueEvent(queueID, "download", queue.LevelInfo, label+" has downloaded it and is now checking and unpacking it")
			}
		}
		if found {
			last = j.State
		}
		select {
		case <-ctx.Done():
			return ctx.Err() // paused or stopped: the job stays in the downloader for later
		case <-time.After(downloadClientPollEvery()):
		}
	}
}

// failText is the reason a job failed, in the downloader's words.
func failText(j usenetclient.Job) string {
	if j.FailMessage != "" {
		return j.FailMessage
	}
	return "no reason given"
}

// forgetFailedUsenetJob removes from the downloader the job an earlier attempt
// of a download left there after the downloader's own trouble, before Retry
// sends the release again.
func (s *Server) forgetFailedUsenetJob(queueID int64) {
	dir, ok := s.existingWorkDir(queueID)
	if !ok {
		return
	}
	b, err := os.ReadFile(filepath.Join(dir, usenetFailedJobFile))
	if err != nil {
		return
	}
	_ = os.Remove(filepath.Join(dir, usenetFailedJobFile))
	id := strings.TrimSpace(string(b))
	cfg := s.usenetClientConfig()
	if id == "" || !cfg.Use {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := newUsenetClient(cfg).Delete(ctx, id, false); err != nil {
		slog.Warn("usenet client: couldn't remove the earlier failed job before sending it again", "job", id, "err", err)
	}
}

// usenetForget removes a job that failed from the downloader, with what it had
// downloaded.
func (s *Server) usenetForget(c usenetclient.API, cfg usenetClientConfig, id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := c.Delete(ctx, id, true); err != nil {
		slog.Warn("usenet client: couldn't remove a failed job", "job", id, "err", err)
	}
}

// usenetDone lays out the finished job's files in the download's folder.
func (s *Server) usenetDone(queueID int64, j usenetclient.Job, cfg usenetClientConfig, label, incompleteDir string) error {
	content := mapClientPath(j.Path, cfg.RemotePath, cfg.LocalPath)
	if content == "" || !filepath.IsAbs(content) {
		return fmt.Errorf("%s didn't say where it saved the files (%q). If it runs on another computer or in another container, set the path mapping under Settings > Downloading > Usenet", label, j.Path)
	}
	if _, err := os.Lstat(content); err != nil {
		if cfg.RemotePath != "" {
			return fmt.Errorf("Mediarium can't see the finished download at %s. If %s runs on another computer or in another container, check the path mapping under Settings > Downloading > Usenet", content, label)
		}
		return fmt.Errorf("Mediarium can't see the finished download at %s. If %s runs on another computer or in another container, set the path mapping under Settings > Downloading > Usenet, or give Mediarium access to its download folder", content, label)
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

// afterUsenetImport runs when a download from the downloader has been imported
// and its folder is about to go: with "Remove from the downloader after
// import" on, the job is removed from the downloader, with its files.
func (s *Server) afterUsenetImport(id string) {
	cfg := s.usenetClientConfig()
	if !cfg.RemoveAfterImport || !s.usenetClientSet() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client := newUsenetClient(cfg)
	// Ask where the files are before the job goes: the client forgets that
	// with the job.
	folder := ""
	if j, found, err := client.Get(ctx, id); err == nil && found {
		folder = mapClientPath(j.Path, cfg.RemotePath, cfg.LocalPath)
	}
	if err := client.Delete(ctx, id, true); err != nil {
		slog.Warn("usenet client: couldn't remove an imported job", "job", id, "err", err)
		return
	}
	slog.Info("usenet client: removed an imported job", "job", id)
	// SABnzbd and NZBGet only delete the files of a job still in their own
	// working folder, not of a finished one in the completed folder, so what
	// the release left there is removed here.
	removeFinishedFolder(folder, cfg.LocalPath, append(s.libraryRoots(), s.musicRoot()))
}

// removeFinishedFolder deletes a finished job's folder from the downloader's
// completed folder once its files are in the library (hard links stay: only
// the folder's own names go). It is careful about what it removes: only a
// real folder (not a link) that is inside the mapped completed folder, at
// least three levels deep, and that is not, and does not hold, a library
// folder. With no path mapping set, nothing is deleted.
func removeFinishedFolder(folder, localRoot string, libraryRoots []string) {
	if folder == "" || !filepath.IsAbs(folder) {
		return
	}
	if localRoot == "" {
		// Without the path mapping there is no telling which folder is the
		// downloader's completed folder, so nothing is deleted.
		slog.Info("usenet client: left an imported job's folder; set the path mapping under Settings > Downloading > Usenet to have it deleted", "folder", folder)
		return
	}
	folder = filepath.Clean(folder)
	// SABnzbd names the single file of a job, not its folder, when it saves
	// straight into the completed folder. A folder of its own around that file
	// goes with it; a file lying in the completed folder itself goes alone.
	if info, err := os.Lstat(folder); err == nil && info.Mode().IsRegular() {
		root := filepath.Clean(localRoot)
		if parent := filepath.Dir(folder); parent != root {
			removeFinishedFolder(parent, localRoot, libraryRoots)
			return
		}
		removeFinishedFile(folder, root, libraryRoots)
		return
	}
	if len(strings.FieldsFunc(folder, func(r rune) bool { return r == '/' || r == 0x5c })) < 3 {
		return
	}
	rel, err := filepath.Rel(filepath.Clean(localRoot), folder)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return
	}
	for _, root := range libraryRoots {
		root = filepath.Clean(root)
		if root == folder || strings.HasPrefix(root, folder+string(filepath.Separator)) || strings.HasPrefix(folder, root+string(filepath.Separator)) {
			return
		}
	}
	info, err := os.Lstat(folder)
	if err != nil || !info.IsDir() {
		return
	}
	if err := os.RemoveAll(folder); err != nil {
		slog.Warn("usenet client: couldn't delete an imported job's folder", "folder", folder, "err", err)
		return
	}
	slog.Info("usenet client: deleted an imported job's folder", "folder", folder)
}

// removeFinishedFile deletes one finished file that lies directly in the
// downloader's completed folder (root), unless it is in a library folder. A
// hard link in the library stays: only this name goes.
func removeFinishedFile(file, root string, libraryRoots []string) {
	if filepath.Dir(file) != root || len(strings.FieldsFunc(root, func(r rune) bool { return r == '/' || r == 0x5c })) < 1 {
		return
	}
	for _, lib := range libraryRoots {
		lib = filepath.Clean(lib)
		if lib == root || strings.HasPrefix(root, lib+string(filepath.Separator)) || strings.HasPrefix(file, lib+string(filepath.Separator)) {
			return
		}
	}
	if err := os.Remove(file); err != nil {
		slog.Warn("usenet client: couldn't delete an imported job's file", "file", file, "err", err)
		return
	}
	slog.Info("usenet client: deleted an imported job's file", "file", file)
}

// usenetRefFor says whether stopping or removing this item should also remove
// its job from the Usenet downloader, and which job. Call it before the item's
// folder is deleted.
func (s *Server) usenetRefFor(item queue.Item) (clientRef, bool) {
	if item.Protocol == queue.ProtocolTorrent || !s.usenetClientSet() || !s.usenetClientConfig().RemoveOnStop {
		return clientRef{}, false
	}
	switch item.Status {
	case queue.StatusQueued, queue.StatusDownloading, queue.StatusPaused, queue.StatusStopped, queue.StatusFailed:
	default:
		return clientRef{}, false // imported or being imported: the job is not Mediarium's to remove
	}
	dir, ok := s.existingWorkDir(item.ID)
	if !ok {
		return clientRef{}, false
	}
	id := readUsenetJob(dir)
	if id == "" {
		return clientRef{}, false
	}
	return clientRef{queueID: item.ID, usenetJob: id}, true
}

// removeUsenetJob removes the job behind ref from the downloader. force also
// removes one that has finished downloading.
func (s *Server) removeUsenetJob(ref clientRef, force bool) {
	cfg := s.usenetClientConfig()
	if !s.usenetClientSet() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := newUsenetClient(cfg)
	j, found, err := c.Get(ctx, ref.usenetJob)
	if err != nil {
		slog.Warn("usenet client: couldn't look up a job to remove", "queueId", ref.queueID, "err", err)
		return
	}
	if !found {
		return
	}
	if !force && (j.State == usenetclient.StateCompleted || j.State == usenetclient.StateProcessing) {
		slog.Debug("usenet client: leaving a finished job in the downloader", "queueId", ref.queueID)
		return
	}
	if err := c.Delete(ctx, ref.usenetJob, true); err != nil {
		slog.Warn("usenet client: couldn't remove a stopped download's job", "queueId", ref.queueID, "err", err)
		return
	}
	slog.Info("usenet client: removed the job of a stopped download", "queueId", ref.queueID)
}

// pauseUsenetJob pauses the job of a download that was paused, so the
// downloader doesn't go on without Mediarium. It starts again by itself when
// the download is resumed (downloadViaUsenetClient).
func (s *Server) pauseUsenetJob(item queue.Item) {
	if item.Protocol == queue.ProtocolTorrent || !s.usenetClientSet() {
		return
	}
	dir, ok := s.existingWorkDir(item.ID)
	if !ok {
		return
	}
	id := readUsenetJob(dir)
	if id == "" {
		return
	}
	cfg := s.usenetClientConfig()
	s.background(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := newUsenetClient(cfg).Pause(ctx, id); err != nil {
			slog.Warn("usenet client: couldn't pause a job", "queueId", item.ID, "err", err)
		}
	})
}

// ---- Settings ----

type usenetClientPayload struct {
	Kind              string `json:"kind"` // "" (off), sabnzbd or nzbget
	Use               bool   `json:"use"`
	URL               string `json:"url"`
	HasAPIKey         bool   `json:"hasApiKey"`
	Username          string `json:"username"`
	HasPassword       bool   `json:"hasPassword"`
	Category          string `json:"category"`
	CategoryMovies    string `json:"categoryMovies"`
	CategoryTV        string `json:"categoryTv"`
	CategoryMusic     string `json:"categoryMusic"`
	CategoryBooks     string `json:"categoryBooks"`
	Priority          string `json:"priority"`
	SendURL           bool   `json:"sendUrl"`
	RemotePath        string `json:"remotePath"`
	LocalPath         string `json:"localPath"`
	RemoveAfterImport bool   `json:"removeAfterImport"`
	RemoveOnStop      bool   `json:"removeOnStop"`
}

func (s *Server) usenetClientPayload() usenetClientPayload {
	c := s.usenetClientConfig()
	prio := c.Priority
	if prio == "" {
		prio = string(usenetclient.PriorityNormal)
	}
	return usenetClientPayload{
		Kind: c.Kind, Use: c.Use && c.Kind != "", URL: c.URL, HasAPIKey: c.APIKey != "", Username: c.Username, HasPassword: c.Password != "",
		Category: strings.TrimSpace(c.Category), CategoryMovies: c.KindCategories[torrentopts.KindMovie], CategoryTV: c.KindCategories[torrentopts.KindTV],
		CategoryMusic: c.KindCategories[torrentopts.KindMusic], CategoryBooks: c.KindCategories[torrentopts.KindBook],
		Priority: prio, SendURL: c.SendURL, RemotePath: c.RemotePath, LocalPath: c.LocalPath,
		RemoveAfterImport: c.RemoveAfterImport, RemoveOnStop: c.RemoveOnStop,
	}
}

// GET /api/usenet-client: the settings, without the API key and password.
func (s *Server) handleGetUsenetClient(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.usenetClientPayload())
}

type usenetClientRequest struct {
	Kind              string `json:"kind"`
	Use               bool   `json:"use"`
	URL               string `json:"url"`
	APIKey            string `json:"apiKey"` // blank keeps the saved one
	Username          string `json:"username"`
	Password          string `json:"password"` // blank keeps the saved one
	Category          string `json:"category"`
	CategoryMovies    string `json:"categoryMovies"`
	CategoryTV        string `json:"categoryTv"`
	CategoryMusic     string `json:"categoryMusic"`
	CategoryBooks     string `json:"categoryBooks"`
	Priority          string `json:"priority"`
	SendURL           bool   `json:"sendUrl"`
	RemotePath        string `json:"remotePath"`
	LocalPath         string `json:"localPath"`
	RemoveAfterImport bool   `json:"removeAfterImport"`
	RemoveOnStop      *bool  `json:"removeOnStop"` // absent keeps the saved choice (on unless switched off)
}

func (req *usenetClientRequest) trim() {
	req.URL = strings.TrimSpace(req.URL)
	req.APIKey = strings.TrimSpace(req.APIKey)
	req.Username = strings.TrimSpace(req.Username)
	req.Category = strings.TrimSpace(req.Category)
	req.CategoryMovies = strings.TrimSpace(req.CategoryMovies)
	req.CategoryTV = strings.TrimSpace(req.CategoryTV)
	req.CategoryMusic = strings.TrimSpace(req.CategoryMusic)
	req.CategoryBooks = strings.TrimSpace(req.CategoryBooks)
	req.Priority = strings.TrimSpace(req.Priority)
	req.RemotePath = strings.TrimSpace(req.RemotePath)
	req.LocalPath = strings.TrimSpace(req.LocalPath)
}

func (req usenetClientRequest) check() string {
	if req.Kind != "" && !usenetclient.ValidKind(req.Kind) {
		return "Choose SABnzbd, NZBGet or none."
	}
	if req.Kind == "" {
		return ""
	}
	const example = "http://192.168.1.10:8080"
	msg := firstProblem(
		checkRequired(req.URL, "Add the address of your Usenet downloader's web page, for example "+example+"."),
		checkHTTPURL(req.URL, example, true),
		checkMaxLen(req.URL, "The address", 500),
		checkMaxLen(req.APIKey, "The API key", 200),
		checkNoControl(req.APIKey, "The API key"),
		checkMaxLen(req.Username, "The user name", 200),
		checkNoControl(req.Username, "The user name"),
		checkMaxLen(req.Password, "The password", 200),
		checkNoControl(req.Password, "The password"),
	)
	if msg != "" {
		return msg
	}
	if !usenetclient.ValidPriority(req.Priority) {
		return "Choose a priority: low, normal, high or force."
	}
	for _, c := range []struct{ label, v string }{
		{"The category", req.Category}, {"The movies category", req.CategoryMovies}, {"The TV shows category", req.CategoryTV},
		{"The music category", req.CategoryMusic}, {"The books category", req.CategoryBooks},
	} {
		if m := firstProblem(checkMaxLen(c.v, c.label, 100), checkNoControl(c.v, c.label)); m != "" {
			return m
		}
		if strings.Contains(c.v, `\`) || strings.HasPrefix(c.v, "/") || strings.HasSuffix(c.v, "/") || strings.Contains(c.v, "//") {
			return fmt.Sprintf("%s can't contain \\ or //, or start or end with /.", c.label)
		}
	}
	if (req.RemotePath == "") != (req.LocalPath == "") {
		return "Fill in both folders of the path mapping, or leave both empty."
	}
	return firstProblem(
		checkAbsPath(req.RemotePath, "/downloads/complete"),
		checkMaxLen(req.RemotePath, "The downloader's folder", 1000),
		checkAbsPath(req.LocalPath, "/data/usenet/complete"),
		checkMaxLen(req.LocalPath, "The folder as Mediarium sees it", 1000),
	)
}

// PUT /api/usenet-client: save the downloader.
func (s *Server) handlePutUsenetClient(w http.ResponseWriter, r *http.Request) {
	var req usenetClientRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that.")
		return
	}
	req.trim()
	if msg := req.check(); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	old := s.usenetClientConfig()
	sameClient := req.Kind == old.Kind && req.URL == old.URL
	if req.Kind == usenetclient.SABnzbd && req.APIKey == "" && (!sameClient || old.APIKey == "") {
		writeError(w, http.StatusBadRequest, "Add your SABnzbd API key. You find it in SABnzbd under Config > General.")
		return
	}
	type kv struct {
		key, value string
		encrypt    bool
	}
	removeOnStop := boolSetting(old.RemoveOnStop)
	if req.RemoveOnStop != nil {
		removeOnStop = boolSetting(*req.RemoveOnStop)
	}
	use := req.Use && req.Kind != ""
	save := []kv{
		{settings.KeyUsenetClientURL, req.URL, false},
		{settings.KeyUsenetClientUsername, req.Username, false},
		{settings.KeyUsenetClientCategory, req.Category, false},
		{settings.KeyUsenetClientCategoryMovies, req.CategoryMovies, false},
		{settings.KeyUsenetClientCategoryTV, req.CategoryTV, false},
		{settings.KeyUsenetClientCategoryMusic, req.CategoryMusic, false},
		{settings.KeyUsenetClientCategoryBooks, req.CategoryBooks, false},
		{settings.KeyUsenetClientPriority, req.Priority, false},
		{settings.KeyUsenetClientSendURL, boolSetting(req.SendURL), false},
		{settings.KeyUsenetClientRemotePath, req.RemotePath, false},
		{settings.KeyUsenetClientLocalPath, req.LocalPath, false},
		{settings.KeyUsenetClientRemoveImport, boolSetting(req.RemoveAfterImport), false},
		{settings.KeyUsenetClientRemoveOnStop, removeOnStop, false},
		{settings.KeyUsenetClientUse, boolSetting(use), false},
	}
	// A key or password left blank keeps the saved one, unless it belongs to
	// another client (turned off, or another address or program).
	switch {
	case req.APIKey != "":
		save = append(save, kv{settings.KeyUsenetClientAPIKey, req.APIKey, true})
	case req.Kind == "" || !sameClient:
		save = append(save, kv{settings.KeyUsenetClientAPIKey, "", true})
	}
	switch {
	case req.Password != "":
		save = append(save, kv{settings.KeyUsenetClientPassword, req.Password, true})
	case req.Kind == "" || !sameClient:
		save = append(save, kv{settings.KeyUsenetClientPassword, "", true})
	}
	save = append(save, kv{settings.KeyUsenetClientKind, req.Kind, false})
	for _, e := range save {
		if err := s.Settings.Set(e.key, e.value, e.encrypt); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	slog.Info("usenet client saved", "kind", req.Kind, "use", use)
	s.dispatch.Kick()
	writeJSON(w, http.StatusOK, s.usenetClientPayload())
}

// POST /api/usenet-client/test: reach the downloader, sign in and make sure the
// categories exist. A blank key or password tests the saved one when the
// address is the saved one.
func (s *Server) handleTestUsenetClient(w http.ResponseWriter, r *http.Request) {
	var req usenetClientRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that.")
		return
	}
	req.trim()
	if msg := req.check(); msg != "" || req.Kind == "" {
		if msg == "" {
			msg = "Choose SABnzbd or NZBGet."
		}
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	cfg := usenetClientConfig{Kind: req.Kind, URL: req.URL, APIKey: req.APIKey, Username: req.Username, Password: req.Password,
		Category: req.Category, KindCategories: map[string]string{
			torrentopts.KindMovie: req.CategoryMovies, torrentopts.KindTV: req.CategoryTV, torrentopts.KindMusic: req.CategoryMusic, torrentopts.KindBook: req.CategoryBooks,
		}}
	if saved := s.usenetClientConfig(); saved.Kind == req.Kind && saved.URL == req.URL {
		if cfg.APIKey == "" {
			cfg.APIKey = saved.APIKey
		}
		if cfg.Password == "" {
			cfg.Password = saved.Password
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	version, err := newUsenetClient(cfg).Check(ctx, cfg.allCategories())
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": plainerror.Message(usenetClientError(req.Kind, err))})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": version})
}

// clientLeftoverAge is how long a finished or failed job stays in SABnzbd or
// NZBGet before the clean-up removes it (counted from when it was added: the
// clients don't say when a job finished).
const clientLeftoverAge = 48 * time.Hour

// cleanUsenetLeftovers removes from SABnzbd or NZBGet the jobs Mediarium sent
// (in its own categories only) that no download of Mediarium's needs any more,
// with their files: failed ones, and finished ones when "Remove after import"
// is on. A job a download still points at (one that waits, runs or failed and
// can be retried) stays, and so does anything in another category. It
// answers how many went.
func (s *Server) cleanUsenetLeftovers(now time.Time) (int, error) {
	if !s.usenetClientSet() {
		return 0, nil
	}
	cfg := s.usenetClientConfig()
	ours := map[string]bool{}
	for _, c := range append([]string{cfg.category()}, slices.Collect(maps.Values(cfg.KindCategories))...) {
		if c = strings.TrimSpace(c); c != "" {
			ours[strings.ToLower(c)] = true
		}
	}
	if len(ours) == 0 {
		return 0, nil
	}
	items, err := s.QueueRepo.List()
	if err != nil {
		return 0, err
	}
	needed := map[string]bool{}
	for _, it := range items {
		if it.Status == queue.StatusCompleted {
			continue
		}
		if dir, ok := s.existingWorkDir(it.ID); ok {
			for _, name := range []string{usenetJobFile, usenetFailedJobFile} {
				if b, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
					needed[strings.TrimSpace(string(b))] = true
				}
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	client := newUsenetClient(cfg)
	jobs, err := client.List(ctx)
	if err != nil {
		return 0, fmt.Errorf("ask the downloader for its jobs: %w", err)
	}
	removed := 0
	for _, j := range jobs {
		switch {
		case !ours[strings.ToLower(strings.TrimSpace(j.Category))] || needed[j.ID]:
			continue
		case j.AddedAt.IsZero() || now.Sub(j.AddedAt) < clientLeftoverAge:
			continue
		case j.State == usenetclient.StateFailed:
		case j.State == usenetclient.StateCompleted && cfg.RemoveAfterImport:
		default:
			continue
		}
		folder := mapClientPath(j.Path, cfg.RemotePath, cfg.LocalPath)
		if err := client.Delete(ctx, j.ID, true); err != nil {
			slog.Warn("cleanup: couldn't remove a job from the downloader", "job", j.ID, "err", err)
			continue
		}
		if j.State == usenetclient.StateCompleted {
			removeFinishedFolder(folder, cfg.LocalPath, append(s.libraryRoots(), s.musicRoot()))
		}
		removed++
	}
	if removed > 0 {
		slog.Info("cleanup: removed old jobs from the downloader", "jobs", removed)
	}
	return removed, nil
}
