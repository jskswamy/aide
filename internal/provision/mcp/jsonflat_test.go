package mcp_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/provision/mcp"
)

func TestJSONFlatReadMissingReturnsEmpty(t *testing.T) {
	h := mcp.NewJSONFlat()
	got, mgd, err := h.Read(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 || len(mgd) != 0 {
		t.Errorf("expected empty, got %+v %+v", got, mgd)
	}
}

func TestJSONFlatReadWithManaged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	body := `{
  "_aide_managed": ["postgres"],
  "mcpServers": {
    "postgres": {"command": "postgres-mcp", "args": ["--port", "5432"]},
    "user-added": {"command": "manual"}
  }
}`
	_ = os.WriteFile(path, []byte(body), 0o600)
	h := mcp.NewJSONFlat()
	got, mgd, err := h.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("servers = %d, want 2", len(got))
	}
	if !mgd["postgres"] || mgd["user-added"] {
		t.Errorf("managed = %+v", mgd)
	}
}

func TestJSONFlatWritePreservesUnmanaged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	body := `{"mcpServers": {"user-added": {"command": "manual"}}}`
	_ = os.WriteFile(path, []byte(body), 0o600)

	desired := map[string]provision.MCPServer{
		"postgres": {Key: "postgres", Command: "postgres-mcp", Args: []string{"--port", "9090"}},
	}
	if err := mcp.NewJSONFlat().Write(path, desired); err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(path)
	var doc struct {
		AideManaged []string                       `json:"_aide_managed"`
		Servers     map[string]map[string]any `json:"mcpServers"`
	}
	_ = json.Unmarshal(raw, &doc)
	if _, ok := doc.Servers["user-added"]; !ok {
		t.Error("user-added must survive")
	}
	if _, ok := doc.Servers["postgres"]; !ok {
		t.Error("postgres not written")
	}
	if len(doc.AideManaged) != 1 || doc.AideManaged[0] != "postgres" {
		t.Errorf("_aide_managed = %v", doc.AideManaged)
	}
}

func TestJSONFlatWriteRemovesPreviouslyManaged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	body := `{
  "_aide_managed": ["old", "stay"],
  "mcpServers": {"old": {"command": "x"}, "stay": {"command": "y"}}
}`
	_ = os.WriteFile(path, []byte(body), 0o600)

	desired := map[string]provision.MCPServer{
		"stay": {Key: "stay", Command: "y"},
	}
	if err := mcp.NewJSONFlat().Write(path, desired); err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(path)
	var doc struct {
		AideManaged []string                       `json:"_aide_managed"`
		Servers     map[string]map[string]any `json:"mcpServers"`
	}
	_ = json.Unmarshal(raw, &doc)
	if _, gone := doc.Servers["old"]; gone {
		t.Error("old should have been removed")
	}
	if _, kept := doc.Servers["stay"]; !kept {
		t.Error("stay should be preserved")
	}
	if len(doc.AideManaged) != 1 || doc.AideManaged[0] != "stay" {
		t.Errorf("_aide_managed = %v", doc.AideManaged)
	}
}
