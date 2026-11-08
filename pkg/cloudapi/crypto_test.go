package cloudapi

import (
	"bytes"
	"crypto/rand"
	"testing"
)

// AC: CLOUD-01 — Credentials encrypted at rest with AES-256-GCM.

// EncryptMetaCredential encrypts plaintext with AES-256-GCM using the given 32-byte key.
// Returns ciphertext (nonce + encrypted data).
// Expected signature when implemented:
//
//	func EncryptMetaCredential(plaintext, key []byte) ([]byte, error)
//	func DecryptMetaCredential(ciphertext, key []byte) ([]byte, error)
//	func GenerateEncryptionKey() (string, error)

func TestCrypto_EncryptDecryptRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("failed to generate test key: %v", err)
	}

	plaintext := []byte("EAAxTestAccessTokenValue12345")

	ciphertext, err := EncryptMetaCredential(plaintext, key)
	if err != nil {
		t.Fatalf("EncryptMetaCredential returned an error — expected success, got: %v", err)
	}

	if ciphertext == nil {
		t.Fatal("EncryptMetaCredential returned nil ciphertext")
	}

	if bytes.Equal(ciphertext, plaintext) {
		t.Fatal("ciphertext equals plaintext — encryption produced no change")
	}

	// Nonce is stored as prefix: first 12 bytes should be the random nonce.
	if len(ciphertext) < 13 {
		t.Fatalf("ciphertext too short (%d bytes) to contain nonce + data", len(ciphertext))
	}

	decrypted, err := DecryptMetaCredential(ciphertext, key)
	if err != nil {
		t.Fatalf("DecryptMetaCredential returned an error — expected success, got: %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("decrypted text %q does not match original %q", string(decrypted), string(plaintext))
	}
}

func TestCrypto_InvalidKeyLength(t *testing.T) {
	shortKey := []byte("too-short")
	plaintext := []byte("test-value")

	_, err := EncryptMetaCredential(plaintext, shortKey)
	if err == nil {
		t.Fatal("EncryptMetaCredential with 7-byte key should return an error (AES-256-GCM requires 32 bytes)")
	}

	ciphertext := make([]byte, 32) // arbitrary invalid ciphertext
	_, err = DecryptMetaCredential(ciphertext, shortKey)
	if err == nil {
		t.Fatal("DecryptMetaCredential with 7-byte key should return an error")
	}
}

func TestCrypto_TamperedCiphertext(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("failed to generate test key: %v", err)
	}

	plaintext := []byte("EAAxSensitiveValue")
	ciphertext, err := EncryptMetaCredential(plaintext, key)
	if err != nil {
		t.Fatalf("EncryptMetaCredential failed: %v", err)
	}

	// Tamper with a byte in the ciphertext (after the nonce prefix).
	tampered := make([]byte, len(ciphertext))
	copy(tampered, ciphertext)
	if len(tampered) > 12 {
		tampered[12] ^= 0xFF // flip bits in the first encrypted byte
	}

	_, err = DecryptMetaCredential(tampered, key)
	if err == nil {
		t.Fatal("DecryptMetaCredential should fail on tampered ciphertext (GCM authentication)")
	}
}

func TestCrypto_UniqueNonce(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("failed to generate test key: %v", err)
	}

	plaintext := []byte("same-value")
	seen := make(map[string]bool)

	for i := 0; i < 10; i++ {
		ciphertext, err := EncryptMetaCredential(plaintext, key)
		if err != nil {
			t.Fatalf("iteration %d: EncryptMetaCredential failed: %v", i, err)
		}

		// Nonce is the first 12 bytes (AES-256-GCM standard nonce size).
		if len(ciphertext) < 12 {
			t.Fatalf("iteration %d: ciphertext too short for nonce", i)
		}
		nonce := string(ciphertext[:12])
		if seen[nonce] {
			t.Fatalf("iteration %d: duplicate nonce detected — random nonce generation required", i)
		}
		seen[nonce] = true
	}
}

func TestCrypto_NilOrEmptyPlaintext(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("failed to generate test key: %v", err)
	}

	_, err := EncryptMetaCredential(nil, key)
	if err == nil {
		t.Fatal("EncryptMetaCredential with nil plaintext should return an error")
	}

	_, err = EncryptMetaCredential([]byte{}, key)
	if err == nil {
		t.Fatal("EncryptMetaCredential with empty plaintext should return an error")
	}
}

func TestGenerateEncryptionKey(t *testing.T) {
	keyStr, err := GenerateEncryptionKey()
	if err != nil {
		t.Fatalf("GenerateEncryptionKey returned an error: %v", err)
	}
	if keyStr == "" {
		t.Fatal("GenerateEncryptionKey returned empty string")
	}

	// Should produce a valid 32-byte base64 key.
	// The exact encoding is implementation-defined but must decode to 32 bytes.
	if len(keyStr) < 40 { // base64 of 32 bytes is at least 44 chars (with padding) or ~43 without
		t.Fatalf("GenerateEncryptionKey returned a key suspiciously short (%d chars)", len(keyStr))
	}
}
