// Package auth implements local accounts, session cookies, and API keys.
// No external identity provider — bcrypt-hashed passwords in the same
// SQLite DB, signed HTTP-only session cookies, and per-user
// API keys for headless/programmatic access.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// hashCost is the bcrypt work factor for new password hashes: 12 is about a
// quarter of a second on a small server, four times what bcrypt's default (10)
// costs an attacker per guess. Older, cheaper hashes still verify and are
// replaced by a stronger one the next time their owner signs in. Tests use the
// cheapest setting so the suites stay fast.
var hashCost = 12

func init() {
	if testing.Testing() {
		hashCost = bcrypt.MinCost
	}
}

var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrUsernameTaken      = errors.New("username already taken")
	ErrSessionNotFound    = errors.New("session not found or expired")
	// ErrSetupDone is returned by CreateFirstAdmin once any account exists.
	ErrSetupDone = errors.New("an admin account already exists")
	// ErrPasswordTooLong is returned for a password bcrypt cannot use whole.
	ErrPasswordTooLong = errors.New("password must be at most 72 bytes")
)

// MaxPasswordBytes is the longest password bcrypt takes: it looks at no more
// than 72 bytes, and refuses anything longer.
const MaxPasswordBytes = 72

const sessionTTL = 7 * 24 * time.Hour

type User struct {
	ID        int64
	Username  string
	IsAdmin   bool
	FirstName string
	LastName  string
	Email     string
}

// Roles. is_admin in the users table is the source of truth; Role is the
// name the API and the docs use for it.
const (
	RoleAdmin  = "admin"  // everything, including settings and accounts
	RoleMember = "member" // browse, add titles and follow downloads; no settings
)

// Role is RoleAdmin or RoleMember.
func (u *User) Role() string {
	if u.IsAdmin {
		return RoleAdmin
	}
	return RoleMember
}

// Name is "First Last" as stored (either half may be missing, both may be
// empty), without DisplayName's fallback to the username.
func (u *User) Name() string {
	return strings.TrimSpace(strings.TrimSpace(u.FirstName) + " " + strings.TrimSpace(u.LastName))
}

// DisplayName is "First Last" (either half may be missing), falling back to
// the username when no name has been set.
func (u *User) DisplayName() string {
	if name := u.Name(); name != "" {
		return name
	}
	return u.Username
}

type Service struct {
	db *sql.DB
}

func New(db *sql.DB) *Service {
	return &Service{db: db}
}

// FirstRunNeeded reports whether no admin account exists yet
// (the onboarding wizard triggers automatically until this is false).
func (s *Service) FirstRunNeeded() (bool, error) {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return false, fmt.Errorf("count users: %w", err)
	}
	return count == 0, nil
}

// CreateUser creates a new local account. The very first account created
// is implicitly admin.
func (s *Service) CreateUser(username, password string) (*User, error) {
	return s.CreateUserWithProfile(username, password, "", "", "")
}

// CreateUserWithProfile is CreateUser plus the profile fields the onboarding
// wizard collects.
func (s *Service) CreateUserWithProfile(username, password, firstName, lastName, email string) (*User, error) {
	if len(password) > MaxPasswordBytes {
		return nil, ErrPasswordTooLong
	}
	firstRun, err := s.FirstRunNeeded()
	if err != nil {
		return nil, err
	}
	if taken, err := s.nameInUse(username, 0); err != nil {
		return nil, err
	} else if taken {
		return nil, ErrUsernameTaken
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), hashCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}
	res, err := s.db.Exec(`INSERT INTO users (username, password_hash, is_admin, first_name, last_name, email) VALUES (?, ?, ?, ?, ?, ?)`,
		username, string(hash), firstRun, firstName, lastName, email)
	if err != nil {
		if isUniqueConstraintErr(err) {
			return nil, ErrUsernameTaken
		}
		return nil, fmt.Errorf("insert user: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("get inserted id: %w", err)
	}
	return &User{ID: id, Username: username, IsAdmin: firstRun, FirstName: firstName, LastName: lastName, Email: email}, nil
}

// CreateFirstAdmin creates the first account, an administrator, for the
// onboarding wizard. The check that no account exists and the insert are one
// statement, so two simultaneous requests can never both become
// administrator: the second gets ErrSetupDone.
func (s *Service) CreateFirstAdmin(username, password, firstName, lastName, email string) (*User, error) {
	if len(password) > MaxPasswordBytes {
		return nil, ErrPasswordTooLong
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), hashCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}
	res, err := s.db.Exec(`INSERT INTO users (username, password_hash, is_admin, first_name, last_name, email)
		SELECT ?, ?, 1, ?, ?, ? WHERE NOT EXISTS (SELECT 1 FROM users)`,
		username, string(hash), firstName, lastName, email)
	if err != nil {
		return nil, fmt.Errorf("insert user: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrSetupDone
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("get inserted id: %w", err)
	}
	return &User{ID: id, Username: username, IsAdmin: true, FirstName: firstName, LastName: lastName, Email: email}, nil
}

// dummyHash is compared against when the username does not exist, so a
// wrong username takes as long to answer as a wrong password.
var (
	dummyHashOnce sync.Once
	dummyHash     []byte
)

func fakeCompare(password string) {
	dummyHashOnce.Do(func() {
		dummyHash, _ = bcrypt.GenerateFromPassword([]byte("mediarium-no-such-user"), hashCost)
	})
	_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
}

// Authenticate verifies a username/password pair.
func (s *Service) Authenticate(username, password string) (*User, error) {
	var (
		id      int64
		hash    string
		isAdmin bool
		first   string
		last    string
		email   string
	)
	err := s.db.QueryRow(`SELECT id, password_hash, is_admin, first_name, last_name, email FROM users WHERE username = ?`, username).
		Scan(&id, &hash, &isAdmin, &first, &last, &email)
	if err == sql.ErrNoRows {
		fakeCompare(password)
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, fmt.Errorf("lookup user: %w", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}
	s.upgradeHash(id, hash, password)
	return &User{ID: id, Username: username, IsAdmin: isAdmin, FirstName: first, LastName: last, Email: email}, nil
}

// upgradeHash replaces a password hash made with a lower work factor by one
// made with the current one. It runs right after a correct password, the only
// moment the password is at hand. A failure changes nothing: the old hash
// keeps working. The update only applies if the stored hash is still the one
// that was checked, so a password changed in the meantime is never undone.
func (s *Service) upgradeHash(id int64, oldHash, password string) {
	if cost, err := bcrypt.Cost([]byte(oldHash)); err != nil || cost >= hashCost {
		return
	}
	newHash, err := bcrypt.GenerateFromPassword([]byte(password), hashCost)
	if err != nil {
		return
	}
	_, _ = s.db.Exec(`UPDATE users SET password_hash = ? WHERE id = ? AND password_hash = ?`, string(newHash), id, oldHash)
}

// nameInUse reports whether another account (not exceptID) already has this
// username in any letter case, so "Admin" cannot be made next to "admin".
func (s *Service) nameInUse(username string, exceptID int64) (bool, error) {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE lower(username) = lower(?) AND id != ?`, username, exceptID).Scan(&n); err != nil {
		return false, fmt.Errorf("check username: %w", err)
	}
	return n > 0, nil
}

// GetUser loads one account by id, or ErrInvalidCredentials if it's gone.
func (s *Service) GetUser(id int64) (*User, error) {
	u := &User{ID: id}
	err := s.db.QueryRow(`SELECT username, is_admin, first_name, last_name, email FROM users WHERE id = ?`, id).
		Scan(&u.Username, &u.IsAdmin, &u.FirstName, &u.LastName, &u.Email)
	if err == sql.ErrNoRows {
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, fmt.Errorf("lookup user: %w", err)
	}
	return u, nil
}

// UpdateProfile changes a user's username and profile details, returning the
// updated account. ErrUsernameTaken when another account already has the name.
func (s *Service) UpdateProfile(userID int64, username, firstName, lastName, email string) (*User, error) {
	if taken, err := s.nameInUse(username, userID); err != nil {
		return nil, err
	} else if taken {
		return nil, ErrUsernameTaken
	}
	_, err := s.db.Exec(`UPDATE users SET username = ?, first_name = ?, last_name = ?, email = ? WHERE id = ?`,
		username, firstName, lastName, email, userID)
	if err != nil {
		if isUniqueConstraintErr(err) {
			return nil, ErrUsernameTaken
		}
		return nil, fmt.Errorf("update profile: %w", err)
	}
	return s.GetUser(userID)
}

// ChangePassword verifies the user's current password before setting a new
// one, so a hijacked-but-still-open session can't be used to lock the real
// owner out by itself. Every session of the account is signed out (the
// caller starts a fresh one for the browser that made the change), so a
// stolen session does not survive the password change.
func (s *Service) ChangePassword(userID int64, currentPassword, newPassword string) error {
	if len(newPassword) > MaxPasswordBytes {
		return ErrPasswordTooLong
	}
	var hash string
	if err := s.db.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, userID).Scan(&hash); err != nil {
		if err == sql.ErrNoRows {
			return ErrInvalidCredentials
		}
		return fmt.Errorf("lookup user: %w", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(currentPassword)); err != nil {
		return ErrInvalidCredentials
	}
	newHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), hashCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin change password: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, string(newHash), userID); err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("clear sessions: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit change password: %w", err)
	}
	return nil
}

// ResetPassword sets a new password for username without needing the old
// one — the recovery path for a forgotten password, reachable only by
// someone with shell access to the container (cmd/app's reset-password
// subcommand), never over HTTP. Also drops every existing session for that
// user, so a stolen/forgotten-open session doesn't outlive the reset.
func (s *Service) ResetPassword(username, newPassword string) error {
	if len(newPassword) > MaxPasswordBytes {
		return ErrPasswordTooLong
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), hashCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	res, err := s.db.Exec(`UPDATE users SET password_hash = ? WHERE username = ?`, string(hash), username)
	if err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("no user named %q", username)
	}
	if _, err := s.db.Exec(`DELETE FROM sessions WHERE user_id = (SELECT id FROM users WHERE username = ?)`, username); err != nil {
		return fmt.Errorf("clear sessions: %w", err)
	}
	return nil
}

// CreateSession issues a new opaque session token, returning the raw token
// (only this call ever sees the plaintext — the DB stores its hash).
func (s *Service) CreateSession(userID int64) (token string, expiresAt time.Time, err error) {
	token, err = randomToken()
	if err != nil {
		return "", time.Time{}, err
	}
	expiresAt = time.Now().Add(sessionTTL)
	// Sessions nobody signed out of would otherwise stay in the table forever.
	_, _ = s.db.Exec(`DELETE FROM sessions WHERE expires_at < ?`, time.Now().UTC().Format(time.RFC3339Nano))
	_, err = s.db.Exec(`INSERT INTO sessions (token_hash, user_id, expires_at) VALUES (?, ?, ?)`,
		hashToken(token), userID, expiresAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("insert session: %w", err)
	}
	return token, expiresAt, nil
}

// UserForSession resolves a raw session token to its user, or
// ErrSessionNotFound if it's missing/expired.
func (s *Service) UserForSession(token string) (*User, error) {
	var (
		id       int64
		username string
		isAdmin  bool
		first    string
		last     string
		email    string
		expires  string
	)
	err := s.db.QueryRow(`
		SELECT u.id, u.username, u.is_admin, u.first_name, u.last_name, u.email, sess.expires_at
		FROM sessions sess JOIN users u ON u.id = sess.user_id
		WHERE sess.token_hash = ?`, hashToken(token)).
		Scan(&id, &username, &isAdmin, &first, &last, &email, &expires)
	if err == sql.ErrNoRows {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lookup session: %w", err)
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, expires)
	if err != nil {
		return nil, fmt.Errorf("parse session expiry: %w", err)
	}
	if time.Now().After(expiresAt) {
		_, _ = s.db.Exec(`DELETE FROM sessions WHERE token_hash = ?`, hashToken(token))
		return nil, ErrSessionNotFound
	}
	return &User{ID: id, Username: username, IsAdmin: isAdmin, FirstName: first, LastName: last, Email: email}, nil
}

// DeleteSession logs a single session out.
func (s *Service) DeleteSession(token string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE token_hash = ?`, hashToken(token))
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// CreateAPIKey generates a new API key for userID. The raw key is returned
// once and never recoverable afterward.
func (s *Service) CreateAPIKey(userID int64, name string) (rawKey string, err error) {
	rawKey, err = randomToken()
	if err != nil {
		return "", err
	}
	_, err = s.db.Exec(`INSERT INTO api_keys (user_id, name, key_hash) VALUES (?, ?, ?)`,
		userID, name, hashToken(rawKey))
	if err != nil {
		return "", fmt.Errorf("insert api key: %w", err)
	}
	return rawKey, nil
}

type APIKey struct {
	ID        int64
	Name      string
	CreatedAt time.Time
	RevokedAt *time.Time
}

// ListAPIKeys returns userID's API keys, active and revoked alike, newest
// first — never the raw key itself, which is only ever seen once, at
// creation (credentials are never exposed beyond what's needed).
func (s *Service) ListAPIKeys(userID int64) ([]APIKey, error) {
	rows, err := s.db.Query(`
		SELECT id, name, created_at, revoked_at FROM api_keys
		WHERE user_id = ? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	defer rows.Close()

	var keys []APIKey
	for rows.Next() {
		var (
			k          APIKey
			created    string
			revokedRaw sql.NullString
		)
		if err := rows.Scan(&k.ID, &k.Name, &created, &revokedRaw); err != nil {
			return nil, fmt.Errorf("scan api key: %w", err)
		}
		createdAt, err := time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return nil, fmt.Errorf("parse created_at: %w", err)
		}
		k.CreatedAt = createdAt
		if revokedRaw.Valid {
			revokedAt, err := time.Parse(time.RFC3339Nano, revokedRaw.String)
			if err != nil {
				return nil, fmt.Errorf("parse revoked_at: %w", err)
			}
			k.RevokedAt = &revokedAt
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// RevokeAPIKey marks id revoked, scoped to userID so one user can't revoke
// another's key by guessing an ID.
func (s *Service) RevokeAPIKey(userID, id int64) error {
	_, err := s.db.Exec(`UPDATE api_keys SET revoked_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE id = ? AND user_id = ? AND revoked_at IS NULL`, id, userID)
	if err != nil {
		return fmt.Errorf("revoke api key: %w", err)
	}
	return nil
}

// DeleteRevokedAPIKey removes a key that was already revoked from the list.
// An active key is never removed this way: it must be revoked first. It
// reports whether a key was removed.
func (s *Service) DeleteRevokedAPIKey(userID, id int64) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM api_keys WHERE id = ? AND user_id = ? AND revoked_at IS NOT NULL`, id, userID)
	if err != nil {
		return false, fmt.Errorf("delete api key: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("delete api key: %w", err)
	}
	return n > 0, nil
}

// UserForAPIKey resolves a raw API key header value to its user.
func (s *Service) UserForAPIKey(rawKey string) (*User, error) {
	var (
		id       int64
		username string
		isAdmin  bool
		first    string
		last     string
		email    string
	)
	err := s.db.QueryRow(`
		SELECT u.id, u.username, u.is_admin, u.first_name, u.last_name, u.email
		FROM api_keys k JOIN users u ON u.id = k.user_id
		WHERE k.key_hash = ? AND k.revoked_at IS NULL`, hashToken(rawKey)).
		Scan(&id, &username, &isAdmin, &first, &last, &email)
	if err == sql.ErrNoRows {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lookup api key: %w", err)
	}
	return &User{ID: id, Username: username, IsAdmin: isAdmin, FirstName: first, LastName: last, Email: email}, nil
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func isUniqueConstraintErr(err error) bool {
	// modernc.org/sqlite surfaces SQLite's own message text; matching on
	// substring avoids a hard dependency on its internal error type.
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique constraint")
}
