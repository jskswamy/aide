package mcp_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/provision/mcp"
)

func TestOpenCodeJSONReadMissingReturnsEmpty(t *testing.T) {
	h := mcp.NewOpenCodeJSON()
	got, mgd, err := h.Read(filepath.Join(t.TempDir(), "absent.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 || len(mgd) != 0 {
		t.Errorf("expected empty, got %+v %+v", got, mgd)
	}
}

// TestOpenCodeJSONReadHandlesComments pins the JSONC requirement: a
// plain encoding/json.Unmarshal errors on the comment below, so this
// test fails without hujson.Standardize in Read.
func TestOpenCodeJSONReadHandlesComments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.jsonc")
	body := `{
  // a user comment that plain encoding/json would choke on
  "_aide_managed": ["postgres"],
  "mcp": {
    "postgres": {"type": "local", "command": ["postgres-mcp", "--port", "5432"], "environment": {"PGUSER": "aide"}},
    "remote-one": {"type": "remote", "url": "https://example.com/mcp"}
  }
}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	h := mcp.NewOpenCodeJSON()
	got, mgd, err := h.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	pg := got["postgres"]
	if pg.Command != "postgres-mcp" || len(pg.Args) != 2 || pg.Args[0] != "--port" || pg.Args[1] != "5432" {
		t.Errorf("postgres = %+v", pg)
	}
	if pg.Env["PGUSER"] != "aide" {
		t.Errorf("postgres env = %+v", pg.Env)
	}
	remote := got["remote-one"]
	if remote.URL != "https://example.com/mcp" {
		t.Errorf("remote-one = %+v", remote)
	}
	if !mgd["postgres"] || mgd["remote-one"] {
		t.Errorf("managed = %+v", mgd)
	}
}

func TestOpenCodeJSONWritePreservesUnmanagedAndOtherKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.jsonc")
	body := `{"$schema": "https://opencode.ai/config.json", "mcp": {"user-added": {"type": "local", "command": ["manual"]}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	desired := map[string]provision.MCPServer{
		"postgres": {Key: "postgres", Command: "postgres-mcp", Args: []string{"--port", "9090"}},
	}
	if err := mcp.NewOpenCodeJSON().Write(path, desired); err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(path)
	var doc struct {
		Schema      string                    `json:"$schema"`
		AideManaged []string                  `json:"_aide_managed"`
		MCP         map[string]map[string]any `json:"mcp"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Schema != "https://opencode.ai/config.json" {
		t.Errorf("$schema not preserved: %q", doc.Schema)
	}
	if _, ok := doc.MCP["user-added"]; !ok {
		t.Error("user-added must survive")
	}
	pg, ok := doc.MCP["postgres"]
	if !ok {
		t.Fatal("postgres not written")
	}
	cmd, _ := pg["command"].([]any)
	if len(cmd) != 3 || cmd[0] != "postgres-mcp" || cmd[1] != "--port" || cmd[2] != "9090" {
		t.Errorf("postgres command = %v", cmd)
	}
	if len(doc.AideManaged) != 1 || doc.AideManaged[0] != "postgres" {
		t.Errorf("_aide_managed = %v", doc.AideManaged)
	}
}

func TestOpenCodeJSONWriteRemovesPreviouslyManaged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.jsonc")
	body := `{
  "_aide_managed": ["old", "stay"],
  "mcp": {"old": {"type": "local", "command": ["x"]}, "stay": {"type": "local", "command": ["y"]}}
}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	desired := map[string]provision.MCPServer{
		"stay": {Key: "stay", Command: "y"},
	}
	if err := mcp.NewOpenCodeJSON().Write(path, desired); err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(path)
	var doc struct {
		AideManaged []string                  `json:"_aide_managed"`
		MCP         map[string]map[string]any `json:"mcp"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if _, gone := doc.MCP["old"]; gone {
		t.Error("old should have been removed")
	}
	if _, kept := doc.MCP["stay"]; !kept {
		t.Error("stay should be preserved")
	}
}
