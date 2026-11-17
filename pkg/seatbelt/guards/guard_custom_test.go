package guards_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

func TestGuard_Custom_Basic(t *testing.T) {
	tmpDir := t.TempDir()
	myToolDir := filepath.Join(tmpDir, ".my-tool")
	if err := os.MkdirAll(myToolDir, 0o700); err != nil {
		t.Fatal(err)
	}

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

	ctx := &seatbelt.Context{HomeDir: tmpDir}
	rules := g.Rules(ctx)
	output := renderTestRules(rules.Rules)

	if !strings.Contains(output, `(deny file-read-data`) {
		t.Error("expected deny file-read-data rule")
	}
	if !strings.Contains(output, `(deny file-write*`) {
		t.Error("expected deny file-write* rule")
	}
	if !strings.Contains(output, myToolDir) {
		t.Errorf("expected path %s in rules", myToolDir)
	}
}

func TestGuard_Custom_EnvOverride(t *testing.T) {
	tmpDir := t.TempDir()
	overridePath := filepath.Join(tmpDir, "override-path")
	if err := os.MkdirAll(overridePath, 0o700); err != nil {
		t.Fatal(err)
	}

	cfg := guards.CustomGuardConfig{
		Type:        "opt-in",
		Description: "Custom tool with env override",
		Paths:       []string{"~/.default-path"},
		EnvOverride: "MY_TOOL_CONFIG",
	}
	g := guards.NewCustomGuard("my-env-tool", cfg)

	// When env var is set, use it instead of default path.
	ctx := &seatbelt.Context{
		HomeDir: tmpDir,
		Env:     []string{"MY_TOOL_CONFIG=" + overridePath},
	}
	rules := g.Rules(ctx)
	output := renderTestRules(rules.Rules)

	if !strings.Contains(output, overridePath) {
		t.Errorf("expected env override path %s in rules", overridePath)
	}
	if strings.Contains(output, `.default-path`) {
		t.Error("default path should not appear when env override is active")
	}
}

func TestGuard_Custom_AllowedPaths(t *testing.T) {
	tmpDir := t.TempDir()
	secretsDir := filepath.Join(tmpDir, ".secrets-dir")
	if err := os.MkdirAll(secretsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	publicFile := filepath.Join(secretsDir, "public.txt")
	if err := os.WriteFile(publicFile, []byte("public"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := guards.CustomGuardConfig{
		Type:        "default",
		Description: "Custom with allowed file",
		Paths:       []string{"~/.secrets-dir"},
		Allowed:     []string{"~/.secrets-dir/public.txt"},
	}
	g := guards.NewCustomGuard("my-secrets", cfg)

	ctx := &seatbelt.Context{HomeDir: tmpDir}
	rules := g.Rules(ctx)
	output := renderTestRules(rules.Rules)

	// Deny rule for the directory.
	if !strings.Contains(output, `(subpath "`+secretsDir+`")`) {
		t.Errorf("expected subpath deny for %s", secretsDir)
	}
	// Allow rule for the specific file.
	if !strings.Contains(output, `(allow file-read*`) {
		t.Error("expected allow file-read* rule for allowed path")
	}
	if !strings.Contains(output, publicFile) {
		t.Errorf("expected allowed path %s in output", publicFile)
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

func TestCustomGuard_MissingPaths(t *testing.T) {
	cfg := guards.CustomGuardConfig{
		Type:  "default",
		Paths: []string{"/nonexistent/path1", "/nonexistent/path2"},
	}
	g := guards.NewCustomGuard("test-custom", cfg)
	ctx := &seatbelt.Context{HomeDir: t.TempDir(), GOOS: "darwin"}
	result := g.Rules(ctx)
	if len(result.Rules) != 0 {
		t.Errorf("expected 0 rules for missing paths, got %d", len(result.Rules))
	}
	if len(result.Skipped) != 2 {
		t.Errorf("expected 2 skipped, got %d", len(result.Skipped))
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

	result = guards.ValidateCustomGuard("aide-secrets", cfg)
	if result.OK() {
		t.Error("expected error for built-in name collision (aide-secrets)")
	}
}

func TestSetCustomGuards_RegistersByType(t *testing.T) {
	t.Cleanup(func() { _ = guards.SetCustomGuards(nil) })
	err := guards.SetCustomGuards(map[string]guards.CustomGuardConfig{
		"audit-logs": {Type: "default", Paths: []string{"~/.config/audit"}},
		"team-vault": {Type: "opt-in", Paths: []string{"~/.config/team-vault"}},
		"untyped":    {Paths: []string{"~/.untyped"}},
	})
	if err != nil {
		t.Fatalf("SetCustomGuards: %v", err)
	}
	if g, ok := guards.GuardByName("team-vault"); !ok || g.Type() != "opt-in" {
		t.Errorf("team-vault lookup = %v, %v; want opt-in guard", g, ok)
	}
	defaults := guards.DefaultGuardNames()
	for _, want := range []string{"audit-logs", "untyped"} {
		if !slices.Contains(defaults, want) {
			t.Errorf("DefaultGuardNames() = %v, missing %s", defaults, want)
		}
	}
	if slices.Contains(defaults, "team-vault") {
		t.Errorf("DefaultGuardNames() = %v, opt-in team-vault should be absent", defaults)
	}
	if !slices.Contains(guards.ExpandGuardName("all-default"), "audit-logs") {
		t.Error("all-default does not expand to custom default guard audit-logs")
	}
	if got := guards.CustomGuardConfigs(); len(got) != 3 {
		t.Errorf("CustomGuardConfigs() = %v, want 3 entries", got)
	}

	if err := guards.SetCustomGuards(nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := guards.GuardByName("audit-logs"); ok {
		t.Error("audit-logs still registered after replacing the custom set")
	}
}

func TestSetCustomGuards_RejectsInvalidAndKeepsPreviousSet(t *testing.T) {
	t.Cleanup(func() { _ = guards.SetCustomGuards(nil) })
	if err := guards.SetCustomGuards(map[string]guards.CustomGuardConfig{"kept": {Paths: []string{"~/.kept"}}}); err != nil {
		t.Fatal(err)
	}
	for name, cfg := range map[string]guards.CustomGuardConfig{
		"base":     {Paths: []string{"~/.x"}},
		"always":   {Type: "always", Paths: []string{"~/.x"}},
		"bad-type": {Type: "compliance", Paths: []string{"~/.x"}},
		"no-paths": {Type: "default"},
	} {
		err := guards.SetCustomGuards(map[string]guards.CustomGuardConfig{name: cfg})
		if err == nil {
			t.Errorf("SetCustomGuards(%s) succeeded, want error", name)
		}
	}
	if _, ok := guards.GuardByName("kept"); !ok {
		t.Error("a rejected set replaced the previous custom guards")
	}
}
