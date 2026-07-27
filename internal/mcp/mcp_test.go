package mcp

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS mcp_servers (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			command TEXT NOT NULL,
			args TEXT NOT NULL DEFAULT '[]',
			env TEXT NOT NULL DEFAULT '{}',
			enabled INTEGER NOT NULL DEFAULT 1,
			restart_policy TEXT NOT NULL DEFAULT 'on-failure',
			max_clients INTEGER NOT NULL DEFAULT 4,
			max_idle_sec INTEGER NOT NULL DEFAULT 60,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)
	`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	return db
}

func TestStoreCRUD(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	store := NewStore(db)

	s := &Server{
		Name:         "test",
		Command:      "echo",
		Args:         []string{"hello"},
		Env:          map[string]string{"KEY": "value"},
		Enabled:      true,
		RestartPolicy: RestartNever,
	}
	if err := store.Create(ctx, s); err != nil {
		t.Fatalf("create: %v", err)
	}
	if s.ID == "" {
		t.Fatal("expected id to be generated")
	}

	got, err := store.Get(ctx, s.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "test" || got.Command != "echo" {
		t.Fatalf("unexpected server: %+v", got)
	}

	list, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 server, got %d", len(list))
	}

	s.Name = "renamed"
	if err := store.Update(ctx, s); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ = store.Get(ctx, s.ID)
	if got.Name != "renamed" {
		t.Fatalf("expected name updated, got %s", got.Name)
	}

	if err := store.Delete(ctx, s.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := store.Get(ctx, s.ID); !isNotFound(err) {
		t.Fatalf("expected not found after delete, got %v", err)
	}
}

func TestValidateServerRejectsInjection(t *testing.T) {
	cases := []struct {
		name    string
		server  Server
		wantErr bool
	}{
		{"valid", Server{Name: "x", Command: "node", Args: []string{"script.js"}, Env: map[string]string{"A": "b"}}, false},
		{"missing name", Server{Command: "node"}, true},
		{"missing command", Server{Name: "x"}, true},
		{"shell injection", Server{Name: "x", Command: "node; rm -rf /"}, true},
		{"bad restart", Server{Name: "x", Command: "node", RestartPolicy: "sometimes"}, true},
		{"null character arg", Server{Name: "x", Command: "node", Args: []string{"bad\x00"}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateServer(&tc.server)
			if tc.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestParseMessage(t *testing.T) {
	msg, err := ParseMessage([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if msg.Method != "tools/list" {
		t.Fatalf("unexpected method: %s", msg.Method)
	}
	if _, err := ParseMessage([]byte("not json")); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestFormatSSE(t *testing.T) {
	out := FormatSSE([]byte(`{"ok":true}`))
	want := "data: {\"ok\":true}\n\n"
	if string(out) != want {
		t.Fatalf("got %q, want %q", out, want)
	}
}

func TestManagerStartSession(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping subprocess test on windows")
	}
	ctx := context.Background()
	db := testDB(t)
	store := NewStore(db)
	mgr := NewManager(store)

	bin := writeEchoServer(t)
	s := &Server{Name: "echo", Command: bin, Args: []string{}, Enabled: true, MaxClients: 2}
	if err := store.Create(ctx, s); err != nil {
		t.Fatalf("create: %v", err)
	}

	sess, err := mgr.StartSession(ctx, s.ID)
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer sess.Close()

	if sess.IsClosed() {
		t.Fatal("session closed immediately")
	}

	ch := sess.Subscribe()
	defer sess.Unsubscribe(ch)

	if err := sess.Write(Message{JSONRPC: "2.0", ID: 1, Method: "ping"}); err != nil {
		t.Fatalf("write: %v", err)
	}

	select {
	case line := <-ch:
		if len(line) == 0 {
			t.Fatal("empty response")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for response")
	}

	_ = sess.Close()
	time.Sleep(100 * time.Millisecond)
	if !sess.IsClosed() {
		t.Fatal("session should be closed")
	}
}

func TestManagerRespectsMaxClients(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping subprocess test on windows")
	}
	ctx := context.Background()
	db := testDB(t)
	store := NewStore(db)
	mgr := NewManager(store)

	bin := writeSleepServer(t)
	s := &Server{Name: "sleep", Command: bin, Args: []string{}, Enabled: true, MaxClients: 1}
	if err := store.Create(ctx, s); err != nil {
		t.Fatalf("create: %v", err)
	}

	sess1, err := mgr.StartSession(ctx, s.ID)
	if err != nil {
		t.Fatalf("start first: %v", err)
	}
	defer sess1.Close()

	if _, err := mgr.StartSession(ctx, s.ID); err == nil {
		t.Fatal("expected max clients error")
	}
}

func TestTestServer(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping subprocess test on windows")
	}
	ctx := context.Background()
	db := testDB(t)
	store := NewStore(db)
	mgr := NewManager(store)

	bin := writeEchoServer(t)
	s := &Server{Name: "echo", Command: bin, Args: []string{}, Enabled: true}
	if err := store.Create(ctx, s); err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := mgr.TestServer(ctx, s.ID); err != nil {
		t.Fatalf("test server: %v", err)
	}
}

func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	return err.Error() == ErrNotFound.Error()
}

func writeEchoServer(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "echoserver")
	src := `package main
import (
	"bufio"
	"encoding/json"
	"os"
)
func main() {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var msg map[string]interface{}
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			continue
		}
		msg["type"] = "pong"
		out, _ := json.Marshal(msg)
		os.Stdout.Write(out)
		os.Stdout.Write([]byte("\n"))
		os.Stdout.Sync()
	}
}
`
	f, err := os.CreateTemp(dir, "*.go")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	f.WriteString(src)
	f.Close()

	cmd := fmt.Sprintf("go build -o %s %s", bin, f.Name())
	if out, err := runShell(cmd); err != nil {
		t.Fatalf("build echo server: %v\n%s", err, out)
	}
	return bin
}

func writeSleepServer(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "sleepserver")
	src := `package main
import "time"
func main() { time.Sleep(30 * time.Second) }
`
	f, err := os.CreateTemp(dir, "*.go")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	f.WriteString(src)
	f.Close()

	cmd := fmt.Sprintf("go build -o %s %s", bin, f.Name())
	if out, err := runShell(cmd); err != nil {
		t.Fatalf("build sleep server: %v\n%s", err, out)
	}
	return bin
}

func runShell(cmd string) (string, error) {
	c := exec.Command("sh", "-c", cmd)
	out, err := c.CombinedOutput()
	return string(out), err
}
