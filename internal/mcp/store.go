package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/active"
)

// Store is a SQLite-backed repository for MCP server registrations.
type Store struct {
	db *sql.DB
}

// NewStore creates a Store backed by the provided SQLite database.
func NewStore(database *sql.DB) *Store {
	return &Store{db: database}
}

// rowScanner abstracts *sql.Row and *sql.Rows for scanServer.
type rowScanner interface {
	Scan(dest ...interface{}) error
}

// scanServer reads a row into a Server value.
func scanServer(row rowScanner) (*Server, error) {
	s := &Server{}
	var argsRaw, envRaw string
	var enabled, maxClients, maxIdleSec int
	if err := row.Scan(
		&s.ID,
		&s.Name,
		&s.Command,
		&argsRaw,
		&envRaw,
		&enabled,
		&s.RestartPolicy,
		&maxClients,
		&maxIdleSec,
		&s.CreatedAt,
		&s.UpdatedAt,
	); err != nil {
		return nil, err
	}
	s.Enabled = enabled == 1
	s.MaxClients = maxClients
	s.MaxIdleSec = maxIdleSec
	if argsRaw != "" {
		_ = json.Unmarshal([]byte(argsRaw), &s.Args)
	}
	if envRaw != "" {
		_ = json.Unmarshal([]byte(envRaw), &s.Env)
	}
	s.defaultConfig()
	return s, nil
}

// ErrNotFound is returned when a server does not exist.
var ErrNotFound = errors.New("mcp server not found")

// List returns all registered MCP servers ordered by most recent.
func (s *Store) List(ctx context.Context) ([]*Server, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, command, args, env, enabled, restart_policy, max_clients, max_idle_sec, created_at, updated_at
		FROM mcp_servers
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	servers := make([]*Server, 0)
	for rows.Next() {
		server, err := scanServer(rows)
		if err != nil {
			return nil, err
		}
		servers = append(servers, server)
	}
	return servers, rows.Err()
}

// Get returns a server by ID.
func (s *Store) Get(ctx context.Context, id string) (*Server, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, command, args, env, enabled, restart_policy, max_clients, max_idle_sec, created_at, updated_at
		FROM mcp_servers
		WHERE id = ?
	`, id)
	server, err := scanServer(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return server, nil
}

// Create persists a new MCP server registration.
func (s *Store) Create(ctx context.Context, server *Server) error {
	if err := validateServer(server); err != nil {
		return err
	}
	now := time.Now().Unix()
	if server.ID == "" {
		server.ID = active.NewID()
	}
	server.CreatedAt = now
	server.UpdatedAt = now

	argsRaw, err := json.Marshal(server.Args)
	if err != nil {
		return err
	}
	envRaw, err := json.Marshal(server.Env)
	if err != nil {
		return err
	}
	server.defaultConfig()

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO mcp_servers (id, name, command, args, env, enabled, restart_policy, max_clients, max_idle_sec, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, server.ID, server.Name, server.Command, string(argsRaw), string(envRaw), boolInt(server.Enabled),
		server.RestartPolicy, server.MaxClients, server.MaxIdleSec, server.CreatedAt, server.UpdatedAt)
	return err
}

// Update modifies an existing server registration. Zero-value runtime settings
// are normalized through defaultConfig.
func (s *Store) Update(ctx context.Context, server *Server) error {
	if err := validateServer(server); err != nil {
		return err
	}
	server.UpdatedAt = time.Now().Unix()
	argsRaw, err := json.Marshal(server.Args)
	if err != nil {
		return err
	}
	envRaw, err := json.Marshal(server.Env)
	if err != nil {
		return err
	}
	server.defaultConfig()

	res, err := s.db.ExecContext(ctx, `
		UPDATE mcp_servers
		SET name = ?, command = ?, args = ?, env = ?, enabled = ?, restart_policy = ?, max_clients = ?, max_idle_sec = ?, updated_at = ?
		WHERE id = ?
	`, server.Name, server.Command, string(argsRaw), string(envRaw), boolInt(server.Enabled),
		server.RestartPolicy, server.MaxClients, server.MaxIdleSec, server.UpdatedAt, server.ID)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete removes a server registration.
func (s *Store) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM mcp_servers WHERE id = ?`, id)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// validateServer checks input for injection or missing required fields.
func validateServer(s *Server) error {
	if strings.TrimSpace(s.Name) == "" {
		return errors.New("name is required")
	}
	s.Name = strings.TrimSpace(s.Name)
	if strings.TrimSpace(s.Command) == "" {
		return errors.New("command is required")
	}
	s.Command = strings.TrimSpace(s.Command)

	if s.RestartPolicy == "" {
		s.RestartPolicy = RestartOnFailure
	}
	if !IsValidRestartPolicy(s.RestartPolicy) {
		return fmt.Errorf("invalid restart_policy: %s", s.RestartPolicy)
	}

	if err := validateCommand(s.Command); err != nil {
		return err
	}
	for _, arg := range s.Args {
		if err := validateArgument(arg); err != nil {
			return err
		}
	}
	cleanEnv := make(map[string]string, len(s.Env))
	for k, v := range s.Env {
		if strings.TrimSpace(k) == "" || strings.ContainsAny(k, "=\x00") {
			return fmt.Errorf("invalid env key: %q", k)
		}
		cleanEnv[k] = v
	}
	s.Env = cleanEnv
	return nil
}

// blacklistedCommandChars blocks shell metacharacters in the command path.
func blacklistedCommandChars() string {
	return ";|&$`<>{}[]\\\"!*?\n\r\x00"
}

func validateCommand(command string) error {
	if strings.HasPrefix(command, "-") {
		return errors.New("command cannot start with '-'")
	}
	if strings.ContainsAny(command, blacklistedCommandChars()) {
		return fmt.Errorf("command contains forbidden characters: %q", command)
	}
	return nil
}

func validateArgument(arg string) error {
	if arg == "" {
		return nil
	}
	if strings.ContainsAny(arg, "\x00\n\r") {
		return errors.New("argument contains forbidden characters")
	}
	return nil
}
