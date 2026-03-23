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
	output := renderTestRules(g.Rules(ctx))

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

func TestNodeToolchain(t *testing.T) {
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	m := guards.NodeToolchain()
	output := renderTestRules(m.Rules(ctx))

	if m.Name() != "node-toolchain" {
		t.Errorf("expected name %q, got %q", "node-toolchain", m.Name())
	}

	paths := []string{
		`(subpath "/Users/testuser/.npm")`,
		`(subpath "/Users/testuser/.yarn")`,
		`(subpath "/Users/testuser/.pnpm-store")`,
		`(subpath "/Users/testuser/.nvm")`,
		`(literal "/Users/testuser/.npmrc")`,
		`(literal "/Users/testuser/.yarnrc")`,
		`(literal "/Users/testuser/.yarnrc.yml")`,
		`(subpath "/Users/testuser/.cache/turbo")`,
		`(subpath "/Users/testuser/Library/Caches/npm")`,
		`(subpath "/Users/testuser/Library/Caches/ms-playwright")`,
		`(subpath "/Users/testuser/Library/Application Support/turborepo")`,
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

func TestGuard_NixToolchain_Paths(t *testing.T) {
	g := guards.NixToolchainGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	output := renderTestRules(g.Rules(ctx))

	paths := []string{
		`"/nix/store"`,
		`"/nix/var"`,
		`"/run/current-system"`,
		`(subpath "/Users/testuser/.nix-profile")`,
		`(subpath "/Users/testuser/.local/state/nix")`,
	}
	for _, p := range paths {
		if !strings.Contains(output, p) {
			t.Errorf("expected output to contain %q", p)
		}
	}
}

func TestNixToolchain(t *testing.T) {
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	m := guards.NixToolchain()
	output := renderTestRules(m.Rules(ctx))

	if m.Name() != "nix-toolchain" {
		t.Errorf("expected name %q, got %q", "nix-toolchain", m.Name())
	}

	paths := []string{
		`"/nix/store"`,
		`"/nix/var"`,
		`"/run/current-system"`,
		`(subpath "/Users/testuser/.nix-profile")`,
		`(subpath "/Users/testuser/.local/state/nix")`,
	}
	for _, p := range paths {
		if !strings.Contains(output, p) {
			t.Errorf("expected output to contain %q", p)
		}
	}
}

func TestGuard_GitIntegration_Metadata(t *testing.T) {
	g := guards.GitIntegrationGuard()

	if g.Name() != "git-integration" {
		t.Errorf("expected Name() = %q, got %q", "git-integration", g.Name())
	}
	if g.Type() != "always" {
		t.Errorf("expected Type() = %q, got %q", "always", g.Type())
	}
	if g.Description() == "" {
		t.Error("expected non-empty Description()")
	}
}

func TestGuard_GitIntegration_Paths(t *testing.T) {
	g := guards.GitIntegrationGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	output := renderTestRules(g.Rules(ctx))

	paths := []string{
		`(prefix "/Users/testuser/.gitconfig")`,
		`(prefix "/Users/testuser/.gitignore")`,
		`(subpath "/Users/testuser/.config/git")`,
		`(literal "/Users/testuser/.gitattributes")`,
		`(literal "/Users/testuser/.ssh")`,
	}
	for _, p := range paths {
		if !strings.Contains(output, p) {
			t.Errorf("expected output to contain %q", p)
		}
	}

	// Should be read-only - no file-write in output
	if strings.Contains(output, "file-write") {
		t.Error("expected git integration to be read-only (no file-write)")
	}
}

func TestGitIntegration(t *testing.T) {
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	m := guards.GitIntegration()
	output := renderTestRules(m.Rules(ctx))

	if m.Name() != "git-integration" {
		t.Errorf("expected name %q, got %q", "git-integration", m.Name())
	}

	paths := []string{
		`(prefix "/Users/testuser/.gitconfig")`,
		`(prefix "/Users/testuser/.gitignore")`,
		`(subpath "/Users/testuser/.config/git")`,
		`(literal "/Users/testuser/.gitattributes")`,
		`(literal "/Users/testuser/.ssh")`,
		`(literal "/Users/testuser/.ssh/config")`,
		`(literal "/Users/testuser/.ssh/known_hosts")`,
	}
	for _, p := range paths {
		if !strings.Contains(output, p) {
			t.Errorf("expected output to contain %q", p)
		}
	}

	// Should be read-only
	if strings.Contains(output, "file-write") {
		t.Error("expected git integration to be read-only (no file-write)")
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

func TestGuard_Keychain_AllowRules(t *testing.T) {
	g := guards.KeychainGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	output := renderTestRules(g.Rules(ctx))

	paths := []string{
		`(subpath "/Users/testuser/Library/Keychains")`,
		`(literal "/Users/testuser/Library/Preferences/com.apple.security.plist")`,
		`(literal "/Library/Preferences/com.apple.security.plist")`,
		`(literal "/Library/Keychains/System.keychain")`,
		"com.apple.SecurityServer",
		"com.apple.secd",
		"com.apple.trustd",
		"com.apple.AppleDatabaseChanged",
	}
	for _, p := range paths {
		if !strings.Contains(output, p) {
			t.Errorf("expected output to contain %q", p)
		}
	}
}

func TestKeychainIntegration(t *testing.T) {
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	m := guards.KeychainIntegration()
	output := renderTestRules(m.Rules(ctx))

	if m.Name() != "keychain" {
		t.Errorf("expected name %q, got %q", "keychain", m.Name())
	}

	// Home keychain paths
	paths := []string{
		`(subpath "/Users/testuser/Library/Keychains")`,
		`(literal "/Users/testuser/Library/Preferences/com.apple.security.plist")`,
	}
	for _, p := range paths {
		if !strings.Contains(output, p) {
			t.Errorf("expected output to contain %q", p)
		}
	}

	// System keychain paths
	sysPaths := []string{
		`(literal "/Library/Preferences/com.apple.security.plist")`,
		`(literal "/Library/Keychains/System.keychain")`,
		`(subpath "/private/var/db/mds")`,
	}
	for _, p := range sysPaths {
		if !strings.Contains(output, p) {
			t.Errorf("expected output to contain system path %q", p)
		}
	}

	// Mach services
	services := []string{
		"com.apple.SecurityServer",
		"com.apple.secd",
		"com.apple.trustd",
		"com.apple.security.agent",
		"com.apple.securityd.xpc",
		"com.apple.security.authhost",
	}
	for _, svc := range services {
		if !strings.Contains(output, svc) {
			t.Errorf("expected output to contain mach service %q", svc)
		}
	}

	// IPC
	if !strings.Contains(output, "com.apple.AppleDatabaseChanged") {
		t.Error("expected output to contain IPC name com.apple.AppleDatabaseChanged")
	}
}

func TestClaudeAgent(t *testing.T) {
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	m := modules.ClaudeAgent()
	output := renderTestRules(m.Rules(ctx))

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
