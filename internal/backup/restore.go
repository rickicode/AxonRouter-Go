package backup

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	appdb "github.com/rickicode/AxonRouter-Go/internal/db"
)

type RestoreOptions struct {
	Password   string
	CurrentDB  *sql.DB
	WriteQueue *appdb.WriteQueue
}

type RestoreResult struct {
	RowsRestored    int
	TablesRestored  []string
	RestartRequired bool
	Warning         string
}

const currentRestoreRestartWarning = "Process restart required after restore to refresh in-memory caches: connection registry, combo config, model lists, proxy resolver, and quota cache."

// maxBackupLineLength is the largest backup row we will accept. Response-cache
// and request-log rows can be large, so allow up to 64 MiB per line.
const maxBackupLineLength = 64 * 1024 * 1024

// restoreBatchSize controls how many rows are inserted in a single INSERT.
// Larger batches reduce SQLite write-lock round-trips but keep memory bounded.
const restoreBatchSize = 500

// maxRestoreRetries and restoreRetryBaseDelay protect against transient
// SQLITE_BUSY contention from concurrent writers or background workers.
const maxRestoreRetries = 5
const restoreRetryBaseDelay = 50 * time.Millisecond

func Restore(ctx context.Context, src io.Reader, opts RestoreOptions) (RestoreResult, error) {
	if src == nil {
		return RestoreResult{}, errors.New("backup restore source is required")
	}
	if opts.CurrentDB == nil {
		return RestoreResult{}, errors.New("current restore target database is required")
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

	if opts.WriteQueue != nil {
		opts.WriteQueue.Pause()
		defer opts.WriteQueue.Resume()
	}

	if err := appdb.RunMigrations(opts.CurrentDB); err != nil {
		return RestoreResult{}, fmt.Errorf("run restore migrations: %w", err)
	}

	result, err := restoreRows(ctx, opts.CurrentDB, header, scanner, opts.Password)
	if err != nil {
		return RestoreResult{}, err
	}

	result.RestartRequired = true
	result.Warning = currentRestoreRestartWarning
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

func restoreRows(ctx context.Context, target *sql.DB, header Header, scanner *bufio.Scanner, password string) (RestoreResult, error) {
	if err := execWithRetry(ctx, "disable foreign keys", func(ctx context.Context) error {
		_, err := target.ExecContext(ctx, "PRAGMA foreign_keys=OFF")
		return err
	}); err != nil {
		return RestoreResult{}, fmt.Errorf("disable restore foreign keys: %w", err)
	}
	defer target.ExecContext(context.Background(), "PRAGMA foreign_keys=ON")

	tx, err := beginRestoreTx(ctx, target)
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
		if err := execWithRetry(ctx, "delete "+table, func(ctx context.Context) error {
			_, err := tx.ExecContext(ctx, "DELETE FROM "+quoteIdentifier(table))
			return err
		}); err != nil {
			return RestoreResult{}, fmt.Errorf("delete restore table %s: %w", table, err)
		}
	}

	batch := make([]Row, 0, restoreBatchSize)
	var currentTable string
	var rowsRestored int
	seenTables := make(map[string]bool)

	flush := func(table string) error {
		if len(batch) == 0 {
			return nil
		}
		if err := execWithRetry(ctx, "insert batch into "+table, func(ctx context.Context) error {
			return insertRestoreBatch(ctx, tx, table, batch)
		}); err != nil {
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
		if len(batch) >= restoreBatchSize {
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

	if err := execWithRetry(context.Background(), "commit restore transaction", func(_ context.Context) error { return tx.Commit() }); err != nil {
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

// beginRestoreTx starts a write transaction with retries for transient busy
// errors so the restore does not fail immediately under light contention.
func beginRestoreTx(ctx context.Context, db *sql.DB) (*sql.Tx, error) {
	var tx *sql.Tx
	err := execWithRetry(ctx, "begin immediate transaction", func(ctx context.Context) error {
		var err error
		tx, err = db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelDefault})
		return err
	})
	if err != nil {
		return nil, err
	}
	return tx, nil
}

// execWithRetry retries transient SQLite busy errors. It only retries when the
// context has not been cancelled and the error looks like a lock/busy problem.
func execWithRetry(ctx context.Context, label string, op func(ctx context.Context) error) error {
	delay := restoreRetryBaseDelay
	var lastErr error
	for attempt := 0; attempt < maxRestoreRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("%s: %w", label, err)
		}
		lastErr = op(ctx)
		if lastErr == nil {
			return nil
		}
		if attempt == maxRestoreRetries-1 || !isRetryableDBError(lastErr) {
			return fmt.Errorf("%s: %w", label, lastErr)
		}
		time.Sleep(delay)
		if delay < 2*time.Second {
			delay *= 2
		}
	}
	return fmt.Errorf("%s: %w", label, lastErr)
}

func isRetryableDBError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "busy") ||
		strings.Contains(msg, "cannot start a transaction within a transaction") ||
		strings.Contains(msg, "retry")
}
