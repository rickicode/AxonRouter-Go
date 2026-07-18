package backup

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"sync"

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

// keyCache avoids re-deriving the PBKDF2 key for every row when encrypting or
// decrypting a whole backup. A backup operation is single-threaded, but the
// cache is guarded for safety.
var keyCache = make(map[string][]byte)
var keyCacheMu sync.Mutex

func Encrypt(plaintext []byte, password string) ([]byte, error) {
	if password == "" {
		return nil, ErrMissingPassword
	}

	salt, err := randomBytes(saltSize)
	if err != nil {
		return nil, fmt.Errorf("generate encryption salt: %w", err)
	}
	return encryptWithSalt(plaintext, password, salt)
}

func encryptWithSalt(plaintext []byte, password string, salt []byte) ([]byte, error) {
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

// EncryptLineWithSalt encrypts a single plaintext line using a caller-supplied
// salt. Using the same salt for every row of a backup lets the key cache avoid
// re-deriving the PBKDF2 key on every row.
func EncryptLineWithSalt(plaintext []byte, password string, salt []byte) (string, error) {
	if password == "" {
		return "", ErrMissingPassword
	}
	sealed, err := encryptWithSalt(plaintext, password, salt)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(sealed), nil
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
	key := deriveKey(password, salt)
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

func deriveKey(password string, salt []byte) []byte {
	cacheKey := password + "|" + string(salt)
	keyCacheMu.Lock()
	defer keyCacheMu.Unlock()
	if key, ok := keyCache[cacheKey]; ok {
		return key
	}
	key := pbkdf2.Key([]byte(password), salt, pbkdf2Iterations, keySize, sha256.New)
	keyCache[cacheKey] = key
	return key
}

func randomBytes(size int) ([]byte, error) {
	buf := make([]byte, size)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// EncryptLine encrypts a single plaintext line and returns it as a base64
// string suitable for inclusion in a line-based backup file.
func EncryptLine(plaintext []byte, password string) (string, error) {
	sealed, err := Encrypt(plaintext, password)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// DecryptLine decrypts a base64-encoded line produced by EncryptLine.
func DecryptLine(ciphertext string, password string) ([]byte, error) {
	sealed, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return nil, ErrInvalidEncryptedData
	}
	return Decrypt(sealed, password)
}
