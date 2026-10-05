package books

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Following authors: their books on Open Library, and the authors a library
// follows so that new books by them are added by themselves.

// Author is a followed author.
type Author struct {
	Key           string `json:"key"` // Open Library author key, "OL26320A"
	Name          string `json:"name"`
	WantEbook     bool   `json:"ebook"`
	WantAudiobook bool   `json:"audiobook"`
	FollowedAt    string `json:"followedAt"`
}

var authorKeyPattern = regexp.MustCompile(`^OL\d+A$`)

// ValidAuthorKey reports whether s looks like an Open Library author key.
func ValidAuthorKey(s string) bool { return authorKeyPattern.MatchString(s) }

// FollowAuthor follows an author, or changes the formats wanted.
func (r *Repo) FollowAuthor(a Author) error {
	_, err := r.db.Exec(`INSERT INTO book_authors (author_key, name, want_ebook, want_audiobook) VALUES (?, ?, ?, ?)
		ON CONFLICT (author_key) DO UPDATE SET name = excluded.name, want_ebook = excluded.want_ebook, want_audiobook = excluded.want_audiobook`,
		a.Key, a.Name, a.WantEbook, a.WantAudiobook)
	if err != nil {
		return fmt.Errorf("follow author: %w", err)
	}
	return nil
}

// UnfollowAuthor stops following an author. Their books stay.
func (r *Repo) UnfollowAuthor(key string) error {
	if _, err := r.db.Exec(`DELETE FROM book_authors WHERE author_key = ?`, key); err != nil {
		return fmt.Errorf("unfollow author: %w", err)
	}
	return nil
}

// FollowedAuthors lists the authors followed, by name.
func (r *Repo) FollowedAuthors() ([]Author, error) {
	rows, err := r.db.Query(`SELECT author_key, name, want_ebook, want_audiobook, followed_at FROM book_authors ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, fmt.Errorf("list followed authors: %w", err)
	}
	defer rows.Close()
	out := []Author{}
	for rows.Next() {
		var a Author
		if err := rows.Scan(&a.Key, &a.Name, &a.WantEbook, &a.WantAudiobook, &a.FollowedAt); err != nil {
			return nil, fmt.Errorf("scan followed author: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// AuthorWorks lists an author's books on Open Library, the most read first
// (sort "popular") or the newest first (sort "newest").
func (c *Client) AuthorWorks(ctx context.Context, authorKey, sort string, limit int) ([]Found, error) {
	if !ValidAuthorKey(authorKey) {
		return nil, ErrNotFound
	}
	olSort := "readinglog"
	if sort == "newest" {
		olSort = "new"
	}
	if limit <= 0 || limit > 100 {
		limit = 40
	}
	return c.cachedFound(fmt.Sprintf("author|%s|%s|%d", authorKey, olSort, limit), func() ([]Found, error) {
		var res struct {
			Docs []olDoc `json:"docs"`
		}
		if err := c.get(ctx, "/search.json", url.Values{
			"q":      {"author_key:" + authorKey},
			"sort":   {olSort},
			"fields": {docFields},
			"limit":  {fmt.Sprint(limit)},
		}, &res); err != nil {
			return nil, err
		}
		var out []Found
		for _, f := range founds(res.Docs, false) {
			if !LooksLikeSet(f.Title) {
				out = append(out, f)
			}
		}
		return out, nil
	})
}

var setWords = regexp.MustCompile(`(?i)\b(box(ed)? ?set|collection|omnibus|trilogy set|books? \d+\s*[-–]\s*\d+|complete series|summary of|study guide)\b`)

// LooksLikeSet reports whether a title is a box set, a collection or a
// guide about a book rather than a book: "The Martian / Artemis / Project
// Hail Mary", "The Hunger Games Box Set", "Summary of Project Hail Mary".
func LooksLikeSet(title string) bool {
	return strings.Contains(title, " / ") || setWords.MatchString(title)
}
