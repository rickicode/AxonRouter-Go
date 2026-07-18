package admin

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/backup"
	appdb "github.com/rickicode/AxonRouter-Go/internal/db"
)

type BackupHandler struct {
	db         *sql.DB
	writeQueue *appdb.WriteQueue
	// onRestoreRestart is called after a successful restore to the current
	// database. The callback should trigger a graceful shutdown so that the
	// process manager (systemd/Docker/etc.) can restart the gateway with fresh
	// in-memory caches.
	onRestoreRestart func()
}

func NewBackupHandler(database *sql.DB, writeQueue *appdb.WriteQueue, onRestoreRestart func()) *BackupHandler {
	return &BackupHandler{db: database, writeQueue: writeQueue, onRestoreRestart: onRestoreRestart}
}

// SetRestoreRestartCallback wires the auto-restart callback after the router has
// been created. The callback is invoked after a successful restore.
func (h *BackupHandler) SetRestoreRestartCallback(fn func()) {
	h.onRestoreRestart = fn
}

func (h *BackupHandler) Download(c *gin.Context) {
	var req struct {
		Password string `json:"password"`
	}
	// An empty body is fine; only the optional encryption password matters.
	_ = c.ShouldBindJSON(&req)

	c.Header("Content-Type", "application/x-ndjson")
	c.Header("Content-Disposition", `attachment; filename="axonrouter-backup.ndjson"`)
	c.Header("Transfer-Encoding", "chunked")
	c.Status(http.StatusOK)

	// Always back up every category so the restore can produce an identical server.
	if err := backup.NewScanner(h.db).Backup(c.Request.Context(), c.Writer, []string{}, req.Password); err != nil {
		c.Error(err)
		return
	}
}

func (h *BackupHandler) Restore(c *gin.Context) {
	file, err := c.FormFile("backup")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "backup file is required"})
		return
	}
	src, err := file.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	defer src.Close()

	result, err := backup.Restore(c.Request.Context(), src, backup.RestoreOptions{
		Password:   c.PostForm("password"),
		CurrentDB:  h.db,
		WriteQueue: h.writeQueue,
	})
	if err != nil {
		status := http.StatusInternalServerError
		if isRestoreClientError(err) {
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})

	if h.onRestoreRestart != nil {
		// Give the HTTP response a moment to flush before shutting down. The
		// service manager / Docker restart policy will bring the gateway back up
		// with refreshed in-memory state.
		go func() {
			time.Sleep(2 * time.Second)
			h.onRestoreRestart()
		}()
	}
}

func validateBackupCategories(categories []string) error {
	for _, category := range categories {
		if _, ok := backup.CategoryTables[category]; !ok {
			return errors.New("unknown backup category " + category)
		}
	}
	return nil
}

func isRestoreClientError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	clientErrors := []string{
		"backup payload is empty",
		"current restore target database is required",
		"decrypt backup payload",
		"decode backup header",
		"decode backup row",
		"unsupported backup format",
		"unsupported backup version",
		"unknown backup category",
	}
	for _, clientErr := range clientErrors {
		if strings.Contains(msg, clientErr) {
			return true
		}
	}
	return errors.Is(err, http.ErrMissingFile)
}
