package mcp_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/provision/mcp"
)

func TestCodexTOMLReadMissingReturnsEmpty(t *testing.T) {
	got, mgd, err := mcp.NewCodexTOML().Read(filepath.Join(t.TempDir(), "absent.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 || len(mgd) != 0 {
		t.Errorf("expected empty")
	}
}

func TestCodexTOMLReadWithManaged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	body := `
_aide_managed_mcp = ["postgres"]

[mcp_servers.postgres]
command = "postgres-mcp"
args = ["--port", "5432"]

[mcp_servers.user-added]
command = "manual"
`
	_ = os.WriteFile(path, []byte(body), 0o600)
	got, mgd, err := mcp.NewCodexTOML().Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d servers, want 2", len(got))
	}
	if got["postgres"].Command != "postgres-mcp" {
		t.Errorf("postgres command = %q", got["postgres"].Command)
	}
	if !mgd["postgres"] || mgd["user-added"] {
		t.Errorf("managed = %+v", mgd)
	}
}

func TestCodexTOMLWritePreservesUnmanaged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	body := `
[mcp_servers.user-added]
command = "manual"
`
	_ = os.WriteFile(path, []byte(body), 0o600)
	desired := map[string]provision.MCPServer{
		"postgres": {Key: "postgres", Command: "postgres-mcp", Args: []string{"--port", "9090"}},
	}
	if err := mcp.NewCodexTOML().Write(path, desired); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	doc := map[string]any{}
	if err := toml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	servers, _ := doc["mcp_servers"].(map[string]any)
	if _, ok := servers["user-added"]; !ok {
		t.Error("user-added must survive")
	}
	if _, ok := servers["postgres"]; !ok {
		t.Error("postgres not written")
	}
	managed, _ := doc["_aide_managed_mcp"].([]any)
	if len(managed) != 1 || managed[0] != "postgres" {
		t.Errorf("_aide_managed_mcp = %v", managed)
	}
}

func TestCodexTOMLWriteRemovesPreviouslyManaged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	body := `
_aide_managed_mcp = ["old", "stay"]

[mcp_servers.old]
command = "x"

[mcp_servers.stay]
command = "y"
`
	_ = os.WriteFile(path, []byte(body), 0o600)
	desired := map[string]provision.MCPServer{
		"stay": {Key: "stay", Command: "y"},
	}
	if err := mcp.NewCodexTOML().Write(path, desired); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	doc := map[string]any{}
	if err := toml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	servers, _ := doc["mcp_servers"].(map[string]any)
	if _, gone := servers["old"]; gone {
		t.Error("old should have been removed")
	}
	if _, kept := servers["stay"]; !kept {
		t.Error("stay should remain")
	}
}

func TestCodexTOMLRoundTripPreservesOtherKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	body := `
model = "gpt-5"

[mcp_servers.foo]
command = "bar"
`
	_ = os.WriteFile(path, []byte(body), 0o600)
	desired := map[string]provision.MCPServer{
		"foo": {Key: "foo", Command: "bar"},
	}
	if err := mcp.NewCodexTOML().Write(path, desired); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	doc := map[string]any{}
	_ = toml.Unmarshal(raw, &doc)
	if doc["model"] != "gpt-5" {
		t.Errorf("non-MCP top-level key 'model' lost: %v", doc["model"])
	}
}
