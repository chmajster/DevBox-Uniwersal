package secrets

import (
	"bytes"
	"testing"
)

func TestAESGCMRoundTrip(t *testing.T) {
	key, err := GenerateMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewAESGCMFromBase64(key)
	if err != nil {
		t.Fatal(err)
	}
	nonce, ciphertext, err := c.Encrypt([]byte("top-secret"), []byte("scope\x00name"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ciphertext, []byte("top-secret")) {
		t.Fatal("ciphertext contains plaintext")
	}
	plain, err := c.Decrypt(nonce, ciphertext, []byte("scope\x00name"))
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != "top-secret" {
		t.Fatalf("unexpected plaintext %q", plain)
	}
}
