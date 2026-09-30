package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/updatecheck"
)

// A key pair made for the test only.
func testKeys(t *testing.T) (seedB64, pubB64 string, pub ed25519.PublicKey) {
	t.Helper()
	p, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(priv.Seed()), base64.StdEncoding.EncodeToString(p), p
}

const sums = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  mediarium_1.2.0_linux_amd64.tar.gz\n"

// What signsums writes is exactly what the app's verifier accepts.
func TestSignatureVerifiesInTheApp(t *testing.T) {
	seed, pubB64, pub := testKeys(t)
	dir := t.TempDir()
	in, out := filepath.Join(dir, "sha256sums.txt"), filepath.Join(dir, "sha256sums.txt.sig")
	os.WriteFile(in, []byte(sums), 0o644)
	var stderr bytes.Buffer
	env := func(k string) string {
		if k == "UPDATE_SIGNING_KEY" {
			return seed
		}
		return ""
	}
	if err := run([]string{"-in", in, "-out", out, "-expect-pub", pubB64}, env, &bytes.Buffer{}, &stderr); err != nil {
		t.Fatalf("run: %v", err)
	}
	sig, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	signedBytes, _ := os.ReadFile(in)
	if err := updatecheck.VerifySignature(pub, signedBytes, string(sig)); err != nil {
		t.Fatalf("the app cannot verify what signsums wrote: %v", err)
	}
	// Change one byte of the list and it no longer verifies.
	if err := updatecheck.VerifySignature(pub, append(signedBytes, ' '), string(sig)); err == nil {
		t.Fatal("a changed list still verified")
	}
	// Another key does not verify it.
	_, _, other := testKeys(t)
	if err := updatecheck.VerifySignature(other, signedBytes, string(sig)); err == nil {
		t.Fatal("another key verified the signature")
	}
	if strings.Contains(stderr.String(), seed) {
		t.Fatal("the private seed was printed")
	}
	if !strings.Contains(stderr.String(), pubB64) {
		t.Fatalf("the public key should be reported: %q", stderr.String())
	}
	if !bytes.HasSuffix(sig, []byte("\n")) || bytes.Count(sig, []byte("\n")) != 1 {
		t.Fatalf("the signature file should be one line: %q", sig)
	}
}

func TestDefaultOutputName(t *testing.T) {
	seed, _, _ := testKeys(t)
	dir := t.TempDir()
	in := filepath.Join(dir, "sha256sums.txt")
	os.WriteFile(in, []byte(sums), 0o644)
	if err := run([]string{"-in", in}, func(string) string { return seed }, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(in + ".sig"); err != nil {
		t.Fatalf("default output not written: %v", err)
	}
}

func TestRefusals(t *testing.T) {
	seed, _, _ := testKeys(t)
	_, wrongPub, _ := testKeys(t)
	dir := t.TempDir()
	in := filepath.Join(dir, "sums")
	os.WriteFile(in, []byte(sums), 0o644)
	empty := filepath.Join(dir, "empty")
	os.WriteFile(empty, []byte("  \n"), 0o644)

	cases := []struct {
		name string
		args []string
		key  string
		want string
	}{
		{"no key", []string{"-in", in}, "", "not set"},
		{"key is not base64", []string{"-in", in}, "%%%not base64", "base64 of a 32-byte"},
		{"key is the wrong length", []string{"-in", in}, base64.StdEncoding.EncodeToString([]byte("short")), "base64 of a 32-byte"},
		{"a whole private key instead of the seed", []string{"-in", in}, base64.StdEncoding.EncodeToString(make([]byte, 64)), "base64 of a 32-byte"},
		{"missing input", []string{"-in", filepath.Join(dir, "nope")}, seed, "read"},
		{"empty input", []string{"-in", empty}, seed, "is empty"},
		{"another key than the app trusts", []string{"-in", in, "-expect-pub", wrongPub}, seed, "not the one Mediarium trusts"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := filepath.Join(dir, strings.ReplaceAll(tc.name, " ", "_")+".sig")
			args := append([]string{"-out", out}, tc.args...)
			var stderr bytes.Buffer
			err := run(args, func(string) string { return tc.key }, &bytes.Buffer{}, &stderr)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want one containing %q", err, tc.want)
			}
			if (tc.key != "" && strings.Contains(err.Error(), tc.key)) || strings.Contains(stderr.String(), seed) {
				t.Fatal("a secret was printed")
			}
			if _, statErr := os.Stat(out); statErr == nil {
				t.Fatal("a signature was written although signing was refused")
			}
		})
	}
}

func TestGenerate(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"-generate"}, func(string) string { return "" }, &out, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("output = %q", out.String())
	}
	seed := strings.TrimSpace(lines[0][strings.LastIndex(lines[0], ": ")+2:])
	pub := strings.TrimSpace(lines[1][strings.LastIndex(lines[1], ": ")+2:])
	sig, derived, err := Sign(seed, []byte("x"))
	if err != nil || derived != pub || sig == "" {
		t.Fatalf("the generated pair does not belong together: %v", err)
	}
}

// The public key built into the app is a real ed25519 key.
func TestShippedPublicKeyParses(t *testing.T) {
	src, err := os.ReadFile("../../cmd/app/main.go")
	if err != nil {
		t.Fatal(err)
	}
	const marker = `var updatePublicKey = "`
	i := strings.Index(string(src), marker)
	if i < 0 {
		t.Fatal("updatePublicKey not found in cmd/app/main.go")
	}
	rest := string(src)[i+len(marker):]
	key := rest[:strings.Index(rest, `"`)]
	if _, err := updatecheck.ParsePublicKey(key); err != nil {
		t.Fatalf("the public key in cmd/app/main.go is not usable: %v", err)
	}
}
