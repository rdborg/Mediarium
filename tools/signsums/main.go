// Command signsums signs the list of release checksums, so the app can prove
// that a release it downloads is the real one before it installs it.
//
//	UPDATE_SIGNING_KEY=<base64 seed> go run ./tools/signsums \
//	    -in release/sha256sums.txt -out release/sha256sums.txt.sig \
//	    -expect-pub <base64 public key>
//
// The private key never lives in the repository. It is the base64 of the
// 32-byte ed25519 seed, kept as the repository secret UPDATE_SIGNING_KEY and
// handed to this program in the environment (never on the command line, so it
// cannot show up in a process list or a log). The output is the base64 of the
// 64-byte signature over the exact bytes of the input file. Only the standard
// library is used.
//
// -expect-pub is the public key built into the app (cmd/app/main.go). When it
// is given and the key derived from the seed is a different one, nothing is
// written and the program fails: signatures no app could verify are worse than
// no signatures.
//
// Make a key pair with:  go run ./tools/signsums -generate
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

func main() {
	if err := run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "signsums:", err)
		os.Exit(1)
	}
}

func run(args []string, getenv func(string) string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("signsums", flag.ContinueOnError)
	fs.SetOutput(stderr)
	in := fs.String("in", "sha256sums.txt", "the checksum list to sign")
	out := fs.String("out", "", "where to write the signature (default: the input name + .sig)")
	expect := fs.String("expect-pub", "", "the public key the signature must verify against (base64)")
	generate := fs.Bool("generate", false, "make a new key pair and print both halves, then stop")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *generate {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return fmt.Errorf("generate a key: %w", err)
		}
		fmt.Fprintf(stdout, "UPDATE_SIGNING_KEY (private seed, keep secret, store as a GitHub secret): %s\n", base64.StdEncoding.EncodeToString(priv.Seed()))
		fmt.Fprintf(stdout, "public key (goes into cmd/app/main.go as updatePublicKey): %s\n", base64.StdEncoding.EncodeToString(pub))
		return nil
	}

	data, err := os.ReadFile(*in)
	if err != nil {
		return fmt.Errorf("read %s: %w", *in, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return fmt.Errorf("%s is empty", *in)
	}
	sig, pub, err := Sign(getenv("UPDATE_SIGNING_KEY"), data)
	if err != nil {
		return err
	}
	if *expect != "" && strings.TrimSpace(*expect) != pub {
		return errors.New("the signing key is not the one Mediarium trusts (the public keys differ), so nothing was signed. Check the UPDATE_SIGNING_KEY secret")
	}
	dest := *out
	if dest == "" {
		dest = *in + ".sig"
	}
	if err := os.WriteFile(dest, []byte(sig+"\n"), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", dest, err)
	}
	fmt.Fprintf(stderr, "signed %s with the key whose public half is %s\n", *in, pub)
	return nil
}

// Sign signs data with the ed25519 key made from seedB64 (base64 of 32 bytes)
// and returns the base64 signature and the base64 public key. Errors never
// contain the seed.
func Sign(seedB64 string, data []byte) (sig, pub string, err error) {
	seedB64 = strings.TrimSpace(seedB64)
	if seedB64 == "" {
		return "", "", errors.New("UPDATE_SIGNING_KEY is not set")
	}
	seed, err := base64.StdEncoding.DecodeString(seedB64)
	if err != nil || len(seed) != ed25519.SeedSize {
		return "", "", fmt.Errorf("UPDATE_SIGNING_KEY must be the base64 of a %d-byte ed25519 seed", ed25519.SeedSize)
	}
	priv := ed25519.NewKeyFromSeed(seed)
	pubKey := priv.Public().(ed25519.PublicKey)
	return base64.StdEncoding.EncodeToString(ed25519.Sign(priv, data)), base64.StdEncoding.EncodeToString(pubKey), nil
}
