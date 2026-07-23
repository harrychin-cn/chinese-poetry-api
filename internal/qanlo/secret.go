package qanlo

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
)

// SecretBox encrypts customer Qanlo Agent Keys before persistence.
type SecretBox struct{ aead cipher.AEAD }

// NewSecretBox accepts a base64-encoded 32-byte operator key.
func NewSecretBox(encoded string) (*SecretBox, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("QANLO_KEY_ENCRYPTION_KEY must be base64-encoded 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &SecretBox{aead: aead}, nil
}
func (b *SecretBox) Seal(plain string) (string, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(append(nonce, b.aead.Seal(nil, nonce, []byte(plain), nil)...)), nil
}
func (b *SecretBox) Open(value string) (string, error) {
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return "", err
	}
	n := b.aead.NonceSize()
	if len(raw) < n {
		return "", fmt.Errorf("invalid encrypted Qanlo key")
	}
	plain, err := b.aead.Open(nil, raw[:n], raw[n:], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
