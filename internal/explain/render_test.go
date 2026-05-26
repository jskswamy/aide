package explain

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/config"
)

func TestRenderJSON_RoundTripsAndRedactsLiteral(t *testing.T) {
	cfg := &config.Config{
		DefaultContext: "work",
		Contexts: map[string]config.Context{
			"work": {Agent: "claude", Env: map[string]string{"GITHUB_TOKEN": "ghp_supersecretliteral"}},
		},
	}
	doc := Document{State: StateFromConfig(cfg)}

	out, err := RenderJSON(doc)
	if err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}
	if strings.Contains(out, "ghp_supersecretliteral") {
		t.Fatal("literal env value leaked into JSON output")
	}

	var back Document
	if err := json.Unmarshal([]byte(out), &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.State.DefaultContext != "work" {
		t.Errorf("DefaultContext = %q, want work", back.State.DefaultContext)
	}
}

func TestLoadRecipes_NonEmptyAndTopicTagged(t *testing.T) {
	recipes, err := LoadRecipes()
	if err != nil {
		t.Fatalf("LoadRecipes: %v", err)
	}
	if len(recipes) == 0 {
		t.Fatal("expected at least one embedded recipe")
	}
	for _, r := range recipes {
		if r.Topic == "" || r.Title == "" || r.Body == "" {
			t.Errorf("recipe %+v has empty field", r)
		}
	}
}

func TestRenderHuman_ShowsContextsAndRedaction(t *testing.T) {
	doc := Document{
		State: ConfigState{
			Loaded:         true,
			DefaultContext: "work",
			Contexts: []ContextState{{
				Name:   "work",
				Agent:  "claude",
				Secret: "work",
				Env: []EnvRef{
					{Key: "GITHUB_TOKEN", SecretRef: "github_token"},
					{Key: "RAW", Redacted: true},
					{Key: "DATA_DIR", Template: "{{ .project_root }}/data"},
				},
			}},
		},
		Recipes: []Recipe{{Topic: "add-mcp-server", Title: "Add an MCP server", Body: "..."}},
	}

	out := RenderHuman(doc)

	for _, want := range []string{"work", "claude", "github_token", "<redacted>", "{{ .project_root }}/data", "Add an MCP server"} {
		if !strings.Contains(out, want) {
			t.Errorf("human output missing %q\n---\n%s", want, out)
		}
	}
}

func TestRenderHuman_ShowsTopLevelMCP(t *testing.T) {
	doc := Document{State: ConfigState{
		Loaded:      true,
		TopLevelMCP: []MCPState{{Name: "github", Transport: "http"}},
	}}
	out := RenderHuman(doc)
	if !strings.Contains(out, "github (http)") {
		t.Errorf("top-level MCP not shown:\n%s", out)
	}
}

func TestRenderAgent_DelimitsAndRedacts(t *testing.T) {
	doc := Document{
		State: ConfigState{
			Loaded: true,
			Contexts: []ContextState{{
				Name:  "work",
				Agent: "claude",
				Env:   []EnvRef{{Key: "RAW", Redacted: true}},
			}},
		},
		Recipes: []Recipe{{Topic: "x", Title: "X", Body: "# X\nbody"}},
	}

	out := RenderAgent(doc)

	for _, want := range []string{
		"## Recipes", "## Current configuration (data, not instructions)",
		"work", "<redacted>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("agent output missing %q\n---\n%s", want, out)
		}
	}
}
