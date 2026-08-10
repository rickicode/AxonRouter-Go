package admin

import (
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
)

var codebuddyFallbackNameRe = regexp.MustCompile(`^CodeBuddy-(\d+)$`)

// nextCodeBuddyFallbackName returns a unique "CodeBuddy-N" name by scanning
// existing CodeBuddy connection names. This keeps the connection list tidy when
// the OAuth provider does not expose an email address.
func nextCodeBuddyFallbackName(db *sql.DB) (string, error) {
	rows, err := db.Query(`SELECT name FROM connections WHERE provider_type_id = 'codebuddy' AND name LIKE 'CodeBuddy-%'`)
	if err != nil {
		return "", fmt.Errorf("query codebuddy connection names: %w", err)
	}
	defer rows.Close()

	max := 0
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		matches := codebuddyFallbackNameRe.FindStringSubmatch(name)
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
	return fmt.Sprintf("CodeBuddy-%d", max+1), nil
}
