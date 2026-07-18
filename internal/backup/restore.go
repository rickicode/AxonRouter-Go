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
	RowsRestored   int
	TablesRestored []string
	RestartRequired bool
	Warning        string
}

const currentRestoreRestartWarning = "Process restart required after restore to refresh in-memory caches: connection registry, combo config, model lists, proxy resolver, and quota cache."

// maxBackupLineLength is the largest backup row we will accept. Response-cache
// and request-log rows can be large, so allow up to 64 MiB per line.
const maxBackupLineLength = 64 * 1024 * 1024

func Restore(ctx context.Context, src io.Reader, opts RestoreOptions) (RestoreResult, error) {
	if src == nil {
		return RestoreResult{}, errors.New("backup restore source is required")
	}

	scanner := bufio.NewScanner(src)
	scanner.Buffer(make([]byte, 64*1024), maxBackupLineLength)

	header, err := readRestoreHeader(scanner)
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

	result, err := restoreRows(ctx, target, header, scanner, opts.Password)
	if err != nil {
		return RestoreResult{}, err
	}

	if opts.Target == RestoreTargetCurrent {
		result.RestartRequired = true
		result.Warning = currentRestoreRestartWarning
	}
	return result, nil
}

func readRestoreHeader(scanner *bufio.Scanner) (Header, error) {
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return Header{}, fmt.Errorf("read backup header: %w", err)
		}
		return Header{}, errors.New("backup payload is empty")
	}
	var header Header
	if err := decodeJSONLine(scanner.Text(), &header); err != nil {
		return Header{}, fmt.Errorf("decode backup header: %w", err)
	}
	return header, nil
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

func restoreRows(ctx context.Context, target *sql.DB, header Header, scanner *bufio.Scanner, password string) (RestoreResult, error) {
	if _, err := target.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
		return RestoreResult{}, fmt.Errorf("disable restore foreign keys: %w", err)
	}
	defer target.ExecContext(context.Background(), "PRAGMA foreign_keys=ON")

	tx, err := target.BeginTx(ctx, nil)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("begin restore transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	for _, table := range deleteOrder(header.Categories) {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+quoteIdentifier(table)); err != nil {
			return RestoreResult{}, fmt.Errorf("delete restore table %s: %w", table, err)
		}
	}

	batch := make([]Row, 0, 100)
	var currentTable string
	var rowsRestored int
	seenTables := make(map[string]bool)

	flush := func(table string) error {
		if len(batch) == 0 {
			return nil
		}
		if err := insertRestoreBatch(ctx, tx, table, batch); err != nil {
			return err
		}
		rowsRestored += len(batch)
		seenTables[table] = true
		batch = batch[:0]
		return nil
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		row, err := decodeRestoreRow(line, header.Encrypted, password)
		if err != nil {
			return RestoreResult{}, err
		}
		if currentTable == "" {
			currentTable = row.Table
		}
		if row.Table != currentTable {
			if err := flush(currentTable); err != nil {
				return RestoreResult{}, err
			}
			currentTable = row.Table
		}
		batch = append(batch, row)
		if len(batch) >= 100 {
			if err := flush(currentTable); err != nil {
				return RestoreResult{}, err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return RestoreResult{}, fmt.Errorf("read backup rows: %w", err)
	}
	if err := flush(currentTable); err != nil {
		return RestoreResult{}, err
	}

	if err := tx.Commit(); err != nil {
		return RestoreResult{}, fmt.Errorf("commit restore transaction: %w", err)
	}
	committed = true

	tables := make([]string, 0, len(seenTables))
	for table := range seenTables {
		tables = append(tables, table)
	}
	sort.Strings(tables)

	return RestoreResult{
		RowsRestored:   rowsRestored,
		TablesRestored: tables,
	}, nil
}

func decodeRestoreRow(line string, encrypted bool, password string) (Row, error) {
	if encrypted {
		plaintext, err := DecryptLine(line, password)
		if err != nil {
			return Row{}, fmt.Errorf("decrypt backup row: %w", err)
		}
		line = string(plaintext)
	}
	var row Row
	if err := decodeJSONLine(line, &row); err != nil {
		return Row{}, fmt.Errorf("decode backup row: %w", err)
	}
	row.Data = normalizeRowData(row.Data)
	return row, nil
}

func normalizeRowData(data map[string]any) map[string]any {
	out := make(map[string]any, len(data))
	for key, value := range data {
		out[key] = normalizeJSONValue(value)
	}
	return out
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

func insertRestoreBatch(ctx context.Context, tx *sql.Tx, table string, rows []Row) error {
	if len(rows) == 0 {
		return nil
	}
	// All rows in a batch come from the same table, so columns are identical.
	columns := make([]string, 0, len(rows[0].Data))
	for column := range rows[0].Data {
		columns = append(columns, column)
	}
	sort.Strings(columns)

	valueGroups := make([]string, 0, len(rows))
	args := make([]any, 0, len(rows)*len(columns))
	for _, row := range rows {
		placeholders := make([]string, len(columns))
		for i, column := range columns {
			placeholders[i] = "?"
			args = append(args, row.Data[column])
		}
		valueGroups = append(valueGroups, "("+strings.Join(placeholders, ", ")+")")
	}

	quotedColumns := make([]string, len(columns))
	for i, column := range columns {
		quotedColumns[i] = quoteIdentifier(column)
	}

	query := "INSERT INTO " + quoteIdentifier(table) + " (" + strings.Join(quotedColumns, ", ") + ") VALUES " + strings.Join(valueGroups, ", ")
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("insert restore batch into %s: %w", table, err)
	}
	return nil
}

func quoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}
