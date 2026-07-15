//go:build tray

package tray

import (
	"database/sql"
	"fmt"
	"log"
	"sync/atomic"

	"github.com/getlantern/systray"
	"github.com/pkg/browser"
	"github.com/rickicode/AxonRouter-Go/internal/api"
	"github.com/rickicode/AxonRouter-Go/internal/config"
)

// Run starts the server under a system tray icon.
//
// The caller is expected to have already constructed the router and database.
// This function blocks until the user selects Exit from the tray menu.
func Run(port string, router *api.Router, database *sql.DB, httpsCfg *config.HTTPSConfig) error {
	addr := fmt.Sprintf(":%s", port)
	running := &atomic.Bool{}
	stopped := &atomic.Bool{}

	startServer := func() {
		if stopped.Load() || running.Load() {
			return
		}
		running.Store(true)
		go func() {
			if err := router.Start(addr, *httpsCfg); err != nil {
				log.Printf("server stopped: %v", err)
			}
			running.Store(false)
		}()
	}

	stopServer := func() {
		stopped.Store(true)
		if !running.Load() {
			return
		}
		router.Shutdown()
	}

	onReady := func() {
		systray.SetTooltip("AxonRouter-Go")
		systray.SetTitle("AxonRouter")

		mOpen := systray.AddMenuItem("Open Dashboard", "Open the dashboard in a browser")
		mStart := systray.AddMenuItem("Start", "Start the server")
		mStop := systray.AddMenuItem("Stop", "Stop the server")
		mExit := systray.AddMenuItem("Exit", "Quit AxonRouter")

		startServer()

		for {
			select {
			case <-mOpen.ClickedCh:
				browser.OpenURL(fmt.Sprintf("http://localhost:%s", port))
			case <-mStart.ClickedCh:
				startServer()
			case <-mStop.ClickedCh:
				stopServer()
			case <-mExit.ClickedCh:
				stopServer()
				if database != nil {
					if err := database.Close(); err != nil {
						log.Printf("WARN: failed to close database: %v", err)
					}
				}
				systray.Quit()
				return
			}
		}
	}

	onExit := func() {}

	systray.Run(onReady, onExit)
	return nil
}
