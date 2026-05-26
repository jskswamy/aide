package explain

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/config"
)

func TestStateFromConfig_RedactsUnrecognizedTemplate(t *testing.T) {
	// Fail-closed (T1): a literal that happens to contain "{{" or an
	// unrecognized template form must be redacted, not echoed.
	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"work": {
				Agent: "claude",
				Env: map[string]string{
					"WEIRD": "sk-live-secret{{ .unknown }}",
				},
			},
		},
	}
	st := StateFromConfig(cfg)
	for _, e := range st.Contexts[0].Env {
		if e.Key == "WEIRD" {
			if !e.Redacted || e.Template != "" {
				t.Errorf("unrecognized template must be redacted, got Redacted=%v Template=%q", e.Redacted, e.Template)
			}
		}
	}
}

func TestStateFromConfig_RedactsInlineLiteralEnv(t *testing.T) {
	cfg := &config.Config{
		DefaultContext: "work",
		Contexts: map[string]config.Context{
			"work": {
				Agent:  "claude",
				Secret: "work",
				Env: map[string]string{
					"GITHUB_TOKEN": "{{ .secrets.github_token }}",
					"RAW_TOKEN":    "ghp_supersecretliteral",
				},
			},
		},
	}

	st := StateFromConfig(cfg)

	if !st.Loaded {
		t.Fatal("expected Loaded=true")
	}
	if len(st.Contexts) != 1 {
		t.Fatalf("expected 1 context, got %d", len(st.Contexts))
	}

	var gotSecretRef, gotRedacted bool
	for _, e := range st.Contexts[0].Env {
		switch e.Key {
		case "GITHUB_TOKEN":
			if e.SecretRef != "github_token" {
				t.Errorf("GITHUB_TOKEN SecretRef = %q, want github_token", e.SecretRef)
			}
			if e.Redacted {
				t.Error("GITHUB_TOKEN should not be marked Redacted")
			}
			gotSecretRef = true
		case "RAW_TOKEN":
			if !e.Redacted {
				t.Error("RAW_TOKEN literal must be Redacted (T1)")
			}
			if e.SecretRef != "" {
				t.Errorf("RAW_TOKEN SecretRef = %q, want empty", e.SecretRef)
			}
			gotRedacted = true
		}
	}
	if !gotSecretRef || !gotRedacted {
		t.Fatalf("missing entries: secretRef=%v redacted=%v", gotSecretRef, gotRedacted)
	}
}

func TestStateFromConfig_NilConfigNotLoaded(t *testing.T) {
	st := StateFromConfig(nil)
	if st.Loaded {
		t.Error("nil config must yield Loaded=false")
	}
}

func TestStateFromConfig_ShowsNonSecretTemplateAndNonCredentialLiteral(t *testing.T) {
	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"work": {
				Agent: "claude",
				Env: map[string]string{
					"DATA_DIR":        "{{ .project_root }}/data",
					"ANTHROPIC_MODEL": "claude-opus-4-7",
					"ANTHROPIC_TOKEN": "sk-ant-supersecret",
				},
			},
		},
	}
	st := StateFromConfig(cfg)
	for _, e := range st.Contexts[0].Env {
		switch e.Key {
		case "DATA_DIR":
			if e.Template != "{{ .project_root }}/data" {
				t.Errorf("DATA_DIR Template = %q, want the template verbatim", e.Template)
			}
			if e.Redacted {
				t.Error("non-secret template must not be Redacted")
			}
		case "ANTHROPIC_MODEL":
			if e.Redacted {
				t.Error("ANTHROPIC_MODEL must not be Redacted — model name is not a credential")
			}
			if e.Template != "claude-opus-4-7" {
				t.Errorf("ANTHROPIC_MODEL Template = %q, want claude-opus-4-7", e.Template)
			}
		case "ANTHROPIC_TOKEN":
			if !e.Redacted || e.Template != "" {
				t.Errorf("ANTHROPIC_TOKEN must be Redacted (credential key name), got Redacted=%v Template=%q", e.Redacted, e.Template)
			}
		}
	}
}

func TestStateFromConfig_ShowsTopLevelHooks(t *testing.T) {
	cfg := &config.Config{
		Hooks: config.HooksMap{
			"pre_tool":  {{Command: "guard1"}, {Command: "guard2"}},
			"post_tool": {{Command: "notify"}},
		},
		Contexts: map[string]config.Context{
			"work": {Agent: "claude"},
		},
	}
	st := StateFromConfig(cfg)
	joined := strings.Join(st.TopLevelHooks, " ")
	if !strings.Contains(joined, "post_tool: 1") || !strings.Contains(joined, "pre_tool: 2") {
		t.Errorf("top-level hooks not summarized, got %v", st.TopLevelHooks)
	}
}

func TestStateFromConfig_ShowsContextHookOverride(t *testing.T) {
	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"work": {
				Agent: "claude",
				Hooks: &config.HooksOverride{
					Exclude: []string{"pre_tool"},
					Extra: config.HooksMap{
						"post_tool": {{Command: "work-notifier"}},
					},
				},
			},
		},
	}
	st := StateFromConfig(cfg)
	joined := strings.Join(st.Contexts[0].Hooks, " ")
	if !strings.Contains(joined, "exclude: pre_tool") {
		t.Errorf("hook exclude not shown, got %v", st.Contexts[0].Hooks)
	}
	if !strings.Contains(joined, "extra: post_tool (1)") {
		t.Errorf("hook extra not shown, got %v", st.Contexts[0].Hooks)
	}
}

func TestContextState_IncludesV2MCPOverride(t *testing.T) {
	ctx := config.Context{
		Agent: "claude",
		MCPServersOverride: &config.ContextOverride[config.MCPServer]{
			Exclude: []string{"postgres"},
			Extra:   map[string]config.MCPServer{"jira": {Command: "npx"}},
		},
	}
	cfg := &config.Config{Contexts: map[string]config.Context{"work": ctx}}
	st := StateFromConfig(cfg)
	joined := strings.Join(st.Contexts[0].MCPServers, " ")
	if !strings.Contains(joined, "exclude: postgres") || !strings.Contains(joined, "extra: jira") {
		t.Errorf("v2 MCP override not summarized, got %v", st.Contexts[0].MCPServers)
	}
}
