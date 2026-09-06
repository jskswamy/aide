package provision_test

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/provision"
)

// TestResolveSecretsInMCPEnv_ReplacesTemplateValues pins T16's headline
// behavior: an MCP server declared with `env: { TOKEN: "{{ .secrets.X }}" }`
// in config.yaml must, after ResolveSecretsInMCPEnv runs, carry the
// actual secret value so the per-agent config file the handlers later
// write contains the resolved value the agent will see at runtime.
//
// The template engine is shared with launcher.go's existing context-env
// resolution (config.ResolveTemplates) — same {{ .secrets.X }} syntax,
// same missingkey=error semantics — so this test only pins the wiring,
// not the templating semantics themselves (those live in
// internal/config/template_test.go).
func TestResolveSecretsInMCPEnv_ReplacesTemplateValues(t *testing.T) {
	desired := provision.Desired{
		MCPServers: map[string]provision.MCPServer{
			"github": {
				Key:     "github",
				Command: "github-mcp-server",
				Env: map[string]string{
					"GITHUB_TOKEN": "{{ .secrets.github_token }}",
					"PROJECT_ROOT": "{{ .project_root }}",
					"PLAIN_VAR":    "verbatim-no-template",
				},
			},
		},
	}
	td := &config.TemplateData{
		Secrets:     map[string]string{"github_token": "ghp_abc123abc123"},
		ProjectRoot: "/Users/sam/projects/aide",
		RuntimeDir:  "/tmp/aide-runtime",
	}

	if err := provision.ResolveSecretsInMCPEnv(&desired, td); err != nil {
		t.Fatalf("ResolveSecretsInMCPEnv: %v", err)
	}

	env := desired.MCPServers["github"].Env
	if env["GITHUB_TOKEN"] != "ghp_abc123abc123" {
		t.Errorf("GITHUB_TOKEN = %q, want %q", env["GITHUB_TOKEN"], "ghp_abc123abc123")
	}
	if env["PROJECT_ROOT"] != "/Users/sam/projects/aide" {
		t.Errorf("PROJECT_ROOT = %q, want %q", env["PROJECT_ROOT"], "/Users/sam/projects/aide")
	}
	if env["PLAIN_VAR"] != "verbatim-no-template" {
		t.Errorf("PLAIN_VAR = %q, want %q (plain values must pass through)", env["PLAIN_VAR"], "verbatim-no-template")
	}
}

// TestResolveSecretsInMCPEnv_MissingSecretErrors pins the loud-fail
// contract. If config references {{ .secrets.X }} and X is not present
// in the decrypted secrets, sync MUST fail with an error naming the
// offending key — silently shipping an empty value into the agent's
// config would burn the user with a confusing auth failure at agent
// runtime instead of at config-validation time.
func TestResolveSecretsInMCPEnv_MissingSecretErrors(t *testing.T) {
	desired := provision.Desired{
		MCPServers: map[string]provision.MCPServer{
			"github": {
				Key: "github",
				Env: map[string]string{"GITHUB_TOKEN": "{{ .secrets.missing_key }}"},
			},
		},
	}
	td := &config.TemplateData{
		Secrets: map[string]string{"different_key": "value"},
	}

	err := provision.ResolveSecretsInMCPEnv(&desired, td)
	if err == nil {
		t.Fatal("expected error for missing secret key, got nil")
	}
	if !strings.Contains(err.Error(), "missing_key") {
		t.Errorf("error must name the missing key; got: %v", err)
	}
}

// TestResolveSecretsInMCPEnv_NoSecretsConfigured pins backward compat:
// when the user has no secrets file and no template references, the
// call is a no-op (existing pre-T16 behavior). Plain env values pass
// through unchanged; empty Secrets map does not trigger errors.
func TestResolveSecretsInMCPEnv_NoSecretsConfigured(t *testing.T) {
	desired := provision.Desired{
		MCPServers: map[string]provision.MCPServer{
			"postgres": {
				Key:     "postgres",
				Command: "postgres-mcp",
				Args:    []string{"--port", "5432"},
				Env:     map[string]string{"LOG_LEVEL": "info"},
			},
		},
	}
	td := &config.TemplateData{Secrets: map[string]string{}}

	if err := provision.ResolveSecretsInMCPEnv(&desired, td); err != nil {
		t.Fatalf("no-template no-secrets case must succeed; got: %v", err)
	}
	if got := desired.MCPServers["postgres"].Env["LOG_LEVEL"]; got != "info" {
		t.Errorf("LOG_LEVEL = %q, want %q", got, "info")
	}
}

// TestResolveSecretsInMCPEnv_NilTemplateDataPlainOnly covers the
// "user has no secrets configured at all" path during aide sync: the
// caller may pass td=nil to signal "no template resolution available".
// Plain env values must still pass through unchanged; any template
// reference must fail with a clear error so the user understands they
// declared a secret reference without a secrets file.
func TestResolveSecretsInMCPEnv_NilTemplateDataPlainOnly(t *testing.T) {
	desired := provision.Desired{
		MCPServers: map[string]provision.MCPServer{
			"plain": {
				Key: "plain",
				Env: map[string]string{"LOG_LEVEL": "info"},
			},
		},
	}
	if err := provision.ResolveSecretsInMCPEnv(&desired, nil); err != nil {
		t.Fatalf("nil td + plain env must succeed; got: %v", err)
	}
	if got := desired.MCPServers["plain"].Env["LOG_LEVEL"]; got != "info" {
		t.Errorf("plain env value mutated: got %q", got)
	}
}

// TestResolveSecretsInMCPEnv_NilTemplateDataWithReferenceErrors
// catches the "I referenced a secret but didn't configure a secrets
// file" misconfiguration loudly at sync time, naming the offending
// MCP server.
func TestResolveSecretsInMCPEnv_NilTemplateDataWithReferenceErrors(t *testing.T) {
	desired := provision.Desired{
		MCPServers: map[string]provision.MCPServer{
			"github": {
				Key: "github",
				Env: map[string]string{"TOKEN": "{{ .secrets.api_key }}"},
			},
		},
	}
	err := provision.ResolveSecretsInMCPEnv(&desired, nil)
	if err == nil {
		t.Fatal("expected error: template reference with no secrets configured")
	}
	if !strings.Contains(err.Error(), "github") {
		t.Errorf("error must name the offending MCP server %q; got: %v", "github", err)
	}
}

// TestMCPTemplatesSatisfiedByInstalled_DoesNotMutateOnPartialMatch guards
// the no-mutation-on-check-failure property directly:
// MCPTemplatesSatisfiedByInstalled must be a pure check. A server with two
// templated env keys where only one is present in installed must report
// false, and — critically — must leave the satisfiable key's template
// literal untouched rather than partially substituting it before
// discovering the other key is missing. An earlier draft of this design
// conflated the check with a check-and-fill loop that mutated as it went;
// this test fails if that regresses.
func TestMCPTemplatesSatisfiedByInstalled_DoesNotMutateOnPartialMatch(t *testing.T) {
	desired := &provision.Desired{
		MCPServers: map[string]provision.MCPServer{
			"github": {
				Command: "github-mcp-server",
				Env: map[string]string{
					"A": "{{ .secrets.a }}",
					"B": "{{ .secrets.b }}",
				},
			},
		},
	}
	installed := provision.Installed{
		MCPServers: map[string]provision.MCPServer{
			"github": {
				Command: "github-mcp-server",
				Env:     map[string]string{"A": "already-installed-a"},
			},
		},
	}

	if got := provision.MCPTemplatesSatisfiedByInstalled(desired, installed); got {
		t.Fatalf("MCPTemplatesSatisfiedByInstalled = true, want false (key B missing from installed)")
	}
	if got := desired.MCPServers["github"].Env["A"]; got != "{{ .secrets.a }}" {
		t.Errorf("desired.MCPServers[\"github\"].Env[\"A\"] = %q, want unchanged template literal %q (check must not partially substitute before failing)", got, "{{ .secrets.a }}")
	}
}

// TestSubstituteFromInstalled_BuildsFreshEnvMap pins the fix from commit
// b39e9a59: SubstituteFromInstalled must build a fresh Env map for the
// desired server rather than writing the resolved value into the
// installed server's Env map in place. installed.MCPServers[name].Env may
// be shared by reference with other in-memory structures (see the
// function's doc comment), so mutating it directly would leak a resolved
// secret into state the substitution wasn't supposed to touch. A
// regression back to in-place mutation of installed.Env would not change
// desired's resolved value (assertion a below would still pass) but would
// corrupt the installed map's contents as a side effect (assertion b
// below would catch it).
func TestSubstituteFromInstalled_BuildsFreshEnvMap(t *testing.T) {
	installedEnv := map[string]string{"TOKEN": "installed-value"}
	installed := provision.Installed{
		MCPServers: map[string]provision.MCPServer{
			"github": {Command: "github-mcp-server", Env: installedEnv},
		},
	}
	desired := &provision.Desired{
		MCPServers: map[string]provision.MCPServer{
			"github": {
				Command: "github-mcp-server",
				Env:     map[string]string{"TOKEN": "{{ .secrets.token }}"},
			},
		},
	}

	provision.SubstituteFromInstalled(desired, installed)

	// (a) The templated key in desired is resolved to installed's value.
	if got := desired.MCPServers["github"].Env["TOKEN"]; got != "installed-value" {
		t.Errorf("desired.MCPServers[\"github\"].Env[\"TOKEN\"] = %q, want %q", got, "installed-value")
	}

	// (b) The original installed Env map object is untouched: same key
	// count, same value, at the same map reference passed in.
	if len(installedEnv) != 1 {
		t.Errorf("installed Env map mutated: len = %d, want 1", len(installedEnv))
	}
	if got := installedEnv["TOKEN"]; got != "installed-value" {
		t.Errorf("installed Env[\"TOKEN\"] = %q, want unchanged %q (SubstituteFromInstalled must not mutate installed's map)", got, "installed-value")
	}
	if got := installed.MCPServers["github"].Env["TOKEN"]; got != "installed-value" {
		t.Errorf("installed.MCPServers[\"github\"].Env[\"TOKEN\"] = %q, want unchanged %q", got, "installed-value")
	}
}
