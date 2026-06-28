package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/api"
	"github.com/rickicode/AxonRouter-Go/internal/config"
	"github.com/rickicode/AxonRouter-Go/internal/db"
)

func main() {
	gin.SetMode(gin.ReleaseMode)

	cfg := config.Get()

	// Open database
	database, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}

	// Create router with all routes and background goroutines
	router := api.New(api.Config{
		DB:               database,
		Port:             cfg.Port,
		AdminKey:         cfg.AdminAPIKey,
		QuotaIntervalMin: 30,
		LogRetentionDays: 30,
	})

	addr := fmt.Sprintf(":%s", cfg.Port)
	log.Printf("Starting AxonRouter-Go on %s", addr)
	log.Printf("Dashboard: http://localhost:%s", cfg.Port)

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
