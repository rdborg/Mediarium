package auth

import (
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
)

// A calendar feed token lets a calendar app read one account's calendar
// without signing in. It can do nothing else. The token is kept as it is
// (not hashed) so the account can see its link again; it only ever reveals
// release dates.

// CalendarFeed returns the account's feed token, or "" when it has none.
func (s *Service) CalendarFeed(userID int64) (string, error) {
	var token string
	err := s.db.QueryRow(`SELECT token FROM calendar_feeds WHERE user_id = ?`, userID).Scan(&token)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read calendar feed: %w", err)
	}
	return token, nil
}

// NewCalendarFeed makes a new feed token for the account, replacing any old
// one (whose link stops working).
func (s *Service) NewCalendarFeed(userID int64) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	if _, err := s.db.Exec(`INSERT INTO calendar_feeds (user_id, token) VALUES (?, ?)
		ON CONFLICT(user_id) DO UPDATE SET token = excluded.token, created_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`, userID, token); err != nil {
		return "", fmt.Errorf("save calendar feed: %w", err)
	}
	return token, nil
}

// RemoveCalendarFeed turns the account's feed off.
func (s *Service) RemoveCalendarFeed(userID int64) error {
	if _, err := s.db.Exec(`DELETE FROM calendar_feeds WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("remove calendar feed: %w", err)
	}
	return nil
}

// UserForCalendarFeed returns the account a feed token belongs to.
func (s *Service) UserForCalendarFeed(token string) (int64, error) {
	if len(token) != 64 {
		return 0, ErrSessionNotFound
	}
	var (
		userID int64
		stored string
	)
	err := s.db.QueryRow(`SELECT user_id, token FROM calendar_feeds WHERE token = ?`, token).Scan(&userID, &stored)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrSessionNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("look up calendar feed: %w", err)
	}
	if subtle.ConstantTimeCompare([]byte(stored), []byte(token)) != 1 {
		return 0, ErrSessionNotFound
	}
	return userID, nil
}
