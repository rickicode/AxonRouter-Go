package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	"github.com/gin-gonic/gin"
	kardianos "github.com/kardianos/service"
	"github.com/rickicode/AxonRouter-Go/internal/api"
	"github.com/rickicode/AxonRouter-Go/internal/config"
	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/rickicode/AxonRouter-Go/internal/models"
	"github.com/rickicode/AxonRouter-Go/internal/service"
	"github.com/rickicode/AxonRouter-Go/internal/tray"
	"github.com/rickicode/AxonRouter-Go/internal/version"
	// Trigger registration of all request/response format translators.
	_ "github.com/rickicode/AxonRouter-Go/internal/translator"

	"golang.org/x/crypto/bcrypt"
)

func printStartupBanner(port string, database *sql.DB) {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		printStartupBannerPlain(port, database)
		return
	}

	const (
		reset   = "\033[0m"
		bold    = "\033[1m"
		dim     = "\033[2m"
		green   = "\033[32m"
		yellow  = "\033[33m"
		blue    = "\033[34m"
		magenta = "\033[35m"
		cyan    = "\033[36m"
	)

	var activeConns int
	if err := database.QueryRow("SELECT COUNT(*) FROM connections WHERE is_active = 1").Scan(&activeConns); err != nil {
		logging.Logger.Warn("failed to read active connection count for startup banner", "error", err)
	}

	providers := models.ProviderCount()
	modelCount := models.ModelCount()
	ver := version.String()
	sep := cyan + "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━" + reset

	fmt.Println()
	fmt.Println(sep)
	fmt.Printf(" %s%sAxonRouter-Go%s %s%s%s ready on port %s%s%s\n", bold, cyan, reset, dim, ver, reset, green, port, reset)
	fmt.Println(sep)
	fmt.Printf(" %sDashboard%s http://localhost:%s\n", yellow, reset, port)
	fmt.Printf("  %sRoutes%s      %s%d active connections%s · %s%d providers%s · %s%d models%s\n",
		yellow, reset,
		cyan, activeConns, reset,
		cyan, providers, reset,
		cyan, modelCount, reset)
	fmt.Printf("  %sBackground%s  cleanup · quota · usage flush\n", yellow, reset)
	fmt.Println(sep)
	fmt.Println()
}

func printStartupBannerPlain(port string, database *sql.DB) {
	var activeConns int
	if err := database.QueryRow("SELECT COUNT(*) FROM connections WHERE is_active = 1").Scan(&activeConns); err != nil {
		logging.Logger.Warn("failed to read active connection count for startup banner", "error", err)
	}
	providers := models.ProviderCount()
	modelCount := models.ModelCount()
	ver := version.String()

	fmt.Println()
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println(" AxonRouter-Go " + ver + " ready on port " + port)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf(" Dashboard http://localhost:%s\n", port)
	fmt.Printf("  Routes      %d active connections · %d providers · %d models\n",
		activeConns, providers, modelCount)
	fmt.Println("  Background  cleanup · quota · usage flush")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
}

func isTerminal(fd uintptr) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	stat, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) != 0
}

func colorize(enable bool, code, text string) string {
	if !enable {
		return text
	}
	return fmt.Sprintf("\033[%sm%s\033[0m", code, text)
}

func printStartupBox(title string, lines []string) {
	enableColor := isTerminal(os.Stdout.Fd())
	sep := colorize(enableColor, "36", "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
	fmt.Println(sep)
	if title != "" {
		fmt.Println(" " + colorize(enableColor, "1;36", title))
	}
	for _, line := range lines {
		fmt.Println(" " + line)
	}
	fmt.Println(sep)
	fmt.Println()
}

func isRoot() bool {
	return os.Geteuid() == 0
}

func handleStartupAction(action string) {
	root := action == "install-root"
	if root {
		action = "install"
	}
	// install = current-user service; everything else targets whichever mode
	// the current privileges imply (non-root -> user service, root -> system).
	userMode := action == "install" || (action != "install-root" && !isRoot())

	enableColor := isTerminal(os.Stdout.Fd())

	red := func(s string) string { return colorize(enableColor, "31", s) }
	yellow := func(s string) string { return colorize(enableColor, "33", s) }
	green := func(s string) string { return colorize(enableColor, "32", s) }
	cyan := func(s string) string { return colorize(enableColor, "36", s) }

	if root && !isRoot() {
		printStartupBox("Root required", []string{
			red("--startup install-root must be run as root."),
			"",
			"Re-run with sudo:",
			cyan(" sudo axonrouter --startup install-root"),
		})
		os.Exit(1)
	}

	if action == "install" && isRoot() {
		printStartupBox("User service only", []string{
			red("--startup install must be run as a normal user."),
			"",
			"For a system-wide service, use:",
			cyan(" sudo axonrouter --startup install-root"),
		})
		os.Exit(1)
	}

	svcCfg, err := service.ServiceConfig(root, userMode)
	if err != nil {
		fmt.Fprintf(os.Stderr, red("Failed to build service config: %v\n"), err)
		os.Exit(1)
	}

	prg := &service.Program{
		Run:      func() error { return nil },
		Shutdown: func() error { return nil },
	}
	svc, err := kardianos.New(prg, svcCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, red("Failed to create service: %v\n"), err)
		os.Exit(1)
	}

	switch action {
	case "status":
		st, err := svc.Status()
		if err != nil {
			installHint := cyan("Install: axonrouter --startup install-root")
			if userMode {
				installHint = cyan("Install: axonrouter --startup install")
			}
			printStartupBox("Service status unavailable", []string{
				red(err.Error()),
				"",
				"The service may not be installed yet.",
				installHint,
			})
			os.Exit(1)
		}
		switch st {
		case kardianos.StatusRunning:
			printStartupBox("Service running", []string{
				green("axonrouter is running as user '" + svcCfg.UserName + "'."),
				"",
				"Stop:    axonrouter --startup stop",
				"Restart: axonrouter --startup restart",
				"Uninstall: axonrouter --startup uninstall",
			})
		case kardianos.StatusStopped:
			printStartupBox("Service stopped", []string{
				yellow("axonrouter is installed but stopped."),
				"User: " + svcCfg.UserName,
				"",
				"Start: axonrouter --startup start",
			})
		default:
			printStartupBox("Service status unknown", []string{
				"Run 'systemctl status axonrouter' (Linux) or",
				"'launchctl list | grep axonrouter' (macOS) for details.",
			})
		}
		os.Exit(0)
	}

	act, err := service.ControlAction(action)
	if err != nil {
		fmt.Fprintln(os.Stderr, red(err.Error()))
		fmt.Fprintln(os.Stderr, "Usage: axonrouter --startup {install|install-root|status|start|stop|restart|uninstall}")
		os.Exit(1)
	}
	if err := kardianos.Control(svc, act); err != nil {
		fmt.Fprintf(os.Stderr, red("Failed to %s service: %v\n"), action, err)
		os.Exit(1)
	}

	switch action {
	case "install":
		installLines := []string{
			green("Service 'axonrouter' installed as user '" + svcCfg.UserName + "'."),
			"Binary: " + svcCfg.Executable,
			"Data directory: " + svcCfg.WorkingDirectory + "/axonrouter",
		}
		if err := kardianos.Control(svc, "start"); err != nil {
			installLines = append(installLines, "", yellow("Service installed but could not be started:")+" "+err.Error())
		} else {
			installLines = append(installLines, "", green("Service started."))
		}
		installLines = append(installLines,
			"",
			"Check status: axonrouter --startup status",
			"User service: systemctl --user status axonrouter",
		)
		printStartupBox("User service installed", installLines)
	case "start":
		printStartupBox("Service started", []string{
			green("axonrouter is starting as user '" + svcCfg.UserName + "'."),
			"",
			"Check status: axonrouter --startup status",
		})
	case "stop":
		printStartupBox("Service stopped", []string{
			green("axonrouter has been stopped."),
			"",
			"Start again: axonrouter --startup start",
		})
	case "restart":
		printStartupBox("Service restarted", []string{
			green("axonrouter is restarting as user '" + svcCfg.UserName + "'."),
			"",
			"Check status: axonrouter --startup status",
		})
	case "uninstall":
		reinstall := "Re-install: axonrouter --startup install"
		if root {
			reinstall = "Re-install: axonrouter --startup install-root"
		}
		printStartupBox("Service uninstalled", []string{
			green("axonrouter has been removed from the service manager."),
			"",
			reinstall,
		})
	}
	os.Exit(0)
}

func setAdminPassword(password string) {
	cfg := config.Get()
	database, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Fatalf("Failed to hash password: %v", err)
	}
	if err := db.SetSetting("admin_password_hash", string(hash)); err != nil {
		log.Fatalf("Failed to save password: %v", err)
	}
	fmt.Println("Admin password updated.")
	os.Exit(0)
}

func main() {
	gin.SetMode(gin.ReleaseMode)

	if len(os.Args) == 2 && (os.Args[1] == "--help" || os.Args[1] == "-h") {
		fmt.Println("AxonRouter-Go - Universal API proxy for coding agents.")
		fmt.Println()
	fmt.Println("Usage:")
	fmt.Println(" axonrouter Start the server")
	fmt.Println(" axonrouter --tray Start the server with a system tray icon")
	fmt.Println(" (requires build tag: tray)")
fmt.Println(" axonrouter --startup install Install user systemd service (no root)")
fmt.Println(" axonrouter --startup install-root Install system service as root/system")
	fmt.Println(" axonrouter --startup {status|start|stop|restart|uninstall}")
	fmt.Println(" Manage system service")
	fmt.Println(" axonrouter --setpass <password> Set admin dashboard password")
	fmt.Println(" axonrouter --help Show this help")
	fmt.Println()
	fmt.Println("Environment:")
	fmt.Println(" AXON_PORT Server port (default: 3777)")
	fmt.Println(" AXONROUTER_DIR Data directory (default: ~/axonrouter)")
	os.Exit(0)
}

if len(os.Args) == 2 && os.Args[1] == "--startup" {
	fmt.Fprintln(os.Stderr, "Usage: axonrouter --startup {install|install-root|status|start|stop|restart|uninstall}")
	os.Exit(1)
}
	if len(os.Args) >= 3 && os.Args[1] == "--startup" {
		handleStartupAction(os.Args[2])
	}

	if len(os.Args) >= 3 && os.Args[1] == "--setpass" {
		setAdminPassword(os.Args[2])
	}

	trayMode := len(os.Args) >= 2 && os.Args[1] == "--tray"
	if trayMode && len(os.Args) >= 3 && os.Args[2] == "--help" {
		fmt.Println("--tray          Start the server with a system tray icon")
		fmt.Println("                (requires building with -tags tray)")
		os.Exit(0)
	}

	cfg := config.Get()

	// Load HTTPS/TLS configuration (may be empty/disabled)
	httpsCfg, err := config.LoadHTTPSConfig(cfg.DataDir)
	if err != nil {
		log.Fatalf("Failed to load HTTPS config: %v", err)
	}

	// Open database
	database, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}

	// Initialise compact logger (switch to json/text via log_format setting)
	logging.Init(db.GetSetting("log_format", "compact"))

	// Create router with all routes and background goroutines
	router := api.New(api.Config{
		DB: database,
		Port: cfg.Port,
		QuotaIntervalMin: 1,
		LogRetentionDays: 30,
	})

	addr := fmt.Sprintf(":%s", cfg.Port)
	printStartupBanner(cfg.Port, database)

	log.Printf("starting server on %s", addr)
	log.Printf("dashboard available at http://localhost:%s", cfg.Port)

	if trayMode {
		if err := tray.Run(cfg.Port, router, database, httpsCfg); err != nil {
			log.Fatalf("Failed to run tray: %v", err)
		}
		return
	}

	svcCfg, err := service.ServiceConfig(false, false)
	if err != nil {
		log.Fatalf("Failed to build service config: %v", err)
	}

	prg := &service.Program{
		Run: func() error { return router.Start(addr, *httpsCfg) },
		Shutdown: func() error {
			log.Println("Shutting down...")
			router.Shutdown()
			if err := database.Close(); err != nil {
				log.Printf("WARN: failed to close database: %v", err)
			}
			return nil
		},
	}

	svc, err := kardianos.New(prg, svcCfg)
	if err != nil {
		log.Fatalf("Failed to create service: %v", err)
	}

	if err := svc.Run(); err != nil {
		log.Fatalf("Failed to run service: %v", err)
	}
}
