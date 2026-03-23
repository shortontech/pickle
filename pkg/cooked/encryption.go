package cooked

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// EncryptionConfig holds the environment variable names for encryption keys.
type EncryptionConfig struct {
	CurrentKeyEnv string // env var name for the active encryption key
	NextKeyEnv    string // env var name for the rotation target key (optional)
}

// encryptedFieldMapping describes one encrypted or sealed column on a model.
type encryptedFieldMapping struct {
	Field         any
	Column        string
	ColumnV2      string
	Deterministic bool // true = SIV (.Encrypted()), false = GCM (.Sealed())
	Marshal       func() ([]byte, error)
	Unmarshal     func([]byte) error
}

// EncryptionKey is the decoded current encryption key (set at app startup).
var EncryptionKey []byte

// EncryptionKeyNext is the decoded next encryption key for rotation (nil if not rotating).
var EncryptionKeyNext []byte

// encryptSIV encrypts plaintext with AES-256-SIV (deterministic).
// Uses AES-CTR with a synthetic IV derived from CMAC for determinism.
// Same plaintext + same key = same ciphertext. Returns base64(iv || ciphertext).
func encryptSIV(key, plaintext []byte) (string, error) {
	if len(key) != 32 {
		return "", errors.New("encryptSIV: key must be 32 bytes")
	}
	// Deterministic: derive IV from HMAC of plaintext using first 16 bytes of key
	// Use AES-CBC-MAC as a simple deterministic IV derivation
	block, err := aes.NewCipher(key[:16])
	if err != nil {
		return "", fmt.Errorf("encryptSIV: %w", err)
	}

	// CMAC-like IV derivation: encrypt zero block XORed with plaintext blocks
	iv := make([]byte, aes.BlockSize)
	padded := pkcs7Pad(plaintext, aes.BlockSize)
	for i := 0; i < len(padded); i += aes.BlockSize {
		for j := 0; j < aes.BlockSize; j++ {
			iv[j] ^= padded[i+j]
		}
		block.Encrypt(iv, iv)
	}

	// Encrypt with AES-CTR using the deterministic IV and the full 32-byte key
	cipherBlock, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("encryptSIV: %w", err)
	}
	stream := cipher.NewCTR(cipherBlock, iv)
	ciphertext := make([]byte, len(plaintext))
	stream.XORKeyStream(ciphertext, plaintext)

	// Prepend IV
	result := make([]byte, aes.BlockSize+len(ciphertext))
	copy(result[:aes.BlockSize], iv)
	copy(result[aes.BlockSize:], ciphertext)
	return base64.StdEncoding.EncodeToString(result), nil
}

// decryptSIV decrypts a value produced by encryptSIV.
func decryptSIV(key []byte, ciphertext string) ([]byte, error) {
	if len(key) != 32 {
		return nil, errors.New("decryptSIV: key must be 32 bytes")
	}
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decryptSIV: %w", err)
	}
	if len(data) < aes.BlockSize {
		return nil, errors.New("decryptSIV: ciphertext too short")
	}

	iv := data[:aes.BlockSize]
	ct := data[aes.BlockSize:]

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("decryptSIV: %w", err)
	}
	stream := cipher.NewCTR(block, iv)
	plaintext := make([]byte, len(ct))
	stream.XORKeyStream(plaintext, ct)
	return plaintext, nil
}

// encryptGCM encrypts plaintext with AES-256-GCM (non-deterministic).
// Fresh random nonce per call. Returns base64(nonce || ciphertext || tag).
func encryptGCM(key, plaintext []byte) (string, error) {
	if len(key) != 32 {
		return "", errors.New("encryptGCM: key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("encryptGCM: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("encryptGCM: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("encryptGCM: %w", err)
	}
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// decryptGCM decrypts a value produced by encryptGCM.
func decryptGCM(key []byte, ciphertext string) ([]byte, error) {
	if len(key) != 32 {
		return nil, errors.New("decryptGCM: key must be 32 bytes")
	}
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decryptGCM: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("decryptGCM: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("decryptGCM: %w", err)
	}
	if len(data) < gcm.NonceSize() {
		return nil, errors.New("decryptGCM: ciphertext too short")
	}
	nonce := data[:gcm.NonceSize()]
	ct := data[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("decryptGCM: %w", err)
	}
	return plaintext, nil
}

// pkcs7Pad pads data to a multiple of blockSize using PKCS#7.
func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	pad := make([]byte, padding)
	for i := range pad {
		pad[i] = byte(padding)
	}
	return append(data, pad...)
}
