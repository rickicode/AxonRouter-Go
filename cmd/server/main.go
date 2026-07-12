package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/api"
	"github.com/rickicode/AxonRouter-Go/internal/config"
	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/rickicode/AxonRouter-Go/internal/models"
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
	sep := cyan + "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━" + reset

	fmt.Println()
	fmt.Println(sep)
	fmt.Printf("  %s%sAxonRouter-Go%s    ready on port %s%s%s\n", bold, cyan, reset, green, port, reset)
	fmt.Println(sep)
	fmt.Printf("  %sDashboard%s   http://localhost:%s\n", yellow, reset, port)
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

	fmt.Println()
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("  AxonRouter-Go    ready on port " + port)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("  Dashboard   http://localhost:%s\n", port)
	fmt.Printf("  Routes      %d active connections · %d providers · %d models\n",
		activeConns, providers, modelCount)
	fmt.Println("  Background  cleanup · quota · usage flush")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
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

	if len(os.Args) >= 3 && os.Args[1] == "--setpass" {
		setAdminPassword(os.Args[2])
	}

	cfg := config.Get()

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
		Port: cfg.Port,
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
		os.Exit(0)
	}()

	if err := router.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
