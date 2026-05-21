package gemini_test

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/provision/agents/gemini"
)

// MCPHandler is intentionally nil for gemini — MCP management goes
// through the MCPInstaller methods in mcp.go (CLI-driven via
// `gemini mcp add/remove/list`). The MCPConfigPathEmpty test in
// gemini_test.go pins the matching path contract.
func TestGeminiMCPHandlerNil(t *testing.T) {
	d := gemini.New(&fakeRunner{})
	if h := d.MCPHandler(provision.Context{}); h != nil {
		t.Errorf("MCPHandler must be nil for CLI-driven driver, got %T", h)
	}
}

func TestGeminiAddMarketplaceReturnsError(t *testing.T) {
	d := gemini.New(&fakeRunner{})
	err := d.AddMarketplace(provision.Context{}, provision.Marketplace{Source: "github:a/b"})
	if err == nil {
		t.Fatal("expected error — gemini has no marketplaces")
	}
	if !strings.Contains(err.Error(), "marketplaces") {
		t.Errorf("error must mention marketplaces, got %q", err)
	}
}
