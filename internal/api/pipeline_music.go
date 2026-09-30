package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/rdborg/mediarium/internal/blocklist"
	"github.com/rdborg/mediarium/internal/cleanup"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/music"
	"github.com/rdborg/mediarium/internal/notify"
	"github.com/rdborg/mediarium/internal/organizer"
	"github.com/rdborg/mediarium/internal/plainerror"
	"github.com/rdborg/mediarium/internal/queue"
	"github.com/rdborg/mediarium/internal/settings"
)

// The album download pipeline: the same queue, downloaders, PAR2 repair and
// unpacking as movies and shows (prepareDownload), then the album's own
// import: find the audio files, match them to the album's tracklist, name
// and place them in <music>/<Artist>/<Album> (<Year>)/, keep the cover art,
// and mark the album downloaded.

// musicTagReader reads embedded audio tags while matching files. It only
// reads: nothing in a user's files is ever rewritten.
var musicTagReader music.TagReader = music.FileTagReader{}

// albumLabel is "Artist – Album" for messages.
func albumLabel(artist music.Artist, album music.Album) string {
	return artist.Name + " – " + album.Title
}

// grabAlbum puts a release for an album in the download line, where it starts
// when a place is free. One download per album: a grab by hand while another
// runs is refused with 409, an automatic one with errAlreadyGrabbed.
func (s *Server) grabAlbum(artist music.Artist, album music.Album, releaseTitle, downloadURL string, sizeBytes int64, protocol indexers.Protocol, kind grabKind) (int64, error) {
	if protocol == indexers.ProtocolTorrent && !s.torrentsEnabled() {
		return 0, errTorrentsDisabled
	}
	s.grabMu.Lock()
	running, err := s.albumDownloadInProgress(album.ID)
	if err != nil {
		s.grabMu.Unlock()
		return 0, err
	}
	if running != "" {
		s.grabMu.Unlock()
		if kind.refusesDuplicates() {
			return 0, errAlreadyGrabbed
		}
		return 0, alreadyDownloadingError{release: running}
	}
	current, err := s.MusicRepo.GetAlbum(album.ID)
	if err != nil {
		s.grabMu.Unlock()
		return 0, err
	}
	restore := music.StatusMissing
	if current.Status == music.StatusDownloaded {
		restore = music.StatusDownloaded // an upgrade that fails leaves the old files
	}
	if err := s.MusicRepo.SetAlbumStatus(album.ID, music.StatusDownloading); err != nil {
		s.grabMu.Unlock()
		return 0, err
	}
	queueID, err := s.QueueRepo.Enqueue(queue.Item{
		AlbumID: album.ID, ReleaseTitle: releaseTitle, NZBURL: downloadURL, SizeBytes: sizeBytes, Protocol: queue.Protocol(protocol), Priority: kind.priority(),
	})
	if err != nil {
		_ = s.MusicRepo.SetAlbumStatus(album.ID, restore)
		s.grabMu.Unlock()
		return 0, err
	}
	s.grabMu.Unlock()

	label := albumLabel(artist, album)
	grabbed := s.grabbedMessage(releaseTitle, label)
	_ = s.QueueRepo.LogItemEvent(queue.ItemEvent{AlbumID: album.ID, Kind: "grabbed", Message: grabbed})
	s.notifyItem("grabbed", notify.Item{
		Media: "album", Title: label, Year: album.Year(), Quality: string(music.ParseRelease(releaseTitle).Tier()), SizeBytes: sizeBytes,
		Release: releaseTitle, LinkPath: fmt.Sprintf("/music/artist/%d", artist.ID),
	})

	s.dispatch.Kick()
	return queueID, nil
}

// albumDownloadInProgress returns the release title of a download of the
// album that is still running, "" when there is none.
func (s *Server) albumDownloadInProgress(albumID int64) (string, error) {
	items, err := s.QueueRepo.List()
	if err != nil {
		return "", fmt.Errorf("check downloads in progress: %w", err)
	}
	for _, it := range items {
		if it.AlbumID == albumID && downloadOpen(it.Status) {
			return it.ReleaseTitle, nil
		}
	}
	return "", nil
}

// runMusicPipeline downloads, unpacks and imports one album release.
// restore is the status the album goes back to when it fails.
func (s *Server) runMusicPipeline(queueID, artistID, albumID int64, restore music.Status, releaseTitle, downloadURL string, protocol indexers.Protocol) error {
	ctx, run := s.pipelines.begin(queueID)
	defer s.pipelines.end(queueID, run)
	if s.pausedBeforeStart(queueID) {
		return errPaused
	}

	label := releaseTitle
	fail := func(stepErr error) error {
		if run.cancelled.Load() {
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
		noteDownloadFailure(queueID, label, fmt.Sprintf("/music/artist/%d", artistID), stepErr)
		blocklisted := isBadRelease(stepErr) && s.blocklistAlbumRelease(albumID, releaseTitle, protocol, stepErr)
		_ = s.MusicRepo.SetAlbumStatus(albumID, restore)
		message := fmt.Sprintf("%s: %s", label, plain)
		_ = s.QueueRepo.LogItemEvent(queue.ItemEvent{AlbumID: albumID, Kind: "failed", Level: queue.LevelError, Message: message})
		s.notifyItem("failed", notify.Item{Media: "album", Title: label, Reason: plain, Release: releaseTitle, LinkPath: fmt.Sprintf("/music/artist/%d", artistID)})
		if blocklisted {
			s.background(func() { s.retryAlbum(albumID) })
		}
		return stepErr
	}

	artist, err := s.MusicRepo.GetArtist(artistID)
	if err != nil {
		return fail(fmt.Errorf("couldn't look up the artist: %w", err))
	}
	album, err := s.MusicRepo.GetAlbum(albumID)
	if err != nil {
		return fail(fmt.Errorf("couldn't look up the album: %w", err))
	}
	label = albumLabel(artist, album)

	if err := s.QueueRepo.SetStatus(queueID, queue.StatusDownloading, ""); err != nil {
		return fail(err)
	}
	dir, err := s.prepareDownload(ctx, queueID, downloadURL, protocol)
	if err != nil {
		return fail(err)
	}
	if ctx.Err() != nil {
		return fail(ctx.Err()) // cancelled: never import for an artist that was removed
	}
	res, err := s.importAlbumFiles(ctx, artist, album, dir, releaseTitle)
	if err != nil {
		return fail(err)
	}
	if err := s.QueueRepo.SetStatus(queueID, queue.StatusCompleted, ""); err != nil {
		return fail(err)
	}
	s.cleanupWorkDir(dir, protocol)
	message := fmt.Sprintf("%s imported to %s: %s", label, res.folder, res.summary())
	_ = s.QueueRepo.LogItemEvent(queue.ItemEvent{AlbumID: albumID, Kind: "imported", Message: message})
	s.notifyItem("imported", notify.Item{
		Media: "album", Title: label, Year: album.Year(), Quality: string(res.tier), SizeBytes: fileSize(res.dests...),
		Path: res.folder, Release: releaseTitle, LinkPath: fmt.Sprintf("/music/artist/%d", artistID),
	})
	s.musicImported(res.dests...)
	return nil
}

// blocklistAlbumRelease blocklists a release that failed through its own
// fault and records it in the album's log (and the activity feed).
func (s *Server) blocklistAlbumRelease(albumID int64, releaseTitle string, protocol indexers.Protocol, cause error) bool {
	if err := s.Blocklist.Add(blocklist.Entry{ReleaseTitle: releaseTitle, Protocol: string(protocol), Reason: cause.Error()}); err != nil {
		slog.Warn("music: blocklist release", "release", releaseTitle, "err", err)
		return false
	}
	_ = s.QueueRepo.LogItemEvent(queue.ItemEvent{AlbumID: albumID, Kind: "blocklisted", Level: queue.LevelWarn,
		Message: "Blocklisted \"" + releaseTitle + "\": " + cause.Error()})
	return true
}

// albumImport is what one album import did.
type albumImport struct {
	folder    string
	dests     []string // the files placed in the library
	tier      music.Tier
	imported  int // tracks placed in the library
	skipped   int // tracks whose file was already there (conflict policy)
	unmatched int // audio files that fit no track
	missing   int // tracks the release did not have
}

func (a albumImport) summary() string {
	parts := []string{plural(a.imported, "track")}
	if a.skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d already there", a.skipped))
	}
	if a.missing > 0 {
		parts = append(parts, fmt.Sprintf("%d missing from the release", a.missing))
	}
	if a.unmatched > 0 {
		parts = append(parts, fmt.Sprintf("%s not imported", plural(a.unmatched, "extra file")))
	}
	return strings.Join(parts, ", ") + " (" + string(a.tier) + ")"
}

// ensureTracks returns an album's tracklist, fetching it from MusicBrainz
// (the canonical release of the release group) the first time.
func (s *Server) ensureTracks(ctx context.Context, album music.Album) ([]music.Track, error) {
	tracks, err := s.MusicRepo.ListTracks(album.ID)
	if err != nil || len(tracks) > 0 {
		return tracks, err
	}
	rel, err := s.MusicBrainz().CanonicalTracklist(ctx, album.MBID)
	if err != nil {
		return nil, fmt.Errorf("couldn't get the track list for %q from the music database: %w", album.Title, err)
	}
	list := rel.Tracklist()
	fresh := make([]music.Track, len(list))
	for i, t := range list {
		fresh[i] = music.Track{Disc: t.Disc, Position: t.Position, Title: t.Title, LengthMs: t.LengthMs}
	}
	if err := s.MusicRepo.ReplaceTracks(album.ID, fresh); err != nil {
		return nil, err
	}
	if err := s.MusicRepo.SetAlbumRelease(album.ID, rel.ID); err != nil {
		return nil, err
	}
	return s.MusicRepo.ListTracks(album.ID)
}

// importAlbumFiles places a finished download's audio files in the music
// library: matched to the tracklist, named "NN - Title.ext" (or
// "D-NN - Title.ext" on a several-disc album) in <Artist>/<Album> (<Year>)/,
// hardlinked when possible and copied otherwise, with the cover art beside
// them. A release whose files match no track is the release's fault.
func (s *Server) importAlbumFiles(ctx context.Context, artist music.Artist, album music.Album, dir, releaseTitle string) (albumImport, error) {
	files, err := music.FindAudioFiles(dir)
	if err != nil {
		return albumImport{}, err
	}
	if len(files) == 0 {
		return albumImport{}, badRelease(errors.New("no audio files found in the download"))
	}
	root := s.musicRoot()
	if root == "" {
		return albumImport{}, fmt.Errorf("the music folder isn't set. Choose one in Settings > Library > Folders and file names")
	}
	tracks, err := s.ensureTracks(ctx, album)
	if err != nil {
		return albumImport{}, err
	}
	if len(tracks) == 0 {
		return albumImport{}, fmt.Errorf("the music database lists no tracks for %q", album.Title)
	}

	infos := make([]music.FileInfo, len(files))
	for i, f := range files {
		infos[i] = music.IdentifyFile(f, musicTagReader)
	}
	slots := make([]music.TrackSlot, len(tracks))
	multiDisc := false
	for i, t := range tracks {
		slots[i] = music.TrackSlot{Disc: t.Disc, Position: t.Position, Title: t.Title}
		multiDisc = multiDisc || t.Disc > 1
	}
	matches, unmatched := music.MatchTracks(infos, slots)
	if len(matches) == 0 {
		return albumImport{}, badRelease(fmt.Errorf("none of the %d audio files could be matched to the %d tracks of %q", len(files), len(tracks), album.Title))
	}

	// The quality is what the files' headers say; only when they cannot be
	// read does the release name (then the file extension) decide.
	matched := make([]string, len(matches))
	for i, m := range matches {
		matched[i] = files[m.File]
	}
	tier := music.AlbumTier(matched)
	if tier == music.TierUnknown {
		tier = music.ParseRelease(releaseTitle).Tier()
	}
	if tier == music.TierUnknown {
		tier = music.TierForExtension(filepath.Ext(matched[0]))
	}
	clean := music.Sanitizer(s.musicSanitizer())
	folder := music.AlbumPath(root, artist.Name, album.Title, album.Year(), clean)
	policy, isBetter := s.musicConflictPolicy(album, tier)

	res := albumImport{folder: folder, tier: tier, unmatched: len(unmatched), missing: len(tracks) - len(matches)}
	for _, m := range matches {
		if ctx.Err() != nil {
			return albumImport{}, ctx.Err()
		}
		track := tracks[m.Slot]
		src := files[m.File]
		dest := filepath.Join(folder, music.TrackFileName(track.Disc, track.Position, track.Title, multiDisc, filepath.Ext(src), clean))
		out, err := organizer.Import(src, dest, policy, isBetter)
		if err != nil {
			return albumImport{}, fmt.Errorf("import %s: %w", filepath.Base(src), err)
		}
		if out.Skipped {
			res.skipped++
			if track.FilePath == "" {
				_ = s.MusicRepo.SetTrackFile(track.ID, dest) // the file there is this track's
			}
			continue
		}
		if err := s.MusicRepo.SetTrackFile(track.ID, dest); err != nil {
			return albumImport{}, err
		}
		if track.FilePath != "" && track.FilePath != dest {
			s.removeReplacedTrack(root, track.FilePath) // an upgrade in another format
		}
		res.imported++
		res.dests = append(res.dests, dest)
	}
	if res.imported == 0 {
		return albumImport{}, fmt.Errorf("the files of this release already exist in %s, so nothing was imported", folder)
	}

	art, err := music.FindArtwork(dir)
	if err != nil {
		slog.Warn("music: look for cover art", "dir", dir, "err", err)
	}
	for _, a := range art {
		if _, err := organizer.Import(a, filepath.Join(folder, strings.ToLower(filepath.Base(a))), organizer.ConflictSkip, nil); err != nil {
			slog.Warn("music: keep cover art", "file", a, "err", err)
		}
	}
	s.saveAlbumCover(ctx, album, folder)
	if err := s.MusicRepo.SetAlbumImported(album.ID, tier, folder); err != nil {
		return albumImport{}, err
	}
	return res, nil
}

// musicConflictPolicy decides what happens when a track's file name is
// already taken: a real upgrade of a downloaded album replaces the album's
// own files; otherwise the import conflict policy applies ("always ask"
// acts as "skip": there is no per-file review for albums).
func (s *Server) musicConflictPolicy(album music.Album, incoming music.Tier) (organizer.ConflictPolicy, func() bool) {
	current := albumCurrentTier(album)
	if current != "" && music.Rank(incoming) > music.Rank(current) {
		return organizer.ConflictOverwrite, nil
	}
	v, _ := s.Settings.Get(settings.KeyImportConflictPolicy)
	switch v {
	case "overwrite":
		return organizer.ConflictOverwrite, nil
	case "overwrite_if_better":
		return organizer.ConflictOverwriteIfBetter, func() bool { return current != "" && music.Rank(incoming) > music.Rank(current) }
	}
	return organizer.ConflictSkip, nil
}

// removeReplacedTrack deletes a track's old file after an upgrade put a new
// one (in another format) in its place. Only a file inside the music folder
// is ever removed.
func (s *Server) removeReplacedTrack(root, path string) {
	if _, err := cleanup.InsideRoot(root, path); err != nil {
		slog.Warn("music: old track file left in place", "path", path, "err", err)
		return
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("music: remove replaced track file", "path", path, "err", err)
	}
}
