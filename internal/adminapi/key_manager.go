package adminapi

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"sync"

	"github.com/rickicode/AxonRouter-Go/internal/db"
)

const (
	settingAdminAPIKey = "admin_api_key"
	keyPrefix          = "axr_"
)

// KeyManager stores the single programmatic admin API key in memory.
type KeyManager struct {
	db      *sql.DB
	mu      sync.RWMutex
	current string
}

// NewKeyManager loads or creates the master admin key.
func NewKeyManager(database *sql.DB) *KeyManager {
	km := &KeyManager{db: database}
	km.ensureKey()
	return km
}

// Current returns the current master key.
func (km *KeyManager) Current() string {
	km.mu.RLock()
	defer km.mu.RUnlock()
	return km.current
}

// Regenerate creates a new master key and persists it.
func (km *KeyManager) Regenerate() (string, error) {
	next, err := generateKey()
	if err != nil {
		return "", err
	}
	if err := db.SetSetting(settingAdminAPIKey, next); err != nil {
		return "", err
	}
	km.mu.Lock()
	km.current = next
	km.mu.Unlock()
	return next, nil
}

func (km *KeyManager) ensureKey() {
	existing := db.GetSetting(settingAdminAPIKey, "")
	if existing != "" {
		km.current = existing
		return
	}
	newKey, err := generateKey()
	if err != nil {
		return
	}
	if err := db.SetSetting(settingAdminAPIKey, newKey); err != nil {
		return
	}
	km.current = newKey
}

func generateKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return keyPrefix + hex.EncodeToString(b), nil
}
