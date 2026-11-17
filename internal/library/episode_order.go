package library

import (
	"cmp"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
	"unicode"
)

// Which list a show's regular seasons follow (#184). The movie database
// counts a double-length episode as one; TheTVDB, and so many people's file
// names, count it as two. A show on EpisodeOrderTVDB takes its regular
// seasons from TheTVDB's aired order. Specials always stay as they are.
const (
	EpisodeOrderTMDB = "tmdb"
	EpisodeOrderTVDB = "tvdb"
)

// ValidEpisodeOrder is o when it is one of the two, else EpisodeOrderTMDB.
func ValidEpisodeOrder(o string) string {
	if o == EpisodeOrderTVDB {
		return o
	}
	return EpisodeOrderTMDB
}

// SetEpisodeOrder stores the order a show (and its second copies) follows.
func (r *Repo) SetEpisodeOrder(id int64, order string) error {
	if _, err := r.db.Exec(`UPDATE series SET episode_order = ? WHERE id = ? OR copy_of = ?`, ValidEpisodeOrder(order), id, id); err != nil {
		return fmt.Errorf("set episode order: %w", err)
	}
	return nil
}

// OrderedEpisodes is the list a show follows in TheTVDB's order: its regular
// seasons from tvdb where TheTVDB has the season, TMDB's seasons that
// TheTVDB lacks, and the specials as TMDB gives them. TheTVDB's own specials
// (season 0) are not used.
func OrderedEpisodes(tmdb, tvdb []Episode) []Episode {
	have := map[int]bool{}
	for _, e := range tvdb {
		if e.Season > 0 && e.Episode > 0 {
			have[e.Season] = true
		}
	}
	var out []Episode
	for _, e := range tmdb {
		if e.Season == 0 || !have[e.Season] {
			out = append(out, e)
		}
	}
	seen := map[[2]int]bool{}
	for _, e := range tvdb {
		if e.Season <= 0 || e.Episode <= 0 || seen[[2]int{e.Season, e.Episode}] {
			continue
		}
		seen[[2]int{e.Season, e.Episode}] = true
		out = append(out, e)
	}
	return out
}

// EpisodeMove renumbers one stored episode (keeping its file, status and
// monitoring) within its season.
type EpisodeMove struct {
	ID      int64
	Season  int
	From    int
	Episode int // the new number
}

// EpisodeOrderPlan is what changing a show's episode list to another order
// does to the episodes it already has.
type EpisodeOrderPlan struct {
	// Moves renumber episodes that sit under another number in the new list.
	Moves []EpisodeMove
	// Drop lists episodes with no place in the new list that never got
	// anything (missing, no file): they go.
	Drop []int64
	// Final is the list to upsert after the moves. For a season in Blocked it
	// is the episodes the show already has, so nothing changes there.
	Final []Episode
	// Blocked lists seasons left as they were because a file there could not
	// be matched to an episode of the new list with confidence. An episode
	// that has a file is never deleted and never guessed.
	Blocked []int
}

// Changes says whether the plan renumbers or removes anything.
func (p EpisodeOrderPlan) Changes() bool { return len(p.Moves) > 0 || len(p.Drop) > 0 }

// PlanEpisodeOrder works out how the episodes a show has (old) fit a new
// list (target), season by season. In each season an episode keeps its number
// when the one in the new list is the same episode (the air dates agree, or
// the titles are alike); one that isn't is looked up by air date and title.
// An episode with a file, or being downloaded, that cannot be placed leaves
// its whole season unchanged. Seasons the new list doesn't have, and the
// specials, are not touched.
func PlanEpisodeOrder(old, target []Episode) EpisodeOrderPlan {
	var plan EpisodeOrderPlan
	oldBy := map[int][]Episode{}
	for _, e := range old {
		if e.Season > 0 {
			oldBy[e.Season] = append(oldBy[e.Season], e)
		}
	}
	targetBy := map[int][]Episode{}
	seen := map[[2]int]bool{}
	for _, e := range target {
		if e.Season <= 0 {
			plan.Final = append(plan.Final, e)
			continue
		}
		if seen[[2]int{e.Season, e.Episode}] {
			continue
		}
		seen[[2]int{e.Season, e.Episode}] = true
		targetBy[e.Season] = append(targetBy[e.Season], e)
	}
	for _, season := range slices.Sorted(maps.Keys(targetBy)) {
		t := targetBy[season]
		slices.SortFunc(t, func(a, b Episode) int { return cmp.Compare(a.Episode, b.Episode) })
		o := oldBy[season]
		slices.SortFunc(o, func(a, b Episode) int { return cmp.Compare(a.Episode, b.Episode) })
		if len(o) == 0 {
			plan.Final = append(plan.Final, t...)
			continue
		}
		assigned, ok := assignSeason(o, t)
		if !ok {
			plan.Blocked = append(plan.Blocked, season)
			plan.Final = append(plan.Final, o...)
			continue
		}
		for _, e := range o {
			n, placed := assigned[e.ID]
			switch {
			case !placed:
				plan.Drop = append(plan.Drop, e.ID)
			case n != e.Episode:
				plan.Moves = append(plan.Moves, EpisodeMove{ID: e.ID, Season: season, From: e.Episode, Episode: n})
			}
		}
		plan.Final = append(plan.Final, t...)
	}
	return plan
}

// hasState is true for an episode that holds something that cannot be
// recreated: a file, or a download in progress.
func hasState(e Episode) bool {
	return e.FilePath != "" || (e.Status != StatusMissing && e.Status != "")
}

// assignSeason places the episodes a season has (old) on the episodes of the
// new list (target), returning each old episode's new number. It is false
// when an episode with a file or download could not be placed.
func assignSeason(old, target []Episode) (map[int64]int, bool) {
	byNumber := map[int]Episode{}
	for _, t := range target {
		byNumber[t.Episode] = t
	}
	sameShape := len(old) == len(target)
	for _, o := range old {
		if _, ok := byNumber[o.Episode]; !ok {
			sameShape = false
		}
	}
	assigned := map[int64]int{}
	taken := map[int]bool{}
	// First the episodes that keep their number.
	for _, o := range old {
		t, ok := byNumber[o.Episode]
		if ok && sameEpisode(o, t, sameShape) {
			assigned[o.ID] = o.Episode
			taken[o.Episode] = true
		}
	}
	// Then the rest, by air date and title: first the ones with something to
	// keep, which must find a place, then the others (so their monitoring
	// follows them when they do; one that finds none is dropped).
	for _, withState := range []bool{true, false} {
		for _, o := range old {
			if _, done := assigned[o.ID]; done || hasState(o) != withState {
				continue
			}
			best, ok := bestPlace(o, target, taken)
			if !ok {
				if withState {
					return nil, false
				}
				continue
			}
			assigned[o.ID] = best
			taken[best] = true
		}
	}
	return assigned, true
}

const dateSlack = 48 * time.Hour

// dateAgreement is 1 when both episodes have an air date and they are at most
// two days apart, -1 when both have one and they differ more, 0 otherwise.
func dateAgreement(a, b string) int {
	if a == "" || b == "" {
		return 0
	}
	da, err1 := time.Parse("2006-01-02", a)
	db, err2 := time.Parse("2006-01-02", b)
	if err1 != nil || err2 != nil {
		return 0
	}
	if d := da.Sub(db); d <= dateSlack && d >= -dateSlack {
		return 1
	}
	return -1
}

// sameEpisode reports whether the episode a show has at a number is the one
// the new list has at that number. Air dates that agree say yes; dates that
// differ need titles that are the same; with no dates, alike titles say yes,
// and so does a season where both lists are the same size and the same
// numbers (there is nothing to move).
func sameEpisode(o, t Episode, sameShape bool) bool {
	switch dateAgreement(o.AirDate, t.AirDate) {
	case 1:
		return true
	case -1:
		return titleLikeness(o.Title, t.Title) >= 0.8
	}
	sim := titleLikeness(o.Title, t.Title)
	return sim < 0 || sim >= 0.3 || sameShape
}

// bestPlace picks the episode of the new list (not yet taken) that o is, or
// false when none is clear enough. Ties go to the lower number, the first
// half of a double episode.
func bestPlace(o Episode, target []Episode, taken map[int]bool) (int, bool) {
	var onDate int
	for _, t := range target {
		if !taken[t.Episode] && dateAgreement(o.AirDate, t.AirDate) == 1 {
			onDate++
		}
	}
	best, bestScore := 0, 0.0
	for _, t := range target { // in number order
		if taken[t.Episode] {
			continue
		}
		d := dateAgreement(o.AirDate, t.AirDate)
		sim := titleLikeness(o.Title, t.Title)
		ok := false
		switch d {
		case 1:
			ok = sim < 0 || sim >= 0.3 || onDate == 1
		case 0:
			ok = sim >= 0.6
		default:
			ok = sim >= 0.8
		}
		if !ok {
			continue
		}
		score := max(sim, 0)
		if d == 1 {
			score++
		}
		if score > bestScore {
			best, bestScore = t.Episode, score
		}
	}
	return best, best != 0
}

// titleLikeness is how alike two episode titles are, from 0 to 1, or -1 when
// either has none. Part markers ("(1)", "Part 2") don't count, so the two
// halves of a double episode both look like the whole one's title.
func titleLikeness(a, b string) float64 {
	if strings.TrimSpace(a) == "" || strings.TrimSpace(b) == "" {
		return -1
	}
	wa, wb := titleTokens(a), titleTokens(b)
	if len(wa) == 0 || len(wb) == 0 {
		// Only part markers: "Part 1" against "Part 1".
		if strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b)) {
			return 1
		}
		return 0
	}
	in := 0
	for w := range wa {
		if wb[w] {
			in++
		}
	}
	switch {
	case in == len(wa) && in == len(wb):
		return 1
	case in == len(wa) || in == len(wb):
		return 0.8
	}
	return float64(in) / float64(len(wa)+len(wb)-in)
}

var partWords = map[string]bool{"part": true, "pt": true, "one": true, "two": true, "three": true, "four": true, "i": true, "ii": true, "iii": true, "iv": true}

func titleTokens(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if partWords[w] || isNumber(w) {
			continue
		}
		out[w] = true
	}
	return out
}

func isNumber(w string) bool {
	for _, r := range w {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return w != ""
}

// ApplyEpisodeOrder carries out a plan's moves and removals in one
// transaction: moved episodes keep their row, so their file, status and
// monitoring come along. The episodes still have to be upserted with
// plan.Final afterwards. Downloads, watched state and delayed releases kept
// under the old season and episode numbers move with them. Only an episode
// that never got anything is removed, and those rows of a removed episode go
// with it, so an episode moving to its number does not inherit them; if a
// move finds its episode changed meanwhile, nothing is done and an error
// comes back.
func (r *Repo) ApplyEpisodeOrder(seriesID int64, plan EpisodeOrderPlan) error {
	if !plan.Changes() {
		return nil
	}
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin episode order: %w", err)
	}
	defer tx.Rollback()
	for _, id := range plan.Drop {
		var season, number int
		err := tx.QueryRow(`DELETE FROM episodes WHERE id = ? AND series_id = ? AND status = 'missing' AND COALESCE(file_path, '') = '' RETURNING season, episode`, id, seriesID).Scan(&season, &number)
		if errors.Is(err, sql.ErrNoRows) {
			continue // it got something meanwhile: it stays, and so do its rows
		}
		if err != nil {
			return fmt.Errorf("remove episode %d: %w", id, err)
		}
		for _, t := range episodeKeyedTables {
			if _, err := tx.Exec(t.drop, seriesID, season, number); err != nil {
				return fmt.Errorf("remove %s of episode %d: %w", t.name, id, err)
			}
		}
	}
	// Out of the way first, so two episodes can swap numbers.
	for _, m := range plan.Moves {
		res, err := tx.Exec(`UPDATE episodes SET episode = ? WHERE id = ? AND series_id = ? AND season = ? AND episode = ?`, -m.ID, m.ID, seriesID, m.Season, m.From)
		if err != nil {
			return fmt.Errorf("renumber episode %d: %w", m.ID, err)
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return fmt.Errorf("renumber episode %d: it changed meanwhile", m.ID)
		}
	}
	for _, m := range plan.Moves {
		if _, err := tx.Exec(`UPDATE episodes SET episode = ? WHERE id = ? AND series_id = ?`, m.Episode, m.ID, seriesID); err != nil {
			return fmt.Errorf("renumber episode %d to %d: %w", m.ID, m.Episode, err)
		}
	}
	if err := renumberKeyedRows(tx, seriesID, plan.Moves); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE season_pack_searches SET tries = 0 WHERE series_id = ?`, seriesID); err != nil {
		return fmt.Errorf("reset season pack searches: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit episode order: %w", err)
	}
	return nil
}

// episodeKeyedTables are the other tables that name an episode of a show by
// its season and episode number rather than by the episode's row: each has
// the UPDATE that renumbers it, with the new number, the series id, season
// and old number as arguments, and the DELETE that removes the rows of an
// episode that goes, with the series id, season and number. Watched state and
// delayed releases have a unique key, so a row already sitting at the new
// number gives way.
var episodeKeyedTables = []struct{ name, update, drop string }{
	{"downloads",
		`UPDATE download_queue SET episode = ? WHERE series_id = ? AND season = ? AND episode = ?`,
		`DELETE FROM download_queue WHERE series_id = ? AND season = ? AND episode = ?`},
	{"watched state",
		`UPDATE OR REPLACE watched SET episode = ? WHERE kind = 'episode' AND title_id = ? AND season = ? AND episode = ?`,
		`DELETE FROM watched WHERE kind = 'episode' AND title_id = ? AND season = ? AND episode = ?`},
	{"delayed releases",
		`UPDATE OR REPLACE delayed_releases SET episode = ? WHERE kind = 'episode' AND series_id = ? AND season = ? AND episode = ?`,
		`DELETE FROM delayed_releases WHERE kind = 'episode' AND series_id = ? AND season = ? AND episode = ?`},
}

// renumberKeyedRows moves the rows of episodeKeyedTables along with the
// episodes, in the same two steps as the episodes themselves (out of the way
// to a number no episode has, then to the new one) so two can swap.
func renumberKeyedRows(tx *sql.Tx, seriesID int64, moves []EpisodeMove) error {
	for _, t := range episodeKeyedTables {
		for _, m := range moves {
			if _, err := tx.Exec(t.update, -m.ID, seriesID, m.Season, m.From); err != nil {
				return fmt.Errorf("renumber %s of episode %d: %w", t.name, m.ID, err)
			}
		}
		for _, m := range moves {
			if _, err := tx.Exec(t.update, m.Episode, seriesID, m.Season, -m.ID); err != nil {
				return fmt.Errorf("renumber %s of episode %d to %d: %w", t.name, m.ID, m.Episode, err)
			}
		}
	}
	return nil
}
