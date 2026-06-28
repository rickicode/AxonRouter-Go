package db

import (
	"database/sql"
	"log"

	"golang.org/x/crypto/bcrypt"
)

// MigrateRawKeysToBcrypt scans api_keys and hashes any raw keys with bcrypt.
// Safe to call on every startup — skips keys that are already bcrypt hashes.
func MigrateRawKeysToBcrypt(db *sql.DB) {
	rows, err := db.Query(`SELECT id, key_hash FROM api_keys WHERE is_active = 1`)
	if err != nil {
		log.Printf("WARN: key migration query failed: %v", err)
		return
	}
	defer rows.Close()

	var migrated int
	for rows.Next() {
		var id, keyHash string
		if err := rows.Scan(&id, &keyHash); err != nil {
			continue
		}

		// Already a bcrypt hash
		if len(keyHash) >= 4 && keyHash[:4] == "$2a$" {
			continue
		}

		// Hash the raw key
		hash, err := bcrypt.GenerateFromPassword([]byte(keyHash), bcrypt.DefaultCost)
		if err != nil {
			log.Printf("WARN: failed to hash key %s: %v", id, err)
			continue
		}

		if _, err := db.Exec(`UPDATE api_keys SET key_hash = ? WHERE id = ?`, string(hash), id); err != nil {
			log.Printf("WARN: failed to update key %s: %v", id, err)
			continue
		}
		migrated++
	}

	if migrated > 0 {
		log.Printf("INFO: migrated %d API keys to bcrypt", migrated)
	}
}
