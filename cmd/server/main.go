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
	_ = database.QueryRow("SELECT COUNT(*) FROM connections WHERE is_active = 1").Scan(&activeConns)

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
	_ = database.QueryRow("SELECT COUNT(*) FROM connections WHERE is_active = 1").Scan(&activeConns)
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

func handleStartupAction(action string) {
	svcCfg, err := service.ServiceConfig()
	if err != nil {
		log.Fatalf("Failed to build service config: %v", err)
	}

	prg := &service.Program{
		Run:      func() error { return nil },
		Shutdown: func() error { return nil },
	}
	svc, err := kardianos.New(prg, svcCfg)
	if err != nil {
		log.Fatalf("Failed to create service: %v", err)
	}

	switch action {
	case "status":
		st, err := svc.Status()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Service status unavailable: %v\n", err)
			os.Exit(1)
		}
		switch st {
		case kardianos.StatusRunning:
			fmt.Println("Service is running")
		case kardianos.StatusStopped:
			fmt.Println("Service is installed but stopped")
		default:
			fmt.Println("Service status unknown")
		}
	default:
		act, err := service.ControlAction(action)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			fmt.Fprintln(os.Stderr, "Usage: axonrouter --startup {install|status|start|stop|restart|uninstall}")
			os.Exit(1)
		}
		if err := kardianos.Control(svc, act); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if action == "install" {
			fmt.Printf("Service '%s' installed as user '%s'.\n", service.Name, svcCfg.UserName)
			fmt.Printf("Data directory: %s/axonrouter\n", svcCfg.WorkingDirectory)
			if err := kardianos.Control(svc, "start"); err != nil {
				fmt.Fprintf(os.Stderr, "Service installed but could not be started: %v\n", err)
			} else {
				fmt.Println("Service started.")
			}
			fmt.Println("Check status: axonrouter --startup status")
		}
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
		fmt.Println(" axonrouter --startup install Install system service (Linux/macOS/Windows)")
		fmt.Println(" axonrouter --startup {status|start|stop|restart|uninstall}")
		fmt.Println(" Manage system service")
		fmt.Println(" axonrouter --setpass <password> Set admin dashboard password")
		fmt.Println(" axonrouter --help Show this help")
		fmt.Println()
		fmt.Println("Environment:")
		fmt.Println(" AXON_PORT Server port (default: 3777)")
		os.Exit(0)
	}

	if len(os.Args) == 2 && os.Args[1] == "--startup" {
		fmt.Fprintln(os.Stderr, "Usage: axonrouter --startup {install|status|start|stop|restart|uninstall}")
		os.Exit(1)
	}
	if len(os.Args) >= 3 && os.Args[1] == "--startup" {
		handleStartupAction(os.Args[2])
	}

	if len(os.Args) >= 3 && os.Args[1] == "--setpass" {
		setAdminPassword(os.Args[2])
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

	svcCfg, err := service.ServiceConfig()
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
