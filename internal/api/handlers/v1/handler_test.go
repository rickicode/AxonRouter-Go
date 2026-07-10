package v1

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/rickicode/AxonRouter-Go/internal/auth"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/quota"
)

// fakeExecutor records calls and returns programmed responses.
type fakeExecutor struct {
	callCount int
	responses []struct {
		resp *executor.Response
		err  error
	}
	streamErr bool
}

func (f *fakeExecutor) Execute(ctx context.Context, req *executor.Request) (*executor.Response, error) {
	idx := f.callCount
	f.callCount++
	if idx >= len(f.responses) {
		return nil, errors.New("no more responses")
	}
	return f.responses[idx].resp, f.responses[idx].err
}

func (f *fakeExecutor) ExecuteStream(ctx context.Context, req *executor.Request) (*executor.StreamResult, error) {
	f.streamErr = true
	return nil, errors.New("streaming not supported")
}

type fakeOAuthService struct {
	refreshFunc func(ctx context.Context, creds *auth.Credentials) (*auth.Credentials, error)
}

func (f *fakeOAuthService) GenerateAuthURL(ctx context.Context, state string) (string, error) {
	return "", nil
}

func (f *fakeOAuthService) ExchangeCode(ctx context.Context, code string) (*auth.Credentials, error) {
	return nil, nil
}

func (f *fakeOAuthService) RefreshToken(ctx context.Context, creds *auth.Credentials) (*auth.Credentials, error) {
	return f.refreshFunc(ctx, creds)
}

func (f *fakeOAuthService) StartLocalServer(ctx context.Context, state string) (int, chan *auth.Credentials, error) {
	return 0, nil, nil
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "handler-test.db")
	database, err := sql.Open("sqlite", tmp)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	database.SetMaxOpenConns(1)
	if err := db.RunMigrations(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	store := connstate.NewStore()
	store.SeedConnection("conn-1", "test", "ready", 0)
	mgr := auth.NewManager()
	return &Handler{
		db:         openTestDB(t),
		store:      store,
		elig:       connstate.NewEligibilityManager(store),
		authMgr:    mgr,
		exhaustion: quota.NewExhaustionCache(),
	}
}

func TestExecuteWithRetry_AuthRefreshThenSuccess(t *testing.T) {
	h := newTestHandler(t)
	h.authMgr.RegisterService(auth.ProviderType("test"), &fakeOAuthService{
		refreshFunc: func(ctx context.Context, creds *auth.Credentials) (*auth.Credentials, error) {
			return &auth.Credentials{AccessToken: "fresh-token", RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Hour)}, nil
		},
	})

	fe := &fakeExecutor{
		responses: []struct {
			resp *executor.Response
			err  error
		}{
			{nil, errors.New("401 unauthorized")},
			{&executor.Response{StatusCode: 200, Body: []byte(`ok`)}, nil},
		},
	}

	conn := &Connection{
		ID:             "conn-1",
		RefreshToken:   "old-refresh",
		OAuthExpiresAt: time.Now().Add(-time.Minute),
	}

	req := &executor.Request{Stream: false}
	resp, _, err := h.executeWithRetry(context.Background(), fe, req, conn, "test", "model-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if req.AccessToken != "fresh-token" {
		t.Errorf("access token was not refreshed, got %q", req.AccessToken)
	}
	if fe.callCount != 2 {
		t.Errorf("expected 2 calls, got %d", fe.callCount)
	}
}

func TestExecuteWithRetry_GivesUpAfter3(t *testing.T) {
	h := newTestHandler(t)

	fe := &fakeExecutor{
		responses: []struct {
			resp *executor.Response
			err  error
		}{
			{nil, errors.New("401 unauthorized")},
			{nil, errors.New("401 unauthorized")},
			{nil, errors.New("401 unauthorized")},
		},
	}

	conn := &Connection{ID: "conn-1"}
	req := &executor.Request{Stream: false}

	start := time.Now()
	_, _, err := h.executeWithRetry(context.Background(), fe, req, conn, "test", "model-1")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error after retries")
	}
	if fe.callCount != 3 {
		t.Errorf("expected 3 calls, got %d", fe.callCount)
	}
	// Linear backoff: sleeps of 1s + 2s = 3s minimum.
	if elapsed < 3*time.Second {
		t.Errorf("expected at least 3s delay, got %v", elapsed)
	}
}
