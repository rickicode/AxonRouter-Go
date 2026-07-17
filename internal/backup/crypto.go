package backup

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/pbkdf2"
)

const (
	backupEncryptionVersion byte = 1
	saltSize                     = 16
	nonceSize                    = 12
	keySize                      = 32
	pbkdf2Iterations             = 210_000
)

var (
	ErrMissingPassword      = errors.New("backup encryption password is required")
	ErrInvalidEncryptedData = errors.New("invalid encrypted backup data")
)

func Encrypt(plaintext []byte, password string) ([]byte, error) {
	if password == "" {
		return nil, ErrMissingPassword
	}

	salt, err := randomBytes(saltSize)
	if err != nil {
		return nil, fmt.Errorf("generate encryption salt: %w", err)
	}
	nonce, err := randomBytes(nonceSize)
	if err != nil {
		return nil, fmt.Errorf("generate encryption nonce: %w", err)
	}
	gcm, err := newGCM(password, salt)
	if err != nil {
		return nil, err
	}

	sealed := make([]byte, 0, 1+saltSize+nonceSize+len(plaintext)+gcm.Overhead())
	sealed = append(sealed, backupEncryptionVersion)
	sealed = append(sealed, salt...)
	sealed = append(sealed, nonce...)
	sealed = gcm.Seal(sealed, nonce, plaintext, nil)
	return sealed, nil
}

func Decrypt(ciphertext []byte, password string) ([]byte, error) {
	if password == "" {
		return nil, ErrMissingPassword
	}
	if len(ciphertext) < 1+saltSize+nonceSize {
		return nil, ErrInvalidEncryptedData
	}
	if ciphertext[0] != backupEncryptionVersion {
		return nil, ErrInvalidEncryptedData
	}

	saltStart := 1
	nonceStart := saltStart + saltSize
	payloadStart := nonceStart + nonceSize
	salt := ciphertext[saltStart:nonceStart]
	nonce := ciphertext[nonceStart:payloadStart]
	payload := ciphertext[payloadStart:]

	gcm, err := newGCM(password, salt)
	if err != nil {
		return nil, err
	}
	plaintext, err := gcm.Open(nil, nonce, payload, nil)
	if err != nil {
		return nil, ErrInvalidEncryptedData
	}
	return plaintext, nil
}

func newGCM(password string, salt []byte) (cipher.AEAD, error) {
	key := pbkdf2.Key([]byte(password), salt, pbkdf2Iterations, keySize, sha256.New)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM cipher: %w", err)
	}
	return gcm, nil
}

func randomBytes(size int) ([]byte, error) {
	buf := make([]byte, size)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return nil, err
	}
	return buf, nil
}
