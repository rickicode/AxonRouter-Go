package backup

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"

	appdb "github.com/rickicode/AxonRouter-Go/internal/db"

	_ "github.com/tursodatabase/libsql-client-go/libsql"
	_ "modernc.org/sqlite"
)

type RestoreTarget string

const (
	RestoreTargetCurrent RestoreTarget = "current"
	RestoreTargetSQLite  RestoreTarget = "sqlite"
	RestoreTargetTurso   RestoreTarget = "turso"
)

type RestoreOptions struct {
	Target     RestoreTarget
	Password   string
	CurrentDB  *sql.DB
	WriteQueue *appdb.WriteQueue
	SQLitePath string
	TursoURL   string
	TursoToken string
}

type RestoreResult struct {
	RowsRestored    int
	TablesRestored  []string
	RestartRequired bool
	Warning         string
}

const currentRestoreRestartWarning = "Process restart required after restore to refresh in-memory caches: connection registry, combo config, model lists, proxy resolver, and quota cache."

func Restore(ctx context.Context, src io.Reader, opts RestoreOptions) (RestoreResult, error) {
	if src == nil {
		return RestoreResult{}, errors.New("backup restore source is required")
	}
	payload, err := io.ReadAll(src)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("read backup payload: %w", err)
	}
	if opts.Password != "" {
		payload, err = Decrypt(payload, opts.Password)
		if err != nil {
			return RestoreResult{}, fmt.Errorf("decrypt backup payload: %w", err)
		}
	}

	header, rows, err := decodeBackupPayload(payload)
	if err != nil {
		return RestoreResult{}, err
	}
	if err := validateHeader(header); err != nil {
		return RestoreResult{}, err
	}

	target, closeTarget, err := openRestoreTarget(opts)
	if err != nil {
		return RestoreResult{}, err
	}
	defer closeTarget()

	if opts.Target == RestoreTargetCurrent && opts.WriteQueue != nil {
		opts.WriteQueue.Pause()
		defer opts.WriteQueue.Resume()
	}

	if err := appdb.RunMigrations(target); err != nil {
		return RestoreResult{}, fmt.Errorf("run restore migrations: %w", err)
	}
	if err := restoreRows(ctx, target, header.Categories, rows); err != nil {
		return RestoreResult{}, err
	}

	result := RestoreResult{
		RowsRestored:   len(rows),
		TablesRestored: restoredTables(rows),
	}
	if opts.Target == RestoreTargetCurrent {
		result.RestartRequired = true
		result.Warning = currentRestoreRestartWarning
	}
	return result, nil
}

func decodeBackupPayload(payload []byte) (Header, []Row, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(payload)))
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)

	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return Header{}, nil, fmt.Errorf("read backup header: %w", err)
		}
		return Header{}, nil, errors.New("backup payload is empty")
	}
	var header Header
	if err := decodeJSONLine(scanner.Text(), &header); err != nil {
		return Header{}, nil, fmt.Errorf("decode backup header: %w", err)
	}

	var rows []Row
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var row Row
		if err := decodeJSONLine(line, &row); err != nil {
			return Header{}, nil, fmt.Errorf("decode backup row: %w", err)
		}
		rows = append(rows, normalizeRow(row))
	}
	if err := scanner.Err(); err != nil {
		return Header{}, nil, fmt.Errorf("read backup rows: %w", err)
	}
	return header, rows, nil
}

func decodeJSONLine(line string, dest any) error {
	decoder := json.NewDecoder(strings.NewReader(line))
	decoder.UseNumber()
	return decoder.Decode(dest)
}

func validateHeader(header Header) error {
	if header.Format != FormatName {
		return fmt.Errorf("unsupported backup format %q", header.Format)
	}
	if header.Version != FormatVersion {
		return fmt.Errorf("unsupported backup version %d", header.Version)
	}
	_, err := normalizeCategories(header.Categories)
	return err
}

func normalizeRow(row Row) Row {
	data := make(map[string]any, len(row.Data))
	for key, value := range row.Data {
		data[key] = normalizeJSONValue(value)
	}
	row.Data = data
	return row
}

func normalizeJSONValue(value any) any {
	switch v := value.(type) {
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return i
		}
		if f, err := v.Float64(); err == nil {
			return f
		}
		return v.String()
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, nested := range v {
			out[key] = normalizeJSONValue(nested)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, nested := range v {
			out[i] = normalizeJSONValue(nested)
		}
		return out
	default:
		return v
	}
}

func openRestoreTarget(opts RestoreOptions) (*sql.DB, func(), error) {
	switch opts.Target {
	case RestoreTargetCurrent:
		if opts.CurrentDB == nil {
			return nil, nil, errors.New("current restore target database is required")
		}
		return opts.CurrentDB, func() {}, nil
	case RestoreTargetSQLite:
		if opts.SQLitePath == "" {
			return nil, nil, errors.New("sqlite restore target path is required")
		}
		d, err := sql.Open("sqlite", opts.SQLitePath)
		if err != nil {
			return nil, nil, fmt.Errorf("open sqlite restore target: %w", err)
		}
		return d, func() { _ = d.Close() }, nil
	case RestoreTargetTurso:
		if opts.TursoURL == "" {
			return nil, nil, errors.New("turso restore target url is required")
		}
		dsn, err := appendRestoreAuthToken(opts.TursoURL, opts.TursoToken)
		if err != nil {
			return nil, nil, fmt.Errorf("prepare turso restore target: %w", err)
		}
		d, err := sql.Open("libsql", dsn)
		if err != nil {
			return nil, nil, fmt.Errorf("open turso restore target: %w", err)
		}
		return d, func() { _ = d.Close() }, nil
	default:
		return nil, nil, fmt.Errorf("unsupported restore target %q", opts.Target)
	}
}

func appendRestoreAuthToken(dsn, token string) (string, error) {
	if token == "" || strings.Contains(dsn, "authToken=") {
		return dsn, nil
	}
	u, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("authToken", token)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func restoreRows(ctx context.Context, target *sql.DB, categories []string, rows []Row) error {
	if _, err := target.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
		return fmt.Errorf("disable restore foreign keys: %w", err)
	}
	defer target.ExecContext(context.Background(), "PRAGMA foreign_keys=ON")

	tx, err := target.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin restore transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	for _, table := range deleteOrder(categories) {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+quoteIdentifier(table)); err != nil {
			return fmt.Errorf("delete restore table %s: %w", table, err)
		}
	}
	for _, row := range rows {
		if err := insertRestoreRow(ctx, tx, row); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit restore transaction: %w", err)
	}
	committed = true
	return nil
}

func deleteOrder(categories []string) []string {
	selected, _ := normalizeCategories(categories)
	var tables []string
	seen := map[string]bool{}
	for _, category := range selected {
		for _, table := range CategoryTables[category] {
			if !seen[table] {
				seen[table] = true
				tables = append(tables, table)
			}
		}
	}
	for i, j := 0, len(tables)-1; i < j; i, j = i+1, j-1 {
		tables[i], tables[j] = tables[j], tables[i]
	}
	return tables
}

func insertRestoreRow(ctx context.Context, tx *sql.Tx, row Row) error {
	if row.Table == "" {
		return errors.New("restore row table is required")
	}
	if len(row.Data) == 0 {
		return fmt.Errorf("restore row for table %s has no data", row.Table)
	}
	columns := make([]string, 0, len(row.Data))
	for column := range row.Data {
		columns = append(columns, column)
	}
	sort.Strings(columns)

	placeholders := make([]string, len(columns))
	quotedColumns := make([]string, len(columns))
	args := make([]any, len(columns))
	for i, column := range columns {
		placeholders[i] = "?"
		quotedColumns[i] = quoteIdentifier(column)
		args[i] = row.Data[column]
	}
	query := "INSERT INTO " + quoteIdentifier(row.Table) + " (" + strings.Join(quotedColumns, ", ") + ") VALUES (" + strings.Join(placeholders, ", ") + ")"
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("insert restore row into %s: %w", row.Table, err)
	}
	return nil
}

func restoredTables(rows []Row) []string {
	seen := make(map[string]bool)
	for _, row := range rows {
		seen[row.Table] = true
	}
	tables := make([]string, 0, len(seen))
	for table := range seen {
		tables = append(tables, table)
	}
	sort.Strings(tables)
	return tables
}
