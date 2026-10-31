package api

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/anacrolix/torrent"

	"github.com/rdborg/mediarium/internal/download"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/netguard"
	"github.com/rdborg/mediarium/internal/netproxy"
	"github.com/rdborg/mediarium/internal/notify"
	"github.com/rdborg/mediarium/internal/organizer"
	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/plainerror"
	"github.com/rdborg/mediarium/internal/quality"
	"github.com/rdborg/mediarium/internal/queue"
	"github.com/rdborg/mediarium/internal/settings"
	"github.com/rdborg/mediarium/internal/torrentclient"
	"github.com/rdborg/mediarium/internal/torrentopts"
	"github.com/rdborg/mediarium/internal/vpn"
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
		if dup := asDuplicate(stepErr); dup != nil {
			s.settleDuplicate(queueID, dup)
			if !dup.holdsTitle() {
				_ = s.MovieRepo.SetStatus(movieID, restoreStatus, "", "")
			}
			return stepErr
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
		if s.failureNotifyWanted(stepErr, downloadURL, releaseTitle) {
			s.notifyItem("failed", failedItem)
		}
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

	videoFile, err := largestVideoFile(incompleteDir)
	if err != nil {
		return fail(badRelease(fmt.Errorf("couldn't find the movie file in the finished download: %w", err)))
	}
	if err := s.checkMovieLength(ctx, tmdbID, videoFile); err != nil {
		// A fake, a sample or a cut-off file: nothing is imported, and the bad
		// download is cleared away before the next release is tried.
		s.discardWorkDir(incompleteDir, protocol)
		return fail(err)
	}

	root := s.moviesRoot()
	if m, err := s.MovieRepo.Get(movieID); err == nil {
		if m.CopyOf != 0 {
			if root = s.movieCopiesRoot(); root == "" {
				return fail(errors.New("the folder for second copies isn't set. Choose one in Settings > Library > Folders"))
			}
		} else {
			root = s.movieRootFor(m) // the folder chosen for this movie, else the main one
		}
	}
	destPath, nameFrom, err := s.buildDestPathFor(root, movieTitle, movieYear, tmdbID, releaseTitle, videoFile, true)
	if err != nil {
		return fail(err)
	}
	if m, err := s.MovieRepo.Get(movieID); err == nil {
		destPath = s.keepMovieFolder(m, root, destPath) // a movie that has a folder keeps it
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
	if owner := otherFileOwner(s.trackedFileOwners(), destPath, movieID, nil); owner != "" {
		return fail(errFileOfAnother(owner, destPath)) // never another title's file
	}
	policy, isBetter := s.importConflictPolicy(movieID, incomingRelease{title: releaseTitle, tier: tier})
	// The file the movie has now, and whether this import may take its place
	// (decided before the movie's quality is updated below).
	var earlierFile string
	if cur, err := s.MovieRepo.Get(movieID); err == nil {
		earlierFile = cur.FilePath
	}
	replaceEarlier := replacesOldFiles(policy, isBetter)
	item, itemErr := s.QueueRepo.Get(queueID)
	// A release picked by hand for a title that has a file, after the person
	// agreed to replace it, takes the file's place whatever "Better versions"
	// and "If a file already exists" say (those decide for automatic grabs).
	byHand := itemErr == nil && item.ReplaceByHand
	swapInPlace := handPickedSwap(byHand, destPath, []string{earlierFile})
	if handPickedReplaces(byHand, []string{earlierFile}) {
		replaceEarlier = true
	}
	if itemErr == nil && item.ReplacesFile != "" {
		// A smaller version a person asked for: it takes the file's place even
		// though it is of a lower quality. The old file is only removed below,
		// once the new one is in the library.
		var replacing bool
		policy, isBetter, replacing = smallerVersionPolicy(item.ReplacesFile, earlierFile, destPath, policy, isBetter)
		replaceEarlier = replaceEarlier || replacing
	}
	// The title's file under another name stays when this release may not
	// replace it: the import is turned away like a same-name collision.
	existing := destPath
	var result organizer.ImportResult
	if keepsEarlierFile(earlierFile, destPath, replaceEarlier) {
		existing = earlierFile
		result.Skipped = true
	} else if swapInPlace {
		var binned bool
		label := movieTitle
		if m, err := s.MovieRepo.Get(movieID); err == nil {
			label = movieLabel(m)
		}
		result, binned, err = s.importSwapping(s.fileRoot(root, destPath), videoFile, destPath, label, movieID, nil)
		if err != nil {
			return fail(fmt.Errorf("couldn't move the file into your library: %w", err))
		}
		if binned {
			_ = s.QueueRepo.LogActivity(movieID, "removed", fmt.Sprintf("%s: removed %s, which the new file replaced", movieTitle, filepath.Base(destPath)))
		}
	} else {
		result, err = s.importDownloaded(videoFile, destPath, policy, isBetter)
		if err != nil {
			return fail(fmt.Errorf("couldn't move the file into your library: %w", err))
		}
	}
	if result.Skipped {
		if policy == organizer.ConflictAsk {
			if err := s.QueueRepo.SetConflict(queueID, videoFile, destPath); err != nil {
				return fail(err)
			}
			message := fmt.Sprintf("%s: a file already exists at %s. Choose what to do in Activity.", movieTitle, existing)
			_ = s.QueueRepo.LogActivity(movieID, "conflict", message)
			conflictItem := s.movieItemByID(movieID, movieTitle, movieYear, tmdbID)
			conflictItem.Path, conflictItem.Release = destPath, releaseTitle
			s.notifyItem("conflict", conflictItem)
			return nil
		}
		// "If a file already exists" said to keep the file that is there (or the
		// new one is not better): that is a decision, not a failure, so no
		// "failed" notification and nothing in Logs and errors.
		if err := s.QueueRepo.SetStatus(queueID, queue.StatusCompleted, keptExistingMessage); err != nil {
			return fail(err)
		}
		data := s.discardWorkDirReport(incompleteDir, protocol)
		s.recordKept(queueID, existing, data)
		_ = s.MovieRepo.SetStatus(movieID, restoreStatus, "", "")
		_ = s.QueueRepo.LogActivity(movieID, "kept", fmt.Sprintf("%s: a file is already at %s, so Mediarium kept it and did not import %s.", movieTitle, existing, releaseTitle))
		s.logKeptData(movieID, 0, data, releaseTitle)
		return nil
	}

	if err := s.MovieRepo.SetStatus(movieID, library.StatusDownloaded, string(tier), result.DestPath); err == nil {
		s.afterMovieImported(movieID, releaseTitle)
	} else {
		return fail(err)
	}
	if replaceEarlier {
		// The new file has another name than the one it replaces: the old one goes.
		s.removeReplacedFile(root, earlierFile, result.DestPath, movieID, 0, movieTitle)
	}
	// Subtitles that came with the release go next to the movie before any
	// download is considered, so they count as already there.
	s.importSidecarSubtitles(s.sidecarsIn(incompleteDir), result.DestPath, movieID, movieTitle)
	movieSub := subtitleItem{kind: "movie", id: movieID, title: movieTitle, tmdbID: tmdbID, filePath: result.DestPath}
	subtitleNote := s.importSubtitleNote(movieSub)
	s.autoSubtitlesFor(movieSub)
	if err := s.QueueRepo.SetStatus(queueID, queue.StatusCompleted, ""); err != nil {
		return fail(err)
	}
	s.cleanupWorkDir(incompleteDir, protocol)
	// Earlier failed tries for this movie are settled now.
	_, _ = s.QueueRepo.ClearFailedForMovie(movieID, queueID)
	importedMessage := fmt.Sprintf("%s imported to %s", movieTitle, result.DestPath) + s.copiedFromTempNote(videoFile, result)
	_ = s.QueueRepo.LogActivity(movieID, "imported", importedMessage)
	if note := renamedNote(movieTitle, nameFrom, 1); note != "" {
		_ = s.QueueRepo.LogActivity(movieID, "renamed", note)
	}
	importedItem := s.movieItemByID(movieID, movieTitle, movieYear, tmdbID)
	importedItem.Quality, importedItem.SizeBytes, importedItem.Path = string(tier), fileSize(result.DestPath), result.DestPath
	if m, err := s.MovieRepo.Get(movieID); err == nil {
		s.movieReleaseDetails(&importedItem, m, releaseTitle, downloadURL)
	}
	importedItem.Subtitles = subtitleNote
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
	incompleteDir := s.workDirFor(queueID)
	// Usenet releases go to an external downloader (SABnzbd, NZBGet) when the
	// person chose that.
	usenetViaClient := protocol != indexers.ProtocolTorrent && s.usenetClientOn()
	if (protocol == indexers.ProtocolTorrent && s.downloadClientOn()) || usenetViaClient {
		// An external client's folder only holds links to its files: it stays in
		// the downloads folder, never in the temporary download folder.
		if _, started := s.existingWorkDir(queueID); !started {
			incompleteDir = s.defaultWorkDirFor(queueID)
		}
	}
	if protocol == indexers.ProtocolTorrent && !s.downloadClientOn() && !s.debridOn() {
		// The built-in client files a torrent under its category's folder.
		incompleteDir = s.assignTorrentDir(queueID)
	}
	if err := s.checkTempReady(incompleteDir); err != nil {
		return "", err
	}

	// The same release for the same title (by name and size) is not fetched a
	// second time, when that is switched on. The NZB itself is compared once it
	// has been read (downloadUsenet).
	if err := s.checkDuplicate(queueID, ""); err != nil {
		return "", err
	}

	var (
		downloadErr error
		missing     int
		viaClient   bool // the files stay in an external client's folder
	)
	// A retry of a failed Usenet download starts from the files it already
	// has when they are still there and pass PAR2 (see reuseDownloaded).
	reused := s.reuseDownloaded(queueID, incompleteDir)
	if !reused && protocol != indexers.ProtocolTorrent && keptResume(incompleteDir) {
		s.queueEvent(queueID, "retried", queue.LevelInfo, "Carrying on from the part already downloaded")
	}
	switch {
	case reused:
	case protocol == indexers.ProtocolTorrent && s.downloadClientOn():
		viaClient = true
		downloadErr = s.downloadViaClient(ctx, queueID, downloadURL, incompleteDir)
	case protocol == indexers.ProtocolTorrent && s.debridOn():
		downloadErr = s.downloadDebrid(ctx, queueID, downloadURL, incompleteDir)
	case protocol == indexers.ProtocolTorrent:
		// The built-in engines keep their own cap, whatever "Downloads at the
		// same time" is set to (builtin_slots.go).
		if release, err := s.torrentSlots.acquire(ctx, builtinTorrentLimit); err != nil {
			downloadErr = err
		} else {
			downloadErr = s.downloadTorrent(ctx, queueID, downloadURL, incompleteDir)
			release()
		}
	case usenetViaClient:
		viaClient = true
		downloadErr = s.downloadViaUsenetClient(ctx, queueID, downloadURL, incompleteDir)
	default:
		if release, err := s.usenetSlots.acquire(ctx, s.builtinUsenetLimit); err != nil {
			downloadErr = err
		} else {
			missing, downloadErr = s.downloadUsenet(ctx, queueID, downloadURL, incompleteDir)
			release()
			if ctx.Err() == nil {
				if downloadErr == nil {
					s.noteClientCheck(watchBuiltinUsenet, builtinUsenetLabel, nil)
				} else if builtinUsenetUnreachable(downloadErr) {
					s.noteClientCheck(watchBuiltinUsenet, builtinUsenetLabel, downloadErr)
				}
			}
		}
	}
	if downloadErr != nil {
		// A release held back for what it contains, or as a duplicate, did not
		// "fail": say what it is without the usual lead-in.
		var unsafe *download.UnsafeFilesError
		if errors.As(downloadErr, &unsafe) || asDuplicate(downloadErr) != nil {
			return "", downloadErr
		}
		return "", fmt.Errorf("the download failed: %w", downloadErr)
	}
	if !reused && protocol != indexers.ProtocolTorrent && !viaClient {
		markDownloaded(incompleteDir, missing)
	}

	if err := ctx.Err(); err != nil {
		return "", err // paused or stopped: leave the files as they are
	}
	if err := s.QueueRepo.SetStatus(queueID, queue.StatusImporting, ""); err != nil {
		return "", err
	}
	// A download client's files are never repaired here: that would write to
	// files the client may still be seeding.
	if !viaClient {
		if err := s.verifyStep(queueID, incompleteDir, missing); err != nil {
			return "", repairFailure(err)
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	// Archives are unpacked into the download's own folder (for a download
	// client, next to the links to its files, never into the client's folder).
	if err := s.unpackStep(ctx, queueID, incompleteDir); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err() // paused or stopped part-way: the unpack starts over next time
		}
		return "", unpackFailure(err)
	}
	// What came out of the archives (or came as it is) is looked at once more
	// before anything is imported: a release with random file names only shows
	// a program once it is unpacked.
	if err := s.checkUnpackedSafe(incompleteDir); err != nil {
		return "", err
	}
	return incompleteDir, nil
}

// downloadUsenet downloads an NZB with the built-in downloader from every
// enabled Usenet server, primary first. It returns how many articles no
// server had: those are left for PAR2 repair rather than failing outright.
func (s *Server) downloadUsenet(ctx context.Context, queueID int64, nzbURL, incompleteDir string) (int, error) {
	nzb, _, err := s.fetchCheckedNZB(ctx, queueID, nzbURL)
	if err != nil {
		return 0, err
	}

	servers, err := s.ClientRepo.List()
	if err != nil {
		return 0, fmt.Errorf("couldn't load your Usenet servers: %w", err)
	}
	if len(servers) == 0 {
		if all, err := s.ClientRepo.ListAll(); err == nil {
			for _, sc := range all {
				if sc.Enabled && sc.SecretUnreadable {
					return 0, fmt.Errorf("Mediarium can't read the saved password of your Usenet server, so nothing can be downloaded. Enter the password again in Settings > Downloading > Usenet.")
				}
			}
		}
		return 0, fmt.Errorf("no Usenet server is set up. Add your provider's server in Settings > Downloading > Usenet first")
	}
	configs := make([]download.ClientConfig, len(servers))
	for i, sc := range servers {
		configs[i] = sc.Config
	}

	// A try that carries on from what it has already saved is not checked
	// again: those articles are known to exist.
	if s.precheckEnabled() && !keptResume(incompleteDir) {
		if err := s.precheckArticles(ctx, queueID, configs, nzb); err != nil {
			return 0, err
		}
	}

	progress := newProgressSaver(func(pct float64) error { return s.QueueRepo.SetProgress(queueID, pct) })
	res, err := download.DownloadFromServers(ctx, configs, nzb, incompleteDir, progress.Report)
	progress.Flush()
	if err != nil {
		return 0, err
	}
	s.keepArchivePassword(queueID, nzb, incompleteDir)
	return res.MissingSegments, nil
}

// notAnNZB returns an error when b is clearly not an NZB file: a web page, or
// an XML reply whose root is not <nzb> (a Newznab <error> such as "Request
// limit reached"). It returns nil for anything that starts like an NZB, broken
// or not: ParseNZB judges those. downloadURL is only used to keep its key out
// of the message.
func notAnNZB(b []byte, downloadURL string) error {
	head := bytes.TrimSpace(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")))
	lower := strings.ToLower(string(head[:min(len(head), 512)]))
	if strings.HasPrefix(lower, "<!doctype html") || strings.HasPrefix(lower, "<html") {
		return &nzbLinkError{"the indexer sent a web page instead of the NZB file. It may be down for maintenance, or want you to sign in again"}
	}
	dec := xml.NewDecoder(bytes.NewReader(head))
	dec.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }
	for {
		tok, err := dec.Token()
		if err != nil {
			if len(head) == 0 || head[0] != '<' {
				return &nzbLinkError{"the indexer didn't send an NZB file"}
			}
			return nil // XML that breaks later: ParseNZB says what is wrong
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch strings.ToLower(start.Name.Local) {
		case "nzb":
			return nil
		case "error":
			description := ""
			for _, a := range start.Attr {
				if a.Name.Local == "description" {
					description = hideURLValues(strings.TrimSpace(a.Value), downloadURL)
				}
			}
			if description == "" {
				return &nzbLinkError{"the indexer refused to send the NZB file"}
			}
			if len(description) > 200 {
				description = description[:200]
			}
			return &nzbLinkError{fmt.Sprintf("the indexer refused to send the NZB file: %s", description)}
		default:
			return &nzbLinkError{"the indexer didn't send an NZB file"}
		}
	}
}

// hideURLValues removes the query values of rawURL (the indexer key among
// them) from text an indexer wrote.
func hideURLValues(text, rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return text
	}
	for _, values := range u.Query() {
		for _, v := range values {
			if len(v) >= 6 {
				text = strings.ReplaceAll(text, v, "...")
			}
		}
	}
	return text
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
	// A .torrent file lists its files: no programs or scripts among them.
	if torrentFilePath != "" {
		if err := s.checkTorrentFileSafe(torrentFilePath); err != nil {
			return err
		}
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
	// A VPN tunnel that is replaced or closed would leave this torrent dialling
	// through a dead connection until it stalls: stop it instead.
	// The other way round, a torrent that started without a VPN must not go on
	// sending from the home address once a VPN is connected, or once the kill
	// switch (or a VPN that is switched on but down) says torrents need one.
	var tunnelGone atomic.Bool
	var still func() bool
	if vt, ok := tunnel.(*vpn.Tunnel); ok && vt != nil {
		still = func() bool { return s.VPNManager.Tunnel() == vt }
	} else if tunnel == nil {
		still = s.directTorrentAllowed
	}
	if still != nil {
		go watchTunnel(ctx, tunnelCheck, still, func() {
			tunnelGone.Store(true)
			slog.Warn("torrent: the VPN connection changed, stopping the torrent", "queueId", queueID)
			s.torrents.stop(job)
		})
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
		if tunnelGone.Load() {
			return errVPNChanged
		}
		if errors.Is(err, torrentclient.ErrNoMetadata) && ctx.Err() == nil {
			// A dead magnet: blocklist it and try another release. The message
			// says what was tried and how the traffic was routed.
			return badRelease(s.explainTorrentFailure(err, tunnel))
		}
		return fmt.Errorf("couldn't add the torrent: %w", err)
	}

	// The choices made for this torrent: files left out, order, speed limits.
	ctl := torrentclient.NewControl(t)
	s.setUpDownloadControl(queueID, ctl)
	if !s.torrents.attachControl(job, ctl) {
		s.torrents.stop(job)
		return context.Canceled
	}
	go ctl.Run(ctx)

	progress := newProgressSaver(func(pct float64) error { return s.QueueRepo.SetProgress(queueID, pct) })
	err = ctl.Download(ctx, s.torrentStall(false), progress.Report)
	progress.Flush()
	if err != nil {
		s.torrents.stop(job)
		if !s.torrentsEnabled() {
			return errTorrentsDisabled
		}
		if tunnelGone.Load() {
			return errVPNChanged
		}
		if errors.Is(err, torrentclient.ErrStalled) {
			// No one is sharing it: blocklist it and try another release.
			return badRelease(s.explainTorrentFailure(err, tunnel))
		}
		return err
	}
	// What was left out stays out of the import (see writeSkipped).
	writeSkipped(incompleteDir, ctl.Skipped())

	goal := s.torrentSeedGoal(queueID)
	size := ctl.WantedBytes()
	go func() {
		met := torrentclient.SeedUntilGoalOf(ctx, t, size, time.Now(), goal, seedCheckInterval)
		s.torrents.stop(job)
		if met {
			s.seedingGoalMet(queueID, incompleteDir)
		}
	}()
	return nil
}

// tunnelCheck is how often a running torrent checks that the VPN tunnel it was
// started on is still the one in use. A variable so tests can shorten it.
var tunnelCheck = 30 * time.Second

// errVPNChanged fails a torrent whose VPN connection was replaced or closed
// while it ran. Nothing is wrong with the release, so it is not blocklisted.
var errVPNChanged = errors.New("the VPN connection changed while this torrent was downloading. Press Retry to carry on through the current connection")

// watchTunnel calls gone once still has reported false on two checks in a row
// (a VPN that is reconnecting gets one check to come back), then returns. It
// returns without calling gone when ctx ends.
func watchTunnel(ctx context.Context, every time.Duration, still func() bool, gone func()) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	misses := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if still() {
				misses = 0
				continue
			}
			if misses++; misses >= 2 {
				gone()
				return
			}
		}
	}
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

// torrentSeedRatioLimit is the global seed ratio limit: torrentopts.Unlimited
// (negative) when empty, 0 for "do not seed".
func (s *Server) torrentSeedRatioLimit() float64 {
	v, _ := s.Settings.Get(settings.KeyTorrentSeedRatioLimit)
	return torrentopts.ParseLimit(v)
}

// torrentSeedHoursLimit is the global seed time limit in hours, read the same
// way.
func (s *Server) torrentSeedHoursLimit() float64 {
	v, _ := s.Settings.Get(settings.KeyTorrentSeedTimeLimitH)
	return torrentopts.ParseLimit(v)
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
		return nil, fmt.Errorf("torrent downloads are waiting for your VPN (%s). %s Check Settings > Downloading > VPN and proxy, or press Disconnect to download without it", st.Label, reason)
	}
	if d := netproxy.For(netproxy.Torrents); d != nil {
		// No VPN, but a proxy for torrents: peers and trackers go through
		// it, with the same rules as the tunnel (no direct connections, no
		// DHT or UDP).
		return d, nil
	}
	if s.vpnRequiredForTorrents() {
		// Fail closed: the setting says torrent traffic must go through the
		// VPN and none is connected, so the grab does not proceed.
		return nil, fmt.Errorf("torrent downloads need a VPN or a proxy and neither is set up. Connect a VPN in Settings > Downloading > VPN and proxy, or set a proxy for torrents")
	}
	return nil, nil
}

// directTorrentAllowed reports whether a torrent may still use the direct
// connection: no VPN is connected, and torrents are not waiting for one (the
// kill switch is off and no switched-on VPN is down).
func (s *Server) directTorrentAllowed() bool {
	_, err := s.torrentTunnel()
	return err == nil && s.VPNManager.Tunnel() == nil
}

func (s *Server) vpnRequiredForTorrents() bool {
	v, _ := s.Settings.GetBool(settings.KeyVPNRequireForTorrents)
	return v
}

// importConflictPolicy reads the configured naming-collision policy for a
// movie import. See conflictPolicy for the shared logic.
func (s *Server) importConflictPolicy(movieID int64, incoming incomingRelease) (organizer.ConflictPolicy, func() bool) {
	upgrades := false
	profile := quality.Profile{}
	if m, err := s.MovieRepo.Get(movieID); err == nil {
		upgrades = !m.NoUpgrade
		if ps, err := s.loadProfiles(); err == nil {
			profile = s.profileForMovie(ps, m)
		}
	}
	return s.conflictPolicy(incoming, upgrades, profile, func() (currentFile, bool) {
		current, err := s.MovieRepo.Get(movieID)
		if err != nil {
			return currentFile{}, false
		}
		t, ok := existingTier(current.Quality, current.ReleaseTitle, current.FilePath)
		return currentFile{tier: t, release: current.ReleaseTitle}, ok
	})
}

// incomingRelease is the release an import brings: its name and the quality it
// was classified as.
type incomingRelease struct {
	title string
	tier  quality.Tier
}

// currentFile is the file a title has now, as far as a new release is compared
// with it: its quality and the release it came from ("" when unknown).
type currentFile struct {
	tier    quality.Tier
	release string
}

// importBetter is the one definition of "a better version" the import uses, and
// the release lists use to say what an automatic grab would take: a higher
// quality tier, or the same quality (or better) with a higher custom-format
// score while the file is below the profile's format cutoff (what an
// automatic upgrade search already treats as an upgrade). A release the search
// picks as an upgrade is therefore never turned away at the import.
func importBetter(profile quality.Profile, in incomingRelease, cur currentFile) bool {
	if profile.TierRank(in.tier) > profile.TierRank(cur.tier) {
		return true
	}
	return profile.IsUpgradeOverRel(cur.tier, cur.release, in.title, parser.Parse(in.title))
}

// conflictPolicy maps the configured policy setting to organizer's enum
// and, for "overwrite if better", builds the isBetter callback by
// comparing the incoming release against whatever current reports for the
// existing file (Unknown-ranked when the item predates tier tracking,
// which correctly counts as an upgrade rather than erroring). If the
// current tier can't be determined it fails closed — never overwrites.
//
// upgrades says the title is allowed to be upgraded (it is not set to leave
// its file alone). With the default "skip" policy such a title still takes a
// better release in place of its file: otherwise "Look for better versions"
// would download the better file and then refuse to import it.
func (s *Server) conflictPolicy(incoming incomingRelease, upgrades bool, profile quality.Profile, current func() (currentFile, bool)) (organizer.ConflictPolicy, func() bool) {
	v, _ := s.Settings.Get(settings.KeyImportConflictPolicy)
	ifBetter := func() (organizer.ConflictPolicy, func() bool) {
		isBetter := func() bool {
			cur, ok := current()
			if !ok {
				return false
			}
			return importBetter(profile, incoming, cur)
		}
		return organizer.ConflictOverwriteIfBetter, isBetter
	}
	switch v {
	case "overwrite":
		return organizer.ConflictOverwrite, nil
	case "overwrite_if_better":
		return ifBetter()
	case "ask":
		return organizer.ConflictAsk, nil
	default:
		if upgrades {
			return ifBetter()
		}
		return organizer.ConflictSkip, nil
	}
}

// resolveConflict finishes a queue item parked in StatusConflict (the
// "always ask" policy) — overwrite replaces the existing file
// with the parked download, skip leaves the existing file untouched
// (nothing new was imported, so the movie goes back to missing, matching
// how any other failed grab is handled).
func (s *Server) resolveConflict(queueID int64, overwrite bool) error {
	// One decision at a time: two clicks (keep and replace, or replace twice)
	// must not both get past the status check below.
	queueControlMu.Lock()
	defer queueControlMu.Unlock()
	item, err := s.QueueRepo.Get(queueID)
	if err != nil {
		return err
	}
	if item.Status != queue.StatusConflict {
		return fmt.Errorf("download %d isn't waiting for a decision (its status is %s)", queueID, item.Status)
	}

	if !overwrite {
		if err := s.QueueRepo.SetStatus(queueID, queue.StatusCompleted, keptExistingMessage); err != nil {
			return err
		}
		_ = s.QueueRepo.LogActivity(item.MovieID, "kept", fmt.Sprintf("You chose to keep the file at %s, so %s was not imported.", item.DestPath, item.ReleaseTitle))
		// The download is not wanted: its working folder goes (a torrent
		// still seeding keeps it until its seeding goal is met). Nothing was
		// imported, so a torrent in an external client is left there.
		data := s.discardWorkDirReport(s.workDirFor(queueID), indexers.Protocol(item.Protocol))
		s.recordKept(queueID, item.DestPath, data)
		s.logKeptData(item.MovieID, 0, data, item.ReleaseTitle)
		// The movie keeps the file it has: downloaded when that is still
		// there, missing otherwise.
		m, err := s.MovieRepo.Get(item.MovieID)
		if err != nil {
			return nil // removed from the library meanwhile
		}
		return s.MovieRepo.SetStatus(item.MovieID, restoredMovieStatus(m), "", "")
	}

	if owner := otherFileOwner(s.trackedFileOwners(), item.DestPath, item.MovieID, nil); owner != "" {
		return errFileOfAnother(owner, item.DestPath)
	}
	var earlierFile, earlierRoot, earlierTitle string
	if m, err := s.MovieRepo.Get(item.MovieID); err == nil {
		earlierFile, earlierTitle = m.FilePath, m.Title
		earlierRoot = s.movieRootFor(m)
		if m.CopyOf != 0 {
			earlierRoot = s.movieCopiesRoot()
		}
	}
	result, err := s.importDownloaded(item.SourcePath, item.DestPath, organizer.ConflictOverwrite, nil)
	if err != nil {
		return fmt.Errorf("couldn't replace the existing file: %w", err)
	}
	tier := quality.Classify(parser.Parse(item.ReleaseTitle))
	if err := s.MovieRepo.SetStatus(item.MovieID, library.StatusDownloaded, string(tier), result.DestPath); err != nil {
		return err
	}
	// The earlier file had another name than the new one: it goes now that
	// the new one is in the library.
	s.removeReplacedFile(earlierRoot, earlierFile, result.DestPath, item.MovieID, 0, earlierTitle)
	s.afterMovieImported(item.MovieID, item.ReleaseTitle)
	if err := s.QueueRepo.SetStatus(queueID, queue.StatusCompleted, ""); err != nil {
		return err
	}
	s.cleanupWorkDir(s.workDirFor(queueID), indexers.Protocol(item.Protocol))
	message := fmt.Sprintf("You chose to replace the existing file. Imported to %s.", result.DestPath)
	_ = s.QueueRepo.LogActivity(item.MovieID, "imported", message)
	replacedItem := notify.Item{Media: "movie", Title: item.ReleaseTitle}
	if m, err := s.MovieRepo.Get(item.MovieID); err == nil {
		replacedItem = s.localizedMovieItem(m)
	}
	replacedItem.Quality, replacedItem.SizeBytes, replacedItem.Path = string(tier), fileSize(result.DestPath), result.DestPath
	s.notifyItem("imported", replacedItem)
	s.movieImported(result.DestPath)
	return nil
}

// keptExistingMessage is what a finished download says when the file already
// in the library was kept and the new one was not imported, because "If a
// file already exists" (or a person answering it) said so.
const keptExistingMessage = "Kept the file you already have."

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
	case isUploadRef(downloadURL):
		data, err := s.readUpload(downloadURL)
		if err != nil {
			return "", "", err
		}
		path, err := writeTorrentFile(data, destDir)
		return "", path, err
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
		magnet, path, err := downloadToTempFile(ctx, downloadURL, destDir)
		return magnet, path, err
	}
}

// fetchRelease downloads a release file (an NZB), resolving links from
// definition-based indexers through their session.
func (s *Server) fetchRelease(ctx context.Context, downloadURL string) ([]byte, error) {
	if isUploadRef(downloadURL) {
		return s.readUpload(downloadURL)
	}
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
// mirroring the anacrolix/torrent API) can load it. A download that redirects
// to a magnet link returns the magnet instead.
func downloadToTempFile(ctx context.Context, url, destDir string) (string, string, error) {
	data, magnet, err := fetchTorrentURL(ctx, url)
	if err != nil {
		return "", "", err
	}
	if magnet != "" {
		return magnet, "", nil
	}
	path, err := writeTorrentFile(data, destDir)
	return "", path, err
}

func percent(done, total int64) float64 {
	if total <= 0 {
		return 0
	}
	return float64(done) / float64(total) * 100
}

func fetchURL(ctx context.Context, url string) ([]byte, error) {
	return fetchURLLimit(ctx, url, 256<<20) // an NZB or torrent file is a few MB at most
}

// fetchURLLimit is fetchURL for a file of at most maxBytes.
func fetchURLLimit(ctx context.Context, url string, maxBytes int64) ([]byte, error) {
	return fetchURLLimitWithClient(ctx, netproxy.Client(30*time.Second, netproxy.Indexers), url, maxBytes)
}

// errMagnetRedirect carries a redirect to a magnet link, which net/http can't
// follow as a request.
type errMagnetRedirect struct{ uri string }

func (e errMagnetRedirect) Error() string { return "redirect to magnet link" }

// fetchTorrentURL is fetchURL for a .torrent file, taking the target of a
// redirect to a magnet link as the torrent's source instead of failing on the
// magnet scheme. The existing redirect check still runs first, so its limit,
// address checks and credential stripping apply to the whole chain.
func fetchTorrentURL(ctx context.Context, url string) (data []byte, magnet string, err error) {
	client := netproxy.Client(30*time.Second, netproxy.Indexers)
	follow := client.CheckRedirect
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if follow != nil {
			if err := follow(next, via); err != nil {
				return err
			}
		}
		if strings.EqualFold(next.URL.Scheme, "magnet") {
			return errMagnetRedirect{uri: next.URL.String()}
		}
		return nil
	}
	data, err = fetchURLLimitWithClient(ctx, client, url, 256<<20)
	if err != nil {
		var redirect errMagnetRedirect
		if errors.As(err, &redirect) {
			return nil, redirect.uri, nil
		}
		return nil, "", err
	}
	return data, "", nil
}

func fetchURLLimitWithClient(ctx context.Context, client *http.Client, url string, maxBytes int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", indexers.UserAgent)
	// Through the address check (no cloud metadata or link-local targets,
	// even via a redirect), and the proxy when one is set for indexers;
	// errors and messages never carry the link's query string, which holds
	// the indexer key.
	resp, err := client.Do(req)
	if err != nil {
		return nil, netguard.CleanError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the server answered with status %d for %s", resp.StatusCode, netguard.RedactURL(url))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("the file at %s is larger than %d MB", netguard.RedactURL(url), maxBytes>>20)
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
	_, _, err := unpackArchivesWatched(context.Background(), dir, nil)
	return err
}

// unpackArchivesWatched is unpackArchives that stops when ctx ends and tells w
// how far it has come. It returns how many bytes of archives it unpacked and
// how long that took.
func unpackArchivesWatched(ctx context.Context, dir string, w *unpackWatch) (int64, time.Duration, error) {
	extractor := organizer.NewExtractor()
	extractor.Context = ctx
	extractor.Password = readArchivePassword(dir)
	// Archives inside a folder of the download count too (a torrent keeps its
	// files in a folder of its own); each is unpacked next to where it lies.
	archives, err := organizer.FindPrimaryArchivesDeep(dir)
	if err != nil {
		return 0, 0, err
	}
	var bytes int64
	var took time.Duration
	for i, a := range archives {
		if w != nil {
			w.begin(i+1, len(archives))
			extractor.Progress = w.report
		}
		if err := extractor.Extract(a, filepath.Dir(a)); err != nil {
			return bytes, took, err
		}
		if w != nil {
			n, d := w.end()
			bytes, took = bytes+n, took+d
		}
	}
	return bytes, took, nil
}

// archivePasswordFile holds the password of a download's archives, inside the
// download's own folder, so it goes away with the folder.
const archivePasswordFile = ".mediarium-password"

// keepArchivePassword saves the archive password the NZB carries (or the
// {{password}} written after the release name) next to the download, for the
// unpack step. It is never logged.
func (s *Server) keepArchivePassword(queueID int64, nzb *download.NZB, dir string) {
	pw := nzb.Password
	if pw == "" {
		if item, err := s.QueueRepo.Get(queueID); err == nil {
			pw = download.PasswordFromTitle(item.ReleaseTitle)
		}
	}
	if pw == "" {
		return
	}
	if err := os.WriteFile(filepath.Join(dir, archivePasswordFile), []byte(pw), 0o600); err != nil {
		slog.Warn("api: save the archive password", "queueId", queueID, "err", err)
	}
}

// readArchivePassword returns the password saved by keepArchivePassword, or "".
func readArchivePassword(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, archivePasswordFile))
	if err != nil || len(b) > 1024 {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// unpackFailure classifies an unpack error: a full disk or a missing 7z
// tool is a problem with our setup and must never blocklist a good release;
// everything else (corrupt, password protected, unsafe archive) is the
// release's fault.
func unpackFailure(err error) error {
	wrapped := fmt.Errorf("couldn't unpack the download: %w", err)
	if errors.Is(err, organizer.ErrInsufficientSpace) || errors.Is(err, organizer.ErrSevenZipMissing) || isLocalWriteProblem(err) {
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

// isLocalWriteProblem reports whether err is this machine failing to write
// (a full disk or quota, a folder that is read-only or not ours, a disk error):
// nothing to do with the release, so it must not be blocklisted.
func isLocalWriteProblem(err error) bool {
	return isDiskFull(err) || errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.EROFS) || errors.Is(err, syscall.EIO)
}

// repairFailure classifies an error from the PAR2 step. PAR2 is an outside
// program, so its trouble comes back as text: a full disk, a folder it may not
// write to, a file list too long for the system, or the time limit. Those are
// this machine's problem and the release is kept; anything else (too much is
// missing, nothing to repair with) is the release's fault.
func repairFailure(err error) error {
	wrapped := fmt.Errorf("repairing the download failed: %w", err)
	text := strings.ToLower(err.Error())
	if isLocalWriteProblem(err) || errors.Is(err, syscall.E2BIG) || errors.Is(err, context.DeadlineExceeded) ||
		strings.Contains(text, "no space left") || strings.Contains(text, "permission denied") ||
		strings.Contains(text, "read-only file system") || strings.Contains(text, "argument list too long") {
		return wrapped
	}
	return badRelease(wrapped)
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
	dest, _, err := s.buildDestPathFor(root, movieTitle, year, tmdbID, releaseTitle, videoFile, false)
	return dest, err
}

// buildDestPathFor is buildDestPath that also says where the file's name came
// from. releaseMayName is true when releaseTitle is the name of the download
// itself (not a guess made from the file): with "Keep the original file
// names" on, a file named at random then takes the release's name.
func (s *Server) buildDestPathFor(root, movieTitle string, year, tmdbID int, releaseTitle, videoFile string, releaseMayName bool) (string, nameSource, error) {
	moviesPath := root
	if moviesPath == "" {
		moviesPath = s.moviesRoot()
	}
	if moviesPath == "" {
		return "", namedByPreset, fmt.Errorf("the movies folder isn't set. Choose one in Settings > Library > Folders")
	}

	preset, err := s.Settings.Get(settings.KeyNamingPreset)
	if err != nil {
		return "", namedByPreset, err
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
		OriginalTitle: releaseTitle, OriginalFilename: originalStem(videoFile),
	}

	sanitizeMode, replacement := s.illegalCharSettings()
	// Folder and file use the title in the info language. Indexers are still
	// searched for the English title the library stores.
	ctx.MovieTitle = s.folderTitle(tmdbID, "movie", movieTitle)
	folder := organizer.Sanitize(organizer.Render(s.movieFolderFormat(), ctx), sanitizeMode, replacement)
	name, src := s.keptName(videoFile, releaseTitle, releaseMayName, sanitizeMode, replacement)
	if name != "" {
		return filepath.Join(moviesPath, folder, name), src, nil
	}
	filename := organizer.Sanitize(organizer.Render(format, ctx), sanitizeMode, replacement) + filepath.Ext(videoFile)
	return filepath.Join(moviesPath, folder, filename), src, nil
}

// keepOriginalNames reports whether "Keep the original file names" is on:
// imported videos keep the name they came with, inside the usual folders.
func (s *Server) keepOriginalNames() bool {
	on, _ := s.Settings.GetBool(settings.KeyKeepOriginalNames)
	return on
}

// nameSource says where a file's final name came from.
type nameSource int

const (
	namedByPreset        nameSource = iota // "Keep the original file names" is off
	namedByFile                            // the file's own name was kept
	namedByRelease                         // the file's name was random: the release's name was used
	namedByPresetInstead                   // the file's name was random and so was the release's (or it may not stand in): the naming setting
)

// keptName is the name videoFile keeps when "Keep the original file names"
// is on. The name is empty when the file is to be named by the naming setting:
// the setting is off, or the file's name is random letters (an obfuscated
// post) and the release's name can't stand in for it. releaseMayName is true
// for a movie or an episode that came on its own, where the release's name
// says what the file is; for a season pack or a file holding several
// episodes it is false, as one release name would fit many files.
func (s *Server) keptName(videoFile, releaseTitle string, releaseMayName bool, mode organizer.SanitizeMode, replacement string) (string, nameSource) {
	if !s.keepOriginalNames() {
		return "", namedByPreset
	}
	if name, ok := organizer.OriginalName(videoFile, mode, replacement); ok {
		return name, namedByFile
	}
	if releaseMayName {
		if name, ok := organizer.ReleaseFileName(releaseTitle, videoFile, mode, replacement); ok {
			return name, namedByRelease
		}
	}
	return "", namedByPresetInstead
}

// renamedLine formats one of renamedNote's sentences. It is a function of its
// own so the sentences are found for translation (tools/msgextract lists its
// first argument).
func renamedLine(format string, args ...any) string { return fmt.Sprintf(format, args...) }

// renamedNote is the activity line that says files were not saved under their
// own names, so it can be understood without reading the code. It is empty
// when there is nothing to say.
func renamedNote(title string, src nameSource, files int) string {
	switch {
	case files <= 0:
		return ""
	case src == namedByRelease && files == 1:
		return renamedLine("%s: the file inside was named at random, so it was saved under the release name.", title)
	case src == namedByRelease:
		return renamedLine("%s: some files inside were named at random, so they were saved under the release name.", title)
	case src == namedByPresetInstead && files == 1:
		return renamedLine("%s: the file inside was named at random, so it was saved under your naming preset.", title)
	case src == namedByPresetInstead:
		return renamedLine("%s: some files inside were named at random, so they were saved under your naming preset.", title)
	}
	return ""
}

// originalStem is a file's own name without its folder and extension, for
// the {Original Filename} token.
func originalStem(file string) string {
	base := filepath.Base(file)
	return strings.TrimSuffix(base, filepath.Ext(base))
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
