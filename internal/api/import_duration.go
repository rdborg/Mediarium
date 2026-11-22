package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"path/filepath"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/mediainfo"
	"github.com/rdborg/mediarium/internal/settings"
)

// Is the video really complete? A fake, a sample or a cut-off release can
// pass every check on names and sizes and still hold one minute of video.
// Before a file is filed into the library, its length is read from its
// header (internal/mediainfo, never the picture) and compared with how long
// the movie or episode is meant to be (TMDB's runtime). A file that is
// far too short is turned away like any other bad release: the download
// fails with a plain reason, the release is blocklisted and the next one is
// tried. A file whose length can't be read is never turned away.

// incompleteReason is what the blocklist keeps as the reason.
const incompleteReason = "incomplete file"

// notVideoReason is the blocklist's reason for a release whose "video" is an
// archive that was never unpacked.
const notVideoReason = "not a video file"

// incompleteFileError is the failure of a video that is far shorter than it
// should be. It is wrapped as a bad release, so it blocklists the release
// and starts the search for another one.
type incompleteFileError struct{ msg string }

func (e *incompleteFileError) Error() string { return e.msg }

// notVideoError is the failure of a file named like a video that is really an
// archive (RAR, ZIP, 7z). Like a short file it is a bad release.
type notVideoError struct{ msg string }

func (e *notVideoError) Error() string { return e.msg }

// blocklistReason is the reason a release is blocklisted for after cause.
func blocklistReason(cause error) string {
	var inc *incompleteFileError
	if errors.As(cause, &inc) {
		return incompleteReason
	}
	var nv *notVideoError
	if errors.As(cause, &nv) {
		return notVideoReason
	}
	return cause.Error()
}

// notAVideo is the failure for a file that is an archive saved under a video's
// name, nil for anything else (a video, or a file that can't be read). No
// player can open such a file, so it is turned away whether or not the length
// check is on.
func notAVideo(path string) error {
	info, err := mediainfo.ReadFile(path)
	if err != nil || !mediainfo.IsArchive(info.Container) {
		return nil
	}
	return badRelease(&notVideoError{msg: fmt.Sprintf("The file %s is a %s archive, not a video: the release was never unpacked. This looks like a broken release.",
		filepath.Base(path), strings.ToUpper(info.Container))})
}

// checkDurationOn says whether the length check is on (it is, unless switched
// off in Settings > Library and files).
func (s *Server) checkDurationOn() bool {
	raw, _ := s.Settings.Get(settings.KeyImportCheckDuration)
	return raw != "0"
}

// lengthKept says whether a person chose to keep this release although its
// file is shorter than expected (Keep anyway on a release turned away for
// its length). Such a release skips the length check, nothing else.
func (s *Server) lengthKept(releaseTitle string) bool {
	return s.Blocklist != nil && s.Blocklist.KeptShort(releaseTitle)
}

// isLengthFailure says whether a download's failure text is the one of a file
// turned away for being far shorter than expected.
func isLengthFailure(msg string) bool {
	return strings.HasPrefix(msg, "The file is only ") && strings.HasSuffix(strings.TrimSpace(msg), "a fake or broken release.")
}

// lengthMinutes is a length in whole minutes for a sentence, at least 1.
func lengthMinutes(seconds float64) int {
	return max(1, int(math.Round(seconds/60)))
}

// incompleteFile builds the failure for a video of the given length.
// expected is the minutes it should have, 0 when not known.
func incompleteFile(kind mediainfo.Kind, seconds, expected float64) error {
	have := lengthMinutes(seconds)
	var msg string
	switch {
	case expected > 0 && kind == mediainfo.KindEpisode:
		msg = fmt.Sprintf("The file is only %d min long, the episode is about %d min. This looks like a fake or broken release.", have, int(math.Round(expected)))
	case expected > 0:
		msg = fmt.Sprintf("The file is only %d min long, the movie is about %d min. This looks like a fake or broken release.", have, int(math.Round(expected)))
	default:
		msg = fmt.Sprintf("The file is only %d min long. This looks like a fake or broken release.", have)
	}
	return badRelease(&incompleteFileError{msg: msg})
}

// videoSeconds reads how long the video at path is. ok is false when the file
// can't be read or does not say (a container this does not open, a damaged
// header, a disk that does not answer): then nothing is judged.
func videoSeconds(path string) (seconds float64, ok bool) {
	info, err := mediainfo.ReadFile(path)
	if err != nil || !(info.Duration > 0) {
		return 0, false
	}
	return info.Duration, true
}

// checkMovieLength is nil when the movie file is long enough (or nothing can
// be said), else the failure to give the download.
func (s *Server) checkMovieLength(ctx context.Context, tmdbID int, releaseTitle, path string) error {
	if err := notAVideo(path); err != nil {
		return err
	}
	if !s.checkDurationOn() || s.lengthKept(releaseTitle) {
		return nil
	}
	seconds, ok := videoSeconds(path)
	if !ok {
		return nil
	}
	expected := s.knownMovieMinutes(ctx, tmdbID)
	if !mediainfo.TooShort(seconds, expected, mediainfo.KindMovie) {
		return nil
	}
	return incompleteFile(mediainfo.KindMovie, seconds, expected)
}

// checkEpisodeLength is checkMovieLength for one episode file. The specials
// (season 0) are judged only by the lowest limit: they are often short.
func (s *Server) checkEpisodeLength(ctx context.Context, tmdbID, season int, releaseTitle, path string) error {
	if err := notAVideo(path); err != nil {
		return err
	}
	if !s.checkDurationOn() || s.lengthKept(releaseTitle) {
		return nil
	}
	seconds, ok := videoSeconds(path)
	if !ok {
		return nil
	}
	var expected float64
	if season > 0 {
		expected = s.knownEpisodeMinutes(ctx, tmdbID)
	}
	if !mediainfo.TooShort(seconds, expected, mediainfo.KindEpisode) {
		return nil
	}
	return incompleteFile(mediainfo.KindEpisode, seconds, expected)
}

// ---- the runtime TMDB gives ----

const (
	// runtimeRefreshAfter: a runtime TMDB gave is trusted this long.
	runtimeRefreshAfter = 90 * 24 * time.Hour
	// runtimeAskAgainAfter: when TMDB did not know, it is asked again after this.
	runtimeAskAgainAfter = 24 * time.Hour
)

// knownMovieMinutes is the movie's runtime in minutes, 0 when TMDB does not
// say (or can't be asked). Unlike movieRuntime it never makes up a typical
// length: an unknown runtime is judged only by the lowest limit.
func (s *Server) knownMovieMinutes(ctx context.Context, tmdbID int) float64 {
	return s.tmdbMinutes(ctx, library.RuntimeMovie, tmdbID)
}

// knownEpisodeMinutes is the length of a show's episodes. When TMDB lists
// several, the shortest is used, so a show with long and short episodes is
// judged by the short ones.
func (s *Server) knownEpisodeMinutes(ctx context.Context, tmdbID int) float64 {
	return s.tmdbMinutes(ctx, library.RuntimeTV, tmdbID)
}

// tmdbMinutes returns the saved runtime, asking TMDB when there is none or it
// is old. Only an answer from TMDB is saved; a failed request leaves what
// was there.
func (s *Server) tmdbMinutes(ctx context.Context, kind string, tmdbID int) float64 {
	if tmdbID <= 0 || s.MovieRepo == nil {
		return 0
	}
	saved, at, found, err := s.MovieRepo.GetRuntime(kind, tmdbID)
	if err != nil {
		slog.Warn("runtime: couldn't look up a saved runtime", "err", err)
	}
	if found {
		age := time.Since(at)
		if (saved > 0 && age < runtimeRefreshAfter) || (saved == 0 && age < runtimeAskAgainAfter) {
			return saved
		}
	}
	minutes, ok := s.askTMDBMinutes(ctx, kind, tmdbID)
	if !ok {
		return saved
	}
	if err := s.MovieRepo.SaveRuntime(kind, tmdbID, minutes, time.Now()); err != nil {
		slog.Warn("runtime: couldn't save a runtime", "err", err)
	}
	return minutes
}

// askTMDBMinutes asks TMDB for the runtime. ok is false when it could not be
// asked (no key, no answer); minutes is 0 when TMDB answered without one.
func (s *Server) askTMDBMinutes(ctx context.Context, kind string, tmdbID int) (minutes float64, ok bool) {
	c := s.TMDB()
	if c == nil || !c.HasAPIKey() {
		return 0, false
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if kind == library.RuntimeMovie {
		d, err := c.GetMovieDetail(ctx, tmdbID)
		if err != nil {
			return 0, false
		}
		return max(0, float64(d.Runtime)), true
	}
	d, err := c.GetShow(ctx, tmdbID)
	if err != nil {
		return 0, false
	}
	for _, n := range d.EpisodeRunTime {
		if n > 0 && (minutes == 0 || float64(n) < minutes) {
			minutes = float64(n)
		}
	}
	return minutes, true
}

// fillRuntimes looks up the runtime of titles whose files have been read but
// whose runtime was never asked, so the Library can tell which files look
// incomplete. It is part of the file details job and does a few at a time.
func (s *Server) fillRuntimes(ctx context.Context, until time.Time) {
	c := s.TMDB()
	if s.MovieRepo == nil || c == nil || !c.HasAPIKey() {
		return
	}
	wanted, err := s.MovieRepo.RuntimesWanted(time.Now().Add(-7*24*time.Hour), 120)
	if err != nil {
		slog.Warn("runtime: couldn't list the titles to look up", "err", err)
		return
	}
	for _, w := range wanted {
		if ctx.Err() != nil || time.Now().After(until) {
			break
		}
		minutes, ok := s.askTMDBMinutes(ctx, w.Kind, w.TMDBID)
		if ok {
			if err := s.MovieRepo.SaveRuntime(w.Kind, w.TMDBID, minutes, time.Now()); err != nil {
				slog.Warn("runtime: couldn't save a runtime", "err", err)
				break
			}
		}
		select {
		case <-ctx.Done():
		case <-time.After(250 * time.Millisecond):
		}
	}
	// The lists renew themselves every few seconds (mediaSummaryTTL), so the
	// new runtimes are picked up without throwing the lists away here.
}
