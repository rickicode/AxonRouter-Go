package admin

import (
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
)

var kiroFallbackNameRe = regexp.MustCompile(`^Kiro-(\d+)$`)

// nextKiroFallbackName returns a unique "Kiro-N" name by scanning existing
// Kiro connection names. This mirrors the fallback naming in AxonRouter's
// provider creation flow.
func nextKiroFallbackName(db *sql.DB) (string, error) {
	rows, err := db.Query(`SELECT name FROM connections WHERE provider_type_id = 'kiro' AND name LIKE 'Kiro-%'`)
	if err != nil {
		return "", fmt.Errorf("query kiro connection names: %w", err)
	}
	defer rows.Close()

	max := 0
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		matches := kiroFallbackNameRe.FindStringSubmatch(name)
		if len(matches) != 2 {
			continue
		}
		n, err := strconv.Atoi(matches[1])
		if err != nil {
			continue
		}
		if n > max {
			max = n
		}
	}
	_ = rows.Err()
	return fmt.Sprintf("Kiro-%d", max+1), nil
}
