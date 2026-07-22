package background

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/auth"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/quota"
)

// tokenRefreshLead maps provider_type_id to how long before expiry we refresh.
// Matches refreshLeadMs in internal/api/handlers/v1/handler.go.
var tokenRefreshLead = map[string]time.Duration{
	"cx":       5 * time.Minute,
	"ag":       15 * time.Minute,
	"kiro":     5 * time.Minute,
	"copilot":  5 * time.Minute,
	"grok-cli": 5 * time.Minute,
}

const defaultTokenRefreshLead = 5 * time.Minute

// TokenRefreshScheduler runs a standalone periodic OAuth token refresh loop.
// It is independent of the quota scheduler and refreshes tokens before they
// expire so that request-time proactive refresh is a last resort.
type TokenRefreshScheduler struct {
	once     sync.Once
	db       *sql.DB
	wq       *db.WriteQueue
	store    *connstate.Store
	elig     *connstate.EligibilityManager
	authMgr  *auth.Manager
	interval time.Duration
	stopCh   chan struct{}
	stopOnce sync.Once

	// inProgress prevents duplicate refreshes for the same connection across ticks.
	inProgress sync.Map
}

// refreshRow is the DB row shape the scheduler needs.
type refreshRow struct {
	id                   string
	providerTypeID       string
	accessToken          string
	refreshToken         string
	expiresAt            int64
	providerSpecificData string
	status               string
}

// NewTokenRefreshScheduler creates a new standalone token refresh scheduler.
// intervalMin controls how often it scans for tokens approaching expiry.
func NewTokenRefreshScheduler(
	database *sql.DB,
	wq *db.WriteQueue,
	store *connstate.Store,
	elig *connstate.EligibilityManager,
	authMgr *auth.Manager,
	intervalMin int,
) *TokenRefreshScheduler {
	if intervalMin <= 0 {
		intervalMin = 1
	}
	return &TokenRefreshScheduler{
		db:       database,
		wq:       wq,
		store:    store,
		elig:     elig,
		authMgr:  authMgr,
		interval: time.Duration(intervalMin) * time.Minute,
		stopCh:   make(chan struct{}),
	}
}

// Start launches the background goroutine (sync.Once).
func (s *TokenRefreshScheduler) Start(ctx context.Context) {
	s.once.Do(func() {
		go s.run(ctx)
	})
}

func (s *TokenRefreshScheduler) run(ctx context.Context) {
	log.Println("background: token refresh scheduler started")

	// Immediate first run
	s.check()

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.check()
		case <-ctx.Done():
			log.Println("background: token refresh scheduler stopped")
			return
		case <-s.stopCh:
			log.Println("background: token refresh scheduler stopped")
			return
		}
	}
}

// check scans for OAuth tokens that need refreshing and triggers refreshes.
func (s *TokenRefreshScheduler) check() {
	if s.authMgr == nil {
		return
	}

	deadline := time.Now().Add(s.maxLead()).Unix()

	rows, err := s.db.Query(`
		SELECT id, provider_type_id, oauth_token, oauth_refresh_token, oauth_expires_at,
		       COALESCE(provider_specific_data, ''), status
		FROM connections
		WHERE auth_type = 'oauth'
		  AND is_active = 1
		  AND oauth_refresh_token IS NOT NULL
		  AND oauth_refresh_token != ''
		  AND oauth_expires_at <= ?
	`, deadline)
	if err != nil {
		log.Printf("background: token refresh scan failed: %v", err)
		return
	}
	defer rows.Close()

	var toRefresh []refreshRow
	for rows.Next() {
		var r refreshRow
		if err := rows.Scan(&r.id, &r.providerTypeID, &r.accessToken, &r.refreshToken, &r.expiresAt, &r.providerSpecificData, &r.status); err != nil {
			log.Printf("background: token refresh scan row: %v", err)
			continue
		}
		toRefresh = append(toRefresh, r)
	}

	for _, r := range toRefresh {
		r := r
		if _, ok := s.inProgress.LoadOrStore(r.id, true); ok {
			continue
		}
		go func() {
			defer s.inProgress.Delete(r.id)
			s.refresh(r)
		}()
	}
}

// refresh performs a single token refresh.
func (s *TokenRefreshScheduler) refresh(r refreshRow) {
	providerType := auth.ProviderType(r.providerTypeID)
	if _, ok := s.authMgr.GetService(providerType); !ok {
		return
	}

	lead := tokenRefreshLead[r.providerTypeID]
	if lead == 0 {
		lead = defaultTokenRefreshLead
	}
	if time.Until(time.Unix(r.expiresAt, 0)) > lead {
		return
	}

	providerSpecific := map[string]string{}
	if r.providerSpecificData != "" {
		var raw map[string]any
		if err := json.Unmarshal([]byte(r.providerSpecificData), &raw); err == nil {
			for k, v := range raw {
				if str, ok := v.(string); ok {
					providerSpecific[k] = str
				}
			}
		}
	}

	creds := &auth.Credentials{
		AccessToken:      r.accessToken,
		RefreshToken:     r.refreshToken,
		ExpiresAt:        time.Unix(r.expiresAt, 0),
		ProviderSpecific: providerSpecific,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	newCreds, err := s.authMgr.RefreshTokenForConnection(ctx, r.id, providerType, creds)
	if err != nil {
		log.Printf("background: token refresh failed for %s/%s: %v", r.providerTypeID, r.id, err)
		if quota.IsUnrecoverableRefreshError(err) {
			s.markFailed(r, err)
		}
		return
	}

	s.persist(r, newCreds)
}

// persist updates the connection status after a successful refresh. Token fields
// are persisted by auth.Manager.RefreshTokenForConnection using a CAS write.
func (s *TokenRefreshScheduler) persist(r refreshRow, newCreds *auth.Credentials) {
	expiresAt := newCreds.ExpiresAt.Unix()
	if expiresAt == 0 {
		expiresAt = time.Now().Add(time.Hour).Unix()
	}

	now := time.Now().Unix()
	update := func(d *sql.DB) error {
		_, err := d.Exec(`
			UPDATE connections
			SET status = 'ready', updated_at = ?
			WHERE id = ?
		`, now, r.id)
		return err
	}

	if s.wq != nil {
		s.wq.Enqueue("tokenRefresh:setReady", update)
	} else {
		if err := update(s.db); err != nil {
			log.Printf("background: failed to update status for %s: %v", r.id, err)
			return
		}
	}

	if s.store != nil {
		if cs := s.store.Get(r.id); cs != nil {
			cs.SetStatus(connstate.StatusReady, "")
		}
	}

	log.Printf("background: refreshed token for %s/%s, expires_at=%d", r.providerTypeID, r.id, expiresAt)
}

// markFailed disables a connection after an unrecoverable refresh error.
func (s *TokenRefreshScheduler) markFailed(r refreshRow, err error) {
	now := time.Now().Unix()
	update := func(d *sql.DB) error {
		_, derr := d.Exec(`
			UPDATE connections
			SET is_active = 0, status = 'auth_failed', updated_at = ?
			WHERE id = ?
		`, now, r.id)
		return derr
	}

	if s.wq != nil {
		s.wq.Enqueue("tokenRefresh:authFailed", update)
	} else {
		if derr := update(s.db); derr != nil {
			log.Printf("background: failed to mark %s auth_failed: %v", r.id, derr)
		}
	}

	if s.store != nil {
		if cs := s.store.Get(r.id); cs != nil {
			cs.SetStatus(connstate.StatusAuthFailed, err.Error())
		}
		if s.elig != nil {
			s.elig.ScheduleUpdateProvider(r.providerTypeID)
		}
	}
}

// maxLead returns the largest refresh lead so the DB scan catches every provider.
func (s *TokenRefreshScheduler) maxLead() time.Duration {
	max := defaultTokenRefreshLead
	for _, lead := range tokenRefreshLead {
		if lead > max {
			max = lead
		}
	}
	return max
}

// Stop signals the scheduler to stop.
func (s *TokenRefreshScheduler) Stop() {
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
}
