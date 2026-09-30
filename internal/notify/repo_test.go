package notify_test

import (
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/notify"
	"github.com/rdborg/mediarium/internal/store"
)

func newTestRepo(t *testing.T) *notify.Repo {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	box, err := crypto.LoadOrCreateKey(filepath.Join(dir, "secret.key"))
	if err != nil {
		t.Fatalf("load key: %v", err)
	}
	return notify.NewRepo(db, box)
}

func TestRepoCreateListDelete(t *testing.T) {
	repo := newTestRepo(t)

	webhook, err := repo.Create(notify.Target{Name: "My Webhook", Type: notify.TargetWebhook, URL: "https://example.com/hook"})
	if err != nil {
		t.Fatalf("create webhook: %v", err)
	}
	_, err = repo.Create(notify.Target{Name: "My Bot", Type: notify.TargetTelegram, BotToken: "secret-token", ChatID: "12345"})
	if err != nil {
		t.Fatalf("create telegram: %v", err)
	}

	list, err := repo.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(list))
	}
	for _, tgt := range list {
		if tgt.Type == notify.TargetTelegram && tgt.BotToken != "secret-token" {
			t.Fatalf("expected decrypted bot token round-trip, got %q", tgt.BotToken)
		}
	}

	senders, err := repo.Senders()
	if err != nil {
		t.Fatalf("senders: %v", err)
	}
	if len(senders) != 2 {
		t.Fatalf("expected 2 senders, got %d", len(senders))
	}

	if err := repo.Delete(webhook.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	list, _ = repo.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 target after delete, got %d", len(list))
	}
}
