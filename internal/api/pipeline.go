package api

import (
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
	"time"

	"github.com/anacrolix/torrent"

	"github.com/ryanborg/mediarium/internal/download"
	"github.com/ryanborg/mediarium/internal/indexers"
	"github.com/ryanborg/mediarium/internal/library"
	"github.com/ryanborg/mediarium/internal/organizer"
	"github.com/ryanborg/mediarium/internal/parser"
	"github.com/ryanborg/mediarium/internal/quality"
	"github.com/ryanborg/mediarium/internal/queue"
	"github.com/ryanborg/mediarium/internal/settings"
	"github.com/ryanborg/mediarium/internal/torrentclient"
)

// runPipeline is the whole grab -> download -> organize flow (PRD §7's
// Phase 1 exit criteria, extended in Phase 2 to also cover torrents) for
// one queued release. It runs in its own goroutine, started from
// handleGrab/handleSearchGrab, and reports every state transition through
// QueueRepo/MovieRepo/activity so the UI (polling /api/queue) sees
// progress without needing a websocket.
func (s *Server) runPipeline(queueID, movieID int64, movieTitle string, movieYear, tmdbID int, releaseTitle, downloadURL string, protocol indexers.Protocol) error {
	ctx := context.Background()

	fail := func(stepErr error) error {
		_ = s.QueueRepo.SetStatus(queueID, queue.StatusFailed, stepErr.Error())
		_ = s.MovieRepo.SetStatus(movieID, library.StatusMissing, "", "")
		message := fmt.Sprintf("%s: %v", movieTitle, stepErr)
		_ = s.QueueRepo.LogActivity(movieID, "failed", message)
		s.notifyEvent("failed", movieTitle, message)
		if isBadRelease(stepErr) {
			s.onBadRelease(releaseTitle, protocol, movieID, 0, stepErr)
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
		return fail(badRelease(fmt.Errorf("locate movie file in completed download: %w", err)))
	}

	destPath, err := s.buildDestPath(movieTitle, movieYear, tmdbID, releaseTitle, videoFile)
	if err != nil {
		return fail(err)
	}

	// Classified up front (not just the bare resolution) so it's available
	// both for ConflictOverwriteIfBetter's comparison below and for what
	// gets stored on the movie once import succeeds — the automation hunt
	// loop's upgrade-hunting needs a tier it can directly Rank() against a
	// candidate release (PRD §7 Phase 2).
	release := parser.Parse(releaseTitle)
	tier := quality.Classify(release)

	policy, isBetter := s.importConflictPolicy(movieID, tier)
	result, err := organizer.Import(videoFile, destPath, policy, isBetter)
	if err != nil {
		return fail(fmt.Errorf("import into library: %w", err))
	}
	if result.Skipped {
		if policy == organizer.ConflictAsk {
			if err := s.QueueRepo.SetConflict(queueID, videoFile, destPath); err != nil {
				return fail(err)
			}
			message := fmt.Sprintf("%s: a file already exists at %s — resolve it in Activity/Queue", movieTitle, destPath)
			_ = s.QueueRepo.LogActivity(movieID, "conflict", message)
			s.notifyEvent("conflict", movieTitle, message)
			return nil
		}
		return fail(fmt.Errorf("a file already exists at %s — resolve the conflict manually", destPath))
	}

	if err := s.MovieRepo.SetStatus(movieID, library.StatusDownloaded, string(tier), result.DestPath); err != nil {
		return fail(err)
	}
	s.autoSubtitlesFor(subtitleItem{kind: "movie", id: movieID, title: movieTitle, tmdbID: tmdbID, filePath: result.DestPath})
	if err := s.QueueRepo.SetStatus(queueID, queue.StatusCompleted, ""); err != nil {
		return fail(err)
	}
	s.cleanupWorkDir(incompleteDir, protocol)
	importedMessage := fmt.Sprintf("%s imported to %s", movieTitle, result.DestPath)
	_ = s.QueueRepo.LogActivity(movieID, "imported", importedMessage)
	s.notifyEvent("imported", movieTitle, importedMessage)
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
	switch protocol {
	case indexers.ProtocolTorrent:
		downloadErr = s.downloadTorrent(ctx, queueID, downloadURL, incompleteDir)
	default:
		missing, downloadErr = s.downloadUsenet(ctx, queueID, downloadURL, incompleteDir)
	}
	if downloadErr != nil {
		return "", fmt.Errorf("download: %w", downloadErr)
	}

	if err := s.QueueRepo.SetStatus(queueID, queue.StatusImporting, ""); err != nil {
		return "", err
	}
	if err := verifyAndRepair(incompleteDir, missing); err != nil {
		return "", badRelease(fmt.Errorf("par2 repair: %w", err))
	}
	if err := unpackArchives(incompleteDir); err != nil {
		return "", unpackFailure(err)
	}
	return incompleteDir, nil
}

// downloadUsenet downloads an NZB with the built-in downloader from every
// enabled Usenet server, primary first. It returns how many articles no
// server had: those are left for PAR2 repair rather than failing outright.
func (s *Server) downloadUsenet(ctx context.Context, queueID int64, nzbURL, incompleteDir string) (int, error) {
	nzbBytes, err := fetchURL(ctx, nzbURL)
	if err != nil {
		return 0, fmt.Errorf("fetch nzb: %w", err)
	}
	nzb, err := download.ParseNZB(nzbBytes)
	if err != nil {
		return 0, badRelease(fmt.Errorf("parse nzb: %w", err))
	}

	servers, err := s.ClientRepo.List()
	if err != nil {
		return 0, fmt.Errorf("load usenet servers: %w", err)
	}
	if len(servers) == 0 {
		return 0, fmt.Errorf("no Usenet server configured — add your provider's server in Settings > Downloads before grabbing from Usenet")
	}
	configs := make([]download.ClientConfig, len(servers))
	for i, sc := range servers {
		configs[i] = sc.Config
	}

	res, err := download.DownloadFromServers(ctx, configs, nzb, incompleteDir, func(done, total int64) {
		_ = s.QueueRepo.SetProgress(queueID, percent(done, total))
	})
	if err != nil {
		return 0, err
	}
	return res.MissingSegments, nil
}

// downloadTorrent handles both magnet URIs and direct .torrent file URLs
// (PRD §7 Phase 2). A dedicated torrentclient.Client is spun up per grab
// with its data dir scoped to this queue item, rather than sharing one
// long-lived client across every torrent — simpler and collision-free for
// Phase 2, at the cost of each grab paying its own client startup (new
// listen socket, DHT bootstrap if enabled). Worth revisiting alongside
// proper category-based save paths (PRD §7 Phase 2 bullet) later.
//
// The client is deliberately NOT closed as soon as the download finishes:
// ownership passes to a background goroutine that keeps seeding (a good
// swarm citizen, and how the seed ratio/time limits in Settings have any
// effect at all) until EnforceSeedLimits decides to stop, at which point
// it closes the client itself. This function returns as soon as the
// download completes so the pipeline can move on to import.
func (s *Server) downloadTorrent(ctx context.Context, queueID int64, downloadURL, incompleteDir string) error {
	if !s.torrentsEnabled() {
		return errTorrentsDisabled
	}
	ctx, cancel := context.WithCancel(ctx)
	cfg := torrentclient.Config{
		DataDir:        incompleteDir,
		ListenPort:     s.torrentListenPort(),
		SeedRatioLimit: s.torrentSeedRatioLimit(),
		SeedTimeLimit:  s.torrentSeedTimeLimit(),
	}
	if s.vpnRequiredForTorrents() {
		tunnel := s.VPNManager.Tunnel()
		if tunnel == nil {
			// Fail closed (PRD §4.7's kill switch): the setting says torrent
			// traffic must go through the VPN, and none is connected, so the
			// grab does not proceed — never silently fall back to a direct
			// connection just because that's easier.
			cancel()
			return fmt.Errorf("VPN required for torrent downloads (see Settings) but none is connected")
		}
		cfg.Tunnel = tunnel
	}
	tc, err := torrentclient.New(cfg)
	if err != nil {
		cancel()
		return fmt.Errorf("start torrent client: %w", err)
	}
	s.torrents.add(tc, cancel)
	// Torrents may be switched off while this one is still starting up.
	if !s.torrentsEnabled() {
		s.torrents.closeClient(tc)
		return errTorrentsDisabled
	}

	var t *torrent.Torrent
	if isMagnetURI(downloadURL) {
		t, err = tc.AddMagnet(ctx, downloadURL)
	} else {
		var torrentFilePath string
		torrentFilePath, err = downloadToTempFile(ctx, downloadURL, incompleteDir)
		if err == nil {
			t, err = tc.AddTorrentFile(ctx, torrentFilePath)
		}
	}
	if err != nil {
		s.torrents.closeClient(tc)
		if !s.torrentsEnabled() {
			return errTorrentsDisabled
		}
		return fmt.Errorf("add torrent: %w", err)
	}

	if err := torrentclient.Download(ctx, t, func(done, total int64) {
		_ = s.QueueRepo.SetProgress(queueID, percent(done, total))
	}); err != nil {
		s.torrents.closeClient(tc)
		if !s.torrentsEnabled() {
			return errTorrentsDisabled
		}
		return err
	}

	go func() {
		defer s.torrents.closeClient(tc)
		tc.EnforceSeedLimits(t, time.Now())
	}()
	return nil
}

func (s *Server) torrentListenPort() int {
	v, _ := s.Settings.Get(settings.KeyTorrentListenPort)
	n, _ := strconv.Atoi(v)
	return n
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

func (s *Server) vpnRequiredForTorrents() bool {
	v, _ := s.Settings.GetBool(settings.KeyVPNRequireForTorrents)
	return v
}

// importConflictPolicy reads the configured naming-collision policy (PRD
// §4.8) for a movie import. See conflictPolicy for the shared logic.
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
// "always ask" policy, PRD §4.8) — overwrite replaces the existing file
// with the parked download, skip leaves the existing file untouched
// (nothing new was imported, so the movie goes back to missing, matching
// how any other failed grab is handled).
func (s *Server) resolveConflict(queueID int64, overwrite bool) error {
	item, err := s.QueueRepo.Get(queueID)
	if err != nil {
		return err
	}
	if item.Status != queue.StatusConflict {
		return fmt.Errorf("queue item %d is not awaiting conflict resolution (status: %s)", queueID, item.Status)
	}

	if !overwrite {
		if err := s.QueueRepo.SetStatus(queueID, queue.StatusFailed, "skipped by user — existing file kept"); err != nil {
			return err
		}
		return s.MovieRepo.SetStatus(item.MovieID, library.StatusMissing, "", "")
	}

	result, err := organizer.Import(item.SourcePath, item.DestPath, organizer.ConflictOverwrite, nil)
	if err != nil {
		return fmt.Errorf("resolve conflict: %w", err)
	}
	tier := quality.Classify(parser.Parse(item.ReleaseTitle))
	if err := s.MovieRepo.SetStatus(item.MovieID, library.StatusDownloaded, string(tier), result.DestPath); err != nil {
		return err
	}
	if err := s.QueueRepo.SetStatus(queueID, queue.StatusCompleted, ""); err != nil {
		return err
	}
	message := fmt.Sprintf("Manually resolved import conflict, imported to %s", result.DestPath)
	_ = s.QueueRepo.LogActivity(item.MovieID, "imported", message)
	s.notifyEvent("imported", item.ReleaseTitle, message)
	return nil
}

func isMagnetURI(s string) bool {
	return strings.HasPrefix(s, "magnet:")
}

// downloadToTempFile fetches a .torrent file's bytes and writes them into
// destDir so torrentclient.AddTorrentFile (which reads from a local path,
// mirroring the anacrolix/torrent API) can load it.
func downloadToTempFile(ctx context.Context, url, destDir string) (string, error) {
	data, err := fetchURL(ctx, url)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", fmt.Errorf("create dest dir: %w", err)
	}
	path := filepath.Join(destDir, "release.torrent")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write .torrent file: %w", err)
	}
	return path, nil
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
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d fetching %s", resp.StatusCode, url)
	}
	return io.ReadAll(resp.Body)
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
	repairer := organizer.NewRepairer()
	if !repairer.Available() {
		if missing > 0 {
			return fmt.Errorf("%d article(s) were missing and PAR2 repair is not available", missing)
		}
		return nil
	}
	par2Files, err := organizer.FindMainPar2Files(dir)
	if err != nil {
		return err
	}
	if missing > 0 && len(par2Files) == 0 {
		return fmt.Errorf("%d article(s) were missing and the release has no PAR2 files to repair it", missing)
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
	wrapped := fmt.Errorf("unpack: %w", err)
	if errors.Is(err, organizer.ErrInsufficientSpace) || errors.Is(err, organizer.ErrSevenZipMissing) {
		return wrapped
	}
	return badRelease(wrapped)
}

// cleanupWorkDir removes a finished Usenet download's working folder
// (downloads/incomplete/queue-N) once its files are safely in the library.
// It only ever touches a queue-N folder directly inside the incomplete
// directory. Torrent data is left alone: it is still being seeded.
func (s *Server) cleanupWorkDir(dir string, protocol indexers.Protocol) {
	if protocol == indexers.ProtocolTorrent {
		return
	}
	root := filepath.Clean(s.downloadsIncompleteDir())
	dir = filepath.Clean(dir)
	if filepath.Dir(dir) != root || !strings.HasPrefix(filepath.Base(dir), "queue-") {
		return
	}
	if err := os.RemoveAll(dir); err != nil {
		log.Printf("pipeline: clean up working folder %s: %v", dir, err)
	}
}

// downloadsIncompleteDir resolves the root of in-progress downloads (PRD
// §4.9). Settings.KeyDownloadsPath — the "Downloads folder" field the
// onboarding wizard and Settings UI collect — takes precedence when set;
// the DOWNLOADS_DIR env var / cfg.DownloadsDir is only the container-level
// default a fresh install starts with (CLAUDE.md's "Config" convention:
// env vars are container-level defaults, everything a user can actually
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
// library path (PRD §4.8).
func (s *Server) buildDestPath(movieTitle string, year, tmdbID int, releaseTitle, videoFile string) (string, error) {
	moviesPath := s.moviesRoot()
	if moviesPath == "" {
		return "", fmt.Errorf("movies library path not configured — set it in Settings")
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
	}

	sanitizeMode, replacement := s.illegalCharSettings()
	folder := organizer.Sanitize(organizer.Render(organizer.Presets["plex"], ctx), sanitizeMode, replacement)
	filename := organizer.Sanitize(organizer.Render(format, ctx), sanitizeMode, replacement) + filepath.Ext(videoFile)
	return filepath.Join(moviesPath, folder, filename), nil
}

// illegalCharSettings reads the configured illegal-filename-character
// handling mode (PRD §4.8 — "configurable (replace vs. strip)"), found
// missing entirely during a full PRD-vs-code audit: organizer.Sanitize
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
