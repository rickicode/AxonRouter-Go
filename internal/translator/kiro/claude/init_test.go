package claude

import (
	"testing"

	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func TestKiroToClaudeResponse_Registration(t *testing.T) {
	if !registry.Default().HasResponseTransformer(types.FormatKiro, types.FormatClaude) {
		t.Fatalf("expected registry to have FormatKiro -> FormatClaude response transformer")
	}
}
