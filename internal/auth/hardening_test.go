package auth_test

import (
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/rdborg/mediarium/internal/auth"
)

func storedHash(t *testing.T, svc *auth.Service, id int64) string {
	t.Helper()
	h := svc.StoredHashForTest(id)
	if h == "" {
		t.Fatal("no stored hash")
	}
	return h
}

func TestCreateFirstAdminOnlyOnce(t *testing.T) {
	svc := newTestService(t)
	u, err := svc.CreateFirstAdmin("ryan", "password123", "", "", "")
	if err != nil || !u.IsAdmin {
		t.Fatalf("first admin: %+v %v", u, err)
	}
	if _, err := svc.CreateFirstAdmin("other", "password123", "", "", ""); err != auth.ErrSetupDone {
		t.Fatalf("second first-admin: got %v, want ErrSetupDone", err)
	}
}

func TestChangePasswordSignsEverySessionOut(t *testing.T) {
	svc := newTestService(t)
	u, _ := svc.CreateUser("ryan", "original-password")
	tok, _, _ := svc.CreateSession(u.ID)
	if err := svc.ChangePassword(u.ID, "original-password", "new-password123"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UserForSession(tok); err != auth.ErrSessionNotFound {
		t.Fatalf("session survived the password change: %v", err)
	}
}

func TestOverlongPasswordsAreRefusedNotTruncated(t *testing.T) {
	svc := newTestService(t)
	long := strings.Repeat("x", auth.MaxPasswordBytes+1)
	if _, err := svc.CreateUser("ryan", long); err != auth.ErrPasswordTooLong {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := svc.CreateFirstAdmin("ryan", long, "", "", ""); err != auth.ErrPasswordTooLong {
		t.Fatalf("CreateFirstAdmin: %v", err)
	}
	u, _ := svc.CreateUser("ryan", "original-password")
	if err := svc.ChangePassword(u.ID, "original-password", long); err != auth.ErrPasswordTooLong {
		t.Fatalf("ChangePassword: %v", err)
	}
}

func TestAuthenticateUnknownUserLooksLikeWrongPassword(t *testing.T) {
	svc := newTestService(t)
	if _, err := svc.CreateUser("ryan", "original-password"); err != nil {
		t.Fatal(err)
	}
	_, e1 := svc.Authenticate("nobody", "whatever-password")
	_, e2 := svc.Authenticate("ryan", "whatever-password")
	if e1 != auth.ErrInvalidCredentials || e2 != auth.ErrInvalidCredentials {
		t.Fatalf("errors differ: %v / %v", e1, e2)
	}
}

// A hash made with a lower work factor still works, and is replaced by one
// with the current factor when its owner signs in.
func TestOldPasswordHashIsUpgradedOnSignIn(t *testing.T) {
	svc := newTestService(t)
	restore := auth.SetHashCostForTest(4)
	u, err := svc.CreateUser("ryan", "original-password")
	restore()
	if err != nil {
		t.Fatal(err)
	}
	before := storedHash(t, svc, u.ID)
	if cost, _ := bcrypt.Cost([]byte(before)); cost != 4 {
		t.Fatalf("setup: cost %d, want 4", cost)
	}
	defer auth.SetHashCostForTest(6)()
	if _, err := svc.Authenticate("ryan", "wrong-password-here"); err != auth.ErrInvalidCredentials {
		t.Fatalf("wrong password: %v", err)
	}
	if got := storedHash(t, svc, u.ID); got != before {
		t.Fatal("a wrong password must not change the stored hash")
	}
	if _, err := svc.Authenticate("ryan", "original-password"); err != nil {
		t.Fatal(err)
	}
	after := storedHash(t, svc, u.ID)
	if cost, _ := bcrypt.Cost([]byte(after)); cost != 6 {
		t.Fatalf("after sign-in: cost %d, want 6", cost)
	}
	if _, err := svc.Authenticate("ryan", "original-password"); err != nil {
		t.Fatalf("the upgraded hash must still accept the password: %v", err)
	}
}

// Two names that differ only in letter case would look like the same person
// in the activity log, so the second one is refused.
func TestUsernamesAreUniqueIgnoringCase(t *testing.T) {
	svc := newTestService(t)
	admin, err := svc.CreateUser("Ryan", "original-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateAccount("ryan", "original-password", "", "", "", false); err != auth.ErrUsernameTaken {
		t.Fatalf("CreateAccount with another letter case: %v, want ErrUsernameTaken", err)
	}
	other, err := svc.CreateAccount("sam", "original-password", "", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateProfile(other.ID, "RYAN", "", "", ""); err != auth.ErrUsernameTaken {
		t.Fatalf("UpdateProfile to another letter case: %v, want ErrUsernameTaken", err)
	}
	// Changing the letter case of your own name is fine.
	if _, err := svc.UpdateProfile(admin.ID, "ryan", "", "", ""); err != nil {
		t.Fatalf("own name in another case: %v", err)
	}
}
