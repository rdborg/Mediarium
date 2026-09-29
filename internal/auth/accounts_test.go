package auth_test

import (
	"errors"
	"testing"

	"github.com/ryanborg/mediarium/internal/auth"
)

func ptr[T any](v T) *T { return &v }

func TestAccountLifecycle(t *testing.T) {
	svc := newTestService(t)
	admin, err := svc.CreateUser("ryan", "hunter2hunter2")
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	kid, err := svc.CreateAccount("sam", "longenoughpw", "Sam", "", "", false)
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	if kid.IsAdmin || kid.Role() != auth.RoleMember || kid.LastLoginAt != nil || kid.CreatedAt.IsZero() {
		t.Fatalf("unexpected new member: %+v", kid)
	}
	if _, err := svc.CreateAccount("sam", "longenoughpw", "", "", "", false); !errors.Is(err, auth.ErrUsernameTaken) {
		t.Fatalf("duplicate username: want ErrUsernameTaken, got %v", err)
	}

	// Signing in stamps last_login_at.
	if _, err := svc.Authenticate("sam", "longenoughpw"); err != nil {
		t.Fatalf("member login: %v", err)
	}
	if err := svc.RecordLogin(kid.ID); err != nil {
		t.Fatalf("record login: %v", err)
	}
	got, err := svc.GetAccount(kid.ID)
	if err != nil || got.LastLoginAt == nil {
		t.Fatalf("expected a last login time, got %+v err=%v", got, err)
	}

	// An admin password reset signs the member out everywhere.
	token, _, err := svc.CreateSession(kid.ID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := svc.UpdateAccount(kid.ID, auth.AccountUpdate{Password: ptr("brand-new-password")}); err != nil {
		t.Fatalf("reset password: %v", err)
	}
	if _, err := svc.UserForSession(token); !errors.Is(err, auth.ErrSessionNotFound) {
		t.Fatalf("session should be gone after a reset, got %v", err)
	}
	if _, err := svc.Authenticate("sam", "brand-new-password"); err != nil {
		t.Fatalf("login with the reset password: %v", err)
	}

	// Deleting removes the account, its sessions and its API keys.
	key, err := svc.CreateAPIKey(kid.ID, "phone")
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}
	if err := svc.DeleteAccount(kid.ID); err != nil {
		t.Fatalf("delete member: %v", err)
	}
	if _, err := svc.UserForAPIKey(key); err == nil {
		t.Fatal("api key of a deleted account must stop working")
	}
	if _, err := svc.GetAccount(kid.ID); !errors.Is(err, auth.ErrUserNotFound) {
		t.Fatalf("deleted account: want ErrUserNotFound, got %v", err)
	}
	if err := svc.DeleteAccount(kid.ID); !errors.Is(err, auth.ErrUserNotFound) {
		t.Fatalf("second delete: want ErrUserNotFound, got %v", err)
	}
	_ = admin
}

func TestThereIsAlwaysAnAdmin(t *testing.T) {
	svc := newTestService(t)
	first, err := svc.CreateUser("ryan", "hunter2hunter2")
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	second, err := svc.CreateAccount("partner", "hunter2hunter2", "", "", "", true)
	if err != nil {
		t.Fatalf("create second admin: %v", err)
	}

	cases := []struct {
		name string
		run  func() error
		want error
	}{
		{"demote one of two admins", func() error {
			_, err := svc.UpdateAccount(second.ID, auth.AccountUpdate{IsAdmin: ptr(false)})
			return err
		}, nil},
		{"demote the last admin", func() error {
			_, err := svc.UpdateAccount(first.ID, auth.AccountUpdate{IsAdmin: ptr(false)})
			return err
		}, auth.ErrLastAdmin},
		{"delete the last admin", func() error { return svc.DeleteAccount(first.ID) }, auth.ErrLastAdmin},
		{"promote back", func() error {
			_, err := svc.UpdateAccount(second.ID, auth.AccountUpdate{IsAdmin: ptr(true)})
			return err
		}, nil},
		{"delete one of two admins", func() error { return svc.DeleteAccount(first.ID) }, nil},
		{"update a missing account", func() error {
			_, err := svc.UpdateAccount(9999, auth.AccountUpdate{Email: ptr("x@example.com")})
			return err
		}, auth.ErrUserNotFound},
	}
	for _, tc := range cases {
		if err := tc.run(); !errors.Is(err, tc.want) {
			t.Fatalf("%s: want %v, got %v", tc.name, tc.want, err)
		}
	}
	list, err := svc.ListAccounts()
	if err != nil || len(list) != 1 || list[0].ID != second.ID || !list[0].IsAdmin {
		t.Fatalf("expected only the second admin to remain, got %+v err=%v", list, err)
	}
}
