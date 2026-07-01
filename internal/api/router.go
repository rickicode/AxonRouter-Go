package api

import (
	"context"
	"database/sql"
	"io"
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/api/handlers/admin"
	v1 "github.com/rickicode/AxonRouter-Go/internal/api/handlers/v1"
	"github.com/rickicode/AxonRouter-Go/internal/api/middleware"
	"github.com/rickicode/AxonRouter-Go/internal/auth"
	"github.com/rickicode/AxonRouter-Go/internal/auth/antigravity"
	"github.com/rickicode/AxonRouter-Go/internal/auth/codex"
	"github.com/rickicode/AxonRouter-Go/internal/auth/kiro"
	"github.com/rickicode/AxonRouter-Go/internal/background"
	"github.com/rickicode/AxonRouter-Go/internal/combo"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/models"
	"github.com/rickicode/AxonRouter-Go/internal/proxypool"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
	"github.com/rickicode/AxonRouter-Go/web"
)

// Router holds all dependencies and mounts all routes.
type Router struct {
	engine  *gin.Engine
	db      *sql.DB
	store   *connstate.Store
	elig    *connstate.EligibilityManager
	combo   *combo.Handler
	tracker *usage.Tracker
	authMgr *auth.Manager

	// Background goroutines
	quotaScheduler *background.QuotaSchedulerDB
	usageFlush     *background.UsageFlush
	cleanup        *background.Cleanup
}

// Config holds configuration for creating a router.
type Config struct {
	DB               *sql.DB
	Port             string
	AdminKey         string
	QuotaIntervalMin int
	LogRetentionDays int
	WebFS            fs.FS // embedded frontend filesystem
}

// New creates and configures the Gin router with all routes and middleware.
func New(cfg Config) *Router {
	// Initialize executor registry
	executor.RegisterDefaults()

	// Initialize core packages
	store := connstate.NewStore()

	elig := connstate.NewEligibilityManager(store)
	elig.RecomputeAll()

	comboHandler := combo.NewHandler(cfg.DB, store, elig)
	tracker := usage.NewTracker(cfg.DB)
	authManager := auth.NewManager()

	// Register OAuth services
	authManager.RegisterService(auth.ProviderCodex, codex.NewOAuthService(http.DefaultClient))
	authManager.RegisterService(auth.ProviderAntigravity, antigravity.NewOAuthService(http.DefaultClient))
	authManager.RegisterService(auth.ProviderKiro, kiro.NewOAuthService(http.DefaultClient))

	// Seed defaults
	combo.SeedDefaultCombos(cfg.DB)
	settingHandler := admin.NewSettingHandler(cfg.DB)
	settingHandler.SeedDefaults()

	// Background goroutines
	ctx := context.Background()
	quotaScheduler := background.NewQuotaSchedulerDB(cfg.DB, store, elig, cfg.QuotaIntervalMin)
	usageFlush := background.NewUsageFlush(tracker)
	cleanup := background.NewCleanup(comboHandler, cfg.DB, cfg.LogRetentionDays)
	quotaScheduler.Start(ctx)
	usageFlush.Start(ctx)
	models.StartUpdater(ctx)

	// Proxy pool system
	proxyResolver := proxypool.NewResolver(cfg.DB)
	proxyHealth := proxypool.NewHealthChecker(cfg.DB)
	proxyHealth.Start(ctx)

	// Create admin handlers
	providerH := admin.NewProviderHandler(cfg.DB, executor.GetRegistry(), store)
	connectionH := admin.NewConnectionHandler(cfg.DB, executor.GetRegistry(), store, elig)

	// Auto-migrate raw API keys to bcrypt
	db.MigrateRawKeysToBcrypt(cfg.DB)

	comboH := admin.NewComboHandler(cfg.DB, comboHandler)
	logH := admin.NewLogHandler(cfg.DB)
	settingH := settingHandler
	dashboardH := admin.NewDashboardHandler(cfg.DB)
	proxyPoolH := admin.NewProxyPoolHandler(cfg.DB, proxyHealth)
	proxyGroupH := admin.NewProxyGroupHandler(cfg.DB)
	proxyDeployH := admin.NewProxyDeployHandler(cfg.DB, proxyHealth)

	// Create v1 handler with all dependencies
	v1H := v1.NewHandler(cfg.DB, store, elig, comboHandler, tracker, authManager, proxyResolver)

	// Create Gin engine
	engine := gin.New()
	engine.Use(gin.Recovery())
	engine.Use(middleware.CORS())
	engine.Use(middleware.Logging())

	// Rate limiter
	limiter := middleware.NewRateLimiter(600)

	// ---- /v1 routes (proxy) ----
	v1Group := engine.Group("/v1")
	v1Group.Use(middleware.Auth(cfg.DB))
	v1Group.Use(middleware.RateLimit(limiter))

	v1Group.POST("/chat/completions", v1H.ChatCompletions)
	v1Group.POST("/messages", v1H.Messages)
	v1Group.POST("/responses", v1H.Responses)
	v1Group.GET("/models", v1H.Models)
	v1Group.POST("/embeddings", v1H.Embeddings)
	v1Group.POST("/audio/speech", v1H.TTS)
	v1Group.POST("/audio/transcriptions", v1H.STT)
	v1Group.POST("/images/generations", v1H.Images)
	v1Group.POST("/video/generations", v1H.Video)
	v1Group.POST("/unified", v1H.Unified)
	v1Group.POST("/messages/count_tokens", v1H.CountTokens)

	// ---- /api/admin routes ----
	adminGroup := engine.Group("/api/admin")
	// Admin auth: check admin key if configured
	if cfg.AdminKey != "" {
		adminGroup.Use(func(c *gin.Context) {
			key := c.GetHeader("X-Admin-Key")
			if key == "" {
				auth := c.GetHeader("Authorization")
				if len(auth) > 7 && auth[:7] == "Bearer " {
					key = auth[7:]
				}
			}
			if key != cfg.AdminKey {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid admin key"})
				c.Abort()
				return
			}
			c.Next()
		})
	}

	// Providers
	adminGroup.GET("/providers", providerH.List)
	adminGroup.GET("/providers/:id", providerH.Get)
	adminGroup.POST("/providers", providerH.Create)
	adminGroup.PATCH("/providers/:id", providerH.Update)
	adminGroup.DELETE("/providers/:id", providerH.Delete)
	adminGroup.POST("/providers/:id/test", providerH.TestAll)
	adminGroup.POST("/providers/:id/connections", providerH.AddConnection)
	adminGroup.POST("/providers/:id/connections/bulk", providerH.BulkAddConnections)
	adminGroup.POST("/providers/validate", providerH.ValidateKey)

	// Connections
	adminGroup.GET("/providers/:id/connections", connectionH.List)
	adminGroup.GET("/connections/:id", connectionH.Get)
	adminGroup.PATCH("/connections/:id", connectionH.Update)
	adminGroup.DELETE("/connections/:id", connectionH.Delete)
	adminGroup.POST("/connections/:id/test", connectionH.TestConnection)
	adminGroup.POST("/connections/:id/reset", connectionH.ResetStatus)
	adminGroup.PATCH("/connections/bulk", connectionH.BulkUpdate)

	// Models
	modelH := admin.NewModelHandler(cfg.DB, executor.GetRegistry(), store)
	adminGroup.GET("/providers/:id/models", modelH.ListModels)
	adminGroup.POST("/providers/:id/models/test", modelH.TestModel)

	// OAuth
	oauthH := admin.NewOAuthHandler(cfg.DB, authManager, store, elig)
	adminGroup.POST("/connections/:id/oauth", oauthH.InitiateOAuth)
	adminGroup.GET("/connections/:id/oauth/status", oauthH.OAuthStatus)
	adminGroup.POST("/connections/:id/oauth/callback", oauthH.SubmitOAuthCallback)

	// Combos
	adminGroup.GET("/combos", comboH.List)
	adminGroup.GET("/combos/:id", comboH.Get)
	adminGroup.POST("/combos", comboH.Create)
	adminGroup.PATCH("/combos/:id", comboH.Update)
	adminGroup.DELETE("/combos/:id", comboH.Delete)
	adminGroup.POST("/combos/:id/steps", comboH.AddStep)
	adminGroup.DELETE("/combos/steps/:stepId", comboH.RemoveStep)
	adminGroup.POST("/combos/seed-defaults", comboH.SeedDefaults)

	// Logs
	adminGroup.GET("/logs", logH.List)
	adminGroup.GET("/logs/:id", logH.Get)
	adminGroup.GET("/logs/stats", logH.Stats)

	// Settings
	adminGroup.GET("/settings", settingH.List)
	adminGroup.GET("/settings/:key", settingH.Get)
	adminGroup.PUT("/settings/:key", settingH.Set)
	adminGroup.DELETE("/settings/:key", settingH.Delete)

	// Dashboard
	adminGroup.GET("/dashboard/stats", dashboardH.Stats)
	adminGroup.GET("/dashboard/providers", dashboardH.ProviderSummary)
	adminGroup.GET("/dashboard/recent-logs", dashboardH.RecentLogs)

	// Quota
	quotaH := admin.NewQuotaHandler(cfg.DB)
	adminGroup.GET("/quota", quotaH.List)
	adminGroup.POST("/quota/:connId/refresh", quotaH.Refresh)

	// Proxy Pools (static routes before :id to avoid wildcard capture)
	adminGroup.GET("/proxy-pools", proxyPoolH.List)
	adminGroup.GET("/proxy-pools/health-check", proxyPoolH.HealthGet)
	adminGroup.POST("/proxy-pools/health-check", proxyPoolH.HealthRun)
	adminGroup.GET("/proxy-pools/generate-source", proxyDeployH.GenerateSource)
	adminGroup.POST("/proxy-pools/vercel-deploy", proxyDeployH.DeployVercel)
	adminGroup.POST("/proxy-pools/deno-deploy", proxyDeployH.DeployDeno)
	adminGroup.POST("/proxy-pools/cloudflare-deploy", proxyDeployH.DeployCloudflare)
	adminGroup.POST("/proxy-pools", proxyPoolH.Create)
	adminGroup.GET("/proxy-pools/:id", proxyPoolH.Get)
	adminGroup.PATCH("/proxy-pools/:id", proxyPoolH.Update)
	adminGroup.DELETE("/proxy-pools/:id", proxyPoolH.Delete)
	adminGroup.POST("/proxy-pools/:id/test", proxyPoolH.Test)

	// Proxy Groups
	adminGroup.GET("/proxy-groups", proxyGroupH.List)
	adminGroup.GET("/proxy-groups/:id", proxyGroupH.Get)
	adminGroup.POST("/proxy-groups", proxyGroupH.Create)
	adminGroup.PATCH("/proxy-groups/:id", proxyGroupH.Update)
	adminGroup.DELETE("/proxy-groups/:id", proxyGroupH.Delete)

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
		engine:         engine,
		db:             cfg.DB,
		store:          store,
		elig:           elig,
		combo:          comboHandler,
		tracker:        tracker,
		authMgr:        authManager,
		quotaScheduler: quotaScheduler,
		usageFlush:     usageFlush,
		cleanup:        cleanup,
	}
}

// Run starts the HTTP server.
func (r *Router) Run(addr string) error {
	return r.engine.Run(addr)
}

// Shutdown gracefully stops background goroutines.
func (r *Router) Shutdown() {
	r.tracker.Stop()
	r.quotaScheduler.Stop()
	r.usageFlush.Stop()
	r.cleanup.Stop()
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
