// Package auth implements GitHub OAuth + session cookies + an allowlist
// gate so stackit-server can be safely exposed on the public web.
//
// The package is internal to the api layer: handlers, middleware, and
// cryptographic helpers live here together so the surface a consumer
// composes is just (Config, Store, Handler, Allowlist, RequireSession).
package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

// SessionKeyBytes is the required key length for the AES-GCM cipher.
const SessionKeyBytes = 32

// Cipher AES-GCM-encrypts short blobs (currently the user's GitHub access
// token) before they sit in the in-memory session store. The key comes from
// STACKIT_SESSION_KEY; rotation requires a process restart, which also
// invalidates every existing session — that's fine for the in-memory store.
type Cipher struct {
	aead cipher.AEAD
}

// NewCipher decodes a base64-encoded 32-byte key and returns a ready
// AES-256-GCM cipher.
func NewCipher(base64Key string) (*Cipher, error) {
	if base64Key == "" {
		return nil, errors.New("session key is empty")
	}
	key, err := base64.StdEncoding.DecodeString(base64Key)
	if err != nil {
		// Tolerate URL-safe base64 too; some operators paste the openssl
		// output without thinking about the alphabet.
		key, err = base64.RawStdEncoding.DecodeString(base64Key)
		if err != nil {
			return nil, fmt.Errorf("session key is not valid base64: %w", err)
		}
	}
	if len(key) != SessionKeyBytes {
		return nil, fmt.Errorf("session key must decode to %d bytes (got %d)", SessionKeyBytes, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

// Seal encrypts plaintext with a fresh random nonce. The returned slice is
// nonce || ciphertext+tag; the caller stores it as-is.
func (c *Cipher) Seal(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("nonce: %w", err)
	}
	return c.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// Open reverses Seal. It rejects anything shorter than a nonce and surfaces
// AEAD verification failures verbatim.
func (c *Cipher) Open(blob []byte) ([]byte, error) {
	ns := c.aead.NonceSize()
	if len(blob) < ns {
		return nil, errors.New("ciphertext too short")
	}
	nonce, ct := blob[:ns], blob[ns:]
	return c.aead.Open(nil, nonce, ct, nil)
}

// GenerateSessionKey returns a fresh base64-encoded key in the format the
// server expects. The operator runs `openssl rand -base64 32` in
// docs/deploy.md; this is the in-Go equivalent used by tests.
func GenerateSessionKey() (string, error) {
	key := make([]byte, SessionKeyBytes)
	if _, err := rand.Read(key); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(key), nil
}
