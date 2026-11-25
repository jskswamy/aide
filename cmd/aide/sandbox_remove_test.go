package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readProfileFile(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "xdg", "aide", "profiles", "work.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSandboxAllowRemove_GlobalRemovesOneEntryAndLeavesAgentStore(t *testing.T) {
	dir := isolatedConfigDir(t)
	writeClaudeAgentConfig(t, dir, "claude")
	for _, args := range [][]string{
		{"allow", "/repo/a", "--global"},
		{"allow", "/repo/b", "--global"},
		{"allow", "/repo/w", "--write", "--global"},
	} {
		if out, err := runSandboxCmd(t, args...); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	out, err := runSandboxCmd(t, "allow", "--remove", "/repo/a", "--global")
	if err != nil {
		t.Fatalf("remove: %v\n%s", err, out)
	}
	prof := readProfileFile(t, dir)
	if strings.Contains(prof, "/repo/a") || !strings.Contains(prof, "/repo/b") || !strings.Contains(prof, "/repo/w") {
		t.Errorf("profile after removing /repo/a:\n%s", prof)
	}
	if got := readAdditionalDirs(t, filepath.Join(dir, ".claude-work", "settings.json")); !strings.Contains(strings.Join(got, ","), "/repo/a") {
		t.Errorf("agent store = %v, want /repo/a kept (remove changes config only)", got)
	}

	if out, err := runSandboxCmd(t, "allow", "--remove", "/repo/w", "--global"); err == nil {
		t.Errorf("removing a writable entry without --write: want an error, got\n%s", out)
	}
	if out, err := runSandboxCmd(t, "allow", "--remove", "--write", "/repo/w", "--global"); err != nil {
		t.Fatalf("remove --write: %v\n%s", err, out)
	}
	if strings.Contains(readProfileFile(t, dir), "/repo/w") {
		t.Error("/repo/w still present after remove --write")
	}
}

func TestSandboxAllowRemove_NamedSandboxIsNotConverted(t *testing.T) {
	dir := isolatedConfigDir(t)
	writeClaudeAgentConfig(t, dir, "claude")
	prof := filepath.Join(dir, "xdg", "aide", "profiles", "work.yaml")
	if err := os.WriteFile(prof, []byte("agent: claude\nsandbox: shared\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runSandboxCmd(t, "allow", "--remove", "/repo/a", "--global")
	if err == nil || !strings.Contains(out+err.Error(), "sandbox edit shared") {
		t.Fatalf("want an error pointing to aide sandbox edit shared, got err=%v\n%s", err, out)
	}
	if b, _ := os.ReadFile(prof); !strings.Contains(string(b), "sandbox: shared") {
		t.Errorf("profile changed:\n%s", b)
	}
}
