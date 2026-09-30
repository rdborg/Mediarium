package crypto

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func newBox(t *testing.T) (*Box, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "secret.key")
	b, err := LoadOrCreateKey(path)
	if err != nil {
		t.Fatal(err)
	}
	return b, path
}

func TestEncryptRoundTripAndFreshNonce(t *testing.T) {
	b, _ := newBox(t)
	const secret = "usenet-password-with-ünïcode and spaces"
	a, err := b.Encrypt(secret)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := b.Encrypt(secret)
	if a == c {
		t.Fatal("two encryptions of the same value are identical: the nonce is being reused")
	}
	if strings.Contains(a, "usenet") {
		t.Fatalf("the ciphertext shows the plain text: %q", a)
	}
	got, err := b.Decrypt(a)
	if err != nil || got != secret {
		t.Fatalf("Decrypt = %q, %v; want %q", got, err, secret)
	}
	if empty, _ := b.Encrypt(""); empty == "" {
		t.Fatal("an empty value should still produce a (sealed) result")
	}
}

func TestDecryptRefusesTamperedOrForeignData(t *testing.T) {
	b, _ := newBox(t)
	other, _ := newBox(t)
	enc, _ := b.Encrypt("secret")
	raw := []byte(enc)
	raw[len(raw)-3] ^= 1 // flip a bit in the base64 text
	for name, in := range map[string]string{
		"tampered":   string(raw),
		"not base64": "%%%%",
		"too short":  "AAAA",
		"empty":      "",
	} {
		if _, err := b.Decrypt(in); err == nil {
			t.Errorf("%s: Decrypt accepted %q", name, in)
		}
	}
	if _, err := other.Decrypt(enc); err == nil {
		t.Error("a value encrypted with one key was decrypted with another")
	}
}

func TestKeyFileIsPrivateAndReloads(t *testing.T) {
	b, path := newBox(t)
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("secret.key has mode %v, want 0600", fi.Mode().Perm())
		}
	}
	enc, _ := b.Encrypt("x")
	again, err := LoadOrCreateKey(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := again.Decrypt(enc); err != nil || got != "x" {
		t.Fatalf("a reloaded key does not decrypt: %q, %v", got, err)
	}
}

// A key file an editor added a newline to still loads.
func TestKeyFileWithTrailingNewline(t *testing.T) {
	b, path := newBox(t)
	data, _ := os.ReadFile(path)
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	enc, _ := b.Encrypt("x")
	again, err := LoadOrCreateKey(path)
	if err != nil {
		t.Fatalf("key with a newline: %v", err)
	}
	if got, err := again.Decrypt(enc); err != nil || got != "x" {
		t.Fatalf("Decrypt = %q, %v", got, err)
	}
	if err := ValidateKeyFile(append(data, '\r', '\n')); err != nil {
		t.Fatalf("ValidateKeyFile with CRLF: %v", err)
	}
}

func TestBadKeyFilesAreRefused(t *testing.T) {
	for name, content := range map[string]string{"not base64": "%%%", "too short": "AAAA", "empty": ""} {
		path := filepath.Join(t.TempDir(), "secret.key")
		os.WriteFile(path, []byte(content), 0o600)
		if _, err := LoadOrCreateKey(path); err == nil {
			t.Errorf("%s: a bad key file was accepted", name)
		}
	}
}
