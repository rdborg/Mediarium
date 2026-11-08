package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/rdborg/mediarium/internal/metadata"
)

// "Wanted for a long time" (#168): the films and shows still missing long
// after they came out, with where each one came from (an import list, a
// request, the person who added it), so a big wanted list can be tidied up:
// keep a title (it leaves the review for a year), stop monitoring it, or
// remove it. How long it has been out is the measure, since a title imported
// from another app has the date of the move as its "added" date.

// keepForReview is how long a kept title stays out of the review.
const keepForReview = 365 * 24 * time.Hour

type reviewOrigin struct {
	Kind string `json:"kind"` // list, request, account or "" (not known)
	Name string `json:"name,omitempty"`
}

type reviewItem struct {
	Kind      string       `json:"kind"` // movie or tv
	ID        int64        `json:"id"`
	TMDBID    int          `json:"tmdbId"`
	Title     string       `json:"title"`
	Year      int          `json:"year,omitempty"`
	PosterURL string       `json:"posterUrl,omitempty"`
	OutSince  string       `json:"outSince"`          // release date, or the oldest missing episode's air date
	AddedAt   string       `json:"addedAt,omitempty"` // when it was added to Mediarium
	Missing   int          `json:"missing,omitempty"` // shows: aired episodes missing
	Origin    reviewOrigin `json:"origin"`
}

// handleWantedReview answers GET /api/wanted/review?days=365: what is still
// missing that came out more than days ago, the oldest first, without the
// titles kept until later.
func (s *Server) handleWantedReview(w http.ResponseWriter, r *http.Request) {
	days := 365
	if v := r.URL.Query().Get("days"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 30 || n > 36500 {
			writeError(w, http.StatusBadRequest, "Choose how long, from 30 days up.")
			return
		}
		days = n
	}
	cutoff := time.Now().AddDate(0, 0, -days).Format("2006-01-02")
	items, err := s.wantedReview(cutoff)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) wantedReview(cutoff string) ([]reviewItem, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	out := []reviewItem{}
	addedBy := map[string]int64{}
	read := func(kind, query string) error {
		rows, err := s.db.Query(query, cutoff, now)
		if err != nil {
			return fmt.Errorf("wanted review: list %s: %w", kind, err)
		}
		defer rows.Close()
		for rows.Next() {
			var it reviewItem
			var poster string
			var by int64
			if err := rows.Scan(&it.ID, &it.TMDBID, &it.Title, &it.Year, &poster, &it.OutSince, &it.Missing, &it.AddedAt, &by); err != nil {
				return fmt.Errorf("wanted review: read %s: %w", kind, err)
			}
			it.Kind, it.PosterURL = kind, metadata.PosterURL(poster)
			addedBy[reviewKey(kind, it.TMDBID)] = by
			out = append(out, it)
		}
		return rows.Err()
	}
	if err := read("movie", `SELECT m.id, m.tmdb_id, m.title, COALESCE(m.year, 0), COALESCE(m.poster_path, ''), m.release_date, 0, COALESCE(m.added_at, ''), COALESCE(m.added_by, 0)
		FROM movies m
		WHERE m.monitored = 1 AND m.status = 'missing' AND m.copy_of IS NULL
		  AND COALESCE(m.release_date, '') != '' AND m.release_date < ?
		  AND NOT EXISTS (SELECT 1 FROM wanted_kept k WHERE k.kind = 'movie' AND k.title_id = m.id AND k.kept_until > ?)`); err != nil {
		return nil, err
	}
	if err := read("tv", `SELECT s.id, s.tmdb_id, s.title, COALESCE(s.year, 0), COALESCE(s.poster_path, ''), MIN(e.air_date), COUNT(*), COALESCE(s.added_at, ''), COALESCE(s.added_by, 0)
		FROM series s JOIN episodes e ON e.series_id = s.id
		WHERE s.monitored = 1 AND e.monitored = 1 AND e.season > 0 AND e.status = 'missing'
		  AND COALESCE(e.air_date, '') != '' AND e.air_date < ?
		  AND NOT EXISTS (SELECT 1 FROM wanted_kept k WHERE k.kind = 'tv' AND k.title_id = s.id AND k.kept_until > ?)
		GROUP BY s.id`); err != nil {
		return nil, err
	}

	origins := s.reviewOrigins()
	names := s.accountNames()
	for i := range out {
		key := reviewKey(out[i].Kind, out[i].TMDBID)
		if o, ok := origins[key]; ok {
			out[i].Origin = o
		} else if ref := names(addedBy[key]); ref != nil {
			out[i].Origin = reviewOrigin{Kind: "account", Name: ref.Name}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].OutSince != out[j].OutSince {
			return out[i].OutSince < out[j].OutSince
		}
		return out[i].Title < out[j].Title
	})
	return out, nil
}

func reviewKey(kind string, tmdb int) string { return kind + ":" + strconv.Itoa(tmdb) }

// reviewOrigins finds where titles came from: the import list that added
// them, or the request someone made. A request wins over a list.
func (s *Server) reviewOrigins() map[string]reviewOrigin {
	out := map[string]reviewOrigin{}
	if rows, err := s.db.Query(`SELECT i.kind, i.tmdb_id, l.name FROM import_list_items i JOIN import_lists l ON l.id = i.list_id WHERE i.outcome = 'added' AND i.tmdb_id > 0`); err == nil {
		for rows.Next() {
			var kind, name string
			var tmdb int
			if rows.Scan(&kind, &tmdb, &name) == nil {
				out[reviewKey(kind, tmdb)] = reviewOrigin{Kind: "list", Name: name}
			}
		}
		rows.Close()
	}
	names := s.accountNames()
	if rows, err := s.db.Query(`SELECT kind, payload, COALESCE(requested_by, 0) FROM requests WHERE status = 'approved' AND kind IN ('movie', 'tv')`); err == nil {
		for rows.Next() {
			var kind, payload string
			var by int64
			if rows.Scan(&kind, &payload, &by) != nil {
				continue
			}
			var p struct {
				TMDBID int `json:"tmdbId"`
			}
			if json.Unmarshal([]byte(payload), &p) != nil || p.TMDBID <= 0 {
				continue
			}
			o := reviewOrigin{Kind: "request"}
			if ref := names(by); ref != nil {
				o.Name = ref.Name
			}
			out[reviewKey(kind, p.TMDBID)] = o
		}
		rows.Close()
	}
	return out
}

// handleWantedKeep answers POST /api/wanted/review/keep: the titles leave the
// review for a year (they stay wanted and monitored).
func (s *Server) handleWantedKeep(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Items []bulkItem `json:"items"`
	}
	if err := decodeJSON(r, &req); err != nil || len(req.Items) == 0 || len(req.Items) > 5000 {
		writeError(w, http.StatusBadRequest, "Choose the titles to keep.")
		return
	}
	for _, it := range req.Items {
		if (it.Kind != "movie" && it.Kind != "tv") || it.ID <= 0 {
			writeError(w, http.StatusBadRequest, "Choose the titles to keep.")
			return
		}
	}
	until := time.Now().Add(keepForReview).UTC().Format(time.RFC3339)
	tx, err := s.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback() //nolint:errcheck // a no-op once committed
	for _, it := range req.Items {
		if _, err := tx.Exec(`INSERT INTO wanted_kept (kind, title_id, kept_until) VALUES (?, ?, ?) ON CONFLICT(kind, title_id) DO UPDATE SET kept_until = excluded.kept_until`, it.Kind, it.ID, until); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"kept": len(req.Items), "until": until})
}
