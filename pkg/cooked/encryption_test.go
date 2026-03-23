package cooked

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func TestEncryptSIVDeterministic(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}

	plaintext := []byte("alice@example.com")

	ct1, err := encryptSIV(key, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	ct2, err := encryptSIV(key, plaintext)
	if err != nil {
		t.Fatal(err)
	}

	if ct1 != ct2 {
		t.Errorf("SIV is not deterministic: %q != %q", ct1, ct2)
	}
}

func TestEncryptSIVRoundtrip(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}

	plaintext := []byte("secret-api-key-12345")

	ct, err := encryptSIV(key, plaintext)
	if err != nil {
		t.Fatal(err)
	}

	got, err := decryptSIV(key, ct)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, plaintext) {
		t.Errorf("got %q, want %q", got, plaintext)
	}
}

func TestEncryptSIVDifferentKeys(t *testing.T) {
	key1 := make([]byte, 32)
	key2 := make([]byte, 32)
	rand.Read(key1)
	rand.Read(key2)

	plaintext := []byte("same-value")

	ct1, _ := encryptSIV(key1, plaintext)
	ct2, _ := encryptSIV(key2, plaintext)

	if ct1 == ct2 {
		t.Error("different keys should produce different ciphertext")
	}
}

func TestEncryptGCMNonDeterministic(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)

	plaintext := []byte("same-value")

	ct1, err := encryptGCM(key, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	ct2, err := encryptGCM(key, plaintext)
	if err != nil {
		t.Fatal(err)
	}

	if ct1 == ct2 {
		t.Error("GCM should be non-deterministic")
	}
}

func TestEncryptGCMRoundtrip(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)

	plaintext := []byte("private-key-data-here")

	ct, err := encryptGCM(key, plaintext)
	if err != nil {
		t.Fatal(err)
	}

	got, err := decryptGCM(key, ct)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, plaintext) {
		t.Errorf("got %q, want %q", got, plaintext)
	}
}

func TestEncryptSIVBadKeyLength(t *testing.T) {
	_, err := encryptSIV([]byte("short"), []byte("test"))
	if err == nil {
		t.Error("expected error for short key")
	}
}

func TestEncryptGCMBadKeyLength(t *testing.T) {
	_, err := encryptGCM([]byte("short"), []byte("test"))
	if err == nil {
		t.Error("expected error for short key")
	}
}

func TestDecryptSIVWrongKey(t *testing.T) {
	key1 := make([]byte, 32)
	key2 := make([]byte, 32)
	rand.Read(key1)
	rand.Read(key2)

	ct, _ := encryptSIV(key1, []byte("test"))
	// SIV doesn't authenticate, so decryption "succeeds" but returns garbage
	got, err := decryptSIV(key2, ct)
	if err != nil {
		t.Fatal(err) // SIV decryption won't error, just wrong result
	}
	if string(got) == "test" {
		t.Error("wrong key should not produce correct plaintext")
	}
}

func TestDecryptGCMWrongKey(t *testing.T) {
	key1 := make([]byte, 32)
	key2 := make([]byte, 32)
	rand.Read(key1)
	rand.Read(key2)

	ct, _ := encryptGCM(key1, []byte("test"))
	_, err := decryptGCM(key2, ct)
	if err == nil {
		t.Error("GCM should fail with wrong key (authentication failure)")
	}
}
