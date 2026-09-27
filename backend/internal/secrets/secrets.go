package secrets

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

type SecretStore interface {
	Put(ctx context.Context, scope, name string, plaintext []byte) error
	Get(ctx context.Context, scope, name string) ([]byte, error)
	Delete(ctx context.Context, scope, name string) error
}

type Cipher interface {
	Encrypt(plaintext, additionalData []byte) (nonce, ciphertext []byte, err error)
	Decrypt(nonce, ciphertext, additionalData []byte) ([]byte, error)
}

type AESGCM struct{ aead cipher.AEAD }

func NewAESGCMFromBase64(encoded string) (*AESGCM, error) {
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode master key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("master key must decode to exactly 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &AESGCM{aead: aead}, nil
}

func GenerateMasterKey() (string, error) {
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(key), nil
}

func (c *AESGCM) Encrypt(plaintext, additionalData []byte) ([]byte, []byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, err
	}
	ciphertext := c.aead.Seal(nil, nonce, plaintext, additionalData)
	return nonce, ciphertext, nil
}

func (c *AESGCM) Decrypt(nonce, ciphertext, additionalData []byte) ([]byte, error) {
	if len(nonce) != c.aead.NonceSize() {
		return nil, errors.New("invalid secret nonce size")
	}
	return c.aead.Open(nil, nonce, ciphertext, additionalData)
}
