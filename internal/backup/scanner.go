package backup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

type Scanner struct {
	db *sql.DB
}

func NewScanner(db *sql.DB) *Scanner {
	return &Scanner{db: db}
}

func (s *Scanner) Backup(ctx context.Context, dest io.Writer, categories []string, password string) error {
	if s == nil || s.db == nil {
		return errors.New("backup scanner database is required")
	}
	selected, err := normalizeCategories(categories)
	if err != nil {
		return err
	}

	writer, err := NewWriter(dest, Header{
		Format:     FormatName,
		Version:    FormatVersion,
		Categories: selected,
		CreatedAt:  time.Now().Unix(),
	}, password)
	if err != nil {
		return err
	}

	for _, category := range selected {
		for _, table := range CategoryTables[category] {
			exists, err := s.tableExists(ctx, table)
			if err != nil {
				return err
			}
			if !exists {
				continue
			}
			if err := s.backupTable(ctx, writer, table); err != nil {
				return err
			}
		}
	}
	return writer.Close()
}

func normalizeCategories(categories []string) ([]string, error) {
	if len(categories) == 0 {
		return AllCategories(), nil
	}
	seen := make(map[string]bool, len(categories))
	selected := make([]string, 0, len(categories))
	for _, category := range categories {
		if _, ok := CategoryTables[category]; !ok {
			return nil, fmt.Errorf("unknown backup category %q", category)
		}
		if seen[category] {
			continue
		}
		seen[category] = true
		selected = append(selected, category)
	}
	sort.Strings(selected)
	return selected, nil
}

func (s *Scanner) tableExists(ctx context.Context, table string) (bool, error) {
	var name string
	err := s.db.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check backup table %s: %w", table, err)
	}
	return true, nil
}

func (s *Scanner) backupTable(ctx context.Context, writer *Writer, table string) error {
	rows, err := s.db.QueryContext(ctx, "SELECT * FROM "+quoteIdentifier(table))
	if err != nil {
		return fmt.Errorf("query backup table %s: %w", table, err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return fmt.Errorf("read backup columns for %s: %w", table, err)
	}
	for rows.Next() {
		data, err := scanRowMap(rows, columns)
		if err != nil {
			return fmt.Errorf("scan backup row from %s: %w", table, err)
		}
		if err := writer.WriteRow(Row{Table: table, Data: data}); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate backup table %s: %w", table, err)
	}
	return nil
}

func scanRowMap(rows *sql.Rows, columns []string) (map[string]any, error) {
	values := make([]any, len(columns))
	dest := make([]any, len(columns))
	for i := range values {
		dest[i] = &values[i]
	}
	if err := rows.Scan(dest...); err != nil {
		return nil, err
	}
	data := make(map[string]any, len(columns))
	for i, column := range columns {
		data[column] = normalizeSQLValue(values[i])
	}
	return data, nil
}

func normalizeSQLValue(value any) any {
	switch v := value.(type) {
	case []byte:
		return string(v)
	default:
		return v
	}
}

func quoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}
