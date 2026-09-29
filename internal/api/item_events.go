package api

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/ryanborg/mediarium/internal/blocklist"
	"github.com/ryanborg/mediarium/internal/indexers"
	"github.com/ryanborg/mediarium/internal/library"
	"github.com/ryanborg/mediarium/internal/parser"
	"github.com/ryanborg/mediarium/internal/quality"
	"github.com/ryanborg/mediarium/internal/queue"
)

// Per-item activity log: every movie and show has its own list of what
// happened to it (searches and why nothing was taken, grabs, download and
// post-processing steps, failures, blocklisting, retries). It is kept in the
// activity table beside the global feed; detailed events are marked
// item-only so the global feed stays short.

// maxItemEvents caps GET /api/{movies,series}/{id}/events.
const maxItemEvents = 200

// noOtherRelease is the item event after a failed release was blocklisted
// and the immediate retry found nothing else to take.
const noOtherRelease = "No other acceptable release; will try again at the next scheduled search"

type itemEventPayload struct {
	At      string `json:"at"`
	Kind    string `json:"kind"`
	Message string `json:"message"`
	Level   string `json:"level"` // info, warn or error
}

// handleMovieEvents lists a movie's own activity log, newest first (at most 200 events).
func (s *Server) handleMovieEvents(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid movie id")
		return
	}
	if _, err := s.MovieRepo.Get(id); errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "movie not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	events, err := s.QueueRepo.MovieEvents(id, maxItemEvents)
	writeItemEvents(w, events, err)
}

// handleSeriesEvents lists a show's own activity log (all its episodes), newest first (at most 200 events).
func (s *Server) handleSeriesEvents(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid series id")
		return
	}
	if _, err := s.MovieRepo.GetSeries(id); errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "series not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	events, err := s.QueueRepo.SeriesEvents(id, maxItemEvents)
	writeItemEvents(w, events, err)
}

func writeItemEvents(w http.ResponseWriter, events []queue.Event, err error) {
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]itemEventPayload, len(events))
	for i, e := range events {
		out[i] = itemEventPayload{At: e.At, Kind: e.Kind, Message: e.Message, Level: string(e.Level)}
	}
	writeJSON(w, http.StatusOK, out)
}

// itemEvent records an item-only event for a movie (movieID) or a show
// (seriesID). A failure to record it is logged: an event must never stop
// the work it describes.
func (s *Server) itemEvent(movieID, seriesID int64, kind string, level queue.Level, message string) {
	if err := s.QueueRepo.LogItemEvent(queue.ItemEvent{
		MovieID: movieID, SeriesID: seriesID, Kind: kind, Level: level, Message: message, ItemOnly: true,
	}); err != nil {
		slog.Warn("api: record item event", "movieId", movieID, "seriesId", seriesID, "kind", kind, "err", err)
	}
}

// retryEvent records why a failed release was not followed by an immediate
// retry with another release.
func (s *Server) retryEvent(movieID, seriesID int64, message string) {
	s.itemEvent(movieID, seriesID, "retry", queue.LevelWarn, message)
}

// retryPausedMessage explains that the retry budget is spent.
func retryPausedMessage(failed int) string {
	return fmt.Sprintf("%d releases failed in the last day, so no immediate retry; will try again at the next scheduled search", failed)
}

// queueEvent is itemEvent for the movie or show a queue item belongs to,
// naming its release.
func (s *Server) queueEvent(queueID int64, kind string, level queue.Level, prefix string) {
	if err := s.QueueRepo.ReleaseEvent(queueID, kind, level, prefix); err != nil {
		slog.Warn("api: record download step", "queueId", queueID, "kind", kind, "err", err)
	}
}

// searchVerdict tallies what one search found and why releases were not
// taken, for the "searched" event.
type searchVerdict struct {
	total, acceptable, fallbackOnly int
	reasons                         map[string]int
	failedIndexers                  []string
}

func (v *searchVerdict) reject(reason string) {
	if v.reasons == nil {
		v.reasons = map[string]int{}
	}
	v.reasons[reason]++
}

// reasonList is "8 CAM/TeleSync, 4 other years": most frequent first.
func (v searchVerdict) reasonList() string {
	type rc struct {
		reason string
		n      int
	}
	var list []rc
	for r, n := range v.reasons {
		list = append(list, rc{r, n})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].n != list[j].n {
			return list[i].n > list[j].n
		}
		return list[i].reason < list[j].reason
	})
	parts := make([]string, len(list))
	for i, x := range list {
		parts[i] = fmt.Sprintf("%d %s", x.n, x.reason)
	}
	return strings.Join(parts, ", ")
}

// message is the event text and level: what was searched, how many
// releases and how many were acceptable, and why none was when none was.
func (v searchVerdict) message(what, picked string) (string, queue.Level) {
	var b strings.Builder
	level := queue.LevelInfo
	switch {
	case v.total == 0:
		fmt.Fprintf(&b, "%s: no releases found", what)
	case v.acceptable == 0:
		fmt.Fprintf(&b, "%s: %s, none acceptable: %s", what, plural(v.total, "release"), v.reasonList())
		level = queue.LevelWarn
	default:
		fmt.Fprintf(&b, "%s: %s, %d acceptable", what, plural(v.total, "release"), v.acceptable)
		if v.fallbackOnly > 0 {
			fmt.Fprintf(&b, " (%d only as a fallback)", v.fallbackOnly)
		}
		if picked != "" {
			b.WriteString("; " + picked)
		}
	}
	if len(v.failedIndexers) > 0 {
		fmt.Fprintf(&b, " (%s did not answer: %s)", plural(len(v.failedIndexers), "indexer"), strings.Join(v.failedIndexers, ", "))
		level = queue.LevelWarn
	}
	return b.String(), level
}

// sourceReason explains a release dropped by the title's downloader choice.
func sourceReason(sources string) string {
	if sources == sourcesUsenet {
		return "from torrent sites (this title uses Usenet only)"
	}
	return "from Usenet (this title uses torrents only)"
}

// qualityReason explains why no profile in the chain takes a release.
func qualityReason(profile quality.Profile, title string) string {
	if ok, _ := profile.TitleAllowed(title); !ok {
		return "excluded by the profile's release terms"
	}
	tier := quality.Classify(parser.Parse(title))
	if tier == quality.TierUnknown {
		return "unknown quality"
	}
	return string(tier)
}

// judgeSearch sorts every result of a search into acceptable or not, in the
// order automation applies its checks: blocklist, downloader choice, match
// (keep), then quality through the profile chain. With upgradeFrom set only
// the item's own profile counts and a release must be an upgrade.
func judgeSearch(results []indexers.Result, blocked map[string]bool, sources string, profile quality.Profile,
	keep func(indexers.Result) (bool, string), upgradeFrom quality.Tier) searchVerdict {
	v := searchVerdict{total: len(results)}
	for _, res := range results {
		switch {
		case blocked[blocklist.Key(res.Title)]:
			v.reject("blocklisted")
			continue
		case sources != sourcesBoth && sources != "" && res.Protocol != indexers.Protocol(sources):
			v.reject(sourceReason(sources))
			continue
		}
		if ok, why := keep(res); !ok {
			v.reject(why)
			continue
		}
		if upgradeFrom != "" {
			switch {
			case !profile.AcceptsTitle(res.Title):
				v.reject(qualityReason(profile, res.Title))
			case !profile.IsUpgradeOverTier(upgradeFrom, parser.Parse(res.Title)):
				v.reject("not an upgrade over " + string(upgradeFrom))
			default:
				v.acceptable++
			}
			continue
		}
		_, fallback, ok := profile.AcceptedBy(res.Title)
		switch {
		case !ok:
			v.reject(qualityReason(profile, res.Title))
		case fallback:
			v.acceptable++
			v.fallbackOnly++
		default:
			v.acceptable++
		}
	}
	return v
}

// pickedText names the release automation chose and the profile that took it.
func pickedText(profile quality.Profile, best *indexers.Result) string {
	if best == nil {
		return ""
	}
	text := fmt.Sprintf("picked %q", best.Title)
	if best.IndexerName != "" {
		text += " from " + best.IndexerName
	}
	if by, fallback, ok := profile.AcceptedBy(best.Title); ok && fallback {
		return text + fmt.Sprintf(" with the fallback profile %q", by.Name)
	}
	return text + fmt.Sprintf(" with the %q profile", profile.Name)
}

// noteMovieSearch records one automatic search for a movie: how many
// releases, how many acceptable, why none was, and what was picked.
// upgradeFrom is the tier on disk for an upgrade search, "" otherwise.
func (s *Server) noteMovieSearch(m library.Movie, outcomes []indexers.Outcome, blocked map[string]bool, sources string,
	profile quality.Profile, best *indexers.Result, upgradeFrom quality.Tier) {
	keep := func(res indexers.Result) (bool, string) {
		rel := parser.Parse(res.Title)
		if normalizeTitle(rel.Title) != normalizeTitle(m.Title) {
			return false, "other films"
		}
		if m.Year != 0 && rel.Year != 0 && rel.Year != m.Year {
			return false, "other years"
		}
		return true, ""
	}
	v := judgeSearch(indexers.MergeResults(outcomes), blocked, sources, profile, keep, upgradeFrom)
	v.failedIndexers = failedIndexers(outcomes)
	what := "Searched"
	if upgradeFrom != "" {
		what = "Searched for an upgrade over " + string(upgradeFrom)
	}
	msg, level := v.message(what, pickedText(profile, best))
	s.itemEvent(m.ID, 0, "searched", level, msg)
}

// noteTVSearch records one automatic search for a season pack (episode 0)
// or an episode of a show. It judges the results the way pickTVResult does
// for something not downloaded yet.
func (s *Server) noteTVSearch(series library.Series, season, episode int, outcomes []indexers.Outcome) {
	profiles, err := s.loadProfiles()
	if err != nil {
		slog.Warn("api: record tv search", "series", series.Title, "err", err)
		return
	}
	keep := func(res indexers.Result) (bool, string) {
		rel := parser.Parse(res.Title)
		switch {
		case !matchesShow(res.Title, series):
			return false, "for another show"
		case !coversTarget(rel, season, episode), episode > 0 && len(rel.Episodes) == 0:
			if episode > 0 {
				return false, "not this episode"
			}
			return false, "not a season pack"
		}
		return true, ""
	}
	profile := profiles.resolve(series.ProfileID)
	v := judgeSearch(indexers.MergeResults(outcomes), s.blockedKeys(), s.sourcesFor(series.SourcePref), profile, keep, "")
	v.failedIndexers = failedIndexers(outcomes)
	what := fmt.Sprintf("Searched for season %d", season)
	if episode > 0 {
		what = fmt.Sprintf("Searched for S%02dE%02d", season, episode)
	}
	msg, level := v.message(what, "")
	s.itemEvent(0, series.ID, "searched", level, msg)
}

func failedIndexers(outcomes []indexers.Outcome) []string {
	var out []string
	for _, o := range outcomes {
		if o.Err != nil {
			out = append(out, o.IndexerName)
		}
	}
	return out
}
