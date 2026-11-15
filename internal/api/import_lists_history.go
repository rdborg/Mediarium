package api

import (
	"net/http"
)

// A list's history (#190): what each import list has added and how much of it
// turned out useful, so a person can judge which lists are worth keeping. The
// lines come from import_list_log, which is kept when a list is told to start
// over; the counts of what was skipped come from what the list remembers now.

// listHistoryTitles is how many titles the history shows (newest first).
const listHistoryTitles = 300

// listHistoryScan is how many logged titles are read to count the ones that
// have a file; a list that added more than this counts only the newest.
const listHistoryScan = 5000

type listHistoryTitle struct {
	Title     string `json:"title"`
	Year      int    `json:"year,omitempty"`
	Kind      string `json:"kind"` // movie or tv
	TMDBID    int    `json:"tmdbId"`
	At        string `json:"at"`
	LibraryID int64  `json:"libraryId,omitempty"` // 0 when the title was removed from the library since
	HasFile   bool   `json:"hasFile"`
}

type listHistory struct {
	ListID int64  `json:"listId"`
	Name   string `json:"name"`
	// Added is how many titles the list has added, WithFiles how many of them
	// have a file now, and Downloads how many downloads it started itself (the
	// releases of an RSS feed, and the searches for what it added) that are
	// still in the download history.
	Added     int `json:"added"`
	WithFiles int `json:"withFiles"`
	Downloads int `json:"downloads"`
	// LastAddedAt is when it last added something ("" if never).
	LastAddedAt string `json:"lastAddedAt,omitempty"`
	// Outcomes counts what the list remembers about the titles on it, by
	// outcome: added, in-library, existing, excluded, filtered, no-match, failed.
	Outcomes map[string]int     `json:"outcomes"`
	Titles   []listHistoryTitle `json:"titles"`
	// More is set when the list added more than Titles shows.
	More bool `json:"more,omitempty"`
}

// GET /api/import-lists/{id}/history
func (s *Server) handleListHistory(w http.ResponseWriter, r *http.Request) {
	l, ok := s.importListFromPath(w, r)
	if !ok {
		return
	}
	out, err := s.listHistory(l)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) listHistory(l importList) (listHistory, error) {
	out := listHistory{ListID: l.ID, Name: l.Name, Outcomes: map[string]int{}, Titles: []listHistoryTitle{}}
	rows, err := s.db.Query(`SELECT outcome, COUNT(*) FROM import_list_items WHERE list_id = ? GROUP BY outcome`, l.ID)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var o string
		var n int
		if err := rows.Scan(&o, &n); err != nil {
			rows.Close()
			return out, err
		}
		out.Outcomes[o] = n
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return out, err
	}
	rows.Close()

	if err := s.db.QueryRow(`SELECT COUNT(*), COALESCE(MAX(at), '') FROM import_list_log WHERE list_id = ?`, l.ID).Scan(&out.Added, &out.LastAddedAt); err != nil {
		return out, err
	}
	// What the list started itself is credited to it by name.
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM download_queue WHERE origin_kind = 'list' AND origin_name = ?`, l.Name).Scan(&out.Downloads); err != nil {
		return out, err
	}

	rows, err = s.db.Query(`SELECT g.title, g.year, g.kind, g.tmdb_id, g.at,
		COALESCE(CASE WHEN g.kind = 'tv'
			THEN (SELECT id FROM series WHERE tmdb_id = g.tmdb_id AND copy_of IS NULL)
			ELSE (SELECT id FROM movies WHERE tmdb_id = g.tmdb_id AND copy_of IS NULL) END, 0),
		CASE WHEN g.kind = 'tv'
			THEN EXISTS(SELECT 1 FROM episodes e JOIN series s ON s.id = e.series_id WHERE s.tmdb_id = g.tmdb_id AND s.copy_of IS NULL AND e.status = 'downloaded')
			ELSE EXISTS(SELECT 1 FROM movies WHERE tmdb_id = g.tmdb_id AND copy_of IS NULL AND status = 'downloaded') END
		FROM import_list_log g WHERE g.list_id = ? ORDER BY g.at DESC, g.id DESC LIMIT ?`, l.ID, listHistoryScan)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var t listHistoryTitle
		if err := rows.Scan(&t.Title, &t.Year, &t.Kind, &t.TMDBID, &t.At, &t.LibraryID, &t.HasFile); err != nil {
			return out, err
		}
		if t.HasFile {
			out.WithFiles++
		}
		if n < listHistoryTitles {
			out.Titles = append(out.Titles, t)
		}
		n++
	}
	out.More = out.Added > listHistoryTitles
	return out, rows.Err()
}
