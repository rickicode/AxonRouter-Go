//go:build !tray

package tray

import (
	"strings"
	"testing"

	"github.com/rickicode/AxonRouter-Go/internal/api"
	"github.com/rickicode/AxonRouter-Go/internal/config"
)

func TestRun_DisabledBuildReturnsError(t *testing.T) {
	err := Run("3777", &api.Router{}, nil, &config.HTTPSConfig{})
	if err == nil {
		t.Fatal("expected error when tray support is not compiled in")
	}
	if !strings.Contains(err.Error(), "tray support") {
		t.Fatalf("unexpected error message: %v", err)
	}
}
