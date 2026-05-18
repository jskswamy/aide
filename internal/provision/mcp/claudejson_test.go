package mcp_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/provision/mcp"
)

func TestClaudeJSONReadMissingReturnsEmpty(t *testing.T) {
	h := mcp.NewClaudeJSON("")
	got, mgd, err := h.Read(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 || len(mgd) != 0 {
		t.Errorf("expected empty")
	}
}

func TestClaudeJSONReadFlatShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".mcp.json")
	body := `{
  "_aide_managed": ["postgres"],
  "mcpServers": {
    "postgres": {"command": "p"},
    "user-added": {"command": "m"}
  }
}`
	_ = os.WriteFile(path, []byte(body), 0o600)
	got, mgd, err := mcp.NewClaudeJSON("").Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d servers", len(got))
	}
	if !mgd["postgres"] || mgd["user-added"] {
		t.Errorf("managed = %+v", mgd)
	}
}

func TestClaudeJSONReadNestedShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claude.json")
	body := `{
  "projects": {
    "/repo/aide": {
      "_aide_managed": ["postgres"],
      "mcpServers": {
        "postgres": {"command": "p"},
        "user-added": {"command": "m"}
      }
    }
  }
}`
	_ = os.WriteFile(path, []byte(body), 0o600)
	got, mgd, err := mcp.NewClaudeJSON("/repo/aide").Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d servers", len(got))
	}
	if !mgd["postgres"] || mgd["user-added"] {
		t.Errorf("managed = %+v", mgd)
	}
}

func TestClaudeJSONReadNestedMissingProjectReturnsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claude.json")
	body := `{"projects": {"/other": {"mcpServers": {"x": {"command":"y"}}}}}`
	_ = os.WriteFile(path, []byte(body), 0o600)
	got, _, err := mcp.NewClaudeJSON("/repo/aide").Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %+v", got)
	}
}

func TestClaudeJSONWritePreservesUnmanaged(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".mcp.json")
	body := `{"mcpServers": {"user-added": {"command": "manual"}}}`
	_ = os.WriteFile(path, []byte(body), 0o600)
	desired := map[string]provision.MCPServer{
		"postgres": {Key: "postgres", Command: "p"},
	}
	if err := mcp.NewClaudeJSON("").Write(path, desired); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	var doc struct {
		AideManaged []string                  `json:"_aide_managed"`
		Servers     map[string]map[string]any `json:"mcpServers"`
	}
	_ = json.Unmarshal(raw, &doc)
	if _, ok := doc.Servers["user-added"]; !ok {
		t.Error("user-added must survive")
	}
	if _, ok := doc.Servers["postgres"]; !ok {
		t.Error("postgres not written")
	}
}

func TestClaudeJSONWriteRemovesPreviouslyManaged(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".mcp.json")
	body := `{
  "_aide_managed": ["old", "stay"],
  "mcpServers": {"old": {"command": "x"}, "stay": {"command": "y"}}
}`
	_ = os.WriteFile(path, []byte(body), 0o600)
	desired := map[string]provision.MCPServer{
		"stay": {Key: "stay", Command: "y"},
	}
	if err := mcp.NewClaudeJSON("").Write(path, desired); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	var doc struct {
		AideManaged []string                  `json:"_aide_managed"`
		Servers     map[string]map[string]any `json:"mcpServers"`
	}
	_ = json.Unmarshal(raw, &doc)
	if _, gone := doc.Servers["old"]; gone {
		t.Error("old should be removed")
	}
	if _, kept := doc.Servers["stay"]; !kept {
		t.Error("stay should remain")
	}
}

func TestClaudeJSONWriteToNestedShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claude.json")
	body := `{
  "projects": {
    "/repo/aide": {
      "mcpServers": {"user-added": {"command": "manual"}}
    }
  }
}`
	_ = os.WriteFile(path, []byte(body), 0o600)
	desired := map[string]provision.MCPServer{
		"postgres": {Key: "postgres", Command: "p"},
	}
	if err := mcp.NewClaudeJSON("/repo/aide").Write(path, desired); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	var doc struct {
		Projects map[string]struct {
			AideManaged []string                  `json:"_aide_managed"`
			Servers     map[string]map[string]any `json:"mcpServers"`
		} `json:"projects"`
	}
	_ = json.Unmarshal(raw, &doc)
	entry := doc.Projects["/repo/aide"]
	if _, ok := entry.Servers["user-added"]; !ok {
		t.Error("user-added must survive in nested shape")
	}
	if _, ok := entry.Servers["postgres"]; !ok {
		t.Error("postgres not written in nested shape")
	}
	if len(entry.AideManaged) != 1 || entry.AideManaged[0] != "postgres" {
		t.Errorf("_aide_managed = %v", entry.AideManaged)
	}
}
