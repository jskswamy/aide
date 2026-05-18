package gemini_test

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/provision/agents/gemini"
)

func TestGeminiMCPHandlerNonNil(t *testing.T) {
	d := gemini.New(&fakeRunner{})
	if d.MCPHandler(provision.Context{}) == nil {
		t.Error("MCPHandler must return non-nil handler")
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
