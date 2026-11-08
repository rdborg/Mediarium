// Package librarysearch answers the type-ahead search over the library: the
// few letters a person has typed so far, matched against the start of any
// word of a movie, show, book, artist or album name. It reads the full-text
// index the database keeps (library_search) and falls back to a plain LIKE
// search when that index cannot be used.
package librarysearch

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"unicode"
)

// Kinds a hit can be.
const (
	KindMovie  = "movie"
	KindShow   = "tv"
	KindBook   = "book"
	KindArtist = "artist"
	KindAlbum  = "album"
)

// The index row id is the table id times idStep plus the kind code.
const idStep = 8

// MaxLimit is the most hits one search returns.
const MaxLimit = 50

// Hit is one library item found by a search.
type Hit struct {
	Kind     string `json:"kind"`
	ID       int64  `json:"id"`
	TMDBID   int64  `json:"tmdbId,omitempty"` // movies and shows
	Title    string `json:"title"`
	Subtitle string `json:"subtitle,omitempty"` // the author or the artist
	Year     int    `json:"year,omitempty"`
}

// Tokens splits text into the words the index would make of it: runs of
// letters, digits and combining marks, lower-cased. Everything else (spaces,
// punctuation, symbols) separates words, the same as in the index.
func Tokens(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.IsMark(r)
	})
}

// Query turns what was typed into an FTS5 query: every word must be found and
// each one matches the start of a word, so "star w" finds "Star Wars". It
// returns "" when the text holds no word.
func Query(text string) string {
	toks := Tokens(text)
	if len(toks) == 0 {
		return ""
	}
	if len(toks) > 8 {
		toks = toks[:8]
	}
	parts := make([]string, len(toks))
	for i, t := range toks {
		// Only letters and digits are left, so quoting is all the escaping
		// the query needs.
		parts[i] = `"` + t + `"*`
	}
	return strings.Join(parts, " ")
}

// Search returns the library items whose name starts with each typed word,
// best first. It uses the full-text index and, if that fails, a LIKE search.
func Search(ctx context.Context, db *sql.DB, text string, limit int) ([]Hit, error) {
	if limit <= 0 || limit > MaxLimit {
		limit = MaxLimit
	}
	q := Query(text)
	if q == "" {
		return nil, nil
	}
	ids, err := searchIndex(ctx, db, q, limit)
	if err != nil {
		slog.Warn("library search: the index could not be used, searching the tables instead", "err", err)
		return searchLike(ctx, db, text, limit)
	}
	return resolve(ctx, db, ids, limit)
}

// searchIndex asks the index. The title counts ten times as much as the other
// names, so a title that starts with the typed words comes before one that is
// only found by a translation.
func searchIndex(ctx context.Context, db *sql.DB, q string, limit int) ([]int64, error) {
	// More rows than asked for, because resolve drops the ones that no longer
	// have a table row (such as a second copy of a movie).
	rows, err := db.QueryContext(ctx,
		`SELECT rowid FROM library_search WHERE library_search MATCH ? ORDER BY bm25(library_search, 10.0, 1.0), rowid LIMIT ?`,
		q, limit*2)
	if err != nil {
		return nil, fmt.Errorf("search index: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("search index: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// resolve turns index row ids into hits, keeping their order.
func resolve(ctx context.Context, db *sql.DB, ids []int64, limit int) ([]Hit, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	byKind := map[int64][]int64{}
	for _, rid := range ids {
		byKind[rid%idStep] = append(byKind[rid%idStep], rid/idStep)
	}
	found := map[int64]Hit{} // by index row id
	for code, list := range byKind {
		hits, err := load(ctx, db, code, list)
		if err != nil {
			return nil, err
		}
		for _, h := range hits {
			found[h.ID*idStep+code] = h
		}
	}
	out := make([]Hit, 0, limit)
	for _, rid := range ids {
		if h, ok := found[rid]; ok {
			out = append(out, h)
			if len(out) == limit {
				break
			}
		}
	}
	return out, nil
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// load reads the names of the items with the given ids from one kind's table.
func load(ctx context.Context, db *sql.DB, code int64, ids []int64) ([]Hit, error) {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	var query, kind string
	switch code {
	case 1:
		kind = KindMovie
		query = `SELECT id, tmdb_id, title, COALESCE(year, 0), '' FROM movies WHERE copy_of IS NULL AND id IN (` + placeholders(len(ids)) + `)`
	case 2:
		kind = KindShow
		query = `SELECT id, tmdb_id, title, COALESCE(year, 0), '' FROM series WHERE id IN (` + placeholders(len(ids)) + `)`
	case 3:
		kind = KindBook
		query = `SELECT id, 0, title, year, author FROM books WHERE id IN (` + placeholders(len(ids)) + `)`
	case 4:
		kind = KindArtist
		query = `SELECT id, 0, name, 0, '' FROM artists WHERE id IN (` + placeholders(len(ids)) + `)`
	case 5:
		kind = KindAlbum
		query = `SELECT a.id, 0, a.title, 0, COALESCE(r.name, '') FROM albums a LEFT JOIN artists r ON r.id = a.artist_id WHERE a.id IN (` + placeholders(len(ids)) + `)`
	default:
		return nil, nil
	}
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read search hits: %w", err)
	}
	defer rows.Close()
	var out []Hit
	for rows.Next() {
		h := Hit{Kind: kind}
		if err := rows.Scan(&h.ID, &h.TMDBID, &h.Title, &h.Year, &h.Subtitle); err != nil {
			return nil, fmt.Errorf("read search hits: %w", err)
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// likeEscape makes text safe inside a LIKE pattern that uses ESCAPE '\'.
func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// searchLike is the slower way, used only when the index fails: every typed
// word has to appear somewhere in the name. Accents are not ignored here and
// the best matches are those that start with the text.
func searchLike(ctx context.Context, db *sql.DB, text string, limit int) ([]Hit, error) {
	words := strings.Fields(strings.ToLower(text))
	if len(words) == 0 {
		return nil, nil
	}
	if len(words) > 8 {
		words = words[:8]
	}
	sources := []struct {
		code        int64
		table, col  string
		extraFilter string
	}{
		{1, "movies", "title", " AND copy_of IS NULL"},
		{2, "series", "title", ""},
		{3, "books", "title", ""},
		{4, "artists", "name", ""},
		{5, "albums", "title", ""},
	}
	type scored struct {
		id   int64 // index row id
		rank int
	}
	var all []scored
	for _, src := range sources {
		conds := make([]string, len(words))
		args := make([]any, 0, len(words)+1)
		for i, w := range words {
			conds[i] = `lower(` + src.col + `) LIKE ? ESCAPE '\'`
			args = append(args, "%"+likeEscape(w)+"%")
		}
		args = append(args, limit)
		rows, err := db.QueryContext(ctx,
			`SELECT id, `+src.col+` FROM `+src.table+` WHERE `+strings.Join(conds, " AND ")+src.extraFilter+` ORDER BY `+src.col+` LIMIT ?`, args...)
		if err != nil {
			return nil, fmt.Errorf("search the %s: %w", src.table, err)
		}
		for rows.Next() {
			var id int64
			var title string
			if err := rows.Scan(&id, &title); err != nil {
				rows.Close()
				return nil, fmt.Errorf("search the %s: %w", src.table, err)
			}
			rank := 1
			if strings.HasPrefix(strings.ToLower(title), words[0]) {
				rank = 0
			}
			all = append(all, scored{id*idStep + src.code, rank})
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].rank < all[j].rank })
	ids := make([]int64, 0, len(all))
	for _, s := range all {
		ids = append(ids, s.id)
	}
	if len(ids) > limit*2 {
		ids = ids[:limit*2]
	}
	return resolve(ctx, db, ids, limit)
}

// Rebuild empties the index and fills it again from the library tables.
func Rebuild(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("rebuild search index: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck
	for _, q := range []string{
		`DELETE FROM library_search`,
		`INSERT INTO library_search(rowid, title, alt)
		 SELECT m.id * 8 + 1, m.title, coalesce(v.alt, '') FROM movies m LEFT JOIN library_search_alt_movies v ON v.id = m.id WHERE m.copy_of IS NULL`,
		`INSERT INTO library_search(rowid, title, alt)
		 SELECT s.id * 8 + 2, s.title, coalesce(v.alt, '') FROM series s LEFT JOIN library_search_alt_series v ON v.id = s.id`,
		`INSERT INTO library_search(rowid, title, alt) SELECT id * 8 + 3, title, coalesce(author, '') FROM books`,
		`INSERT INTO library_search(rowid, title, alt) SELECT id * 8 + 4, name, trim(coalesce(sort_name, '')) FROM artists`,
		`INSERT INTO library_search(rowid, title, alt)
		 SELECT a.id * 8 + 5, a.title, coalesce(r.name, '') FROM albums a LEFT JOIN artists r ON r.id = a.artist_id`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("rebuild search index: %w", err)
		}
	}
	return tx.Commit()
}

// Ensure fills the index when it is empty but the library is not (a database
// restored from a copy made before the index existed, or an index that was
// emptied). It reports whether it had to rebuild.
func Ensure(ctx context.Context, db *sql.DB) (bool, error) {
	var indexed int
	if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM library_search)`).Scan(&indexed); err != nil {
		return false, fmt.Errorf("check search index: %w", err)
	}
	if indexed == 1 {
		return false, nil
	}
	var anyRows int
	if err := db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM movies) OR EXISTS (SELECT 1 FROM series) OR EXISTS (SELECT 1 FROM books)
		        OR EXISTS (SELECT 1 FROM artists) OR EXISTS (SELECT 1 FROM albums)`).Scan(&anyRows); err != nil {
		return false, fmt.Errorf("check search index: %w", err)
	}
	if anyRows == 0 {
		return false, nil
	}
	if err := Rebuild(ctx, db); err != nil {
		return false, err
	}
	return true, nil
}
