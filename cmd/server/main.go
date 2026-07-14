package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/api"
	"github.com/rickicode/AxonRouter-Go/internal/config"
	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/rickicode/AxonRouter-Go/internal/models"
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

const serviceUnit = "axonrouter.service"

func runSystemctl(args ...string) error {
	cmd := exec.Command("systemctl", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func findUseradd() string {
	for _, p := range []string{"useradd", "/usr/sbin/useradd", "/sbin/useradd", "/usr/local/sbin/useradd"} {
		if _, err := exec.LookPath(p); err == nil {
			return p
		}
	}
	return ""
}

func installSystemdService() {
	if runtime.GOOS != "linux" {
		log.Fatalf("--startup install is only supported on Linux")
	}

	execPath, err := os.Executable()
	if err != nil {
		log.Fatalf("Failed to locate executable: %v", err)
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		log.Fatalf("Failed to resolve executable path: %v", err)
	}

	const svcUser = "axonrouter"
	const dataDir = "/var/lib/axonrouter"

	dropErr := func(out []byte, err error) {
		if err != nil {
			fmt.Fprint(os.Stderr, string(out))
			log.Fatalf("Failed to install service: %v", err)
		}
	}

	if out, err := exec.Command("id", "-u", svcUser).CombinedOutput(); err != nil {
		if len(out) == 0 || !strings.Contains(string(out), "no such user") {
			dropErr(out, err)
		}
		useraddPath := findUseradd()
		if useraddPath == "" {
			log.Fatalf("Failed to install service: useradd not found (looked in PATH, /usr/sbin, /sbin, /usr/local/sbin)")
		}
		if out, err = exec.Command(useraddPath, "--system", "--home", dataDir, "--create-home", svcUser).CombinedOutput(); err != nil {
			dropErr(out, err)
		}
	}

	if err := os.MkdirAll(dataDir, 0755); err != nil {
		log.Fatalf("Failed to create data dir: %v", err)
	}
	if out, err := exec.Command("chown", "-R", fmt.Sprintf("%s:%s", svcUser, svcUser), dataDir).CombinedOutput(); err != nil {
		dropErr(out, err)
	}

	unit := fmt.Sprintf(`[Unit]
Description=AxonRouter-Go API Proxy
After=network.target

[Service]
Type=simple
User=%s
WorkingDirectory=%s
ExecStart=%s
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
`, svcUser, dataDir, execPath)

	if err := os.WriteFile("/etc/systemd/system/"+serviceUnit, []byte(unit), 0644); err != nil {
		log.Fatalf("Failed to write service unit: %v", err)
	}

	dropErr(exec.Command("systemctl", "daemon-reload").CombinedOutput())
	dropErr(exec.Command("systemctl", "enable", "--now", "axonrouter").CombinedOutput())

	fmt.Println("Service installed and started.")
	fmt.Println("Check status: axonrouter --startup status")
}

func handleStartupAction(action string) {
	if runtime.GOOS != "linux" {
		// Allow status/start/stop/restart on macOS via launchctl? Not implemented.
		if action != "status" {
			log.Fatalf("--startup %s is only supported on Linux", action)
		}
	}

	switch action {
	case "install":
		installSystemdService()
	case "status", "start", "stop", "restart":
		if err := runSystemctl(action, "axonrouter"); err != nil {
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "Unknown startup action: %s\n", action)
		fmt.Fprintln(os.Stderr, "Usage: axonrouter --startup {install|status|start|stop|restart}")
		os.Exit(1)
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
		fmt.Println("  axonrouter                                  Start the server")
		fmt.Println("  axonrouter --startup install                Install systemd service (Linux)")
		fmt.Println("  axonrouter --startup {status|start|stop|restart}")
		fmt.Println("                                              Manage systemd service (Linux)")
		fmt.Println("  axonrouter --setpass <password>             Set admin dashboard password")
		fmt.Println("  axonrouter --help                           Show this help")
	fmt.Println()
	fmt.Println("Environment:")
	fmt.Println("  AXON_PORT        Server port (default: 3777)")
	os.Exit(0)
	}

	if len(os.Args) == 2 && os.Args[1] == "--startup" {
		fmt.Fprintln(os.Stderr, "Usage: axonrouter --startup {install|status|start|stop|restart}")
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
		DB:               database,
		Port:             cfg.Port,
		QuotaIntervalMin: 1,
		LogRetentionDays: 30,
	})

	addr := fmt.Sprintf(":%s", cfg.Port)
	printStartupBanner(cfg.Port, database)

	log.Printf("starting server on %s", addr)
	log.Printf("dashboard available at http://localhost:%s", cfg.Port)

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Println("Shutting down...")
		router.Shutdown()
		if err := database.Close(); err != nil {
			log.Printf("WARN: failed to close database: %v", err)
		}
		os.Exit(0)
	}()

	if err := router.Start(addr, *httpsCfg); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
