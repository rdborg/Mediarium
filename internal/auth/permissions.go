package auth

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// Permissions are what a basic account may do. Administrators may always do
// everything. Every switch is on by default, which is what a basic account
// could always do; an administrator can switch things off per account.
type Permissions struct {
	// AddDirect adds titles straight to the library. Off, an add becomes a
	// request that an administrator approves or declines.
	AddDirect bool `json:"addDirect"`
	// Which kinds of media the account may add or ask for.
	Movies bool `json:"movies"`
	TV     bool `json:"tv"`
	Music  bool `json:"music"`
	Books  bool `json:"books"`
	// Releases: search the indexers and pick a release to download.
	Releases bool `json:"releases"`
	// Manage: search now, monitoring, tags, following authors and series.
	Manage bool `json:"manage"`
	// Retry failed or stopped downloads.
	Retry bool `json:"retry"`
	// Subtitles: find and download subtitles.
	Subtitles bool `json:"subtitles"`
	// Play: play videos, preview files, read and listen in Mediarium Books.
	Play bool `json:"play"`
}

// Permission names used by the route table.
const (
	PermReleases  = "releases"
	PermManage    = "manage"
	PermRetry     = "retry"
	PermSubtitles = "subtitles"
	PermPlay      = "play"
	PermMovies    = "movies"
	PermTV        = "tv"
	PermMusic     = "music"
	PermBooks     = "books"
)

// DefaultPermissions is everything a basic account may do.
func DefaultPermissions() Permissions {
	return Permissions{AddDirect: true, Movies: true, TV: true, Music: true, Books: true, Releases: true, Manage: true, Retry: true, Subtitles: true, Play: true}
}

// Allows reports whether p includes the named permission.
func (p Permissions) Allows(name string) bool {
	switch name {
	case PermReleases:
		return p.Releases
	case PermManage:
		return p.Manage
	case PermRetry:
		return p.Retry
	case PermSubtitles:
		return p.Subtitles
	case PermPlay:
		return p.Play
	case PermMovies:
		return p.Movies
	case PermTV:
		return p.TV
	case PermMusic:
		return p.Music
	case PermBooks:
		return p.Books
	}
	return false
}

// parsePermissions reads stored permissions; switches it doesn't mention keep
// their default (on), so a new switch never takes anything away.
func parsePermissions(s string) Permissions {
	p := DefaultPermissions()
	if s != "" {
		_ = json.Unmarshal([]byte(s), &p)
	}
	return p
}

// PermissionsOf returns an account's permissions. Administrators get all.
func (s *Service) PermissionsOf(userID int64) (Permissions, error) {
	var raw string
	var isAdmin bool
	err := s.db.QueryRow(`SELECT permissions, is_admin FROM users WHERE id = ?`, userID).Scan(&raw, &isAdmin)
	if errors.Is(err, sql.ErrNoRows) {
		return Permissions{}, ErrUserNotFound
	}
	if err != nil {
		return Permissions{}, fmt.Errorf("read permissions: %w", err)
	}
	if isAdmin {
		return DefaultPermissions(), nil
	}
	return parsePermissions(raw), nil
}

// SetPermissions saves an account's permissions.
func (s *Service) SetPermissions(userID int64, p Permissions) error {
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	res, err := s.db.Exec(`UPDATE users SET permissions = ? WHERE id = ?`, string(b), userID)
	if err != nil {
		return fmt.Errorf("save permissions: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrUserNotFound
	}
	return nil
}
