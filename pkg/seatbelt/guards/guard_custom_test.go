package guards_test

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

func TestGuard_Custom_Basic(t *testing.T) {
	cfg := guards.CustomGuardConfig{
		Type:        "default",
		Description: "My custom secrets dir",
		Paths:       []string{"~/.my-tool"},
	}
	g := guards.NewCustomGuard("my-tool", cfg)

	if g.Name() != "my-tool" {
		t.Errorf("expected name %q, got %q", "my-tool", g.Name())
	}
	if g.Type() != "default" {
		t.Errorf("expected type %q, got %q", "default", g.Type())
	}
	if g.Description() != "My custom secrets dir" {
		t.Errorf("expected description %q, got %q", "My custom secrets dir", g.Description())
	}

	ctx := &seatbelt.Context{HomeDir: "/home/user"}
	rules := g.Rules(ctx)
	output := renderTestRules(rules.Rules)

	if !strings.Contains(output, `(deny file-read-data`) {
		t.Error("expected deny file-read-data rule")
	}
	if !strings.Contains(output, `(deny file-write*`) {
		t.Error("expected deny file-write* rule")
	}
	if !strings.Contains(output, `/home/user/.my-tool`) {
		t.Error("expected path /home/user/.my-tool in rules")
	}
}

func TestGuard_Custom_EnvOverride(t *testing.T) {
	cfg := guards.CustomGuardConfig{
		Type:        "opt-in",
		Description: "Custom tool with env override",
		Paths:       []string{"~/.default-path"},
		EnvOverride: "MY_TOOL_CONFIG",
	}
	g := guards.NewCustomGuard("my-env-tool", cfg)

	// When env var is set, use it instead of default path.
	ctx := &seatbelt.Context{
		HomeDir: "/home/user",
		Env:     []string{"MY_TOOL_CONFIG=/custom/override/path"},
	}
	rules := g.Rules(ctx)
	output := renderTestRules(rules.Rules)

	if !strings.Contains(output, `/custom/override/path`) {
		t.Error("expected env override path /custom/override/path in rules")
	}
	if strings.Contains(output, `.default-path`) {
		t.Error("default path should not appear when env override is active")
	}
}

func TestGuard_Custom_AllowedPaths(t *testing.T) {
	cfg := guards.CustomGuardConfig{
		Type:        "default",
		Description: "Custom with allowed file",
		Paths:       []string{"~/.secrets-dir"},
		Allowed:     []string{"~/.secrets-dir/public.txt"},
	}
	g := guards.NewCustomGuard("my-secrets", cfg)

	ctx := &seatbelt.Context{HomeDir: "/home/user"}
	rules := g.Rules(ctx)
	output := renderTestRules(rules.Rules)

	// Deny rule for the directory.
	if !strings.Contains(output, `(subpath "/home/user/.secrets-dir")`) {
		t.Error("expected subpath deny for .secrets-dir")
	}
	// Allow rule for the specific file.
	if !strings.Contains(output, `(allow file-read*`) {
		t.Error("expected allow file-read* rule for allowed path")
	}
	if !strings.Contains(output, `/home/user/.secrets-dir/public.txt`) {
		t.Error("expected allowed path in output")
	}
}

func TestGuard_CustomValidation_EnvOverrideMultiPath(t *testing.T) {
	cfg := guards.CustomGuardConfig{
		Type:        "default",
		Description: "Multi-path with env override",
		Paths:       []string{"~/.path1", "~/.path2"},
		EnvOverride: "MY_ENV",
	}
	result := guards.ValidateCustomGuard("multi-path", cfg)
	if result.OK() {
		t.Error("expected error for EnvOverride with multiple paths")
	}
}

func TestGuard_CustomValidation_AlwaysType(t *testing.T) {
	cfg := guards.CustomGuardConfig{
		Type:        "always",
		Description: "Invalid always type",
		Paths:       []string{"~/.some-path"},
	}
	result := guards.ValidateCustomGuard("my-always-guard", cfg)
	if result.OK() {
		t.Error("expected error for always type on custom guard")
	}
}

func TestGuard_CustomValidation_EmptyPaths(t *testing.T) {
	r := guards.ValidateCustomGuard("my-guard", guards.CustomGuardConfig{
		Type:        "default",
		Description: "no paths",
	})
	if r.OK() {
		t.Error("expected error when no paths provided")
	}
}

func TestGuard_CustomValidation_BuiltinNameCollision(t *testing.T) {
	cfg := guards.CustomGuardConfig{
		Type:        "default",
		Description: "Collides with built-in",
		Paths:       []string{"~/.some-path"},
	}
	result := guards.ValidateCustomGuard("base", cfg)
	if result.OK() {
		t.Error("expected error for built-in name collision")
	}

	result = guards.ValidateCustomGuard("docker", cfg)
	if result.OK() {
		t.Error("expected error for built-in name collision (docker)")
	}
}
