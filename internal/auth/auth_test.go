package auth_test

import (
	"path/filepath"
	"testing"

	"github.com/ryanborg/mediarium/internal/auth"
	"github.com/ryanborg/mediarium/internal/store"
)

func newTestService(t *testing.T) *auth.Service {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "app.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return auth.New(db)
}

func TestFirstUserIsAdmin(t *testing.T) {
	svc := newTestService(t)

	needed, err := svc.FirstRunNeeded()
	if err != nil || !needed {
		t.Fatalf("expected first-run needed, got needed=%v err=%v", needed, err)
	}

	admin, err := svc.CreateUser("ryan", "hunter2hunter2")
	if err != nil {
		t.Fatalf("create first user: %v", err)
	}
	if !admin.IsAdmin {
		t.Fatal("expected first user to be admin")
	}

	second, err := svc.CreateUser("family", "anotherpassword")
	if err != nil {
		t.Fatalf("create second user: %v", err)
	}
	if second.IsAdmin {
		t.Fatal("expected second user to NOT be admin")
	}

	needed, err = svc.FirstRunNeeded()
	if err != nil || needed {
		t.Fatalf("expected first-run no longer needed, got needed=%v err=%v", needed, err)
	}
}

func TestAuthenticate(t *testing.T) {
	svc := newTestService(t)
	if _, err := svc.CreateUser("ryan", "correct-horse-battery"); err != nil {
		t.Fatalf("create user: %v", err)
	}

	if _, err := svc.Authenticate("ryan", "correct-horse-battery"); err != nil {
		t.Fatalf("expected successful auth, got %v", err)
	}
	if _, err := svc.Authenticate("ryan", "wrong-password"); err != auth.ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
	if _, err := svc.Authenticate("nobody", "whatever"); err != auth.ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials for unknown user, got %v", err)
	}
}

func TestDuplicateUsername(t *testing.T) {
	svc := newTestService(t)
	if _, err := svc.CreateUser("ryan", "password123"); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := svc.CreateUser("ryan", "different"); err != auth.ErrUsernameTaken {
		t.Fatalf("expected ErrUsernameTaken, got %v", err)
	}
}

func TestSessionLifecycle(t *testing.T) {
	svc := newTestService(t)
	user, err := svc.CreateUser("ryan", "password123")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	token, _, err := svc.CreateSession(user.ID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	got, err := svc.UserForSession(token)
	if err != nil {
		t.Fatalf("resolve session: %v", err)
	}
	if got.Username != "ryan" {
		t.Fatalf("expected username ryan, got %s", got.Username)
	}

	if err := svc.DeleteSession(token); err != nil {
		t.Fatalf("delete session: %v", err)
	}
	if _, err := svc.UserForSession(token); err != auth.ErrSessionNotFound {
		t.Fatalf("expected ErrSessionNotFound after delete, got %v", err)
	}
}

func TestChangePassword(t *testing.T) {
	svc := newTestService(t)
	user, err := svc.CreateUser("ryan", "original-password")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if err := svc.ChangePassword(user.ID, "wrong-current", "new-password123"); err != auth.ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials for wrong current password, got %v", err)
	}
	if _, err := svc.Authenticate("ryan", "original-password"); err != nil {
		t.Fatalf("expected original password to still work after failed change, got %v", err)
	}

	if err := svc.ChangePassword(user.ID, "original-password", "new-password123"); err != nil {
		t.Fatalf("change password: %v", err)
	}
	if _, err := svc.Authenticate("ryan", "new-password123"); err != nil {
		t.Fatalf("expected new password to work, got %v", err)
	}
	if _, err := svc.Authenticate("ryan", "original-password"); err != auth.ErrInvalidCredentials {
		t.Fatalf("expected old password to no longer work, got %v", err)
	}
}

func TestResetPassword(t *testing.T) {
	svc := newTestService(t)
	user, err := svc.CreateUser("ryan", "forgotten-password")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	token, _, err := svc.CreateSession(user.ID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	if err := svc.ResetPassword("ryan", "brand-new-password"); err != nil {
		t.Fatalf("reset password: %v", err)
	}
	if _, err := svc.Authenticate("ryan", "brand-new-password"); err != nil {
		t.Fatalf("expected the new password to work, got %v", err)
	}
	if _, err := svc.Authenticate("ryan", "forgotten-password"); err != auth.ErrInvalidCredentials {
		t.Fatalf("expected the old password to stop working, got %v", err)
	}
	if _, err := svc.UserForSession(token); err != auth.ErrSessionNotFound {
		t.Fatalf("expected existing sessions to be signed out by a reset, got %v", err)
	}
	if err := svc.ResetPassword("nobody", "whatever-password"); err == nil {
		t.Fatal("expected an error resetting a nonexistent user")
	}
}

func TestListAndRevokeAPIKeys(t *testing.T) {
	svc := newTestService(t)
	user, err := svc.CreateUser("ryan", "password123")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	other, err := svc.CreateUser("family", "anotherpassword")
	if err != nil {
		t.Fatalf("create second user: %v", err)
	}

	rawKey, err := svc.CreateAPIKey(user.ID, "my script")
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}

	keys, err := svc.ListAPIKeys(user.ID)
	if err != nil {
		t.Fatalf("list api keys: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keys))
	}
	if keys[0].Name != "my script" {
		t.Fatalf("expected name %q, got %q", "my script", keys[0].Name)
	}
	if keys[0].CreatedAt.IsZero() {
		t.Fatal("expected a non-zero CreatedAt")
	}
	if keys[0].RevokedAt != nil {
		t.Fatal("expected a freshly created key to not be revoked")
	}

	// A different user's key list must stay empty — no cross-user leakage.
	otherKeys, err := svc.ListAPIKeys(other.ID)
	if err != nil {
		t.Fatalf("list api keys for other user: %v", err)
	}
	if len(otherKeys) != 0 {
		t.Fatalf("expected the other user to have 0 keys, got %d", len(otherKeys))
	}

	// The other user can't revoke this user's key.
	if err := svc.RevokeAPIKey(other.ID, keys[0].ID); err != nil {
		t.Fatalf("revoke (wrong user, should no-op not error): %v", err)
	}
	if _, err := svc.UserForAPIKey(rawKey); err != nil {
		t.Fatalf("expected the key to still work after a wrong-user revoke attempt, got %v", err)
	}

	if err := svc.RevokeAPIKey(user.ID, keys[0].ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := svc.UserForAPIKey(rawKey); err != auth.ErrSessionNotFound {
		t.Fatalf("expected a revoked key to stop working, got %v", err)
	}

	keys, err = svc.ListAPIKeys(user.ID)
	if err != nil {
		t.Fatalf("list api keys after revoke: %v", err)
	}
	if len(keys) != 1 || keys[0].RevokedAt == nil {
		t.Fatalf("expected the revoked key to still be listed with a RevokedAt set, got %+v", keys)
	}
}

func TestAPIKeyLifecycle(t *testing.T) {
	svc := newTestService(t)
	user, err := svc.CreateUser("ryan", "password123")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	rawKey, err := svc.CreateAPIKey(user.ID, "test key")
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}

	got, err := svc.UserForAPIKey(rawKey)
	if err != nil {
		t.Fatalf("resolve api key: %v", err)
	}
	if got.Username != "ryan" {
		t.Fatalf("expected username ryan, got %s", got.Username)
	}

	if _, err := svc.UserForAPIKey("not-a-real-key"); err != auth.ErrSessionNotFound {
		t.Fatalf("expected ErrSessionNotFound for bogus key, got %v", err)
	}
}

func TestUpdateProfile(t *testing.T) {
	svc := newTestService(t)
	user, err := svc.CreateUserWithProfile("ryan", "password123", "Ryan", "Borg", "ryan@example.com")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if user.DisplayName() != "Ryan Borg" {
		t.Fatalf("display name = %q", user.DisplayName())
	}
	if _, err := svc.CreateUser("family", "anotherpassword"); err != nil {
		t.Fatalf("create second user: %v", err)
	}
	if _, err := svc.UpdateProfile(user.ID, "family", "Ryan", "Borg", ""); err != auth.ErrUsernameTaken {
		t.Fatalf("expected ErrUsernameTaken, got %v", err)
	}
	updated, err := svc.UpdateProfile(user.ID, "ryanb", "Ry", "B", "")
	if err != nil {
		t.Fatalf("update profile: %v", err)
	}
	if updated.Username != "ryanb" || updated.FirstName != "Ry" || updated.Email != "" {
		t.Fatalf("unexpected profile: %+v", updated)
	}
	if bare := (&auth.User{Username: "solo"}); bare.DisplayName() != "solo" {
		t.Fatalf("display name should fall back to the username, got %q", bare.DisplayName())
	}
}
