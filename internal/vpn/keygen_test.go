package vpn_test

import (
	"encoding/base64"
	"testing"

	"github.com/rdborg/mediarium/internal/vpn"
)

func TestGenerateKeyPair(t *testing.T) {
	kp, err := vpn.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key pair: %v", err)
	}
	priv, err := base64.StdEncoding.DecodeString(kp.PrivateKey)
	if err != nil || len(priv) != 32 {
		t.Fatalf("expected a valid 32-byte base64 private key, got %q (err=%v)", kp.PrivateKey, err)
	}
	pub, err := base64.StdEncoding.DecodeString(kp.PublicKey)
	if err != nil || len(pub) != 32 {
		t.Fatalf("expected a valid 32-byte base64 public key, got %q (err=%v)", kp.PublicKey, err)
	}

	kp2, err := vpn.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate second key pair: %v", err)
	}
	if kp.PrivateKey == kp2.PrivateKey {
		t.Fatal("expected two independently generated key pairs to differ")
	}
}
