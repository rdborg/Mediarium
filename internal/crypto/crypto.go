// Package crypto encrypts credentials (indexer API keys, download client
// passwords, VPN configs) at rest in the SQLite DB, per PRD.md §11.
//
// The encryption key lives in a separate file (secret.key, 0600) next to
// app.db, generated on first run — so the DB file alone is not sufficient
// to decrypt stored credentials.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
)

const keySize = 32 // AES-256

// Box encrypts/decrypts values with a fixed key loaded from disk.
type Box struct {
	key []byte
}

// parseKey decodes the on-disk secret.key format (base64 of a 32-byte key,
// no surrounding whitespace).
func parseKey(data []byte) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(string(data))
	if err != nil {
		return nil, fmt.Errorf("decode secret key: %w", err)
	}
	if len(key) != keySize {
		return nil, fmt.Errorf("secret key has unexpected length %d", len(key))
	}
	return key, nil
}

// ValidateKeyFile reports whether data is a well-formed secret.key file
// (exactly what LoadOrCreateKey would accept), without keeping the key.
func ValidateKeyFile(data []byte) error {
	_, err := parseKey(data)
	return err
}

// LoadOrCreateKey reads the encryption key from path, generating and
// persisting a new random one (0600) if it doesn't exist yet.
func LoadOrCreateKey(path string) (*Box, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		key, decErr := parseKey(data)
		if decErr != nil {
			return nil, fmt.Errorf("secret key at %s: %w", path, decErr)
		}
		return &Box{key: key}, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read secret key: %w", err)
	}

	key := make([]byte, keySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate secret key: %w", err)
	}
	encoded := base64.StdEncoding.EncodeToString(key)
	if err := os.WriteFile(path, []byte(encoded), 0o600); err != nil {
		return nil, fmt.Errorf("write secret key: %w", err)
	}
	return &Box{key: key}, nil
}

// Encrypt returns a base64-encoded nonce||ciphertext string.
func (b *Box) Encrypt(plaintext string) (string, error) {
	block, err := aes.NewCipher(b.key)
	if err != nil {
		return "", fmt.Errorf("new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("new gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt reverses Encrypt.
func (b *Box) Decrypt(encoded string) (string, error) {
	sealed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}
	block, err := aes.NewCipher(b.key)
	if err != nil {
		return "", fmt.Errorf("new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("new gcm: %w", err)
	}
	if len(sealed) < gcm.NonceSize() {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ct := sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return string(plaintext), nil
}
