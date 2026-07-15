//go:build !tray

package tray

import (
	"database/sql"
	"errors"

	"github.com/rickicode/AxonRouter-Go/internal/api"
	"github.com/rickicode/AxonRouter-Go/internal/config"
)

// Run returns an error explaining that tray support was not compiled into
// this binary. Rebuild with -tags tray to enable it.
func Run(port string, router *api.Router, database *sql.DB, httpsCfg *config.HTTPSConfig) error {
	_ = port
	_ = router
	_ = database
	_ = httpsCfg
	return errors.New("system tray support is not enabled in this build; rebuild with -tags tray")
}
