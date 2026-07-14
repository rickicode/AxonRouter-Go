package admin

import (
	"net"
	"net/http"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/config"
	"github.com/rickicode/AxonRouter-Go/internal/network"
)

// TLSHandler exposes the HTTPS/TLS configuration endpoints used by the
// dashboard setup wizard.
type TLSHandler struct {
	dataDir string
	client  *http.Client
}

// NewTLSHandler creates a handler backed by dataDir. The HTTP client is used
// for public IP discovery; nil defaults to http.DefaultClient.
func NewTLSHandler(dataDir string, client *http.Client) *TLSHandler {
	if client == nil {
		client = http.DefaultClient
	}
	return &TLSHandler{
		dataDir: dataDir,
		client:  client,
	}
}

// Get returns the current TLS configuration augmented with a validity flag
// and the absolute certificate cache directory.
func (h *TLSHandler) Get(c *gin.Context) {
	cfg, err := config.LoadHTTPSConfig(h.dataDir)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	valid, _ := cfg.IsValid()
	certDir := filepath.Join(h.dataDir, cfg.CertCache)

	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"enabled":   cfg.Enabled,
		"domain":    cfg.Domain,
		"email":     cfg.Email,
		"acceptTOS": cfg.AcceptTOS,
		"staging":   cfg.Staging,
		"certCache": cfg.CertCache,
		"valid":     valid,
		"certDir":   certDir,
	}})
}

// Put validates and persists a new TLS configuration.
func (h *TLSHandler) Put(c *gin.Context) {
	var req struct {
		Enabled   bool   `json:"enabled"`
		Domain    string `json:"domain"`
		Email     string `json:"email"`
		AcceptTOS bool   `json:"acceptTOS"`
		Staging   bool   `json:"staging"`
		CertCache string `json:"certCache"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	cfg := &config.HTTPSConfig{
		Enabled:   req.Enabled,
		Domain:    req.Domain,
		Email:     req.Email,
		AcceptTOS: req.AcceptTOS,
		Staging:   req.Staging,
		CertCache: req.CertCache,
	}
	if ok, msg := cfg.IsValid(); !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": msg})
		return
	}

	if err := config.SaveHTTPSConfig(h.dataDir, cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// PublicIP returns the host's public IPv4 address.
func (h *TLSHandler) PublicIP(c *gin.Context) {
	ip, err := network.PublicIP(h.client)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"ip": ip}})
}

// CheckDNS resolves the requested domain and reports whether it points to the
// host's public IP.
func (h *TLSHandler) CheckDNS(c *gin.Context) {
	domain := c.Query("domain")
	if domain == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "domain query parameter is required"})
		return
	}

	publicIP, err := network.PublicIP(h.client)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	addrs, err := net.LookupHost(domain)
	resolved := ""
	if err == nil && len(addrs) > 0 {
		resolved = addrs[0]
	}

	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"domain":     domain,
		"publicIP":   publicIP,
		"resolvedIP": resolved,
		"matches":    resolved != "" && resolved == publicIP,
	}})
}
