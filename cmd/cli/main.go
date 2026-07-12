package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"golang.org/x/crypto/bcrypt"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/api"
	"github.com/rickicode/AxonRouter-Go/internal/config"
	"github.com/rickicode/AxonRouter-Go/internal/db"
)

const version = "1.0.0"

func main() {
	if len(os.Args) < 2 {
		showStatus()
		return
	}

	switch os.Args[1] {
	case "run":
		cmdRun()
	case "stop":
		cmdStop()
	case "status":
		cmdStatus()
	case "restart":
		cmdRestart()
	case "version":
		fmt.Printf("AxonRouter-Go v%s\n", version)
	case "setpass":
		cmdSetpass()
	case "help", "--help", "-h":
		showHelp()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", os.Args[1])
		showHelp()
		os.Exit(1)
	}
}

// ---- Commands ----

func cmdRun() {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	port := fs.String("port", "", "Port to listen on (default: from config)")
	noKill := fs.Bool("no-kill", false, "Fail if port is in use instead of killing")
	fs.Parse(os.Args[2:])

	cfg := config.Get()
	if *port != "" {
		cfg.Port = *port
	}

	// Check port conflict
	if isPortInUse(cfg.Port) {
		if *noKill {
			fmt.Fprintf(os.Stderr, "Port %s is in use and --no-kill is set\n", cfg.Port)
			os.Exit(1)
		}
		killProcessOnPort(cfg.Port)
	}

	// Write PID file
	writePID(cfg.PIDFile)
	defer removePID(cfg.PIDFile)

	// Set Gin to release mode
	gin.SetMode(gin.ReleaseMode)

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
	fmt.Printf("Starting AxonRouter-Go on %s...\n", addr)
	fmt.Printf("Dashboard: http://localhost:%s\n", cfg.Port)

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		fmt.Println("\nShutting down...")
		router.Shutdown()
		os.Exit(0)
	}()

	if err := router.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func cmdStop() {
	cfg := config.Get()
	pid := readPID(cfg.PIDFile)
	if pid == 0 {
		fmt.Println("AxonRouter is not running")
		return
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		fmt.Printf("Process %d not found\n", pid)
		removePID(cfg.PIDFile)
		return
	}

	fmt.Printf("Stopping AxonRouter (PID: %d)...\n", pid)
	process.Signal(syscall.SIGTERM)

	// Wait for process to exit
	for i := 0; i < 50; i++ {
		if !isProcessAlive(pid) {
			fmt.Println("Stopped")
			removePID(cfg.PIDFile)
			return
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Force kill
	fmt.Printf("Force killing PID %d...\n", pid)
	process.Kill()
	removePID(cfg.PIDFile)
	fmt.Println("Killed")
}

func cmdStatus() {
	cfg := config.Get()
	pid := readPID(cfg.PIDFile)

	if pid == 0 || !isProcessAlive(pid) {
		fmt.Println("Status: STOPPED")
		return
	}

	running := isPortInUse(cfg.Port)
	if !running {
		fmt.Printf("Status: PID %d (port %s not listening)\n", pid, cfg.Port)
		return
	}

	uptime := getUptime(pid)
	fmt.Printf("Status:  RUNNING\n")
	fmt.Printf("PID:     %d\n", pid)
	fmt.Printf("Port:    %s\n", cfg.Port)
	if uptime > 0 {
		fmt.Printf("Uptime:  %s\n", formatDuration(uptime))
	}
	fmt.Printf("URL:     http://localhost:%s\n", cfg.Port)
}

func cmdRestart() {
	cmdStop()
	time.Sleep(500 * time.Millisecond)
	cmdRun()
}
	
	// cmdSetpass hashes a new admin dashboard password and persists it to settings.
	func cmdSetpass() {
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: axonrouter setpass <password>")
			os.Exit(1)
		}
		pw := os.Args[2]
		database, err := db.Open(config.Get().DBPath)
		if err != nil {
			log.Fatalf("Failed to open database: %v", err)
		}
		defer database.Close()
		hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
		if err != nil {
			log.Fatalf("Failed to hash password: %v", err)
		}
		if err := db.SetSetting("admin_password_hash", string(hash)); err != nil {
			log.Fatalf("Failed to save password: %v", err)
		}
		fmt.Println("Admin password updated.")
	}

// ---- Status display ----

func showStatus() {
	cfg := config.Get()
	pid := readPID(cfg.PIDFile)

	fmt.Println("╔══════════════════════════════════════════╗")
	fmt.Printf("║           AxonRouter-Go v%-16s║\n", version)
	fmt.Println("╠══════════════════════════════════════════╣")

	if pid == 0 || !isProcessAlive(pid) {
		fmt.Println("║                                          ║")
		fmt.Println("║  Service Status: ○ NOT RUNNING           ║")
		fmt.Println("║                                          ║")
		fmt.Println("║  Commands:                               ║")
		fmt.Println("║    axonrouter run     Start service       ║")
		fmt.Println("║    axonrouter status  Show status         ║")
		fmt.Println("║    axonrouter help    Show all commands   ║")
		fmt.Println("║                                          ║")
	} else {
		running := isPortInUse(cfg.Port)
		status := "● RUNNING"
		if !running {
			status = "◐ PARTIAL"
		}
		uptime := getUptime(pid)
		uptimeStr := "unknown"
		if uptime > 0 {
			uptimeStr = formatDuration(uptime)
		}

		fmt.Println("║                                          ║")
		fmt.Printf("║  Service Status: %-24s║\n", status)
		fmt.Printf("║  Port: %-33s║\n", cfg.Port)
		fmt.Printf("║  Uptime: %-31s║\n", uptimeStr)
		fmt.Printf("║  PID: %-34d║\n", pid)
		fmt.Println("║                                          ║")
		fmt.Println("║  Commands:                               ║")
		fmt.Println("║    axonrouter run      Restart service    ║")
		fmt.Println("║    axonrouter stop     Stop service       ║")
		fmt.Println("║    axonrouter status   Show status        ║")
		fmt.Println("║    axonrouter help     Show all commands  ║")
		fmt.Println("║                                          ║")
	}
	fmt.Println("╚══════════════════════════════════════════╝")
}

func showHelp() {
	fmt.Println(`AxonRouter-Go — Universal API proxy for coding agents

Usage:
  axonrouter                   Show status (default)
  axonrouter run               Start server (foreground, auto-kill on port conflict)
  axonrouter run --port 8080   Start on custom port
  axonrouter run --no-kill     Fail if port is in use
  axonrouter stop              Stop running service
  axonrouter status            Show service status
  axonrouter restart           Restart service
  axonrouter version           Show version
  axonrouter help              Show this help

Port: Default 3777, configurable via --port or AXON_PORT env var.
Data: ~/.axonrouter/axonrouter.db (SQLite)
PID:  ~/.axonrouter/axonrouter.pid`)
}

// ---- PID file management ----

func writePID(path string) error {
	dir := filepath.Dir(path)
	os.MkdirAll(dir, 0755)
	return os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0644)
}

func readPID(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return pid
}

func removePID(path string) {
	os.Remove(path)
}

// ---- Process management ----

func isProcessAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

func isPortInUse(port string) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%s", port))
	if err != nil {
		return true
	}
	ln.Close()
	return false
}

func killProcessOnPort(port string) {
	cmd := []string{"sh", "-c", fmt.Sprintf("lsof -ti:%s 2>/dev/null", port)}
	out, err := exec.Command(cmd[0], cmd[1:]...).Output()
	if err != nil {
		return
	}
	pids := strings.Fields(strings.TrimSpace(string(out)))
	for _, pidStr := range pids {
		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			continue
		}
		fmt.Printf("Killing existing process on port %s (PID: %d)...\n", port, pid)
		process, err := os.FindProcess(pid)
		if err != nil {
			continue
		}
		process.Signal(syscall.SIGTERM)
		time.Sleep(500 * time.Millisecond)
		if isProcessAlive(pid) {
			process.Kill()
		}
	}
}

func getUptime(pid int) time.Duration {
	// ponytail: simplified — just return 0 if we can't parse
	return 0
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
}
