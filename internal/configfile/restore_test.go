package configfile

import (
	"errors"
	"strings"
	"testing"
)

func noEnvTest(string) (string, bool) { return "", false }

func TestParseFormatVersion(t *testing.T) {
	cases := []struct {
		name, file, wantErr string
		wantVersion         int
	}{
		{"no version is fine", "settings:\n  trashDays: 5\n", "", 0},
		{"the current one", "formatVersion: 1\nsettings:\n  trashDays: 5\n", "", 1},
		{"a newer one", "formatVersion: 2\nsettings:\n  trashDays: 5\n", "newer version of Mediarium", 0},
		{"zero", "formatVersion: 0\n", "whole number", 0},
		{"text", "formatVersion: one\n", "whole number", 0},
		{"a decimal", "formatVersion: 1.5\n", "whole number", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := Parse([]byte(tc.file), noEnvTest)
			if tc.wantErr == "" {
				if err != nil || f.Version != tc.wantVersion {
					t.Fatalf("version %d, err %v", f.Version, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want %q, got %v", tc.wantErr, err)
			}
		})
	}
	var newer *NewerFormatError
	if _, err := Parse([]byte("formatVersion: 9\n"), noEnvTest); !errors.As(err, &newer) || newer.Version != 9 {
		t.Fatalf("a newer file is a NewerFormatError: %v", err)
	}
}

func TestParseTakesSecretPlaceholders(t *testing.T) {
	f, err := Parse([]byte(`
settings:
  tmdbApiKey: <secret>
  trashDays: 5
notifications:
  - name: Mail
    type: email
    config:
      host: smtp.example.com
      password: <secret>
usenetServers:
  - name: Main
    password: <secret>
    host: news.example.com
indexers: []
`), noEnvTest)
	if err != nil {
		t.Fatal(err)
	}
	if _, has := f.Blocks["settings"]["tmdbApiKey"]; has {
		t.Fatal("a <secret> in a block is dropped: it means keep the one saved")
	}
	mail := f.Lists["notifications"][0]["config"].(map[string]any)
	if _, has := mail["password"]; has || mail["host"] != "smtp.example.com" {
		t.Fatalf("a <secret> inside config is dropped: %v", mail)
	}
	if got := f.Secrets[secretKey("notifications", "mail")]; len(got) != 1 || got[0] != "config.password" {
		t.Fatalf("where the secret was: %v", f.Secrets)
	}
	if got := f.Secrets[secretKey("usenetServers", "Main")]; len(got) != 1 || got[0] != "password" {
		t.Fatalf("where the secret was: %v", f.Secrets)
	}
	if items, present := f.Lists["indexers"]; !present || len(items) != 0 {
		t.Fatalf("an empty list is kept, it means remove everything in replace mode: %v", f.Lists)
	}
}

func TestValueProblem(t *testing.T) {
	cases := []struct {
		name       string
		key        string
		have, want any
		problem    string
	}{
		{"number for a number", "days", float64(7), 14, ""},
		{"text for a number", "days", float64(7), "lots", "should be a number"},
		{"number for text", "path", "/a", 5, "should be text"},
		{"text for true or false", "on", true, "yes", "should be true or false"},
		{"a list for a list", "allowed", []any{"a"}, []any{"b"}, ""},
		{"text for a list", "allowed", []any{"a"}, "b", "should be a list"},
		{"anything where Mediarium shows nothing", "x", nil, 5, ""},
		{"priority may be a word", "priority", float64(25), "preferred", ""},
		{"a web address", "baseUrl", "", "https://indexer.example.com", ""},
		{"a web address without the scheme", "baseUrl", "", "indexer.example.com", "isn't a web address"},
		{"a web address with another scheme", "url", "", "ftp://x.example.com", "isn't a web address"},
		{"an empty web address clears it", "url", "http://x", "", ""},
		{"a web address that is a number", "publicUrl", "", 5, "should be text"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := valueProblem("section."+tc.key, tc.key, tc.have, tc.want)
			if tc.problem == "" && got != "" || tc.problem != "" && !strings.Contains(got, tc.problem) {
				t.Fatalf("want %q, got %q", tc.problem, got)
			}
		})
	}
}

func TestChangeText(t *testing.T) {
	cases := []struct {
		c    Change
		want string
	}{
		{Change{Section: "indexers[A]", New: true}, "indexers[A]: will be added"},
		{Change{Section: "indexers[A]", Removed: true}, "indexers[A]: will be removed"},
		{Change{Section: "settings", Key: "trashDays", Have: "7", Want: "14"}, "settings.trashDays: 7 -> 14"},
		{Change{Section: "proxy", Key: "password", Secret: true}, "proxy.password: (secret) will be set"},
	}
	for _, tc := range cases {
		if got := tc.c.String(); got != tc.want {
			t.Errorf("got %q, want %q", got, tc.want)
		}
	}
}

func TestUndoBodyLeavesSecretsAlone(t *testing.T) {
	ls, _ := listByName("indexers")
	have := map[string]any{"name": "A", "baseUrl": "https://old.example.com", "priority": float64(25), "settings": map[string]any{"site": "x"}}
	body := map[string]any{"baseUrl": "https://new.example.com", "apiKey": "new-key", "settings": map[string]any{"site": "y", "cookie": "c"}}
	back, lost := undoBody(ls, have, body)
	if back["baseUrl"] != "https://old.example.com" {
		t.Fatalf("the address goes back: %v", back)
	}
	if _, has := back["apiKey"]; has {
		t.Fatalf("a key can't be put back and is not sent: %v", back)
	}
	if s := back["settings"].(map[string]any); s["site"] != "x" || len(s) != 1 {
		t.Fatalf("the site's own settings go back: %v", s)
	}
	if lost != 2 {
		t.Fatalf("the key and the cookie can't be put back, lost = %d", lost)
	}
}
