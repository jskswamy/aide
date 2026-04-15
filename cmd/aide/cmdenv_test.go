// cmd/aide/cmdenv_test.go
package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	"github.com/jskswamy/aide/internal/capability"
)

// helperCmd returns a barebones *cobra.Command for passing to cmdEnv.
func helperCmd() *cobra.Command {
	return &cobra.Command{Use: "test"}
}

func TestCmdEnv_Success_TempDirWithNoConfig(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	env, _ := cmdEnv(helperCmd())
	if env == nil {
		t.Fatal("cmdEnv returned nil *Env")
	}
	if env.Config() == nil {
		t.Errorf("Env.Config() is nil; contract says never nil")
	}
	// macOS /private/var vs /var symlink can cause a prefix mismatch;
	// resolve both sides before comparing.
	resolved, _ := filepath.EvalSymlinks(dir)
	if env.CWD() != dir && env.CWD() != resolved {
		t.Errorf("Env.CWD() = %q, want %q (or symlink-resolved %q)",
			env.CWD(), dir, resolved)
	}
}

func TestCmdEnv_Registry_LazyAndMemoized(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	env, _ := cmdEnv(helperCmd())
	if env == nil {
		t.Fatal("cmdEnv returned nil")
	}
	first := env.Registry()
	if first == nil {
		t.Fatal("Registry() returned nil; want non-nil merged registry")
	}
	// Functional assertion: built-ins are present.
	if _, ok := first["python"]; !ok {
		t.Errorf("Registry missing built-in 'python'")
	}
	// Memoization: second call returns equivalent content.
	second := env.Registry()
	if len(second) != len(first) {
		t.Errorf("Registry second call size differs: first=%d second=%d",
			len(first), len(second))
	}
}

func TestCmdEnv_Registry_IncludesUserCaps(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	// config.Dir() reads from $XDG_CONFIG_HOME/aide/, so point XDG
	// at a temp dir and write the global config there.
	xdgHome := filepath.Join(dir, "xdg")
	aideDir := filepath.Join(xdgHome, "aide")
	if err := os.MkdirAll(aideDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", xdgHome)

	configBody := `
capabilities:
  my-custom:
    description: "Custom user capability"
    writable:
      - "~/.custom"
`
	if err := os.WriteFile(filepath.Join(aideDir, "config.yaml"),
		[]byte(configBody), 0o600); err != nil {
		t.Fatal(err)
	}

	env, err := cmdEnv(helperCmd())
	if err != nil {
		t.Fatalf("cmdEnv: %v", err)
	}
	reg := env.Registry()
	if _, ok := reg["my-custom"]; !ok {
		t.Errorf("Registry missing user-defined 'my-custom'; got keys: %v", keysOf(reg))
	}
	if _, ok := reg["python"]; !ok {
		t.Errorf("Registry missing built-in 'python' (merge dropped built-ins?)")
	}
}

func keysOf(m map[string]capability.Capability) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
