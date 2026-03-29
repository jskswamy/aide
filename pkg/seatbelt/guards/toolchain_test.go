package guards_test

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
	"github.com/jskswamy/aide/pkg/seatbelt/modules"
)

func TestGuard_NodeToolchain_Metadata(t *testing.T) {
	g := guards.NodeToolchainGuard()

	if g.Name() != "node-toolchain" {
		t.Errorf("expected Name() = %q, got %q", "node-toolchain", g.Name())
	}
	if g.Type() != "always" {
		t.Errorf("expected Type() = %q, got %q", "always", g.Type())
	}
	if g.Description() == "" {
		t.Error("expected non-empty Description()")
	}
}

func TestGuard_NodeToolchain_Paths(t *testing.T) {
	g := guards.NodeToolchainGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	paths := []string{
		`(subpath "/Users/testuser/.npm")`,
		`(subpath "/Users/testuser/.yarn")`,
		`(subpath "/Users/testuser/.pnpm-store")`,
		`(subpath "/Users/testuser/.nvm")`,
		`(literal "/Users/testuser/.npmrc")`,
		`(subpath "/Users/testuser/.cache/turbo")`,
	}
	for _, p := range paths {
		if !strings.Contains(output, p) {
			t.Errorf("expected output to contain %q", p)
		}
	}
}

func TestGuard_NixToolchain_Metadata(t *testing.T) {
	g := guards.NixToolchainGuard()

	if g.Name() != "nix-toolchain" {
		t.Errorf("expected Name() = %q, got %q", "nix-toolchain", g.Name())
	}
	if g.Type() != "always" {
		t.Errorf("expected Type() = %q, got %q", "always", g.Type())
	}
	if g.Description() == "" {
		t.Error("expected non-empty Description()")
	}
}

func TestGuard_NixToolchain_DetectionGate(t *testing.T) {
	g := guards.NixToolchainGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	result := g.Rules(ctx)

	if guards.TestDirExists("/nix/store") {
		if len(result.Skipped) > 0 {
			t.Error("nix is installed but guard returned Skipped")
		}
		if len(result.Rules) == 0 {
			t.Error("nix is installed but guard returned no rules")
		}
	} else {
		if len(result.Rules) > 0 {
			t.Error("nix is not installed but guard returned rules")
		}
		if len(result.Skipped) == 0 {
			t.Error("nix is not installed but guard returned no Skipped messages")
		}
	}
}

func TestGuard_NixToolchain_Paths(t *testing.T) {
	if !guards.TestDirExists("/nix/store") {
		t.Skip("nix not installed")
	}
	g := guards.NixToolchainGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	// Daemon socket (still owned by nix-toolchain)
	if !strings.Contains(output, `network-outbound`) {
		t.Error("expected network-outbound rule for daemon socket")
	}
	if !strings.Contains(output, `/nix/var/nix/daemon-socket/socket`) {
		t.Error("expected daemon socket path")
	}

	// Nix user paths (read+write, self-contained)
	userPaths := []string{
		`(subpath "/Users/testuser/.nix-profile")`,
		`(subpath "/Users/testuser/.local/state/nix")`,
		`(subpath "/Users/testuser/.cache/nix")`,
	}
	for _, p := range userPaths {
		if !strings.Contains(output, p) {
			t.Errorf("expected user path %q", p)
		}
	}

	// Must have file-read* (self-contained, not relying on filesystem guard)
	if !strings.Contains(output, "file-read*") {
		t.Error("nix user paths must include file-read* (guard must be self-contained)")
	}

	// Nix channel definitions and user config (read-only)
	readPaths := []string{
		`(subpath "/Users/testuser/.nix-defexpr")`,
		`(subpath "/Users/testuser/.config/nix")`,
	}
	for _, p := range readPaths {
		if !strings.Contains(output, p) {
			t.Errorf("expected read path %q", p)
		}
	}

	// System paths should NOT be in nix-toolchain (owned by system-runtime)
	systemPaths := []string{
		`"/nix/store"`,
		`"/nix/var"`,
		`"/run/current-system"`,
	}
	for _, p := range systemPaths {
		if strings.Contains(output, p) {
			t.Errorf("should NOT contain system path %q (owned by system-runtime)", p)
		}
	}
}

func TestGuard_Keychain_Metadata(t *testing.T) {
	g := guards.KeychainGuard()

	if g.Name() != "keychain" {
		t.Errorf("expected Name() = %q, got %q", "keychain", g.Name())
	}
	if g.Type() != "always" {
		t.Errorf("expected Type() = %q, got %q", "always", g.Type())
	}
	if g.Description() == "" {
		t.Error("expected non-empty Description()")
	}
}

func TestGuard_Keychain_Rules(t *testing.T) {
	g := guards.KeychainGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	// User keychain read paths
	if !strings.Contains(output, `(subpath "/Users/testuser/Library/Keychains")`) {
		t.Error("expected user Library/Keychains path")
	}
	if !strings.Contains(output, `(literal "/Users/testuser/Library/Preferences/com.apple.security.plist")`) {
		t.Error("expected security plist path")
	}

	// Should be read-only, not read-write
	if !strings.Contains(output, "file-read*") {
		t.Error("expected file-read* for keychain paths")
	}
	if strings.Contains(output, "file-write*") {
		t.Error("keychain paths should be read-only, not writable")
	}

	// System keychain reads are now covered by system-runtime broad reads
	if strings.Contains(output, `(literal "/Library/Keychains/System.keychain")`) {
		t.Error("system keychain read should be removed (covered by system-runtime)")
	}

	// Mach services and IPC should still be present
	machServices := []string{
		"com.apple.SecurityServer",
		"com.apple.secd",
		"com.apple.trustd",
		"com.apple.AppleDatabaseChanged",
	}
	for _, svc := range machServices {
		if !strings.Contains(output, svc) {
			t.Errorf("expected output to contain %q", svc)
		}
	}
}

func TestClaudeAgent(t *testing.T) {
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	m := modules.ClaudeAgent()
	result := m.Rules(ctx)
	output := renderTestRules(result.Rules)

	if m.Name() != "Claude Agent" {
		t.Errorf("expected name %q, got %q", "Claude Agent", m.Name())
	}

	// Read-write paths
	rwPaths := []string{
		`(subpath "/Users/testuser/.claude")`,
		`(literal "/Users/testuser/.mcp.json")`,
		`(subpath "/Users/testuser/.local/share/claude")`,
		`(subpath "/Users/testuser/.local/state/claude")`,
		`(subpath "/Users/testuser/.cache/claude")`,
		`(subpath "/Users/testuser/.config/claude")`,
	}
	for _, p := range rwPaths {
		if !strings.Contains(output, p) {
			t.Errorf("expected output to contain %q", p)
		}
	}

	// Read-only paths
	roPaths := []string{
		`(literal "/Users/testuser/Library/Application Support/Claude/claude_desktop_config.json")`,
		`(subpath "/Library/Application Support/ClaudeCode/.claude")`,
		`(literal "/Library/Application Support/ClaudeCode/managed-settings.json")`,
		`(literal "/Library/Application Support/ClaudeCode/managed-mcp.json")`,
		`(literal "/Library/Application Support/ClaudeCode/CLAUDE.md")`,
	}
	for _, p := range roPaths {
		if !strings.Contains(output, p) {
			t.Errorf("expected output to contain read-only path %q", p)
		}
	}
}

func TestGuard_NixToolchain_EtcNixCoverage(t *testing.T) {
	// /etc/nix paths are covered by system-runtime guard's (subpath "/etc")
	// and (subpath "/private") rules, not by the nix-toolchain guard.
	// This test verifies the coverage exists.
	sysGuard := guards.SystemRuntimeGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser", GOOS: "darwin"}
	result := sysGuard.Rules(ctx)
	output := renderTestRules(result.Rules)

	// Verify /etc subpath is present (covers /etc/nix/registry.json)
	if !strings.Contains(output, `(subpath "/etc")`) {
		t.Error("expected system-runtime to have (subpath \"/etc\") for nix system paths")
	}
}
