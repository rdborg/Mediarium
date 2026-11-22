package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/anacrolix/torrent/metainfo"

	"github.com/rdborg/mediarium/internal/bencsafe"
	"github.com/rdborg/mediarium/internal/books"
	"github.com/rdborg/mediarium/internal/download"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/music"
	"github.com/rdborg/mediarium/internal/queue"
	"github.com/rdborg/mediarium/internal/settings"
)

// Checks made on a release before it is downloaded, and again before it is
// imported: how old a Usenet post may be, whether the release holds programs
// or scripts, and whether the same release was already downloaded.

// maxRetentionDays is the largest retention that can be saved (about 27 years;
// Usenet providers keep between a few hundred days and about 6,000).
const maxRetentionDays = 10000

// retentionDays is the Usenet retention in days, 0 when the setting is off.
func (s *Server) retentionDays() int {
	v, _ := s.Settings.Get(settings.KeyRetentionDays)
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 0 || n > maxRetentionDays {
		return 0
	}
	return n
}

// rejectExecutables says whether releases that hold programs or scripts are
// refused. It is on unless it was switched off.
func (s *Server) rejectExecutables() bool {
	v, _ := s.Settings.Get(settings.KeyRejectExecutables)
	return strings.TrimSpace(v) != "0"
}

// skipDuplicates says whether the same release is kept from being downloaded
// twice. It is off unless it was switched on.
func (s *Server) skipDuplicates() bool {
	v, _ := s.Settings.Get(settings.KeySkipDuplicates)
	return strings.TrimSpace(v) == "1"
}

// precheckEnabled says whether the articles of an NZB are looked for on the
// news servers before it is downloaded. It is off unless it was switched on.
func (s *Server) precheckEnabled() bool {
	v, _ := s.Settings.Get(settings.KeyPrecheck)
	return strings.TrimSpace(v) == "1"
}

// precheckArticles asks the news servers about a sample of the NZB's articles
// (see download.Precheck) and notes the result in the item's activity. It
// returns an error only when the release is beyond repair, as a bad release:
// it goes on the blocklist and the search for another one starts. A check that
// could not be made is noted and the download goes on.
func (s *Server) precheckArticles(ctx context.Context, queueID int64, servers []download.ClientConfig, nzb *download.NZB) error {
	res := download.Precheck(ctx, servers, nzb, download.PrecheckOptions{})
	if err := ctx.Err(); err != nil {
		return err // paused or stopped
	}
	level := queue.LevelInfo
	if res.Missing > 0 && !res.Skipped {
		level = queue.LevelWarn
	}
	s.queueEvent(queueID, "download", level, res.Summary())
	return res.Err()
}

// postAgeDays is how many whole days ago a Usenet result was posted. ok is
// false for a result with no post date and for anything that is not a Usenet
// release: torrents have no retention, and a result that doesn't say when it
// was posted can't be judged, so it stays.
func postAgeDays(res indexers.Result, now time.Time) (days int, ok bool) {
	if res.Protocol != indexers.ProtocolUsenet || res.Torrent || res.PublishDate.IsZero() {
		return 0, false
	}
	if res.PublishDate.After(now) {
		return 0, true
	}
	return int(now.Sub(res.PublishDate).Hours() / 24), true
}

// beyondRetention reports whether a result was posted longer ago than the
// retention (0 = never), and how many days ago that was.
func beyondRetention(res indexers.Result, retention int, now time.Time) (age int, old bool) {
	if retention <= 0 {
		return 0, false
	}
	age, ok := postAgeDays(res, now)
	return age, ok && age > retention
}

// retentionReason is the sentence shown beside a release the retention rules
// out.
func retentionReason(age, retention int) string {
	return fmt.Sprintf("Posted %s ago, longer than your Usenet retention of %s", plural(age, "day"), plural(retention, "day"))
}

// retentionTally is the reason counted in the notes of an automatic search.
func retentionTally(retention int) string {
	return fmt.Sprintf("older than your Usenet retention of %s", plural(retention, "day"))
}

// freshResults drops the Usenet results that are older than the retention. The
// automatic searches use it, so they never pick a post the provider no longer
// has. It returns the rest and how many it dropped. With the setting off it
// returns the results as they are.
func (s *Server) freshResults(results []indexers.Result) ([]indexers.Result, int) {
	retention := s.retentionDays()
	if retention == 0 {
		return results, 0
	}
	now := time.Now()
	out := make([]indexers.Result, 0, len(results))
	dropped := 0
	for _, res := range results {
		if _, old := beyondRetention(res, retention, now); old {
			dropped++
			continue
		}
		out = append(out, res)
	}
	return out, dropped
}

// freshOnly is freshResults for a caller that doesn't need the count.
func (s *Server) freshOnly(results []indexers.Result) []indexers.Result {
	out, _ := s.freshResults(results)
	return out
}

// addExpired counts results dropped for being older than the retention in the
// note of an automatic search.
func (v *searchVerdict) addExpired(n, retention int) {
	if n <= 0 {
		return
	}
	v.total += n
	if v.reasons == nil {
		v.reasons = map[string]int{}
	}
	v.reasons[retentionTally(retention)] += n
}

// retentionRejection is the rejection to show beside a result in an
// interactive search, "" when the result is fine. Grabbing it by hand stays
// possible: the person may know the provider keeps more than they set.
func (s *Server) retentionRejection(res indexers.Result) string {
	retention := s.retentionDays()
	if age, old := beyondRetention(res, retention, time.Now()); old {
		return retentionReason(age, retention)
	}
	return ""
}

// --- duplicates ---

// duplicateReleaseError says a download was left out because the same release
// is already in the queue or was already downloaded.
type duplicateReleaseError struct {
	other  queue.Item
	byHash bool
}

func (e *duplicateReleaseError) Error() string {
	if e.byHash {
		return fmt.Sprintf("it is the same NZB as %q, which is already in the queue or downloaded", e.other.ReleaseTitle)
	}
	return fmt.Sprintf("%q is already in the queue or downloaded", e.other.ReleaseTitle)
}

func asDuplicate(err error) *duplicateReleaseError {
	var d *duplicateReleaseError
	if errors.As(err, &d) {
		return d
	}
	return nil
}

// checkDuplicate looks for an earlier download of the same release for the same
// title, when the setting is on. hash is the NZB's fingerprint ("" before it is
// known, for torrents and for the check made before the NZB is read). A
// finished earlier download only counts while the title still has what it
// delivered: a file deleted by hand can be downloaded again.
func (s *Server) checkDuplicate(queueID int64, hash string) error {
	if !s.skipDuplicates() {
		return nil
	}
	item, err := s.QueueRepo.Get(queueID)
	if err != nil {
		return nil // a missing item is dealt with by the pipeline itself
	}
	other, found, err := s.QueueRepo.FindDuplicate(item, hash)
	if err != nil {
		slog.Warn("api: look for a duplicate download", "queueId", queueID, "err", err)
		return nil
	}
	if !found {
		return nil
	}
	if other.Status == queue.StatusCompleted && !s.titleStillHas(other) {
		return nil
	}
	return &duplicateReleaseError{other: other, byHash: hash != "" && other.ReleaseTitle != item.ReleaseTitle}
}

// titleStillHas reports whether the movie, episode, album or book a finished
// download was for still has its file.
func (s *Server) titleStillHas(it queue.Item) bool {
	switch {
	case it.MovieID > 0:
		m, err := s.MovieRepo.Get(it.MovieID)
		return err == nil && restoredMovieStatus(m) == library.StatusDownloaded
	case it.SeriesID > 0:
		eps, err := s.MovieRepo.ListEpisodes(it.SeriesID)
		if err != nil {
			return false
		}
		for _, ep := range eps {
			if ep.Season != it.Season || (it.Episode > 0 && ep.Episode != it.Episode) {
				continue
			}
			// The status may read "downloading" already (the pipeline has claimed
			// the episodes), so the file decides.
			if ep.Status == library.StatusDownloaded || fileExists(ep.FilePath) {
				return true
			}
		}
		return false
	case it.AlbumID > 0:
		a, err := s.MusicRepo.GetAlbum(it.AlbumID)
		return err == nil && s.restoredAlbumStatus(a) == music.StatusDownloaded
	case it.BookID > 0:
		b, err := s.BookRepo.Get(it.BookID)
		return err == nil && fileExists(b.Path(books.Format(it.BookFormat)))
	case it.OtherID > 0:
		o, err := s.OtherRepo.Get(it.OtherID)
		return err == nil && o.Path != "" && o.FileCount > 0 && fileExists(o.Path)
	}
	return false
}

// settleDuplicate finishes a download that was skipped as a duplicate: the item
// fails with a plain reason, and a note goes to the activity feed. It is not a
// problem to report, it does not blocklist anything and it does not start
// another search: the release is fine, and the title already has it or is
// getting it.
func (s *Server) settleDuplicate(queueID int64, dup *duplicateReleaseError) {
	plain := fmt.Sprintf("Skipped: %s.", dup.Error())
	if err := s.QueueRepo.SetStatus(queueID, queue.StatusFailed, plain); err != nil {
		slog.Warn("api: settle a skipped duplicate", "queueId", queueID, "err", err)
	}
	item, err := s.QueueRepo.Get(queueID)
	if err != nil {
		return
	}
	message := fmt.Sprintf("Skipped %q: %s.", item.ReleaseTitle, dup.Error())
	switch {
	case item.AlbumID > 0:
		_ = s.QueueRepo.LogItemEvent(queue.ItemEvent{AlbumID: item.AlbumID, Kind: "skipped", Level: queue.LevelInfo, Message: message})
	default:
		_ = s.QueueRepo.LogItemEvent(queue.ItemEvent{MovieID: item.MovieID, SeriesID: item.SeriesID, Kind: "skipped", Level: queue.LevelInfo, Message: message})
	}
}

// --- programs and scripts ---

// checkNZBSafe refuses an NZB that lists programs or scripts, when the setting
// is on. The error marks the release as bad, so it goes on the blocklist and
// the search for another one starts.
func (s *Server) checkNZBSafe(nzb *download.NZB) error {
	if !s.rejectExecutables() {
		return nil
	}
	if err := download.UnsafeInNZB(nzb); err != nil {
		return badRelease(err)
	}
	return nil
}

// checkUnpackedSafe refuses a finished, unpacked download that holds programs
// or scripts, when the setting is on.
func (s *Server) checkUnpackedSafe(dir string) error {
	if !s.rejectExecutables() {
		return nil
	}
	if err := download.UnsafeInDir(dir); err != nil {
		var unsafe *download.UnsafeFilesError
		if errors.As(err, &unsafe) {
			return badRelease(err)
		}
		// The folder could not be read: that is our problem, not the release's.
		return err
	}
	return nil
}

// checkTorrentFileSafe is checkNZBSafe for a .torrent file on disk. A file that
// cannot be read is left for the torrent client to report.
func (s *Server) checkTorrentFileSafe(path string) error {
	if !s.rejectExecutables() {
		return nil
	}
	data, err := bencsafe.ReadFile(path)
	if err != nil {
		return nil
	}
	mi, err := metainfo.Load(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	info, err := mi.UnmarshalInfo()
	if err != nil {
		return nil
	}
	var names []string
	for _, f := range info.UpvertedFiles() {
		name := strings.Join(f.BestPath(), "/")
		if name == "" {
			name = info.BestName()
		}
		names = append(names, name)
	}
	if err := download.UnsafeInNames(names); err != nil {
		return badRelease(err)
	}
	return nil
}

// holdsTitle reports whether the earlier download is still on its way, in which
// case it still has the title marked as being downloaded and a skipped copy
// must leave that alone.
func (e *duplicateReleaseError) holdsTitle() bool {
	return downloadOpen(e.other.Status) || e.other.Status == queue.StatusConflict
}
