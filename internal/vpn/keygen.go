package vpn

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"golang.org/x/crypto/curve25519"
)

// KeyPair is a WireGuard key pair, base64-encoded the same way a
// wg-quick config file or `wg genkey`/`wg pubkey` would produce — this is
// what the Settings UI would generate for a user picking "generate a new
// key" (or, more commonly for this app, what gets pasted in from a VPN
// provider's own dashboard).
type KeyPair struct {
	PrivateKey string
	PublicKey  string
}

// GenerateKeyPair creates a new random X25519 key pair.
func GenerateKeyPair() (KeyPair, error) {
	var priv [32]byte
	if _, err := rand.Read(priv[:]); err != nil {
		return KeyPair{}, fmt.Errorf("generate private key: %w", err)
	}
	// Standard X25519 clamping (RFC 7748 §5).
	priv[0] &= 248
	priv[31] &= 127
	priv[31] |= 64

	pub, err := curve25519.X25519(priv[:], curve25519.Basepoint)
	if err != nil {
		return KeyPair{}, fmt.Errorf("derive public key: %w", err)
	}

	return KeyPair{
		PrivateKey: base64.StdEncoding.EncodeToString(priv[:]),
		PublicKey:  base64.StdEncoding.EncodeToString(pub),
	}, nil
}
