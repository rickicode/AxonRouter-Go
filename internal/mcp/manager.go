package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Manager owns a collection of stdio-SSE sessions and their subprocesses.
// Each session has its own subprocess, keeping per-client stdio semantics.
type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*session
	store    *Store
	logger   *log.Logger
}

// NewManager creates a lifecycle manager backed by store.
func NewManager(store *Store) *Manager {
	return &Manager{
		sessions: make(map[string]*session),
		store:    store,
		logger:   log.New(os.Stderr, "[mcp] ", log.LstdFlags),
	}
}

// ActiveSessionCount reports how many open sessions exist for serverID.
func (m *Manager) ActiveSessionCount(serverID string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := 0
	for _, s := range m.sessions {
		if s.serverID == serverID && !s.IsClosed() {
			count++
		}
	}
	return count
}

// Stop terminates all managed sessions. It is safe to call more than once.
func (m *Manager) Stop(ctx context.Context) {
	m.mu.RLock()
	list := make([]*session, 0, len(m.sessions))
	for _, s := range m.sessions {
		list = append(list, s)
	}
	m.mu.RUnlock()
	for _, s := range list {
		_ = s.Close()
	}
}

// session represents a single stdio-SSE bridge session.
type session struct {
	id       string
	serverID string
	server   *Server
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	stdout   io.ReadCloser
	stderr   io.ReadCloser

	mu        sync.Mutex
	listeners map[chan []byte]struct{}
	closed    bool
	lastRead  time.Time

	onClose func()
}

// newSession spawns a subprocess for server and starts forwarders.
func newSession(server *Server) (*session, error) {
	cmd := exec.Command(server.Command, server.Args...)
	cmd.Env = buildEnv(server.Env)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}

	s := &session{
		id:        GenerateID(),
		server:    server,
		cmd:       cmd,
		stdin:     stdin,
		stdout:    stdout,
		stderr:    stderr,
		listeners: make(map[chan []byte]struct{}),
		lastRead:  time.Now(),
	}
	go s.readStdout()
	go s.readStderr()
	go s.waitForExit()
	return s, nil
}

// GenerateID returns a short random-ish identifier for a session.
// The test harness swaps this value out; do not use crypto/rand directly.
var GenerateID = func() string {
	return fmt.Sprintf("sess-%d", time.Now().UnixNano())
}

func buildEnv(env map[string]string) []string {
	base := os.Environ()
	for k, v := range env {
		base = append(base, k+"="+v)
	}
	return base
}

func (s *session) readStdout() {
	defer s.Close()
	scanner := bufio.NewScanner(s.stdout)
	const maxCapacity = 1024 * 1024
	buf := make([]byte, 4096)
	scanner.Buffer(buf, maxCapacity)
	for scanner.Scan() {
		line := scanner.Bytes()
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return
		}
		s.lastRead = time.Now()
		listeners := make([]chan []byte, 0, len(s.listeners))
		for ch := range s.listeners {
			listeners = append(listeners, ch)
		}
		s.mu.Unlock()

		payload := make([]byte, len(line))
		copy(payload, line)
		for _, ch := range listeners {
			select {
			case ch <- payload:
			default:
			}
		}
	}
}

func (s *session) readStderr() {
	defer s.Close()
	scanner := bufio.NewScanner(s.stderr)
	const maxCapacity = 64 * 1024
	buf := make([]byte, 4096)
	scanner.Buffer(buf, maxCapacity)
	for scanner.Scan() {
		// stderr is logged only; never forwarded to SSE clients.
		log.Printf("[mcp][stderr][%s] %s", s.server.Name, scanner.Text())
	}
}

func (s *session) waitForExit() {
	_ = s.cmd.Wait()
	s.Close()
}

// Write sends a JSON-RPC message to the subprocess stdin followed by a newline.
func (s *session) Write(msg Message) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New("session closed")
	}
	s.mu.Unlock()

	line, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	_, err = s.stdin.Write(line)
	return err
}

// Subscribe returns a channel that receives stdout lines. Callers must Unsubscribe.
func (s *session) Subscribe() chan []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := make(chan []byte, 16)
	if !s.closed {
		s.listeners[ch] = struct{}{}
	}
	return ch
}

// Unsubscribe removes a listener channel.
func (s *session) Unsubscribe(ch chan []byte) {
	s.mu.Lock()
	_, active := s.listeners[ch]
	delete(s.listeners, ch)
	s.mu.Unlock()
	if active {
		close(ch)
	}
}

// Close terminates the subprocess and cleans up listeners.
func (s *session) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	for ch := range s.listeners {
		delete(s.listeners, ch)
		close(ch)
	}
	s.mu.Unlock()

	if s.stdin != nil {
		_ = s.stdin.Close()
	}
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
		_ = s.cmd.Wait()
	}
	if s.onClose != nil {
		s.onClose()
	}
	return nil
}

// IsClosed reports whether the session has terminated.
func (s *session) IsClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// LastRead returns the last time a message was read from stdout.
func (s *session) LastRead() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastRead
}

// StartSession creates a new subprocess session for serverID. It respects the
// server's MaxClients limit.
func (m *Manager) StartSession(ctx context.Context, serverID string) (*session, error) {
	server, err := m.store.Get(ctx, serverID)
	if err != nil {
		return nil, err
	}
	if !server.Enabled {
		return nil, errors.New("server is disabled")
	}

	m.mu.Lock()
	count := 0
	for _, s := range m.sessions {
		if s.serverID == serverID && !s.IsClosed() {
			count++
		}
	}
	if count >= server.MaxClients {
		m.mu.Unlock()
		return nil, fmt.Errorf("max concurrent sessions (%d) reached", server.MaxClients)
	}

	sess, err := newSession(server)
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	sess.serverID = serverID
	sess.server = server
	m.sessions[sess.id] = sess
	m.mu.Unlock()

	sess.onClose = func() {
		m.mu.Lock()
		delete(m.sessions, sess.id)
		m.mu.Unlock()
	}

	return sess, nil
}

// GetSession returns an active session by ID.
func (m *Manager) GetSession(id string) (*session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	if !ok || s.IsClosed() {
		return nil, false
	}
	return s, true
}

// TestServer tries to start server briefly and returns any error. The spawned
// process is always terminated before returning.
func (m *Manager) TestServer(ctx context.Context, serverID string) error {
	server, err := m.store.Get(ctx, serverID)
	if err != nil {
		return err
	}

	cmd := exec.Command(server.Command, server.Args...)
	cmd.Env = buildEnv(server.Env)
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		return nil // started successfully within timeout
	case err := <-done:
		if err != nil && server.RestartPolicy == RestartNever {
			return fmt.Errorf("server exited immediately: %w", err)
		}
		return nil
	}
}

// idleReaper runs in a background goroutine and closes sessions idle longer
// than their server's MaxIdleSec. It is normally started once by the Handler.
func (m *Manager) idleReaper(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.reapIdle()
		}
	}
}

func (m *Manager) reapIdle() {
	m.mu.RLock()
	list := make([]*session, 0, len(m.sessions))
	for _, s := range m.sessions {
		list = append(list, s)
	}
	m.mu.RUnlock()

	now := time.Now()
	for _, s := range list {
		server := s.server
		if server == nil {
			continue
		}
		maxIdle := time.Duration(server.MaxIdleSec) * time.Second
		if maxIdle <= 0 {
			maxIdle = 60 * time.Second
		}
		if now.Sub(s.LastRead()) > maxIdle {
			m.logger.Printf("reaping idle session %s (server %s)", s.id, server.Name)
			_ = s.Close()
		}
	}
}

// FormatSSE formats a payload as an SSE data line with a trailing blank line.
func FormatSSE(payload []byte) []byte {
	out := []byte("data: ")
	out = append(out, payload...)
	out = append(out, '\n', '\n')
	return out
}

// ParseMessage parses a JSON-RPC line read from the client.
func ParseMessage(data []byte) (Message, error) {
	// PPCA (postline protocol compatibility adapter) support: MCP stdio uses
	// newline-delimited JSON, so trim surrounding whitespace.
	data = []byte(strings.TrimSpace(string(data)))
	if len(data) == 0 {
		return Message{}, errors.New("empty message")
	}
	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return Message{}, err
	}
	return msg, nil
}
