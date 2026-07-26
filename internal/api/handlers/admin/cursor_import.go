package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/auth/cursor"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/db"
)

// CursorImportHandler exposes endpoints for importing Cursor IDE credentials.
type CursorImportHandler struct {
	db       *sql.DB
	store    *connstate.Store
	elig     *connstate.EligibilityManager
	client   *http.Client
	searchFn func(ctx context.Context, roots cursor.SearchRoots) (*cursor.DiscoveredAuth, error)
}

// NewCursorImportHandler creates a Cursor import handler.
func NewCursorImportHandler(database *sql.DB, store *connstate.Store, elig *connstate.EligibilityManager) *CursorImportHandler {
	return &CursorImportHandler{
		db:       database,
		store:    store,
		elig:     elig,
		client:   &http.Client{Timeout: 15 * time.Second},
		searchFn: cursor.Discover,
	}
}

// ImportCursorToken reads the Cursor IDE access token from the local VS Code:
// SQLite state file, validates it with Cursor's upstream API, and creates a
// ready OAuth connection for the 'cursor' provider.
func (h *CursorImportHandler) ImportCursorToken(c *gin.Context) {
	ctx := c.Request.Context()

	discovered, err := h.searchFn(ctx, cursor.DefaultSearchRoots())
	if err != nil {
		if de, ok := err.(*cursor.DiscoveryError); ok {
			c.JSON(http.StatusNotFound, gin.H{
				"error":       de.Error(),
				"tried_paths": de.TriedPaths,
				"guide":       "Open Cursor IDE, sign in, then retry. If Cursor is on a different machine, use the manual OAuth import-token flow instead.",
			})
			return
		}
		log.Printf("Cursor token discovery failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read Cursor IDE state"})
		return
	}

	if discovered == nil || discovered.AccessToken == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "Cursor access token not found"})
		return
	}

	// Validate the token without logging it.
	emailHash := cursor.HashedEmail(discovered.Email)
	log.Printf("Cursor token discovered source=%s email_hash=%s token_hash=%s", discovered.Source, emailHash, tokenHash(discovered.AccessToken))

	if _, err := cursor.ValidateToken(ctx, h.client, discovered.AccessToken); err != nil {
		log.Printf("Cursor token validation failed email_hash=%s: %v", emailHash, err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	now := time.Now().Unix()
	expiresAt := cursor.ExpiresAt(discovered.AccessToken).Unix()
	if expiresAt <= 0 {
		// JWT had no exp; fall back to a short-lived ready state.
		expiresAt = now + int64(24*time.Hour.Seconds())
	}

	psd := map[string]string{"authMethod": "cursor_ide_import"}
	if discovered.SignUpType != "" {
		psd["signUpType"] = discovered.SignUpType
	}
	psdJSON, _ := json.Marshal(psd)
	providerSpecific := sql.NullString{String: string(psdJSON), Valid: true}

	connName := discovered.Email
	if connName == "" {
		connName = "Cursor IDE"
	}
	accountKey := discovered.Email
	if accountKey == "" {
		accountKey = "cursor-ide"
	}

	connID, _, err := db.UpsertOAuthConnection(ctx, h.db, "cursor", accountKey, connName, discovered.AccessToken, discovered.RefreshToken, expiresAt, providerSpecific, now)
	if err != nil {
		log.Printf("Cursor connection persistence failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save Cursor connection"})
		return
	}

	if h.store != nil {
		h.store.SeedConnection(connID, "cursor", "ready", 0)
		if h.elig != nil {
			h.elig.Update(h.store)
		}
	}

	log.Printf("Cursor connection created id=%s email_hash=%s", connID, emailHash)
	c.JSON(http.StatusCreated, gin.H{
		"id":         connID,
		"name":       connName,
		"status":     "ready",
		"expires_at": expiresAt,
		"email_hash": emailHash,
		"source":     discovered.Source,
	})
}

func tokenHash(token string) string {
	return cursor.HashedEmail(token)[:16]
}
