package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/anacrolix/torrent"

	"github.com/rdborg/mediarium/internal/download"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/netguard"
	"github.com/rdborg/mediarium/internal/notify"
	"github.com/rdborg/mediarium/internal/organizer"
	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/plainerror"
	"github.com/rdborg/mediarium/internal/quality"
	"github.com/rdborg/mediarium/internal/queue"
	"github.com/rdborg/mediarium/internal/settings"
	"github.com/rdborg/mediarium/internal/torrentclient"
)

// runPipeline is the whole grab -> download -> organize flow (the
// Phase 1 exit criteria, extended in Phase 2 to also cover torrents) for
// one queued release. It runs in its own goroutine, started from
// handleGrab/handleSearchGrab, and reports every state transition through
// QueueRepo/MovieRepo/activity so the UI (polling /api/queue) sees
// progress without needing a websocket.
func (s *Server) runPipeline(queueID, movieID int64, movieTitle string, movieYear, tmdbID int, releaseTitle, downloadURL string, protocol indexers.Protocol) error {
	ctx, run := s.pipelines.begin(queueID)
	defer s.pipelines.end(queueID, run)
	if s.pausedBeforeStart(queueID) {
		return errPaused
	}

	// A failed upgrade leaves the movie "downloaded" (its old file is still
	// there); anything else goes back to "missing".
	restoreStatus := library.StatusMissing
	if m, err := s.MovieRepo.Get(movieID); err == nil {
		restoreStatus = restoredMovieStatus(m)
	}
	fail := func(stepErr error) error {
		if run.cancelled.Load() {
			// The movie was removed from the library: nothing to report or retry.
			_ = s.QueueRepo.SetStatus(queueID, queue.StatusFailed, errRemovedFromLibrary.Error())
			return errRemovedFromLibrary
		}
		if handled, err := s.settleInterrupted(queueID, run); handled {
			return err // paused or stopped by a person: not a failure
		}
		// The person reads this message; the raw error goes to the log only
		// (noteDownloadFailure keeps it as the technical detail).
		plain := plainerror.Message(stepErr)
		_ = s.QueueRepo.SetStatus(queueID, queue.StatusFailed, plain)
		noteDownloadFailure(queueID, movieTitle, fmt.Sprintf("/title/%d", tmdbID), stepErr)
		blocklisted := isBadRelease(stepErr) && s.blocklistBadRelease(releaseTitle, protocol, movieID, 0, stepErr)
		_ = s.MovieRepo.SetStatus(movieID, restoreStatus, "", "")
		message := fmt.Sprintf("%s: %s", movieTitle, plain)
		_ = s.QueueRepo.LogActivity(movieID, "failed", message)
		failedItem := s.movieItemByID(movieID, movieTitle, movieYear, tmdbID)
		failedItem.Reason, failedItem.Release = plain, releaseTitle
		s.notifyItem("failed", failedItem)
		if blocklisted {
			s.retryAfterBadRelease(movieID, 0)
		}
		return stepErr
	}

	_ = s.MovieRepo.SetStatus(movieID, library.StatusDownloading, "", "")
	if err := s.QueueRepo.SetStatus(queueID, queue.StatusDownloading, ""); err != nil {
		return fail(err)
	}

	incompleteDir, err := s.prepareDownload(ctx, queueID, downloadURL, protocol)
	if err != nil {
		return fail(err)
	}

	videoFile, err := organizer.FindLargestVideoFile(incompleteDir)
	if err != nil {
		return fail(badRelease(fmt.Errorf("couldn't find the movie file in the finished download: %w", err)))
	}

	destPath, err := s.buildDestPath(s.moviesRoot(), movieTitle, movieYear, tmdbID, releaseTitle, videoFile)
	if err != nil {
		return fail(err)
	}

	// Classified up front (not just the bare resolution) so it's available
	// both for ConflictOverwriteIfBetter's comparison below and for what
	// gets stored on the movie once import succeeds — the automation hunt
	// loop's upgrade-hunting needs a tier it can directly Rank() against a
	// candidate release.
	release := parser.Parse(releaseTitle)
	tier := quality.Classify(release)

	if ctx.Err() != nil {
		return fail(ctx.Err()) // cancelled: never import for a movie that was removed
	}
	policy, isBetter := s.importConflictPolicy(movieID, tier)
	// The file the movie has now, and whether this import may take its place
	// (decided before the movie's quality is updated below).
	var earlierFile string
	if cur, err := s.MovieRepo.Get(movieID); err == nil {
		earlierFile = cur.FilePath
	}
	replaceEarlier := replacesOldFiles(policy, isBetter)
	result, err := organizer.Import(videoFile, destPath, policy, isBetter)
	if err != nil {
		return fail(fmt.Errorf("couldn't move the file into your library: %w", err))
	}
	if result.Skipped {
		if policy == organizer.ConflictAsk {
			if err := s.QueueRepo.SetConflict(queueID, videoFile, destPath); err != nil {
				return fail(err)
			}
			message := fmt.Sprintf("%s: a file already exists at %s. Choose what to do in Activity.", movieTitle, destPath)
			_ = s.QueueRepo.LogActivity(movieID, "conflict", message)
			conflictItem := s.movieItemByID(movieID, movieTitle, movieYear, tmdbID)
			conflictItem.Path, conflictItem.Release = destPath, releaseTitle
			s.notifyItem("conflict", conflictItem)
			return nil
		}
		return fail(fmt.Errorf("a file already exists at %s. Remove it, or change \"If a file already exists\" in Settings > Library > Folders and file names", destPath))
	}

	if err := s.MovieRepo.SetStatus(movieID, library.StatusDownloaded, string(tier), result.DestPath); err != nil {
		return fail(err)
	}
	if replaceEarlier {
		// The new file has another name than the one it replaces: the old one goes.
		s.removeReplacedFile(s.moviesRoot(), earlierFile, result.DestPath, movieID, 0, movieTitle)
	}
	// Subtitles that came with the release go next to the movie before any
	// download is considered, so they count as already there.
	s.importSidecarSubtitles(sidecarsIn(incompleteDir), result.DestPath, movieID, movieTitle)
	s.autoSubtitlesFor(subtitleItem{kind: "movie", id: movieID, title: movieTitle, tmdbID: tmdbID, filePath: result.DestPath})
	if err := s.QueueRepo.SetStatus(queueID, queue.StatusCompleted, ""); err != nil {
		return fail(err)
	}
	s.cleanupWorkDir(incompleteDir, protocol)
	// Earlier failed tries for this movie are settled now.
	_, _ = s.QueueRepo.ClearFailedForMovie(movieID, queueID)
	importedMessage := fmt.Sprintf("%s imported to %s", movieTitle, result.DestPath)
	_ = s.QueueRepo.LogActivity(movieID, "imported", importedMessage)
	importedItem := s.movieItemByID(movieID, movieTitle, movieYear, tmdbID)
	importedItem.Quality, importedItem.SizeBytes, importedItem.Path = string(tier), fileSize(result.DestPath), result.DestPath
	s.notifyItem("imported", importedItem)
	s.movieImported(result.DestPath)
	s.pushTagsAfterImport(library.TagMovie, movieID)
	return nil
}

// prepareDownload is the protocol-agnostic first half of every pipeline
// (movie or TV): download, then PAR2-repair and unpack, leaving the
// finished files in the returned directory ready to be picked apart and
// imported. Callers own the downloading status transition and their own
// failure handling.
func (s *Server) prepareDownload(ctx context.Context, queueID int64, downloadURL string, protocol indexers.Protocol) (string, error) {
	incompleteDir := filepath.Join(s.downloadsIncompleteDir(), fmt.Sprintf("queue-%d", queueID))

	var (
		downloadErr error
		missing     int
	)
	// A retry of a failed Usenet download starts from the files it already
	// has when they are still there and pass PAR2 (see reuseDownloaded).
	reused := s.reuseDownloaded(queueID, downloadURL, incompleteDir)
	if !reused && protocol != indexers.ProtocolTorrent && keptResume(incompleteDir) {
		s.queueEvent(queueID, "retried", queue.LevelInfo, "Carrying on from the part already downloaded")
	}
	switch {
	case reused:
	case protocol == indexers.ProtocolTorrent:
		downloadErr = s.downloadTorrent(ctx, queueID, downloadURL, incompleteDir)
	default:
		missing, downloadErr = s.downloadUsenet(ctx, queueID, downloadURL, incompleteDir)
	}
	if downloadErr != nil {
		return "", fmt.Errorf("the download failed: %w", downloadErr)
	}
	if !reused && protocol != indexers.ProtocolTorrent {
		markDownloaded(incompleteDir, missing)
	}

	if err := ctx.Err(); err != nil {
		return "", err // paused or stopped: leave the files as they are
	}
	if err := s.QueueRepo.SetStatus(queueID, queue.StatusImporting, ""); err != nil {
		return "", err
	}
	if err := s.verifyStep(queueID, incompleteDir, missing); err != nil {
		return "", badRelease(fmt.Errorf("repairing the download failed: %w", err))
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := s.unpackStep(queueID, incompleteDir); err != nil {
		return "", unpackFailure(err)
	}
	return incompleteDir, nil
}

// downloadUsenet downloads an NZB with the built-in downloader from every
// enabled Usenet server, primary first. It returns how many articles no
// server had: those are left for PAR2 repair rather than failing outright.
func (s *Server) downloadUsenet(ctx context.Context, queueID int64, nzbURL, incompleteDir string) (int, error) {
	if isMagnetURI(nzbURL) {
		return 0, errors.New("this is a torrent (a magnet link), but its indexer is set up as a Usenet indexer. Open Settings > Indexers & Search, remove it and add it again on the Torrent tab")
	}
	nzbBytes, err := s.fetchRelease(ctx, nzbURL)
	if err != nil {
		return 0, fmt.Errorf("couldn't get the NZB file: %w", err)
	}
	if looksLikeTorrentFile(nzbBytes) {
		return 0, errors.New("this is a .torrent file, but its indexer is set up as a Usenet indexer. Open Settings > Indexers & Search, remove it and add it again on the Torrent tab")
	}
	nzb, err := download.ParseNZB(nzbBytes)
	if err != nil {
		return 0, badRelease(fmt.Errorf("couldn't read the NZB file: %w", err))
	}

	servers, err := s.ClientRepo.List()
	if err != nil {
		return 0, fmt.Errorf("couldn't load your Usenet servers: %w", err)
	}
	if len(servers) == 0 {
		return 0, fmt.Errorf("no Usenet server is set up. Add your provider's server in Settings > Downloading > Usenet and torrents first")
	}
	configs := make([]download.ClientConfig, len(servers))
	for i, sc := range servers {
		configs[i] = sc.Config
	}

	progress := newProgressSaver(func(pct float64) error { return s.QueueRepo.SetProgress(queueID, pct) })
	res, err := download.DownloadFromServers(ctx, configs, nzb, incompleteDir, progress.Report)
	progress.Flush()
	if err != nil {
		return 0, err
	}
	return res.MissingSegments, nil
}

// downloadTorrent handles both magnet URIs and direct .torrent file URLs.
// Every torrent runs in one shared engine listening on the torrent port
// (see torrentRegistry), saved in this queue item's own folder.
//
// The torrent is deliberately NOT stopped as soon as the download finishes:
// a background goroutine keeps it seeding (a good swarm citizen, and how the
// seed ratio/time limits in Settings have any effect at all) until its
// seeding goal is met, then stops it and, once the download has been
// imported, removes its data (seedingGoalMet). This function returns as soon
// as the download completes so the pipeline can move on to import.
func (s *Server) downloadTorrent(ctx context.Context, queueID int64, downloadURL, incompleteDir string) error {
	if !s.torrentsEnabled() {
		return errTorrentsDisabled
	}
	// Get the magnet link or .torrent file first. For a definition-based
	// indexer this goes through its signed-in session; it is indexer
	// traffic, not torrent traffic, so it doesn't wait for the VPN check.
	magnet, torrentFilePath, err := s.torrentSource(ctx, downloadURL, incompleteDir)
	if err != nil {
		return fmt.Errorf("couldn't get the torrent: %w", err)
	}
	tunnel, err := s.torrentTunnel()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	job, err := s.torrents.start(queueID, incompleteDir, s.downloadsIncompleteDir(), s.torrentListenPort(), tunnel, cancel)
	if err != nil {
		cancel()
		return fmt.Errorf("couldn't start the torrent client: %w", err)
	}
	// Torrents may be switched off while this one is still starting up.
	if !s.torrentsEnabled() {
		s.torrents.stop(job)
		return errTorrentsDisabled
	}

	var t *torrent.Torrent
	if magnet != "" {
		t, err = job.engine.tc.AddMagnetIn(ctx, magnet, incompleteDir)
	} else {
		t, err = job.engine.tc.AddTorrentFileIn(ctx, torrentFilePath, incompleteDir)
	}
	if err == nil && !s.torrents.attach(job, t) {
		err = context.Canceled
	}
	if err != nil {
		s.torrents.stop(job)
		if !s.torrentsEnabled() {
			return errTorrentsDisabled
		}
		return fmt.Errorf("couldn't add the torrent: %w", err)
	}

	progress := newProgressSaver(func(pct float64) error { return s.QueueRepo.SetProgress(queueID, pct) })
	err = torrentclient.Download(ctx, t, progress.Report)
	progress.Flush()
	if err != nil {
		s.torrents.stop(job)
		if !s.torrentsEnabled() {
			return errTorrentsDisabled
		}
		if errors.Is(err, torrentclient.ErrStalled) {
			// No one is sharing it: blocklist it and try another release.
			return badRelease(err)
		}
		return err
	}

	goal := torrentclient.SeedGoal{Ratio: s.torrentSeedRatioLimit(), Time: s.torrentSeedTimeLimit()}
	go func() {
		met := torrentclient.SeedUntilGoal(ctx, t, time.Now(), goal, seedCheckInterval)
		s.torrents.stop(job)
		if met {
			s.seedingGoalMet(queueID, incompleteDir)
		}
	}()
	return nil
}

// torrentListenPort is the port the torrent engine listens on: the saved
// setting, or torrentclient.DefaultListenPort (58264) when it is unset or 0
// (which older versions saved to mean "any port").
func (s *Server) torrentListenPort() int {
	v, _ := s.Settings.Get(settings.KeyTorrentListenPort)
	if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 && n <= 65535 {
		return n
	}
	return torrentclient.DefaultListenPort
}

func (s *Server) torrentSeedRatioLimit() float64 {
	v, _ := s.Settings.Get(settings.KeyTorrentSeedRatioLimit)
	f, _ := strconv.ParseFloat(v, 64)
	return f
}

func (s *Server) torrentSeedTimeLimit() time.Duration {
	v, _ := s.Settings.Get(settings.KeyTorrentSeedTimeLimitH)
	h, _ := strconv.ParseFloat(v, 64)
	return time.Duration(h * float64(time.Hour))
}

// torrentTunnel picks the connection torrent traffic uses. A connected VPN is
// always used, whatever the kill switch says. With no tunnel up, torrents wait
// (an error here) if the kill switch is on, or if a VPN is switched on but not
// up yet, as after a restart or when its server stopped answering: going out
// directly then would expose the home address to a person who expects the VPN
// to be in use. Only with the switch off and no VPN selected do torrents go
// direct. A nil result means a direct connection.
func (s *Server) torrentTunnel() (torrentclient.TunnelDialer, error) {
	// Check for a nil *Tunnel before converting to the interface, or a nil
	// pointer would look like a real tunnel.
	if t := s.VPNManager.Tunnel(); t != nil {
		return t, nil
	}
	if st := s.VPNManager.Status(); st.Label != "" {
		// Fail closed: a VPN is switched on and is not up.
		reason := st.Reason
		if reason == "" {
			reason = "It isn't connected yet."
		}
		return nil, fmt.Errorf("torrent downloads are waiting for your VPN (%s). %s Check Settings > Downloading > VPN protection, or press Disconnect to download without it", st.Label, reason)
	}
	if s.vpnRequiredForTorrents() {
		// Fail closed: the setting says torrent traffic must go through the
		// VPN and none is connected, so the grab does not proceed.
		return nil, fmt.Errorf("torrent downloads need a VPN and none is connected. Connect one in Settings > Downloading > VPN protection")
	}
	return nil, nil
}

func (s *Server) vpnRequiredForTorrents() bool {
	v, _ := s.Settings.GetBool(settings.KeyVPNRequireForTorrents)
	return v
}

// importConflictPolicy reads the configured naming-collision policy for a
// movie import. See conflictPolicy for the shared logic.
func (s *Server) importConflictPolicy(movieID int64, incomingTier quality.Tier) (organizer.ConflictPolicy, func() bool) {
	return s.conflictPolicy(incomingTier, func() (quality.Tier, bool) {
		current, err := s.MovieRepo.Get(movieID)
		if err != nil {
			return "", false
		}
		return quality.Tier(current.Quality), true
	})
}

// conflictPolicy maps the configured policy setting to organizer's enum
// and, for "overwrite if better", builds the isBetter callback by
// comparing incomingTier against whatever currentTier reports for the
// existing file (Unknown-ranked when the item predates tier tracking,
// which correctly counts as an upgrade rather than erroring). If the
// current tier can't be determined it fails closed — never overwrites.
func (s *Server) conflictPolicy(incomingTier quality.Tier, currentTier func() (quality.Tier, bool)) (organizer.ConflictPolicy, func() bool) {
	v, _ := s.Settings.Get(settings.KeyImportConflictPolicy)
	switch v {
	case "overwrite":
		return organizer.ConflictOverwrite, nil
	case "overwrite_if_better":
		isBetter := func() bool {
			current, ok := currentTier()
			if !ok {
				return false
			}
			return quality.Rank(incomingTier) > quality.Rank(current)
		}
		return organizer.ConflictOverwriteIfBetter, isBetter
	case "ask":
		return organizer.ConflictAsk, nil
	default:
		return organizer.ConflictSkip, nil
	}
}

// resolveConflict finishes a queue item parked in StatusConflict (the
// "always ask" policy) — overwrite replaces the existing file
// with the parked download, skip leaves the existing file untouched
// (nothing new was imported, so the movie goes back to missing, matching
// how any other failed grab is handled).
func (s *Server) resolveConflict(queueID int64, overwrite bool) error {
	item, err := s.QueueRepo.Get(queueID)
	if err != nil {
		return err
	}
	if item.Status != queue.StatusConflict {
		return fmt.Errorf("download %d isn't waiting for a decision (its status is %s)", queueID, item.Status)
	}

	if !overwrite {
		if err := s.QueueRepo.SetStatus(queueID, queue.StatusFailed, "you chose to keep the existing file"); err != nil {
			return err
		}
		// The download is not wanted: its working folder goes (a torrent
		// still seeding keeps it until its seeding goal is met).
		s.cleanupWorkDir(s.workDirFor(queueID), indexers.Protocol(item.Protocol))
		return s.MovieRepo.SetStatus(item.MovieID, library.StatusMissing, "", "")
	}

	result, err := organizer.Import(item.SourcePath, item.DestPath, organizer.ConflictOverwrite, nil)
	if err != nil {
		return fmt.Errorf("couldn't replace the existing file: %w", err)
	}
	tier := quality.Classify(parser.Parse(item.ReleaseTitle))
	if err := s.MovieRepo.SetStatus(item.MovieID, library.StatusDownloaded, string(tier), result.DestPath); err != nil {
		return err
	}
	if err := s.QueueRepo.SetStatus(queueID, queue.StatusCompleted, ""); err != nil {
		return err
	}
	s.cleanupWorkDir(s.workDirFor(queueID), indexers.Protocol(item.Protocol))
	message := fmt.Sprintf("You chose to replace the existing file. Imported to %s.", result.DestPath)
	_ = s.QueueRepo.LogActivity(item.MovieID, "imported", message)
	replacedItem := notify.Item{Media: "movie", Title: item.ReleaseTitle}
	if m, err := s.MovieRepo.Get(item.MovieID); err == nil {
		replacedItem = movieItem(m)
	}
	replacedItem.Quality, replacedItem.SizeBytes, replacedItem.Path = string(tier), fileSize(result.DestPath), result.DestPath
	s.notifyItem("imported", replacedItem)
	s.movieImported(result.DestPath)
	return nil
}

func isMagnetURI(s string) bool {
	return strings.HasPrefix(s, "magnet:")
}

// looksLikeTorrentFile reports whether the bytes are a bencoded .torrent file
// (a dictionary that starts with d and holds an "announce" or "info" key)
// rather than an NZB, which is XML.
func looksLikeTorrentFile(b []byte) bool {
	if len(b) < 8 || b[0] != 'd' {
		return false
	}
	head := b
	if len(head) > 256 {
		head = head[:256]
	}
	return bytes.Contains(head, []byte("announce")) || bytes.Contains(head, []byte("4:info")) || bytes.Contains(head, []byte("8:encoding"))
}

// torrentSource turns a grab's download URL into either a magnet link or a
// local .torrent file. Links from definition-based indexers are resolved
// through that indexer's session (login, details page, Cloudflare).
func (s *Server) torrentSource(ctx context.Context, downloadURL, destDir string) (magnet, torrentFile string, err error) {
	switch {
	case isMagnetURI(downloadURL):
		return downloadURL, "", nil
	case indexers.IsLinkRef(downloadURL):
		dl, err := s.resolveIndexerLink(ctx, downloadURL)
		if err != nil {
			return "", "", err
		}
		if dl.Magnet != "" {
			return dl.Magnet, "", nil
		}
		path, err := writeTorrentFile(dl.Data, destDir)
		return "", path, err
	default:
		path, err := downloadToTempFile(ctx, downloadURL, destDir)
		return "", path, err
	}
}

// fetchRelease downloads a release file (an NZB), resolving links from
// definition-based indexers through their session.
func (s *Server) fetchRelease(ctx context.Context, downloadURL string) ([]byte, error) {
	if !indexers.IsLinkRef(downloadURL) {
		return fetchURL(ctx, downloadURL)
	}
	dl, err := s.resolveIndexerLink(ctx, downloadURL)
	if err != nil {
		return nil, err
	}
	if dl.Magnet != "" {
		return nil, fmt.Errorf("the indexer returned a magnet link, not a file")
	}
	return dl.Data, nil
}

// resolveIndexerLink asks the definition-based indexer a link came from for
// the actual download.
func (s *Server) resolveIndexerLink(ctx context.Context, ref string) (*indexers.Download, error) {
	id, link, ok := indexers.DecodeLinkRef(ref)
	if !ok {
		return nil, fmt.Errorf("the indexer gave an invalid download link")
	}
	inst, err := s.IndexerRepo.Get(id)
	if errors.Is(err, indexers.ErrNotFound) {
		return nil, fmt.Errorf("the indexer this release came from has been removed")
	}
	if err != nil {
		return nil, err
	}
	if !inst.IsCardigann() {
		return nil, fmt.Errorf("indexer %q can't resolve this link", inst.Name)
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	return s.Cardigann.Download(ctx, inst, link)
}

func writeTorrentFile(data []byte, destDir string) (string, error) {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", fmt.Errorf("couldn't create the folder: %w", err)
	}
	path := filepath.Join(destDir, "release.torrent")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("couldn't save the .torrent file: %w", err)
	}
	return path, nil
}

// downloadToTempFile fetches a .torrent file's bytes and writes them into
// destDir so torrentclient.AddTorrentFile (which reads from a local path,
// mirroring the anacrolix/torrent API) can load it.
func downloadToTempFile(ctx context.Context, url, destDir string) (string, error) {
	data, err := fetchURL(ctx, url)
	if err != nil {
		return "", err
	}
	return writeTorrentFile(data, destDir)
}

func percent(done, total int64) float64 {
	if total <= 0 {
		return 0
	}
	return float64(done) / float64(total) * 100
}

func fetchURL(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	// Through the address check (no cloud metadata or link-local targets,
	// even via a redirect); errors and messages never carry the link's
	// query string, which holds the indexer key.
	client := netguard.Client(30 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return nil, netguard.CleanError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the server answered with status %d for %s", resp.StatusCode, netguard.RedactURL(url))
	}
	const maxDownloadFile = 256 << 20 // an NZB or torrent file is a few MB at most
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDownloadFile+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxDownloadFile {
		return nil, fmt.Errorf("the file at %s is larger than %d MB", netguard.RedactURL(url), maxDownloadFile>>20)
	}
	return data, nil
}

// verifyAndRepair runs PAR2 repair when both recovery files and the `par2`
// binary are available. Usenet posts without PAR2 data, or a build/host
// without the binary installed, simply skip this step rather than failing
// the whole import (PAR2 is a best-effort integrity layer, not a hard
// requirement to produce a playable file).
//
// missing is how many articles no server had. Then repair is mandatory: if
// PAR2 cannot rebuild the file the release is unusable, so the step fails
// rather than quietly passing on a file with holes in it.
func verifyAndRepair(dir string, missing int) error {
	// Obfuscated releases first get their real names back from the PAR2
	// data, so repair, unpacking and import all see the names they expect.
	if n, err := organizer.Deobfuscate(dir); err != nil {
		log.Printf("pipeline: restoring real file names in %s: %v", dir, err)
	} else if n > 0 {
		log.Printf("pipeline: gave %d obfuscated file(s) their real names in %s", n, dir)
	}
	repairer := organizer.NewRepairer()
	if !repairer.Available() {
		if missing > 0 {
			return fmt.Errorf("%s couldn't be found on your Usenet servers and PAR2 repair isn't available", plural(missing, "article"))
		}
		return nil
	}
	par2Files, err := organizer.FindMainPar2Files(dir)
	if err != nil {
		return err
	}
	if missing > 0 && len(par2Files) == 0 {
		return fmt.Errorf("%s couldn't be found on your Usenet servers and the release has no PAR2 files to repair it", plural(missing, "article"))
	}
	for _, f := range par2Files {
		result, err := repairer.Verify(f)
		if err != nil {
			return err
		}
		if !result.OK {
			if err := repairer.Repair(f); err != nil {
				return err
			}
		}
	}
	return nil
}

// unpackArchives extracts every primary archive volume found. RAR and ZIP
// are unpacked natively; only .7z needs the external 7z tool (Extract
// returns organizer.ErrSevenZipMissing if one turns up without it).
func unpackArchives(dir string) error {
	extractor := organizer.NewExtractor()
	archives, err := organizer.FindPrimaryArchives(dir)
	if err != nil {
		return err
	}
	for _, a := range archives {
		if err := extractor.Extract(a, dir); err != nil {
			return err
		}
	}
	return nil
}

// unpackFailure classifies an unpack error: a full disk or a missing 7z
// tool is a problem with our setup and must never blocklist a good release;
// everything else (corrupt, password protected, unsafe archive) is the
// release's fault.
func unpackFailure(err error) error {
	wrapped := fmt.Errorf("couldn't unpack the download: %w", err)
	if errors.Is(err, organizer.ErrInsufficientSpace) || errors.Is(err, organizer.ErrSevenZipMissing) || isDiskFull(err) {
		return wrapped
	}
	return badRelease(wrapped)
}

// isDiskFull reports whether err is the operating system saying the disk (or
// the user's quota on it) ran out while writing. It can happen part-way
// through a write even after the free-space check passed, for example when
// another download fills the disk at the same time.
func isDiskFull(err error) bool {
	return errors.Is(err, syscall.ENOSPC) || errors.Is(err, syscall.EDQUOT)
}

// downloadsIncompleteDir resolves the root of in-progress downloads.
// Settings.KeyDownloadsPath — the "Downloads folder" field the
// onboarding wizard and Settings UI collect — takes precedence when set;
// the DOWNLOADS_DIR env var / cfg.DownloadsDir is only the container-level
// default a fresh install starts with (the "Config" convention: env
// vars are container-level defaults, everything a user can actually
// change belongs in Settings). This was previously a real bug: the
// pipeline always used cfg.DownloadsIncomplete directly, so changing
// "Downloads folder" in Settings silently did nothing.
func (s *Server) downloadsIncompleteDir() string {
	root, _ := s.Settings.Get(settings.KeyDownloadsPath)
	if root == "" {
		return s.cfg.DownloadsIncomplete
	}
	return filepath.Join(root, "incomplete")
}

// buildDestPath renders the configured naming preset/format into a final
// library path.
func (s *Server) buildDestPath(root, movieTitle string, year, tmdbID int, releaseTitle, videoFile string) (string, error) {
	moviesPath := root
	if moviesPath == "" {
		moviesPath = s.moviesRoot()
	}
	if moviesPath == "" {
		return "", fmt.Errorf("the movies folder isn't set. Choose one in Settings > Library > Folders and file names")
	}

	preset, err := s.Settings.Get(settings.KeyNamingPreset)
	if err != nil {
		return "", err
	}
	format := organizer.Presets["plex"]
	if preset == "custom" {
		if custom, err := s.Settings.Get(settings.KeyMovieNameFormat); err == nil && custom != "" {
			format = custom
		}
	} else if p, ok := organizer.Presets[preset]; ok {
		format = p
	}

	release := parser.Parse(releaseTitle)
	ctx := organizer.NamingContext{
		MovieTitle: movieTitle, Year: year, TMDBID: tmdbID,
		Quality: release.Resolution, Source: release.Source, Codec: release.Codec,
		Edition: release.Edition, ReleaseGroup: release.Group,
		AudioCodec: release.AudioCodec, HDR: release.HDR, Is3D: release.Is3D,
		Proper: release.Proper, Repack: release.Repack, Languages: release.Languages,
	}

	sanitizeMode, replacement := s.illegalCharSettings()
	folder := organizer.Sanitize(organizer.Render(organizer.Presets["plex"], ctx), sanitizeMode, replacement)
	filename := organizer.Sanitize(organizer.Render(format, ctx), sanitizeMode, replacement) + filepath.Ext(videoFile)
	return filepath.Join(moviesPath, folder, filename), nil
}

// illegalCharSettings reads the configured illegal-filename-character
// handling mode ("configurable (replace vs. strip)"), found
// missing entirely during a full feature-vs-code audit: organizer.Sanitize
// always supported both modes, but nothing let a user actually choose.
func (s *Server) illegalCharSettings() (organizer.SanitizeMode, string) {
	mode, _ := s.Settings.Get(settings.KeyIllegalCharMode)
	if mode != "replace" {
		return organizer.SanitizeStrip, ""
	}
	replacement, _ := s.Settings.Get(settings.KeyIllegalCharReplacement)
	if replacement == "" {
		replacement = "-"
	}
	return organizer.SanitizeReplace, replacement
}
