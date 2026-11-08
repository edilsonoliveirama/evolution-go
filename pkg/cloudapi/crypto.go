package cloudapi

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

const (
	// nonceSize is the standard AES-GCM nonce size (12 bytes).
	nonceSize = 12
	// keySize is the required AES-256 key size.
	keySize = 32
)

// EncryptMetaCredential encrypts plaintext with AES-256-GCM using the given
// 32-byte key. It returns ciphertext = nonce (12 bytes) + encrypted data.
// Returns an error if plaintext is nil/empty or key is not 32 bytes.
func EncryptMetaCredential(plaintext, key []byte) ([]byte, error) {
	if len(plaintext) == 0 {
		return nil, errors.New("plaintext must not be empty")
	}
	if len(key) != keySize {
		return nil, fmt.Errorf("key must be %d bytes", keySize)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// DecryptMetaCredential decrypts ciphertext (nonce + encrypted data) with
// AES-256-GCM using the given 32-byte key. Returns the original plaintext.
func DecryptMetaCredential(ciphertext, key []byte) ([]byte, error) {
	if len(ciphertext) < nonceSize {
		return nil, errors.New("ciphertext too short")
	}
	if len(key) != keySize {
		return nil, fmt.Errorf("key must be %d bytes", keySize)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := ciphertext[:nonceSize]
	encrypted := ciphertext[nonceSize:]

	plaintext, err := gcm.Open(nil, nonce, encrypted, nil)
	if err != nil {
		return nil, fmt.Errorf("decryption failed: %w", err)
	}

	return plaintext, nil
}

// DecryptWithFallback tries to decrypt with currentKey first. If that fails
// and previousKey is non-nil, it tries previousKey. This supports zero-downtime
// key rotation.
func DecryptWithFallback(ciphertext, currentKey, previousKey []byte) ([]byte, error) {
	result, err := DecryptMetaCredential(ciphertext, currentKey)
	if err == nil {
		return result, nil
	}
	if len(previousKey) == keySize {
		result, err2 := DecryptMetaCredential(ciphertext, previousKey)
		if err2 == nil {
			return result, nil
		}
	}
	return nil, fmt.Errorf("decryption failed with all keys: %w", err)
}

// ValidateEncryptionKey checks that key is a valid 32-byte base64-encoded
// AES-256 key. Returns nil if valid, error otherwise.
func ValidateEncryptionKey(key []byte) error {
	if len(key) != keySize {
		return fmt.Errorf("encryption key must be %d bytes, got %d", keySize, len(key))
	}
	_, err := aes.NewCipher(key)
	return err
}

// GenerateEncryptionKey generates a random 32-byte AES-256 key and returns
// it as a base64-encoded string.
func GenerateEncryptionKey() (string, error) {
	key := make([]byte, keySize)
	if _, err := rand.Read(key); err != nil {
		return "", fmt.Errorf("failed to generate key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(key), nil
}
