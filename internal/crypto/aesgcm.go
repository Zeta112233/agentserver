package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
)

// LoadKeyFromEnv reads a 32-byte AES-256 key from the named environment variable.
// Accepts standard base64, URL-safe base64, hex, or raw 32 bytes.
func LoadKeyFromEnv(envVar string) ([]byte, error) {
	raw := os.Getenv(envVar)
	if raw == "" {
		return nil, fmt.Errorf("%s is not set", envVar)
	}

	// Try hex (64-char hex string -> 32 bytes).
	if b, err := hex.DecodeString(raw); err == nil && len(b) == 32 {
		return b, nil
	}
	// Try standard base64.
	if b, err := base64.StdEncoding.DecodeString(raw); err == nil && len(b) == 32 {
		return b, nil
	}
	// Try URL-safe base64.
	if b, err := base64.URLEncoding.DecodeString(raw); err == nil && len(b) == 32 {
		return b, nil
	}

	return nil, fmt.Errorf("%s: expected 32-byte AES-256 key encoded as 64-char hex or 44-char base64; got %d characters", envVar, len(raw))
}

// Encrypt encrypts plaintext with AES-GCM-256.
// Output layout: [12-byte nonce][ciphertext][16-byte GCM tag].
func Encrypt(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize()) // 12 bytes
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	// Seal appends ciphertext+tag to nonce.
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt reverses Encrypt. Returns an error if the tag check fails.
func Decrypt(key, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize+gcm.Overhead() {
		return nil, fmt.Errorf("ciphertext too short: need at least %d bytes, got %d", nonceSize+gcm.Overhead(), len(ciphertext))
	}

	nonce, ct := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	return plaintext, nil
}
