package library

import (
	"fmt"
	"sort"
	"strings"
)

// Tags on movies and shows: free words like "Kids" or "4K" to sort a library
// that lives in one folder. Matching ignores case; the spelling a tag was
// first given is kept.

// TitleKind says which table a tag belongs to.
type TitleKind string

const (
	TagMovie  TitleKind = "movie"
	TagSeries TitleKind = "series"
)

const (
	maxTagLen     = 30
	maxTagsPerOne = 20
)

// TagCount is a tag with how many movies and shows carry it.
type TagCount struct {
	Name   string `json:"name"`
	Movies int    `json:"movies"`
	Shows  int    `json:"shows"`
}

func tagTable(k TitleKind) (table, col string) {
	if k == TagSeries {
		return "series_tags", "series_id"
	}
	return "movie_tags", "movie_id"
}

// NormalizeTags tidies tags typed by a person: spaces trimmed and squeezed,
// commas split, each at most 30 characters, no repeats (ignoring case), at
// most 20.
func NormalizeTags(in []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, raw := range in {
		for _, part := range strings.Split(raw, ",") {
			t := strings.Join(strings.Fields(part), " ")
			if r := []rune(t); len(r) > maxTagLen {
				t = strings.TrimSpace(string(r[:maxTagLen]))
			}
			if t == "" || seen[strings.ToLower(t)] {
				continue
			}
			seen[strings.ToLower(t)] = true
			out = append(out, t)
			if len(out) == maxTagsPerOne {
				return out
			}
		}
	}
	return out
}

// TitleTags returns the tags of one title, sorted.
func (r *Repo) TitleTags(k TitleKind, id int64) ([]string, error) {
	table, col := tagTable(k)
	rows, err := r.db.Query(`SELECT tag FROM `+table+` WHERE `+col+` = ? ORDER BY tag COLLATE NOCASE`, id)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, fmt.Errorf("scan tag: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// AllTitleTags returns every title's tags of one kind, by title id.
func (r *Repo) AllTitleTags(k TitleKind) (map[int64][]string, error) {
	table, col := tagTable(k)
	rows, err := r.db.Query(`SELECT ` + col + `, tag FROM ` + table + ` ORDER BY tag COLLATE NOCASE`)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	defer rows.Close()
	out := map[int64][]string{}
	for rows.Next() {
		var id int64
		var t string
		if err := rows.Scan(&id, &t); err != nil {
			return nil, fmt.Errorf("scan tag: %w", err)
		}
		out[id] = append(out[id], t)
	}
	return out, rows.Err()
}

// canonical gives each tag the spelling it already has in the library, so
// "kids" joins "Kids" instead of starting a second tag.
func (r *Repo) canonical(tags []string) []string {
	out := make([]string, len(tags))
	for i, t := range tags {
		out[i] = t
		var have string
		if r.db.QueryRow(`SELECT tag FROM movie_tags WHERE tag = ? UNION ALL SELECT tag FROM series_tags WHERE tag = ? LIMIT 1`, t, t).Scan(&have) == nil && have != "" {
			out[i] = have
		}
	}
	return out
}

// SetTitleTags replaces a title's tags and reports which were added and
// which removed (for the media servers).
func (r *Repo) SetTitleTags(k TitleKind, id int64, tags []string) (added, removed []string, err error) {
	want := r.canonical(NormalizeTags(tags))
	have, err := r.TitleTags(k, id)
	if err != nil {
		return nil, nil, err
	}
	wantSet, haveSet := map[string]bool{}, map[string]bool{}
	for _, t := range want {
		wantSet[strings.ToLower(t)] = true
	}
	for _, t := range have {
		haveSet[strings.ToLower(t)] = true
	}
	table, col := tagTable(k)
	tx, err := r.db.Begin()
	if err != nil {
		return nil, nil, fmt.Errorf("set tags: %w", err)
	}
	defer tx.Rollback()
	for _, t := range have {
		if !wantSet[strings.ToLower(t)] {
			if _, err := tx.Exec(`DELETE FROM `+table+` WHERE `+col+` = ? AND tag = ?`, id, t); err != nil {
				return nil, nil, fmt.Errorf("remove tag: %w", err)
			}
			removed = append(removed, t)
		}
	}
	for _, t := range want {
		if !haveSet[strings.ToLower(t)] {
			if _, err := tx.Exec(`INSERT INTO `+table+` (`+col+`, tag) VALUES (?, ?)`, id, t); err != nil {
				return nil, nil, fmt.Errorf("add tag: %w", err)
			}
			added = append(added, t)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("set tags: %w", err)
	}
	return added, removed, nil
}

// ChangeTitleTags adds and removes tags on one title, keeping the others.
func (r *Repo) ChangeTitleTags(k TitleKind, id int64, add, remove []string) (added, removed []string, err error) {
	have, err := r.TitleTags(k, id)
	if err != nil {
		return nil, nil, err
	}
	drop := map[string]bool{}
	for _, t := range NormalizeTags(remove) {
		drop[strings.ToLower(t)] = true
	}
	var next []string
	for _, t := range have {
		if !drop[strings.ToLower(t)] {
			next = append(next, t)
		}
	}
	return r.SetTitleTags(k, id, append(next, add...))
}

// Tags lists every tag in use, by name, with how many movies and shows have it.
func (r *Repo) Tags() ([]TagCount, error) {
	rows, err := r.db.Query(`SELECT tag, 'm' FROM movie_tags UNION ALL SELECT tag, 's' FROM series_tags`)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	defer rows.Close()
	byName := map[string]*TagCount{}
	for rows.Next() {
		var t, kind string
		if err := rows.Scan(&t, &kind); err != nil {
			return nil, fmt.Errorf("scan tag: %w", err)
		}
		c := byName[strings.ToLower(t)]
		if c == nil {
			c = &TagCount{Name: t}
			byName[strings.ToLower(t)] = c
		}
		if kind == "m" {
			c.Movies++
		} else {
			c.Shows++
		}
	}
	out := make([]TagCount, 0, len(byName))
	for _, c := range byName {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, rows.Err()
}
