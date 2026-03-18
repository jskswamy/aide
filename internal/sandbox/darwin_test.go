//go:build darwin

package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateSeatbeltProfile_DenyDefault(t *testing.T) {
	policy := Policy{
		Network:         NetworkNone,
		AllowSubprocess: false,
	}

	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(profile, "(version 1)") {
		t.Error("profile should contain (version 1)")
	}
	if !strings.Contains(profile, "(deny default)") {
		t.Error("profile should contain (deny default)")
	}
}

func TestGenerateSeatbeltProfile_WritablePaths(t *testing.T) {
	dir := t.TempDir()
	policy := Policy{
		Writable:        []string{dir},
		Network:         NetworkNone,
		AllowSubprocess: false,
	}

	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Writable paths should appear in an allow file-read* file-write* block
	if !strings.Contains(profile, "(allow file-read*") {
		t.Error("profile should contain (allow file-read* for writable paths")
	}
	if !strings.Contains(profile, "(allow file-write*") {
		t.Error("profile should contain (allow file-write* for writable paths")
	}
	if !strings.Contains(profile, dir) {
		t.Errorf("profile should contain writable path %q", dir)
	}
}

func TestGenerateSeatbeltProfile_ReadablePaths(t *testing.T) {
	dir := t.TempDir()
	policy := Policy{
		Readable:        []string{dir},
		Network:         NetworkNone,
		AllowSubprocess: false,
	}

	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(profile, "(allow file-read*") {
		t.Error("profile should contain (allow file-read* for readable paths")
	}
	if !strings.Contains(profile, dir) {
		t.Errorf("profile should contain readable path %q", dir)
	}
}

func TestGenerateSeatbeltProfile_DeniedPaths(t *testing.T) {
	dir := t.TempDir()
	policy := Policy{
		Denied:          []string{dir},
		Network:         NetworkNone,
		AllowSubprocess: false,
	}

	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(profile, "(deny file-read*") {
		t.Error("profile should contain (deny file-read* for denied paths")
	}
	if !strings.Contains(profile, "(deny file-write*") {
		t.Error("profile should contain (deny file-write* for denied paths")
	}
}

func TestGenerateSeatbeltProfile_DeniedBeforeAllows(t *testing.T) {
	dir := t.TempDir()
	denied := filepath.Join(dir, "denied")
	writable := filepath.Join(dir, "writable")
	os.MkdirAll(denied, 0755)
	os.MkdirAll(writable, 0755)

	policy := Policy{
		Writable:        []string{writable},
		Denied:          []string{denied},
		Network:         NetworkNone,
		AllowSubprocess: false,
	}

	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	denyIdx := strings.Index(profile, "(deny file-read*")
	allowIdx := strings.Index(profile, "(allow file-read*")
	// The first deny file rule should appear before the first allow file rule
	// (skipping system essentials which also use allow file-read*)
	// We check that deny appears in the profile before the writable allow block
	if denyIdx < 0 {
		t.Fatal("profile should contain deny file rules")
	}
	if allowIdx < 0 {
		t.Fatal("profile should contain allow file rules")
	}
	if denyIdx > allowIdx {
		t.Error("denied paths should appear before allow paths in profile for precedence")
	}
}

func TestGenerateSeatbeltProfile_NetworkOutbound(t *testing.T) {
	policy := Policy{
		Network:         NetworkOutbound,
		AllowSubprocess: false,
	}

	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(profile, "(allow network-outbound)") {
		t.Error("profile should contain (allow network-outbound)")
	}
	if strings.Contains(profile, "(allow network-inbound)") {
		t.Error("profile should NOT contain (allow network-inbound) for outbound mode")
	}
}

func TestGenerateSeatbeltProfile_NetworkNone(t *testing.T) {
	policy := Policy{
		Network:         NetworkNone,
		AllowSubprocess: false,
	}

	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(profile, "(allow network-outbound)") {
		t.Error("profile should NOT contain (allow network-outbound) for none mode")
	}
	if strings.Contains(profile, "(allow network-inbound)") {
		t.Error("profile should NOT contain (allow network-inbound) for none mode")
	}
	if strings.Contains(profile, "(allow network*)") {
		t.Error("profile should NOT contain (allow network*) for none mode")
	}
}

func TestGenerateSeatbeltProfile_NetworkUnrestricted(t *testing.T) {
	policy := Policy{
		Network:         NetworkUnrestricted,
		AllowSubprocess: false,
	}

	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(profile, "(allow network*)") {
		t.Error("profile should contain (allow network*) for unrestricted mode")
	}
}

func TestGenerateSeatbeltProfile_NoSubprocess(t *testing.T) {
	policy := Policy{
		Network:         NetworkNone,
		AllowSubprocess: false,
	}

	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(profile, "(allow process-fork)") {
		t.Error("profile should NOT contain (allow process-fork) when AllowSubprocess=false")
	}
}

func TestGenerateSeatbeltProfile_WithSubprocess(t *testing.T) {
	policy := Policy{
		Network:         NetworkNone,
		AllowSubprocess: true,
	}

	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(profile, "(allow process-exec)") {
		t.Error("profile should contain (allow process-exec)")
	}
	if !strings.Contains(profile, "(allow process-fork)") {
		t.Error("profile should contain (allow process-fork) when AllowSubprocess=true")
	}
}

func TestGenerateSeatbeltProfile_SystemEssentials(t *testing.T) {
	policy := Policy{
		Network:         NetworkNone,
		AllowSubprocess: false,
	}

	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	essentials := []string{
		"/usr/lib",
		"/System/Library",
		"/dev/null",
		"/dev/urandom",
		"(allow sysctl-read)",
		"(allow mach-lookup)",
	}
	for _, e := range essentials {
		if !strings.Contains(profile, e) {
			t.Errorf("profile should contain system essential %q", e)
		}
	}
}

func TestGenerateSeatbeltProfile_GlobExpansion(t *testing.T) {
	dir := t.TempDir()
	// Create test files matching a glob
	for _, name := range []string{"id_rsa", "id_ed25519"} {
		os.WriteFile(filepath.Join(dir, name), []byte("test"), 0600)
	}

	policy := Policy{
		Denied:          []string{filepath.Join(dir, "id_*")},
		Network:         NetworkNone,
		AllowSubprocess: false,
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

	// Verify profile content
	content, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatalf("failed to read profile: %v", err)
	}
	if !strings.Contains(string(content), "(deny default)") {
		t.Error("profile file should contain (deny default)")
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
		Network:         NetworkNone,
		AllowSubprocess: false,
		CleanEnv:        true,
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
