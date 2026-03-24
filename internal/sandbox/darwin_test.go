//go:build darwin

package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
	"github.com/jskswamy/aide/pkg/seatbelt/modules"
)

func TestGenerateSeatbeltProfile_DenyDefault(t *testing.T) {
	policy := Policy{
		Guards:  guards.DefaultGuardNames(),
		Network: NetworkNone,
	}
	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(profile, "(deny default)") {
		t.Error("profile should contain (deny default)")
	}
	if strings.Contains(profile, "(allow default)") {
		t.Error("profile should NOT contain (allow default)")
	}
}

func TestGenerateSeatbeltProfile_WritablePaths(t *testing.T) {
	dir := t.TempDir()
	policy := Policy{
		Guards:      guards.DefaultGuardNames(),
		ProjectRoot: dir,
		Network:     NetworkNone,
	}

	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// With deny-default, writable paths appear in (allow file-read* file-write* ...) blocks
	if !strings.Contains(profile, "(allow file-read* file-write*") {
		t.Error("profile should contain (allow file-read* file-write* for writable paths")
	}
	if !strings.Contains(profile, dir) {
		t.Errorf("profile should contain writable path %q", dir)
	}
}

func TestGenerateSeatbeltProfile_DeniedPaths(t *testing.T) {
	dir := t.TempDir()
	denied := filepath.Join(dir, "denied")
	if err := os.MkdirAll(denied, 0755); err != nil {
		t.Fatalf("failed to create denied dir: %v", err)
	}
	policy := Policy{
		Guards:      guards.DefaultGuardNames(),
		ExtraDenied: []string{denied},
		Network:     NetworkNone,
	}

	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Denied paths still use (deny file-read-data ...) and (deny file-write* ...)
	if !strings.Contains(profile, "(deny file-read-data") {
		t.Error("denied paths should use (deny file-read-data")
	}
	if !strings.Contains(profile, "(deny file-write*") {
		t.Error("denied paths should include (deny file-write* for defense-in-depth")
	}
}

func TestGenerateSeatbeltProfile_NetworkOutbound(t *testing.T) {
	policy := Policy{
		Guards:  guards.DefaultGuardNames(),
		Network: NetworkOutbound,
	}

	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// With deny-default, outbound mode should emit (allow network-outbound)
	if !strings.Contains(profile, "(allow network-outbound)") {
		t.Error("profile should contain (allow network-outbound) for outbound mode")
	}
}

func TestGenerateSeatbeltProfile_NetworkNone(t *testing.T) {
	policy := Policy{
		Guards:  guards.DefaultGuardNames(),
		Network: NetworkNone,
	}

	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// With deny-default, NetworkNone needs no rules (deny default covers it)
	if strings.Contains(profile, "(deny network*)") {
		t.Error("profile should NOT contain (deny network*) with deny-default (already denied)")
	}
}

func TestGenerateSeatbeltProfile_NetworkUnrestricted(t *testing.T) {
	policy := Policy{
		Guards:  guards.DefaultGuardNames(),
		Network: NetworkUnrestricted,
	}

	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// With deny-default, unrestricted should emit (allow network*)
	if !strings.Contains(profile, "(allow network*)") {
		t.Error("profile should contain (allow network*) for unrestricted mode")
	}
}

func TestGenerateSeatbeltProfile_SystemEssentials(t *testing.T) {
	policy := Policy{
		Guards:          guards.DefaultGuardNames(),
		Network:         NetworkNone,
		AllowSubprocess: true,
	}

	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// With deny-default, system essentials are individually listed by SystemRuntime
	essentials := []string{
		"(allow sysctl-read)",
		"(allow mach-lookup",
		"(allow pseudo-tty)",
		"(allow process-exec)",
		"(allow process-fork)",
	}
	for _, e := range essentials {
		if !strings.Contains(profile, e) {
			t.Errorf("profile should contain %q from SystemRuntime module", e)
		}
	}
}

func TestGenerateSeatbeltProfile_GlobExpansion(t *testing.T) {
	dir := t.TempDir()
	// Create test files matching a glob
	for _, name := range []string{"id_rsa", "id_ed25519"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("test"), 0600); err != nil {
			t.Fatal(err)
		}
	}

	policy := Policy{
		Guards:      guards.DefaultGuardNames(),
		ExtraDenied: []string{filepath.Join(dir, "id_*")},
		Network:     NetworkNone,
	}

	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(profile, filepath.Join(dir, "id_rsa")) {
		t.Error("profile should contain expanded glob match id_rsa")
	}
	if !strings.Contains(profile, filepath.Join(dir, "id_ed25519")) {
		t.Error("profile should contain expanded glob match id_ed25519")
	}
}

func TestDarwinSandbox_Apply_RewritesCmd(t *testing.T) {
	runtimeDir := t.TempDir()
	cmd := exec.Command("/usr/bin/echo", "hello", "world")
	policy := Policy{
		Guards:          guards.DefaultGuardNames(),
		Network:         NetworkNone,
		AllowSubprocess: true,
	}

	s := &darwinSandbox{}
	err := s.Apply(cmd, policy, runtimeDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cmd.Path != "/usr/bin/sandbox-exec" {
		t.Errorf("expected cmd.Path=/usr/bin/sandbox-exec, got %q", cmd.Path)
	}

	if len(cmd.Args) < 4 {
		t.Fatalf("expected at least 4 args, got %v", cmd.Args)
	}
	if cmd.Args[0] != "sandbox-exec" {
		t.Errorf("expected Args[0]=sandbox-exec, got %q", cmd.Args[0])
	}
	if cmd.Args[1] != "-f" {
		t.Errorf("expected Args[1]=-f, got %q", cmd.Args[1])
	}

	profilePath := cmd.Args[2]
	if !strings.HasPrefix(profilePath, runtimeDir) {
		t.Errorf("profile path should be in runtimeDir, got %q", profilePath)
	}

	// Verify original command is preserved
	if cmd.Args[3] != "/usr/bin/echo" {
		t.Errorf("expected original command as Args[3], got %q", cmd.Args[3])
	}
	if cmd.Args[4] != "hello" {
		t.Errorf("expected 'hello' as Args[4], got %q", cmd.Args[4])
	}

	// Verify profile file exists
	if _, err := os.Stat(profilePath); os.IsNotExist(err) {
		t.Error("profile file should exist in runtimeDir")
	}

	// Verify profile content uses deny-default
	content, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatalf("failed to read profile: %v", err)
	}
	if !strings.Contains(string(content), "(deny default)") {
		t.Error("profile file should contain (deny default)")
	}
}

func TestSeatbeltProfile_PortFiltering(t *testing.T) {
	policy := Policy{
		Guards:     guards.DefaultGuardNames(),
		Network:    NetworkOutbound,
		AllowPorts: []int{443, 53},
	}

	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// With AllowPorts, should deny all outbound then allow specific ports
	if !strings.Contains(profile, "(deny network-outbound)") {
		t.Error("profile should contain (deny network-outbound) when AllowPorts is set")
	}
	if !strings.Contains(profile, `(allow network-outbound (remote tcp "*:443"))`) {
		t.Error("profile should contain per-port TCP rule for 443")
	}
	if !strings.Contains(profile, `(allow network-outbound (remote tcp "*:53"))`) {
		t.Error("profile should contain per-port TCP rule for 53")
	}
}

func TestSeatbeltProfile_DenyPorts(t *testing.T) {
	policy := Policy{
		Guards:    guards.DefaultGuardNames(),
		Network:   NetworkOutbound,
		DenyPorts: []int{8080},
	}

	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(profile, `(deny network-outbound (remote tcp "*:8080"))`) {
		t.Error("profile should contain deny rule for port 8080")
	}
}

func TestSeatbeltProfile_NoPortFiltering(t *testing.T) {
	policy := Policy{
		Guards:  guards.DefaultGuardNames(),
		Network: NetworkOutbound,
	}

	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// With deny-default and no port restrictions, outbound mode should just
	// allow network-outbound (no deny needed)
	if !strings.Contains(profile, "(allow network-outbound)") {
		t.Error("profile should contain (allow network-outbound) for outbound mode without port restrictions")
	}
}

func TestSeatbeltProfile_PortFiltering_DNS(t *testing.T) {
	policy := Policy{
		Guards:     guards.DefaultGuardNames(),
		Network:    NetworkOutbound,
		AllowPorts: []int{53},
	}

	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(profile, `(allow network-outbound (remote tcp "*:53"))`) {
		t.Error("profile should contain TCP rule for DNS port 53")
	}
	if !strings.Contains(profile, `(allow network-outbound (remote udp "*:53"))`) {
		t.Error("profile should contain UDP rule for DNS port 53")
	}
}

func TestProfile_NoKeychainConflict(t *testing.T) {
	policy := DefaultPolicy("/tmp/proj", "/tmp/rt", "/tmp", nil)
	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No deny rules should target Library/Keychains
	lines := strings.Split(profile, "\n")
	for _, line := range lines {
		if strings.Contains(line, "deny") && strings.Contains(line, "Library/Keychains") {
			t.Errorf("profile should not deny Library/Keychains (managed by keychain guard): %s", line)
		}
	}
}

func TestProfile_SSHAllowBeatsSubpathDeny(t *testing.T) {
	policy := DefaultPolicy("/tmp/proj", "/tmp/rt", "/tmp", nil)
	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(profile, "(deny file-read-data") || !strings.Contains(profile, ".ssh") {
		t.Error("expected deny file-read-data rule covering .ssh directory")
	}
	if !strings.Contains(profile, "(allow file-read*") || !strings.Contains(profile, "known_hosts") {
		t.Error("expected allow file-read* rule for known_hosts")
	}
}

func TestProfile_NpmGuardOverridesToolchain(t *testing.T) {
	policy := DefaultPolicy("/tmp/proj", "/tmp/rt", "/tmp", nil)
	policy.Guards = append(policy.Guards, "npm")
	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(profile, ".npmrc") {
		t.Error("expected .npmrc in profile")
	}
}

func TestGenerateSeatbeltProfile_EmptyGuards_Error(t *testing.T) {
	policy := Policy{Guards: []string{}, Network: "none"}
	_, err := generateSeatbeltProfile(policy)
	if err == nil {
		t.Error("expected error for empty Guards list (no base guard)")
	}
}

func TestGenerateSeatbeltProfile_AlwaysGuardsOnly(t *testing.T) {
	// Only always-type guards, no default/opt-in
	var names []string
	for _, g := range guards.ByType("always") {
		names = append(names, g.Name())
	}
	policy := Policy{Guards: names, Network: "none"}
	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(profile, "(version 1)") {
		t.Error("always-guards-only should contain (version 1)")
	}
	if !strings.Contains(profile, "(deny default)") {
		t.Error("always-guards-only should contain (deny default)")
	}
}

func TestProfile_RoundTrip_UnguardSSHKeys(t *testing.T) {
	cfg := &config.SandboxPolicy{Unguard: []string{"ssh-keys"}}
	homeDir, _ := os.UserHomeDir()
	policy, _, err := PolicyFromConfig(cfg, "/tmp/proj", "/tmp/rt", homeDir, "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	profile, err := generateSeatbeltProfile(*policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The ssh-keys guard emits a multiline raw block:
	//   (deny file-read-data
	//       (subpath "<home>/.ssh")
	//   )
	// Check for the specific subpath deny rule targeting .ssh.
	sshDenySubpath := "(subpath \"" + homeDir + "/.ssh\")"
	sshDenyPresent := strings.Contains(profile, "(deny file-read-data\n") &&
		strings.Contains(profile, sshDenySubpath)
	if sshDenyPresent {
		t.Error("unguarding ssh-keys should remove .ssh deny from profile")
	}
}

func TestProfile_RoundTrip_GuardDocker(t *testing.T) {
	// Docker is now a default guard. It only emits rules when
	// ~/.docker/config.json exists. Verify it's in the guard list.
	homeDir, _ := os.UserHomeDir()
	dockerConfig := filepath.Join(homeDir, ".docker", "config.json")

	policy := DefaultPolicy("/tmp/proj", "/tmp/rt", "/tmp", nil)
	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, statErr := os.Stat(dockerConfig); statErr == nil {
		if !strings.Contains(profile, ".docker") {
			t.Error("docker guard (now default) should add .docker deny when config exists")
		}
	} else {
		// Docker config doesn't exist — guard skips, no rules emitted
		t.Logf("~/.docker/config.json not found, docker guard skipped (expected)")
	}
}

// ruleBlockType returns "allow" or "deny" for a top-level seatbelt rule line,
// or "" if the line is not a top-level rule opener. Top-level rules start at
// column 0 with "(allow" or "(deny".
func ruleBlockType(line string) string {
	if strings.HasPrefix(line, "(allow") {
		return "allow"
	}
	if strings.HasPrefix(line, "(deny") {
		return "deny"
	}
	return ""
}

// scanBlockContext walks profile lines and tracks block type (allow/deny).
// It calls fn(lineIndex, line, blockType, blockStartLine) for every line.
// blockType is the current top-level block type ("allow", "deny", or "").
// blockStartLine is the line index where the current block opened.
func scanBlockContext(lines []string, fn func(i int, line, blockType string, blockStart int)) {
	blockType := ""
	blockStart := 0
	for i, line := range lines {
		bt := ruleBlockType(line)
		if bt != "" {
			blockType = bt
			blockStart = i
			// Single-line top-level rule: "(deny ..." ending with ")" on same line
			if strings.HasSuffix(strings.TrimRight(line, " \t"), ")") {
				fn(i, line, blockType, blockStart)
				blockType = ""
				continue
			}
		} else if line == ")" {
			blockType = ""
		}
		fn(i, line, blockType, blockStart)
	}
}

func TestProfile_IntentOrdering(t *testing.T) {
	policy := DefaultPolicy("/tmp/proj", "/tmp/rt", "/tmp", nil)
	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Two-tier ordering: Allow(100) rules appear before Deny(200) rules.
	// "(deny default)" is a DenyOp which has Allow intent (infrastructure),
	// so it appears in the Allow section. Credential deny rules (file-read-data
	// denies) have Deny intent and appear later.
	denyDefaultPos := strings.Index(profile, "(deny default)")
	if denyDefaultPos == -1 {
		t.Fatal("expected (deny default) in profile")
	}

	// Find last Allow-intent rule (allow file-read* blocks)
	lastAllow := strings.LastIndex(profile, `(allow file-read*`)
	if lastAllow == -1 {
		t.Fatal("expected at least one (allow file-read*) rule in profile")
	}

	// Find first Deny-intent rule (deny file-read-data from credential guards)
	denyFileRead := strings.Index(profile, `(deny file-read-data`)
	if denyFileRead != -1 {
		// If credential deny rules exist, they should appear after allow rules
		if denyFileRead < lastAllow {
			// This is fine — deny-wins means position doesn't matter,
			// but the profile sorts Allow(100) before Deny(200)
			t.Error("Deny-intent rules (deny file-read-data) should appear after Allow-intent rules")
		}
	}

	// (deny default) should appear before allow rules (it's in the Allow section)
	if denyDefaultPos > lastAllow {
		t.Error("(deny default) should appear before allow file-read rules")
	}
}

func TestProfile_SSHKnownHostsSurvives(t *testing.T) {
	// The SSH keys guard now uses per-file discovery: it scans ~/.ssh,
	// allows safe files (known_hosts, config, *.pub) and denies everything
	// else individually. It skips entirely if ~/.ssh doesn't exist.
	// Comprehensive tests live in guards/guard_ssh_keys_test.go.
	homeDir, _ := os.UserHomeDir()
	sshDir := filepath.Join(homeDir, ".ssh")

	policy := DefaultPolicy("/tmp/proj", "/tmp/rt", "/tmp", nil)
	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, statErr := os.Stat(sshDir); os.IsNotExist(statErr) {
		// If ~/.ssh doesn't exist, the guard skips entirely
		t.Skip("~/.ssh does not exist, SSH guard skips; see guards/guard_ssh_keys_test.go")
		return
	}

	// SSH guard should only have per-file deny rules, not subpath deny.
	// The filesystem guard provides a subpath allow for ~/.ssh (for reads)
	// which is expected. Only check for subpath *deny* targeting .ssh.
	lines := strings.Split(profile, "\n")
	scanBlockContext(lines, func(_ int, line, blockType string, _ int) {
		if strings.Contains(line, "subpath") && strings.Contains(line, ".ssh") && blockType == "deny" {
			t.Errorf("SSH guard should not use subpath deny, found: %s", strings.TrimSpace(line))
		}
	})

	// ~/.ssh reads are now covered by the filesystem guard's scoped home
	// reads (subpath allow for ~/.ssh). known_hosts no longer needs
	// individual allow rules from the SSH guard.
}

func TestProfile_NpmOptInOverridesNodeToolchain(t *testing.T) {
	policy := DefaultPolicy("/tmp/proj", "/tmp/rt", "/tmp", nil)
	policy.Guards = append(policy.Guards, "npm")
	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The node-toolchain guard always emits an allow rule for .npmrc.
	// The npm guard only emits a deny rule if ~/.npmrc actually exists on disk.
	// With deny-wins semantics, the deny rule (if present) overrides the allow
	// regardless of position.

	// Node-toolchain allow for .npmrc should always be present
	npmAllowBlock := -1
	lines := strings.Split(profile, "\n")
	scanBlockContext(lines, func(_ int, line, blockType string, blockStart int) {
		if strings.Contains(line, `.npmrc"`) && blockType == "allow" {
			npmAllowBlock = blockStart
		}
	})

	if npmAllowBlock == -1 {
		t.Fatal("expected node-toolchain .npmrc allow in profile")
	}

	// If ~/.npmrc exists, the npm guard should emit a deny rule for it
	homeDir, _ := os.UserHomeDir()
	npmrc := filepath.Join(homeDir, ".npmrc")
	if _, statErr := os.Stat(npmrc); statErr == nil {
		npmDenyBlock := -1
		scanBlockContext(lines, func(_ int, line, blockType string, blockStart int) {
			if strings.Contains(line, `.npmrc"`) && blockType == "deny" {
				npmDenyBlock = blockStart
			}
		})
		if npmDenyBlock == -1 {
			t.Fatal("expected npm guard .npmrc deny in profile when ~/.npmrc exists")
		}
	}
}

func TestProfile_GPGPublicKeyringNotDenied(t *testing.T) {
	policy := DefaultPolicy("/tmp/proj", "/tmp/rt", "/tmp", nil)
	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The password-managers guard only denies GPG private key paths that
	// actually exist on disk. Verify that IF the paths exist, they appear
	// in the profile; and that the entire .gnupg directory is never denied.
	homeDir, _ := os.UserHomeDir()
	gpgPrivDir := filepath.Join(homeDir, ".gnupg", "private-keys-v1.d")
	gpgSecring := filepath.Join(homeDir, ".gnupg", "secring.gpg")

	if _, statErr := os.Stat(gpgPrivDir); statErr == nil {
		if !strings.Contains(profile, "private-keys-v1.d") {
			t.Error("expected deny for .gnupg/private-keys-v1.d when it exists")
		}
	}
	if _, statErr := os.Stat(gpgSecring); statErr == nil {
		if !strings.Contains(profile, "secring.gpg") {
			t.Error("expected deny for .gnupg/secring.gpg when it exists")
		}
	}

	// Should NOT deny the entire .gnupg directory regardless
	lines := strings.Split(profile, "\n")
	for _, line := range lines {
		if strings.Contains(line, "deny") && strings.Contains(line, `.gnupg"`) && !strings.Contains(line, "private-keys") && !strings.Contains(line, "secring") {
			t.Errorf("should not deny entire .gnupg directory, found: %s", strings.TrimSpace(line))
		}
	}
}

func TestProfile_KeychainNotDenied(t *testing.T) {
	policy := DefaultPolicy("/tmp/proj", "/tmp/rt", "/tmp", nil)
	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(profile, "\n")
	for _, line := range lines {
		if strings.Contains(line, "deny") && strings.Contains(line, "Library/Keychains") {
			t.Errorf("no guard should deny Library/Keychains, found: %s", strings.TrimSpace(line))
		}
	}
}

func TestProfile_ClaudeAgentAllowsSurvive(t *testing.T) {
	policy := DefaultPolicy("/tmp/proj", "/tmp/rt", "/tmp", nil)
	// Set agent module to ClaudeAgent
	policy.AgentModule = modules.ClaudeAgent()
	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// With deny-wins semantics, allow rules appear in the Allow(100) section
	// and deny rules in the Deny(200) section. Position does not matter for
	// override behavior — deny always wins over allow for the same path.
	// This test verifies that Claude config paths are present in the profile
	// as allow rules (they should survive the deny-wins architecture).
	claudeAllowBlock := -1

	lines := strings.Split(profile, "\n")
	scanBlockContext(lines, func(_ int, line, blockType string, blockStart int) {
		if strings.Contains(line, ".claude") && blockType == "allow" {
			claudeAllowBlock = blockStart
		}
	})

	if claudeAllowBlock == -1 {
		t.Fatal("expected Claude allow rule in profile")
	}

	// Verify Claude-specific paths are present
	if !strings.Contains(profile, ".claude") {
		t.Error("profile should contain .claude config path")
	}
	if !strings.Contains(profile, ".cache/claude") {
		t.Error("profile should contain .cache/claude runtime path")
	}
}

func TestSeatbeltProfile_CustomClaudeConfigDir(t *testing.T) {
	env := []string{"CLAUDE_CONFIG_DIR=/Users/testuser/.claude-work"}
	policy := DefaultPolicy("/tmp/proj", "/tmp/rt", "/tmp", env)
	policy.AgentModule = modules.ClaudeAgent()

	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The custom config dir should appear as a subpath rule
	if !strings.Contains(profile, `(subpath "/Users/testuser/.claude-work")`) {
		t.Error("profile should contain custom CLAUDE_CONFIG_DIR as subpath rule")
	}

	// Runtime paths should still be present regardless of CLAUDE_CONFIG_DIR
	if !strings.Contains(profile, ".cache/claude") {
		t.Error("profile should still contain .cache/claude runtime path")
	}
}

func TestDarwinSandbox_Apply_CleanEnv(t *testing.T) {
	runtimeDir := t.TempDir()
	cmd := exec.Command("/usr/bin/echo", "hello")
	cmd.Env = []string{
		"PATH=/usr/bin",
		"HOME=/home/user",
		"SECRET_KEY=abc123",
		"AWS_SECRET=xyz",
		"TERM=xterm",
	}
	policy := Policy{
		Guards:   guards.DefaultGuardNames(),
		Network:  NetworkNone,
		CleanEnv: true,
	}

	s := &darwinSandbox{}
	err := s.Apply(cmd, policy, runtimeDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should keep essential vars but not secrets
	envMap := make(map[string]string)
	for _, e := range cmd.Env {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}

	if _, ok := envMap["PATH"]; !ok {
		t.Error("PATH should be preserved")
	}
	if _, ok := envMap["HOME"]; !ok {
		t.Error("HOME should be preserved")
	}
	if _, ok := envMap["TERM"]; !ok {
		t.Error("TERM should be preserved")
	}
	if _, ok := envMap["SECRET_KEY"]; ok {
		t.Error("SECRET_KEY should be filtered out")
	}
	if _, ok := envMap["AWS_SECRET"]; ok {
		t.Error("AWS_SECRET should be filtered out")
	}
}

func TestGenerateSeatbeltProfile_BroadSystemReads(t *testing.T) {
	policy := Policy{
		Guards:          guards.DefaultGuardNames(),
		Network:         NetworkNone,
		AllowSubprocess: true,
	}
	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The system-runtime guard now uses broad top-level subpaths.
	broadPaths := []string{
		`(subpath "/System")`,
		`(subpath "/Library")`,
		`(subpath "/nix")`,
		`(subpath "/Applications")`,
		`(subpath "/usr")`,
		`(subpath "/bin")`,
		`(subpath "/private")`,
		`(subpath "/dev")`,
		`(subpath "/tmp")`,
		`(subpath "/var")`,
	}
	for _, p := range broadPaths {
		if !strings.Contains(profile, p) {
			t.Errorf("profile should contain broad system read %s", p)
		}
	}

	// Old granular paths should NOT appear (they were replaced by broad reads).
	oldGranular := []string{
		`(subpath "/System/Library")`,
		`(subpath "/Library/Apple")`,
		`(subpath "/Library/Frameworks")`,
	}
	for _, p := range oldGranular {
		if strings.Contains(profile, p) {
			t.Errorf("profile should NOT contain old granular path %s (replaced by broad reads)", p)
		}
	}
}

func TestGenerateSeatbeltProfile_ScopedHomeReads(t *testing.T) {
	policy := DefaultPolicy("/tmp/proj", "/tmp/rt", "/tmp", nil)
	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Scoped home reads should include specific dev directories
	scopedDirs := []string{".config", ".cache", ".ssh", ".cargo", ".rustup", ".local"}
	for _, d := range scopedDirs {
		if !strings.Contains(profile, d) {
			t.Errorf("profile should contain scoped home path %q", d)
		}
	}

	// Should NOT contain a bare (subpath "$HOME") allow — only specific subdirectories.
	homeDir, _ := os.UserHomeDir()
	bareHomeSubpath := `(subpath "` + homeDir + `")`
	lines := strings.Split(profile, "\n")
	scanBlockContext(lines, func(_ int, line, blockType string, _ int) {
		if strings.Contains(line, bareHomeSubpath) && blockType == "allow" {
			// Check it's not inside a more specific context (like file-write for project)
			// A bare subpath allow for $HOME would be too broad
			if !strings.Contains(line, "file-write") {
				t.Errorf("profile should NOT contain bare home subpath allow %s in read rules", bareHomeSubpath)
			}
		}
	})
}

func TestProfile_NewDefaultGuards(t *testing.T) {
	policy := DefaultPolicy("/tmp/proj", "/tmp/rt", "/tmp", nil)
	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// New default guards: mounted-volumes, shell-history, dev-credentials, project-secrets.
	// These guards check for file existence, so some may produce no rules.
	// Verify the profile renders without error (done above) and contains
	// at least some deny rules from the credential/history guards.
	hasDenyFileRead := strings.Contains(profile, "(deny file-read-data")
	hasDenyFileWrite := strings.Contains(profile, "(deny file-write*")

	if !hasDenyFileRead && !hasDenyFileWrite {
		t.Error("default profile should contain at least some deny rules from new default guards (shell-history, dev-credentials, etc.)")
	}

	// Verify the guard names are in the default list
	defaultNames := guards.DefaultGuardNames()
	newGuards := []string{"mounted-volumes", "shell-history", "dev-credentials", "project-secrets"}
	nameSet := make(map[string]bool, len(defaultNames))
	for _, n := range defaultNames {
		nameSet[n] = true
	}
	for _, g := range newGuards {
		if !nameSet[g] {
			t.Errorf("expected %q in DefaultGuardNames()", g)
		}
	}
}

func TestProfile_PromotedGuardsInDefault(t *testing.T) {
	defaultNames := guards.DefaultGuardNames()
	nameSet := make(map[string]bool, len(defaultNames))
	for _, n := range defaultNames {
		nameSet[n] = true
	}

	// These guards were promoted from opt-in to default.
	promoted := []string{"docker", "github-cli", "npm", "netrc", "kubernetes"}
	for _, g := range promoted {
		if !nameSet[g] {
			t.Errorf("expected promoted guard %q in DefaultGuardNames()", g)
		}
	}

	// git-integration was removed — it should NOT be in any guard list.
	if nameSet["git-integration"] {
		t.Error("git-integration guard was removed and should not be in DefaultGuardNames()")
	}
}
