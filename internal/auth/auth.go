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
	"runtime"
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
	// ErrNotLocal is returned when a password is set or changed on an account
	// that signs in through single sign-on or the directory.
	ErrNotLocal = errors.New("this account signs in through single sign-on or the directory and has no password here")
	// ErrAccountDisabled is returned for an account an administrator turned off.
	ErrAccountDisabled = errors.New("this account is turned off")
	ErrUsernameTaken   = errors.New("username already taken")
	ErrSessionNotFound = errors.New("session not found or expired")
	// ErrSessionIdle is ErrSessionNotFound for a session that ended because it
	// was not used for too long (Settings > People and access > Security). The
	// sign-in page can then say so.
	ErrSessionIdle = fmt.Errorf("%w: it was idle for too long", ErrSessionNotFound)
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
	// Viewer is the read-only role: browse, never change (see ReadOnly).
	Viewer bool
	// Disabled accounts cannot sign in and their sessions and keys stop working.
	Disabled bool
	// Source is where the password lives: SourceLocal, SourceOIDC or SourceLDAP.
	Source string
	// ReadOnlyKey is set for the length of a request made with a read-only
	// API key. It is never stored on the account.
	ReadOnlyKey bool
}

// Where an account's sign-in lives. Accounts made on a first sign-in through
// single sign-on or the directory have no password here.
const (
	SourceLocal = "local"
	SourceOIDC  = "oidc"
	SourceLDAP  = "ldap"
)

// Roles. is_admin in the users table is the source of truth for the
// administrator; is_viewer marks the read-only role. Role is the name the API
// and the docs use for it.
const (
	RoleAdmin  = "admin"  // everything, including settings and accounts
	RoleMember = "member" // browse, add titles and follow downloads; no settings
	RoleViewer = "viewer" // browse, discover and follow activity; changes nothing
)

// Role is RoleAdmin, RoleMember or RoleViewer.
func (u *User) Role() string {
	switch {
	case u.IsAdmin:
		return RoleAdmin
	case u.Viewer:
		return RoleViewer
	}
	return RoleMember
}

// ReadOnly reports whether the caller may only read: a viewer account, or a
// request made with a read-only API key. An administrator account is never
// read-only by role, only through a read-only key.
func (u *User) ReadOnly() bool {
	return u.ReadOnlyKey || (u.Viewer && !u.IsAdmin)
}

// IsLocal reports whether the account signs in with a password kept here.
func (u *User) IsLocal() bool { return u.Source == "" || u.Source == SourceLocal }

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
	// idle says how long a browser session may go unused before it ends; nil or
	// zero means no limit. clock is time.Now unless a test sets it.
	idle  func() time.Duration
	clock func() time.Time
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
	return &User{ID: id, Username: username, IsAdmin: firstRun, FirstName: firstName, LastName: lastName, Email: email, Source: SourceLocal}, nil
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
	return &User{ID: id, Username: username, IsAdmin: true, FirstName: firstName, LastName: lastName, Email: email, Source: SourceLocal}, nil
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
	_ = compareHash(dummyHash, password)
}

// bcryptSlots limits how many password checks run at once. A burst of sign-in
// attempts would otherwise queue enough bcrypt work to use every core for as
// long as the burst lasts; the rest wait their turn here instead.
var bcryptSlots = make(chan struct{}, max(2, runtime.NumCPU()))

// compareHash is bcrypt.CompareHashAndPassword that waits for a free slot.
func compareHash(hash []byte, password string) error {
	bcryptSlots <- struct{}{}
	defer func() { <-bcryptSlots }()
	return bcrypt.CompareHashAndPassword(hash, []byte(password))
}

// Authenticate verifies a username/password pair.
func (s *Service) Authenticate(username, password string) (*User, error) {
	u := &User{}
	var hash string
	err := s.db.QueryRow(`SELECT `+userCols+`, password_hash FROM users WHERE username = ?`, username).
		Scan(append(u.scanDest(), &hash)...)
	if err == sql.ErrNoRows {
		fakeCompare(password)
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, fmt.Errorf("lookup user: %w", err)
	}
	// An account that signs in through single sign-on or the directory has no
	// password here, so no password may ever open it, whatever is typed.
	if !u.IsLocal() || hash == "" {
		fakeCompare(password)
		return nil, ErrInvalidCredentials
	}
	if err := compareHash([]byte(hash), password); err != nil {
		return nil, ErrInvalidCredentials
	}
	if u.Disabled {
		return nil, ErrAccountDisabled
	}
	s.upgradeHash(u.ID, hash, password)
	return u, nil
}

// userCols are the columns every lookup of an account reads, in the order
// User.scanDest expects.
const userCols = `id, username, is_admin, first_name, last_name, email, is_viewer, disabled, auth_source`

// userColsOf is userCols for a table alias (a join).
func userColsOf(alias string) string {
	return alias + `.id, ` + alias + `.username, ` + alias + `.is_admin, ` + alias + `.first_name, ` + alias + `.last_name, ` + alias + `.email, ` + alias + `.is_viewer, ` + alias + `.disabled, ` + alias + `.auth_source`
}

func (u *User) scanDest() []any {
	return []any{&u.ID, &u.Username, &u.IsAdmin, &u.FirstName, &u.LastName, &u.Email, &u.Viewer, &u.Disabled, &u.Source}
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
	u := &User{}
	err := s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE id = ?`, id).Scan(u.scanDest()...)
	if err == sql.ErrNoRows {
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, fmt.Errorf("lookup user: %w", err)
	}
	return u, nil
}

// UserByName looks an account up by its user name, ignoring case.
// ErrInvalidCredentials when there is none.
func (s *Service) UserByName(username string) (*User, error) {
	u := &User{}
	err := s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE username = ? COLLATE NOCASE`, username).Scan(u.scanDest()...)
	if err == sql.ErrNoRows {
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, fmt.Errorf("lookup user by name: %w", err)
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
	// An address the person types themselves is not one an administrator
	// vouched for (see LocalUsersByEmail), unless it stays the same.
	_, err := s.db.Exec(`UPDATE users SET username = ?, first_name = ?, last_name = ?,
		email_by_admin = CASE WHEN lower(email) = lower(?) THEN email_by_admin ELSE 0 END, email = ? WHERE id = ?`,
		username, firstName, lastName, email, email, userID)
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
	var hash, source string
	if err := s.db.QueryRow(`SELECT password_hash, auth_source FROM users WHERE id = ?`, userID).Scan(&hash, &source); err != nil {
		if err == sql.ErrNoRows {
			return ErrInvalidCredentials
		}
		return fmt.Errorf("lookup user: %w", err)
	}
	if source != SourceLocal || hash == "" {
		return ErrNotLocal
	}
	if err := compareHash([]byte(hash), currentPassword); err != nil {
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
	if err := revokeUserGrants(tx.Exec, userID); err != nil { // apps signed in over MCP
		return err
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
	id, err := s.localIDByName(strings.TrimSpace(username))
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin reset password: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, string(hash), id); err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id = ?`, id); err != nil {
		return fmt.Errorf("clear sessions: %w", err)
	}
	if err := revokeUserGrants(tx.Exec, id); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit reset password: %w", err)
	}
	return nil
}

// localIDByName finds the account with a password here called username, in
// any letter case like signing in does. An older database can hold two names
// that differ only in case; the exact one wins then, and otherwise it is
// refused rather than guessed.
func (s *Service) localIDByName(username string) (int64, error) {
	rows, err := s.db.Query(`SELECT id, username FROM users WHERE username = ? COLLATE NOCASE AND auth_source = 'local'`, username)
	if err != nil {
		return 0, fmt.Errorf("look up account: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return 0, fmt.Errorf("look up account: %w", err)
		}
		if name == username {
			return id, nil
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("look up account: %w", err)
	}
	switch len(ids) {
	case 0:
		return 0, fmt.Errorf("no password account named %q (accounts that sign in through single sign-on or the directory have no password here)", username)
	case 1:
		return ids[0], nil
	}
	return 0, fmt.Errorf("more than one account is named %q in different letter case; type the name exactly as it is spelled", username)
}

// CreateSession issues a new opaque session token, returning the raw token
// (only this call ever sees the plaintext — the DB stores its hash).
func (s *Service) CreateSession(userID int64) (token string, expiresAt time.Time, err error) {
	return s.CreateSessionVia(userID, MethodPassword)
}

// How a session was started.
const (
	MethodPassword = "password"
	MethodOIDC     = "oidc"
	MethodLDAP     = "ldap"
	MethodPasskey  = "passkey"
	MethodProxy    = "proxy"
)

// CreateSessionVia is CreateSession that also records how the person signed in.
func (s *Service) CreateSessionVia(userID int64, method string) (token string, expiresAt time.Time, err error) {
	token, err = randomToken()
	if err != nil {
		return "", time.Time{}, err
	}
	now := s.now()
	expiresAt = now.Add(sessionTTL)
	// Sessions nobody signed out of would otherwise stay in the table forever.
	_, _ = s.db.Exec(`DELETE FROM sessions WHERE expires_at < ?`, now.UTC().Format(time.RFC3339Nano))
	_, err = s.db.Exec(`INSERT INTO sessions (token_hash, user_id, expires_at, method, last_used_at) VALUES (?, ?, ?, ?, ?)`,
		hashToken(token), userID, expiresAt.UTC().Format(time.RFC3339Nano), method, now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("insert session: %w", err)
	}
	return token, expiresAt, nil
}

// UserForSession resolves a raw session token to its user, or
// ErrSessionNotFound if it's missing/expired.
func (s *Service) UserForSession(token string) (*User, error) {
	u := &User{}
	var expires, created string
	var lastUsed, idleEnded sql.NullString
	err := s.db.QueryRow(`
		SELECT `+userColsOf("u")+`, sess.expires_at, sess.created_at, sess.last_used_at, sess.idle_ended_at
		FROM sessions sess JOIN users u ON u.id = sess.user_id
		WHERE sess.token_hash = ?`, hashToken(token)).
		Scan(append(u.scanDest(), &expires, &created, &lastUsed, &idleEnded)...)
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
	now := s.now()
	if now.After(expiresAt) {
		_, _ = s.db.Exec(`DELETE FROM sessions WHERE token_hash = ?`, hashToken(token))
		return nil, ErrSessionNotFound
	}
	// An account an administrator turned off is signed out at once.
	if u.Disabled {
		return nil, ErrSessionNotFound
	}
	if err := s.checkIdle(token, created, lastUsed, idleEnded, now); err != nil {
		return nil, err
	}
	return u, nil
}

// SessionMethod says how the session behind token was started ("password",
// "oidc", "ldap" or "passkey"), or "" when there is no such session.
func (s *Service) SessionMethod(token string) string {
	var m string
	if err := s.db.QueryRow(`SELECT method FROM sessions WHERE token_hash = ?`, hashToken(token)).Scan(&m); err != nil {
		return ""
	}
	return m
}

// SessionInfo is one place an account is signed in, as the profile page lists it.
type SessionInfo struct {
	CreatedAt time.Time
	ExpiresAt time.Time
	Method    string // how it was started: see MethodPassword and friends
	Current   bool   // the session making the request
}

// SessionsOf lists the unexpired sessions of userID, newest first. current is
// the raw token of the session asking, so it can be marked.
func (s *Service) SessionsOf(userID int64, current string) ([]SessionInfo, error) {
	rows, err := s.db.Query(`SELECT token_hash, created_at, expires_at, method, last_used_at, idle_ended_at FROM sessions WHERE user_id = ? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()
	mine := hashToken(current)
	var out []SessionInfo
	for rows.Next() {
		var hash, created, expires, method string
		var lastUsed, idleEnded sql.NullString
		if err := rows.Scan(&hash, &created, &expires, &method, &lastUsed, &idleEnded); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		exp, err := time.Parse(time.RFC3339Nano, expires)
		if err != nil || s.now().After(exp) {
			continue
		}
		if idleEnded.Valid || s.idleOver(created, lastUsed, s.now()) {
			continue // ended for being idle: not a place the person is signed in any more
		}
		made, _ := time.Parse(time.RFC3339Nano, created)
		out = append(out, SessionInfo{CreatedAt: made, ExpiresAt: exp, Method: method, Current: current != "" && hash == mine})
	}
	return out, rows.Err()
}

// DeleteOtherSessions signs userID out everywhere except the session with the
// raw token keep, and returns how many it ended.
func (s *Service) DeleteOtherSessions(userID int64, keep string) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM sessions WHERE user_id = ? AND token_hash != ?`, userID, hashToken(keep))
	if err != nil {
		return 0, fmt.Errorf("delete other sessions: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
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
	return s.CreateAPIKeyScoped(userID, name, false)
}

// CreateAPIKeyScoped is CreateAPIKey for a key that may be read-only: such a
// key only ever gets GET requests through.
func (s *Service) CreateAPIKeyScoped(userID int64, name string, readOnly bool) (rawKey string, err error) {
	rawKey, err = randomToken()
	if err != nil {
		return "", err
	}
	_, err = s.db.Exec(`INSERT INTO api_keys (user_id, name, key_hash, read_only) VALUES (?, ?, ?, ?)`,
		userID, name, hashToken(rawKey), readOnly)
	if err != nil {
		return "", fmt.Errorf("insert api key: %w", err)
	}
	return rawKey, nil
}

type APIKey struct {
	ID        int64
	Name      string
	ReadOnly  bool
	CreatedAt time.Time
	RevokedAt *time.Time
}

// ListAPIKeys returns userID's API keys, active and revoked alike, newest
// first — never the raw key itself, which is only ever seen once, at
// creation (credentials are never exposed beyond what's needed).
func (s *Service) ListAPIKeys(userID int64) ([]APIKey, error) {
	rows, err := s.db.Query(`
		SELECT id, name, read_only, created_at, revoked_at FROM api_keys
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
		if err := rows.Scan(&k.ID, &k.Name, &k.ReadOnly, &created, &revokedRaw); err != nil {
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
	u := &User{}
	var readOnly bool
	err := s.db.QueryRow(`
		SELECT `+userColsOf("u")+`, k.read_only
		FROM api_keys k JOIN users u ON u.id = k.user_id
		WHERE k.key_hash = ? AND k.revoked_at IS NULL`, hashToken(rawKey)).
		Scan(append(u.scanDest(), &readOnly)...)
	if err == sql.ErrNoRows {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lookup api key: %w", err)
	}
	if u.Disabled {
		return nil, ErrSessionNotFound
	}
	u.ReadOnlyKey = readOnly
	return u, nil
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
