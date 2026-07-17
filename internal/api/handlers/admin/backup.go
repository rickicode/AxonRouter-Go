package admin

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/backup"
	appdb "github.com/rickicode/AxonRouter-Go/internal/db"
)

type BackupHandler struct {
	db         *sql.DB
	writeQueue *appdb.WriteQueue
}

func NewBackupHandler(database *sql.DB, writeQueue *appdb.WriteQueue) *BackupHandler {
	return &BackupHandler{db: database, writeQueue: writeQueue}
}

func (h *BackupHandler) Download(c *gin.Context) {
	categories := parseBackupCategories(c.Query("categories"))
	if err := validateBackupCategories(categories); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	password := c.Query("password")

	c.Header("Content-Type", "application/x-ndjson")
	c.Header("Content-Disposition", `attachment; filename="axonrouter-backup.ndjson"`)
	c.Header("Transfer-Encoding", "chunked")
	c.Status(http.StatusOK)

	if err := backup.NewScanner(h.db).Backup(c.Request.Context(), c.Writer, categories, password); err != nil {
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

	target := backup.RestoreTarget(c.DefaultPostForm("target", string(backup.RestoreTargetCurrent)))
	result, err := backup.Restore(c.Request.Context(), src, backup.RestoreOptions{
		Target:     target,
		Password:   c.PostForm("password"),
		CurrentDB:  h.db,
		WriteQueue: h.writeQueue,
		SQLitePath: c.PostForm("sqlite_path"),
		TursoURL:   c.PostForm("turso_url"),
		TursoToken: c.PostForm("turso_token"),
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
}

func parseBackupCategories(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	categories := make([]string, 0, len(parts))
	for _, part := range parts {
		category := strings.TrimSpace(part)
		if category != "" {
			categories = append(categories, category)
		}
	}
	return categories
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
		"sqlite restore target path is required",
		"turso restore target url is required",
		"unsupported backup format",
		"unsupported backup version",
		"unsupported restore target",
		"unknown backup category",
	}
	for _, clientErr := range clientErrors {
		if strings.Contains(msg, clientErr) {
			return true
		}
	}
	return errors.Is(err, http.ErrMissingFile)
}
