package backup

import (
	"bytes"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	plaintext := []byte("backup payload with sensitive database rows")
	password := "correct horse battery staple"

	sealed, err := Encrypt(plaintext, password)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if bytes.Contains(sealed, plaintext) {
		t.Fatalf("encrypted payload contains plaintext")
	}

	opened, err := Decrypt(sealed, password)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if !bytes.Equal(opened, plaintext) {
		t.Fatalf("Decrypt() = %q, want %q", opened, plaintext)
	}
}
