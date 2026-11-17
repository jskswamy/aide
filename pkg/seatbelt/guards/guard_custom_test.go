package guards_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/testutil"
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
	sshDir := filepath.Join(tmpDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	prodDB := filepath.Join(sshDir, "prod_db")
	prodPub := filepath.Join(sshDir, "prod_db.pub")
	for _, f := range []string{prodDB, prodPub} {
		if err := os.WriteFile(f, []byte("key"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	cfg := guards.CustomGuardConfig{
		Type:        "default",
		Description: "Custom with allowed exception",
		Paths:       []string{"~/.ssh/prod_*"},
		Allowed:     []string{"~/.ssh/prod_db.pub"},
	}
	g := guards.NewCustomGuard("prod-ssh", cfg)

	ctx := &seatbelt.Context{HomeDir: tmpDir}
	rules := g.Rules(ctx)
	output := renderTestRules(rules.Rules)

	if !strings.Contains(output, `(subpath "`+prodDB+`")`) {
		t.Errorf("expected subpath deny for %s", prodDB)
	}
	if strings.Contains(output, `(subpath "`+prodPub+`")`) {
		t.Errorf("allowed path %s must be exempted from deny rules, got:\n%s", prodPub, output)
	}
	if slices.Contains(rules.Protected, prodPub) {
		t.Errorf("allowed path %s must not appear in Protected: %v", prodPub, rules.Protected)
	}
	if !slices.Contains(rules.Allowed, prodPub) {
		t.Errorf("expected %s in Allowed: %v", prodPub, rules.Allowed)
	}
}

func TestCustomGuard_SymlinkTargetDenied(t *testing.T) {
	home := t.TempDir()
	ssh := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(ssh, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(ssh, "key-material")
	if err := os.WriteFile(target, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(ssh, "prod_db")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	g := guards.NewCustomGuard("prod-ssh", guards.CustomGuardConfig{
		Paths: []string{"~/.ssh/prod_*"},
	})
	result := g.Rules(&seatbelt.Context{HomeDir: home, GOOS: "darwin"})
	output := renderTestRules(result.Rules)

	resolvedTarget := seatbelt.ResolveOrSelf(target)
	if !slices.Contains(result.Protected, link) || !slices.Contains(result.Protected, resolvedTarget) {
		t.Errorf("Protected = %v, want both symlink %s and resolved target %s", result.Protected, link, resolvedTarget)
	}
	if !strings.Contains(output, `(subpath "`+resolvedTarget+`")`) {
		t.Errorf("Seatbelt rules must deny resolved target %s, got:\n%s", resolvedTarget, output)
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

func TestCustomGuard_LiteralBracketPathKeptAlongsideSiblingGlobMatch(t *testing.T) {
	home := testutil.CanonicalTempDir(t)
	ssh := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(ssh, 0o700); err != nil {
		t.Fatal(err)
	}
	literalBracket := filepath.Join(ssh, "prod_[1]")
	sibling := filepath.Join(ssh, "prod_1")
	for _, f := range []string{literalBracket, sibling} {
		if err := os.WriteFile(f, []byte("k"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	g := guards.NewCustomGuard("prod-ssh", guards.CustomGuardConfig{
		Paths: []string{"~/.ssh/prod_[1]"},
	})
	result := g.Rules(&seatbelt.Context{HomeDir: home, GOOS: "darwin"})
	if !slices.Contains(result.Protected, literalBracket) {
		t.Errorf("Protected = %v, missing configured literal bracket file %s", result.Protected, literalBracket)
	}
}

func TestCustomGuard_GlobPaths(t *testing.T) {
	home := testutil.CanonicalTempDir(t)
	ssh := filepath.Join(home, ".ssh")
	for _, f := range []string{"id_ed25519", "prod_db", "prod_web"} {
		if err := os.MkdirAll(ssh, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(ssh, f), []byte("k"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	g := guards.NewCustomGuard("prod-ssh", guards.CustomGuardConfig{
		Paths: []string{"~/.ssh/prod_*", "~/.ssh/staging_*"},
	})
	result := g.Rules(&seatbelt.Context{HomeDir: home, GOOS: "darwin"})
	want := []string{filepath.Join(ssh, "prod_db"), filepath.Join(ssh, "prod_web")}
	if !slices.Equal(result.Protected, want) {
		t.Errorf("Protected = %v, want %v", result.Protected, want)
	}
	if len(result.Skipped) != 1 {
		t.Errorf("Skipped = %v, want the unmatched staging_* glob", result.Skipped)
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

	result = guards.ValidateCustomGuard("all-default", cfg)
	if result.OK() {
		t.Error("expected error for reserved meta-guard name all-default")
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
