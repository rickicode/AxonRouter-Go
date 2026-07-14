package api

import (
	"context"
	"crypto/tls"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/config"

	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/adminapi"
	"github.com/rickicode/AxonRouter-Go/internal/api/handlers/admin"
	v1 "github.com/rickicode/AxonRouter-Go/internal/api/handlers/v1"
	"github.com/rickicode/AxonRouter-Go/internal/api/middleware"
	"github.com/rickicode/AxonRouter-Go/internal/auth"
	"github.com/rickicode/AxonRouter-Go/internal/auth/antigravity"
	"github.com/rickicode/AxonRouter-Go/internal/auth/codex"
	"github.com/rickicode/AxonRouter-Go/internal/auth/kiro"
	"github.com/rickicode/AxonRouter-Go/internal/background"
	"github.com/rickicode/AxonRouter-Go/internal/cache"
	"github.com/rickicode/AxonRouter-Go/internal/combo"
	"github.com/rickicode/AxonRouter-Go/internal/compression"
	_ "github.com/rickicode/AxonRouter-Go/internal/compression/engines/caveman"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/models"
	"github.com/rickicode/AxonRouter-Go/internal/providercfg"
	"github.com/rickicode/AxonRouter-Go/internal/proxypool"
	"github.com/rickicode/AxonRouter-Go/internal/quota"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
	"github.com/rickicode/AxonRouter-Go/internal/version"
	"github.com/rickicode/AxonRouter-Go/web"
)

// Router holds all dependencies and mounts all routes.
type Router struct {
	engine *gin.Engine
	db *sql.DB
	writeQueue *db.WriteQueue // centralized async writer; drained on Shutdown
	store *connstate.Store
	elig *connstate.EligibilityManager
	combo *combo.Handler
	tracker *usage.Tracker
	authMgr *auth.Manager

	// HTTP/HTTPS servers
	httpServer  *http.Server
	httpsServer *http.Server

	// Background goroutines
	quotaScheduler *background.QuotaSchedulerDB
	usageFlush *background.UsageFlush
	cleanup *background.Cleanup
	rateLimitProber *background.RateLimitProber
}

// Config holds configuration for creating a router.
type Config struct {
	DB               *sql.DB
	WriteQueue       *db.WriteQueue // centralized async writer (nil → one is created)
	Port             string
	QuotaIntervalMin int
	LogRetentionDays int
	WebFS            fs.FS // embedded frontend filesystem
}

// New creates and configures the Gin router with all routes and middleware.
func New(cfg Config) *Router {
	// Initialize executor registry
	executor.RegisterDefaults()
	// Register user-added custom providers so they are routable immediately.
	executor.RegisterCustomProviders(cfg.DB)

	// Initialize core packages
	store := connstate.NewStore()

	// Seed in-memory store from DB on startup so existing connections enter eligibility
	seedConnectionsFromDB(cfg.DB, store)

	elig := connstate.NewEligibilityManager(store)
	elig.RecomputeAll()

	comboHandler := combo.NewHandler(cfg.DB, store, elig)
	// Centralized async write queue: all non-critical DB writes (cooldowns, ban
	// counts, OAuth token persistence) funnel through this single writer goroutine.
	// SQLite only allows one writer at a time anyway, so serializing at the app
	// layer avoids write-lock contention reaching the connection pool.
	writeQueue := cfg.WriteQueue
	if writeQueue == nil {
		writeQueue = db.NewWriteQueue(cfg.DB)
	}
	// Auth cache: eliminates 2 DB queries + bcrypt per request on the hot path.
	authCache := middleware.NewAuthCache(30 * time.Second)
	tracker := usage.NewTracker(cfg.DB)
	tracker.SetWriteQueue(writeQueue) // route all batch inserts through the single writer goroutine
	usage.InitPricing(cfg.DB)
	usage.StartPeriodicReload(context.Background(), time.Hour)
	km := adminapi.NewKeyManager(cfg.DB)
	authManager := auth.NewManager()

	// Register OAuth services
	authManager.RegisterService(auth.ProviderCodex, codex.NewOAuthService(http.DefaultClient))
	authManager.RegisterService(auth.ProviderAntigravity, antigravity.NewOAuthService(http.DefaultClient))
	authManager.RegisterService(auth.ProviderKiro, kiro.NewOAuthService(http.DefaultClient))

	// Seed defaults
	combo.SeedDefaultCombos(cfg.DB)
	settingHandler := admin.NewSettingHandler(cfg.DB)
	settingHandler.SeedDefaults()
	// Bootstrap dashboard login auth (JWT secret + default admin password)
	InitAuth(cfg.DB)

	// Background goroutines
	ctx := context.Background()
	exhaustionCache := quota.NewExhaustionCache()
	quotaScheduler := background.NewQuotaSchedulerDB(cfg.DB, writeQueue, store, elig, cfg.QuotaIntervalMin, exhaustionCache)
	usageFlush := background.NewUsageFlush(tracker)
	cleanup := background.NewCleanup(comboHandler, cfg.DB, cfg.LogRetentionDays)
	quotaScheduler.Start(ctx)
	usageFlush.Start(ctx)
	cleanup.Start(ctx)
	// Proxy pool system
	proxyResolver := proxypool.NewResolver(cfg.DB)
	rateLimitProber := background.NewRateLimitProber(cfg.DB, writeQueue, store, elig, exhaustionCache, executor.GetRegistry(), proxyResolver)
	rateLimitProber.Start(ctx)
	models.StartUpdater(ctx)
	proxyHealth := proxypool.NewHealthChecker(cfg.DB)
	proxyHealth.Start(ctx)

	// Provider-level settings stored in JSON files under the data directory.
	providerCfg := providercfg.NewManager(config.Get().DataDir)

	// Create admin handlers
	connectionH := admin.NewConnectionHandler(cfg.DB, executor.GetRegistry(), store, elig, exhaustionCache, authManager)
	providerH := admin.NewProviderHandler(cfg.DB, executor.GetRegistry(), store, elig, providerCfg)

	// Auto-migrate raw API keys to bcrypt
	db.MigrateRawKeysToBcrypt(cfg.DB)

	// Compression & cache setup
	modeStr := db.GetSetting("compression_mode", "lite")
	compStrategy := compression.Strategy{
		Mode: compression.CompressionMode(modeStr),
		Lite: compression.LiteConfig{
			CollapseWhitespace:     db.GetSetting("compression_lite_collapse", "true") == "true",
			ReplaceImageUrls:       db.GetSetting("compression_lite_image_urls", "true") == "true",
			RemoveRedundantContent: db.GetSetting("compression_lite_redundant", "false") == "true",
			DedupSystemPrompt:      db.GetSetting("compression_lite_dedup", "false") == "true",
		},
	}

	ttlSec, _ := strconv.Atoi(db.GetSetting("cache_ttl_seconds", "300"))
	if ttlSec <= 0 {
		ttlSec = 300
	}
	exactCache := cache.NewPersistentCache(cfg.DB, 1000, time.Duration(ttlSec)*time.Second)

	comboH := admin.NewComboHandler(cfg.DB, comboHandler)
	logH := admin.NewLogHandler(cfg.DB)
	settingH := settingHandler
	dashboardH := admin.NewDashboardHandler(cfg.DB, store, tracker)
	versionChecker := version.NewChecker(&http.Client{Timeout: 10 * time.Second})
	healthH := admin.NewHealthHandler(cfg.DB, store, tracker, versionChecker)
	upgradeH := admin.NewUpgradeHandler(versionChecker)
	proxyPoolH := admin.NewProxyPoolHandler(cfg.DB, proxyHealth, proxyResolver)
	proxyGroupH := admin.NewProxyGroupHandler(cfg.DB, proxyResolver)
	proxyDeployH := admin.NewProxyDeployHandler(cfg.DB, proxyHealth, proxyResolver)
	optimizationH := admin.NewOptimizationHandler(cfg.DB, exactCache)
	tlsH := admin.NewTLSHandler(config.Get().DataDir, http.DefaultClient)

	// Additional admin handlers (moved here so the JWT /api/admin and master-key
	// /admin/api/v1 groups can share the same route table).
	apiKeyH := admin.NewAPIKeyHandler(cfg.DB)
	usageH := admin.NewUsageHandler(cfg.DB)
	modelH := admin.NewModelHandler(cfg.DB, executor.GetRegistry(), store, authManager)
	oauthH := admin.NewOAuthHandler(cfg.DB, authManager, store, elig)
	quotaH := admin.NewQuotaHandler(cfg.DB)
	modelPricingH := admin.NewModelPricingHandler()
	developersH := admin.NewDevelopersHandler(cfg.DB, km, cfg.Port)

	// Create Gin engine
	engine := gin.New()
	engine.Use(gin.Recovery())
	engine.Use(middleware.CORS())
	engine.Use(middleware.RequestID())
	engine.Use(middleware.Logging())

	// Rate limiter
	limiter := middleware.NewRateLimiter(600)
	loginLimiter := middleware.NewRateLimiter(10)
	// Create v1 handler with all dependencies (must exist before wiring routes)
	v1H := v1.NewHandler(cfg.DB, writeQueue, store, elig, comboHandler, tracker, authManager, proxyResolver, exhaustionCache, compStrategy, exactCache, providerCfg)
	// ---- /v1 routes (proxy) ----
	v1Group := engine.Group("/v1")
	v1Group.Use(middleware.Auth(cfg.DB, authCache))
	v1Group.Use(middleware.RateLimit(limiter))
	v1Group.Use(v1H.TrackActive())

	v1Group.POST("/chat/completions", v1H.ChatCompletions)
	v1Group.GET("/models", v1H.Models)
	v1Group.POST("/audio/speech", v1H.TTS)
	v1Group.POST("/audio/transcriptions", v1H.STT)
	v1Group.POST("/images/generations", v1H.Images)
	v1Group.POST("/video/generations", v1H.Video)
	v1Group.POST("/embeddings", v1H.Embeddings)
	v1Group.POST("/responses", v1H.Responses)
	v1Group.POST("/unified", v1H.Unified)
	v1Group.POST("/messages/count_tokens", v1H.CountTokens)
	v1Group.POST("/messages", v1H.Messages)
	// Some Anthropic clients append an extra /v1 segment to the base URL.
	v1Group.POST("/v1/messages", v1H.Messages)

	// CLI Tools model catalog used by both the dashboard and programmatic admin API.
	modelLister := func() []map[string]string {
		list := v1H.ListModels()
		out := make([]map[string]string, 0, len(list))
		for _, m := range list {
			if id, ok := m["id"].(string); ok {
				out = append(out, map[string]string{"id": id})
			}
		}
		return out
	}
	cliToolsH := admin.NewCLIToolsHandler(cfg.DB, modelLister)

	// Health check is reachable without admin auth for sidebar/lb probes.
	engine.HEAD("/api/admin/health", healthH.Health)
	engine.GET("/api/admin/health", healthH.Health)

	// Public login endpoint (issues a session JWT). Rate-limited per IP to slow brute force.
	engine.POST("/api/admin/login", middleware.RateLimit(loginLimiter), LoginHandler(cfg.DB))

	// ---- /api/admin routes (JWT session protected) ----
	adminGroup := engine.Group("/api/admin")
	adminGroup.Use(SessionAuth(cfg.DB))

	registerAdminRoutes := func(g *gin.RouterGroup) {
		// Auth / security
		g.POST("/change-password", ChangePasswordHandler(cfg.DB))
		g.POST("/defer-password-change", DeferPasswordChangeHandler(cfg.DB))
		// Providers
		g.GET("/providers", providerH.List)
		g.GET("/providers/:id", providerH.Get)
		g.POST("/providers", providerH.Create)
		g.PATCH("/providers/:id", providerH.Update)
		g.DELETE("/providers/:id", providerH.Delete)
		g.POST("/providers/:id/test", providerH.TestAll)
		g.POST("/providers/:id/connections", providerH.AddConnection)
		g.POST("/providers/:id/connections/bulk", providerH.BulkAddConnections)
		g.POST("/providers/validate", providerH.ValidateKey)
		g.GET("/providers/:id/settings", providerH.GetSettings)
		g.PATCH("/providers/:id/settings", providerH.UpdateSettings)

		// Connections
		g.GET("/providers/:id/connections", connectionH.List)
		g.GET("/connections/:id", connectionH.Get)
		g.PATCH("/connections/:id", connectionH.Update)
		g.DELETE("/connections/:id", connectionH.Delete)
		g.POST("/connections/:id/test", connectionH.TestConnection)
		g.POST("/connections/:id/refresh", connectionH.RefreshToken)
		g.POST("/connections/:id/reset", connectionH.ResetStatus)
		g.PATCH("/connections/bulk", connectionH.BulkUpdate)

		// Models
		g.GET("/providers/:id/models", modelH.ListModels)
		g.POST("/providers/:id/models", modelH.CreateModel)
		g.DELETE("/providers/:id/models", modelH.DeleteModel)
		g.POST("/providers/:id/models/test", modelH.TestModel)
		g.POST("/models/sync", modelH.SyncModels)

		// OAuth — connection created only on success (no orphaned connections)
		g.POST("/oauth/start", oauthH.StartOAuth)
		g.GET("/oauth/:sessionId/poll", oauthH.PollOAuth)
		g.POST("/oauth/callback", oauthH.SubmitOAuthCallback)

		// Combos
		g.GET("/combos", comboH.List)
		g.GET("/combos/:id", comboH.Get)
		g.POST("/combos", comboH.Create)
		g.PATCH("/combos/:id", comboH.Update)
		g.DELETE("/combos/:id", comboH.Delete)
		g.POST("/combos/:id/steps", comboH.AddStep)
		g.DELETE("/combos/steps/:stepId", comboH.RemoveStep)
		g.POST("/combos/seed-defaults", comboH.SeedDefaults)

		// Logs
		g.GET("/logs", logH.List)
		g.GET("/logs/stats", logH.Stats)
		g.GET("/logs/active", logH.ActiveRequests)

		// Settings
		g.GET("/settings", settingH.List)
		g.GET("/settings/:key", settingH.Get)
		g.PUT("/settings/:key", settingH.Set)
		g.DELETE("/settings/:key", settingH.Delete)

		// TLS config
		g.GET("/tls-config", tlsH.Get)
		g.PUT("/tls-config", tlsH.Put)
		g.GET("/tls-config/public-ip", tlsH.PublicIP)
		g.GET("/tls-config/check-dns", tlsH.CheckDNS)

		// Dashboard
		g.GET("/dashboard/stats", dashboardH.Stats)
		g.GET("/dashboard/providers", dashboardH.ProviderSummary)
		g.GET("/dashboard/recent-logs", dashboardH.RecentLogs)

		// Metrics
		g.GET("/metrics", healthH.Metrics)

		// Upgrade
		g.POST("/upgrade", upgradeH.Upgrade)

		// Quota
		g.GET("/quota", quotaH.List)
		g.GET("/quota/summary", quotaH.Summary)
		g.POST("/quota/:connId/refresh", quotaH.Refresh)
		// Model Pricing — single source of truth for per-model cost rates.
		g.GET("/model-pricing", modelPricingH.List)
		g.POST("/model-pricing", modelPricingH.Create)
		g.PATCH("/model-pricing/:id", modelPricingH.Update)
		g.DELETE("/model-pricing/:id", modelPricingH.Delete)

		// Proxy Pools (static routes before :id to avoid wildcard capture)
		g.GET("/proxy-pools", proxyPoolH.List)
		g.GET("/proxy-pools/health-check", proxyPoolH.HealthGet)
		g.POST("/proxy-pools/health-check", proxyPoolH.HealthRun)
		g.GET("/proxy-pools/generate-source", proxyDeployH.GenerateSource)
		g.POST("/proxy-pools/vercel-deploy", proxyDeployH.DeployVercel)
		g.POST("/proxy-pools/deno-deploy", proxyDeployH.DeployDeno)
		g.POST("/proxy-pools/cloudflare-deploy", proxyDeployH.DeployCloudflare)
	g.POST("/proxy-pools", proxyPoolH.Create)
	g.POST("/proxy-pools/bulk", proxyPoolH.BulkCreate)
	g.POST("/proxy-pools/bulk-delete", proxyPoolH.BulkDelete)
	g.GET("/proxy-pools/:id", proxyPoolH.Get)
		g.PATCH("/proxy-pools/:id", proxyPoolH.Update)
		g.DELETE("/proxy-pools/:id", proxyPoolH.Delete)
		g.POST("/proxy-pools/:id/test", proxyPoolH.Test)

		// Proxy Groups
		g.GET("/proxy-groups", proxyGroupH.List)
		g.GET("/proxy-groups/:id", proxyGroupH.Get)
		g.POST("/proxy-groups", proxyGroupH.Create)
		g.PATCH("/proxy-groups/:id", proxyGroupH.Update)
		g.DELETE("/proxy-groups/:id", proxyGroupH.Delete)

		// API Keys
		g.GET("/api-keys", apiKeyH.List)
		g.POST("/api-keys", apiKeyH.Create)
		g.DELETE("/api-keys/:id", apiKeyH.Delete)
		g.PATCH("/api-keys/:id/toggle", apiKeyH.ToggleActive)
		g.GET("/api-keys/:id/value", apiKeyH.GetValue)

		// Usage
		g.GET("/usage", usageH.Get)

		// CLI Tools — unified model catalog for dashboard pickers + per-tool config generation
		g.GET("/models", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"data": v1H.ListActiveModels()})
		})
		g.GET("/cli-tools/statuses", cliToolsH.AllStatuses)
		g.GET("/cli-tools/:toolId", cliToolsH.GetConfig)
		g.POST("/cli-tools/:toolId", cliToolsH.SaveConfig)
		g.DELETE("/cli-tools/:toolId", cliToolsH.DeleteConfig)
		g.GET("/cli-tools", cliToolsH.ListTools)

		// Compression & Cache
		g.GET("/settings/compression", optimizationH.GetCompressionSettings)
		g.PUT("/settings/compression", optimizationH.UpdateCompressionSettings)
		g.GET("/cache/stats", optimizationH.GetCacheStats)
		g.POST("/cache/flush", optimizationH.FlushCache)
		g.POST("/optimization/preview", optimizationH.PreviewCompression)

		// Developers
		g.GET("/developers/master-key", developersH.GetMasterKey)
		g.POST("/developers/master-key/regenerate", developersH.RegenerateMasterKey)
	}

	registerAdminRoutes(adminGroup)

	// ---- /admin/api/v1 routes (master key protected) ----
	masterGroup := engine.Group("/admin/api/v1")
	masterGroup.Use(middleware.MasterAuth(km))
	masterGroup.Use(admin.ProgrammaticResponseWrapper())
	registerAdminRoutes(masterGroup)

	// ---- Static frontend (SPA) ----
	fsys := web.GetBuildFS()

	// Serve static files with correct MIME types
	httpFS := http.FS(fsys)
	fileServer := http.FileServer(httpFS)

	engine.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path

		// Try to open the file from the embedded FS
		file, err := fsys.Open(strings.TrimPrefix(path, "/"))
		if err == nil {
			file.Close()
			// File exists — serve it with proper MIME type
			fileServer.ServeHTTP(c.Writer, c.Request)
			return
		}

		// File not found — serve index.html (SPA fallback)
		indexFile, err := fsys.Open("index.html")
		if err != nil {
			c.String(http.StatusNotFound, "Not Found")
			return
		}
		defer indexFile.Close()
		stat, _ := indexFile.Stat()
		c.Header("Content-Type", "text/html; charset=utf-8")
		http.ServeContent(c.Writer, c.Request, stat.Name(), stat.ModTime(), indexFile.(io.ReadSeeker))
	})

	return &Router{
		engine:          engine,
		db:              cfg.DB,
		writeQueue:      writeQueue,
		store:           store,
		elig:            elig,
		combo:           comboHandler,
		tracker:         tracker,
		authMgr:         authManager,
		quotaScheduler:  quotaScheduler,
		usageFlush:      usageFlush,
		cleanup:         cleanup,
		rateLimitProber: rateLimitProber,
	}
}

// Start starts the HTTP server and optionally an HTTPS server on :443.
func (r *Router) Start(addr string, httpsCfg config.HTTPSConfig) error {
	// HTTP listener
	httpListener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}
	r.httpServer = &http.Server{
		Handler:           r.engine,
		ReadHeaderTimeout: 30 * time.Second,
	}

	// HTTPS listener (optional)
	if httpsCfg.Enabled {
		if valid, msg := httpsCfg.IsValid(); valid {
			if err := r.startHTTPS(httpsCfg); err != nil {
				log.Printf("WARN: HTTPS disabled: %v", err)
			}
		} else {
			log.Printf("WARN: HTTPS config invalid: %s", msg)
		}
	}

	errCh := make(chan error, 2)
	go func() {
		log.Printf("starting HTTP server on %s", httpListener.Addr())
		if err := r.httpServer.Serve(httpListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	if r.httpsServer != nil {
		go func() {
			log.Printf("starting HTTPS server on :443 for domain %s", httpsCfg.Domain)
			if err := r.httpsServer.ServeTLS(nil, "", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errCh <- err
			}
		}()
	}

	return <-errCh
}

func (r *Router) startHTTPS(cfg config.HTTPSConfig) error {
	cacheDir := filepath.Join(config.Get().DataDir, cfg.CertCache)
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return fmt.Errorf("create cert cache: %w", err)
	}

	var directoryURL string
	if cfg.Staging {
		directoryURL = "https://acme-staging-v02.api.letsencrypt.org/directory"
	} else {
		directoryURL = "https://acme-v02.api.letsencrypt.org/directory"
	}

	m := &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(cfg.Domain),
		Cache:      autocert.DirCache(cacheDir),
		Email:      cfg.Email,
		Client: &acme.Client{
			DirectoryURL: directoryURL,
		},
	}

	listener, err := net.Listen("tcp", ":443")
	if err != nil {
		return fmt.Errorf("listen :443: %w", err)
	}

	r.httpsServer = &http.Server{
		Handler:           r.engine,
		ReadHeaderTimeout: 30 * time.Second,
		TLSConfig: &tls.Config{
			GetCertificate: m.GetCertificate,
			NextProtos:     []string{"h2", "http/1.1"},
		},
	}

	go func() {
		log.Printf("starting HTTPS server on :443 for domain %s", cfg.Domain)
		if err := r.httpsServer.ServeTLS(listener, "", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("WARN: HTTPS server error: %v", err)
		}
	}()
	return nil
}

// Shutdown gracefully stops servers and background goroutines.
func (r *Router) Shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if r.httpServer != nil {
		_ = r.httpServer.Shutdown(ctx)
	}
	if r.httpsServer != nil {
		_ = r.httpsServer.Shutdown(ctx)
	}
	r.tracker.Stop()
	r.quotaScheduler.Stop()
	r.usageFlush.Stop()
	r.cleanup.Stop()
	r.rateLimitProber.Stop()
}

// Engine returns the underlying Gin engine (for testing).
func (r *Router) Engine() *gin.Engine {
	return r.engine
}

// Tracker returns the usage tracker (for proxy handlers to log requests).
func (r *Router) Tracker() *usage.Tracker {
	return r.tracker
}

// Store returns the connection state store.
func (r *Router) Store() *connstate.Store {
	return r.store
}

// Eligibility returns the eligibility manager.
func (r *Router) Eligibility() *connstate.EligibilityManager {
	return r.elig
}

// ComboHandler returns the combo handler.
func (r *Router) ComboHandler() *combo.Handler {
	return r.combo
}

// seedConnectionsFromDB loads all active connections from the database into the in-memory store.
// Called once on startup so existing connections enter eligibility routing immediately.
func seedConnectionsFromDB(db *sql.DB, store *connstate.Store) {
	rows, err := db.Query(`
		SELECT c.id, c.provider_type_id, COALESCE(c.status, 'ready'),
		COALESCE(c.priority, 0), COALESCE(c.consecutive_ban_count, 0),
		COALESCE(c.cooldown_until, 0)
		FROM connections c
		WHERE c.is_active = 1
	`)
	if err != nil {
		log.Printf("WARN: seed connections failed: %v", err)
		return
	}
	defer rows.Close()

	count := 0
	now := time.Now()
	for rows.Next() {
		var connID, providerID, status string
		var priority, banCount int
		var cooldownUntil int64
		if err := rows.Scan(&connID, &providerID, &status, &priority, &banCount, &cooldownUntil); err != nil {
			continue
		}
		// If a cooldown window is still active, reflect it in-memory so
		// getConnection() does not select the connection right after restart.
		activeCooldown := cooldownUntil > 0 && now.Before(time.Unix(cooldownUntil, 0))
		if !activeCooldown && (status == "rate_limited" || status == "quota_exhausted") {
			// Cooldown has expired while we were down; treat connection as ready.
			status = "ready"
		}
		store.SeedConnection(connID, providerID, status, priority)
		if cs := store.Get(connID); cs != nil {
			cs.BanCount = banCount
			if activeCooldown {
				until := time.Unix(cooldownUntil, 0)
				if status == "quota_exhausted" {
					cs.SetQuotaCooldown(until)
				} else {
					cs.SetCooldown(until)
				}
			}
		}
		count++
	}
	if count > 0 {
		log.Printf("Seeded %d connections into eligibility store", count)
	}
}
