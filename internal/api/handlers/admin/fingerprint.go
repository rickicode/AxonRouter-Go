package admin

import (
	"crypto/rand"
	"encoding/hex"
)

// generateFingerprint returns a 64-character lowercase hex string suitable for
// use as a MiMoCode device fingerprint.
func generateFingerprint() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
