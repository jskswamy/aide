package launcher

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/trust"
)

func TestFilterEssentialEnv(t *testing.T) {
	env := []string{
		"PATH=/usr/bin",
		"HOME=/home/user",
		"ANTHROPIC_API_KEY=sk-ant-123",
		"SHELL=/bin/bash",
		"RANDOM_VAR=value",
		"TERM=xterm",
	}
	got := filterEssentialEnv(env)
	want := map[string]bool{
		"PATH=/usr/bin":   true,
		"HOME=/home/user": true,
		"SHELL=/bin/bash": true,
		"TERM=xterm":      true,
	}
	if len(got) != len(want) {
		t.Errorf("filterEssentialEnv() returned %d entries, want %d", len(got), len(want))
	}
	for _, e := range got {
		if !want[e] {
			t.Errorf("unexpected entry: %s", e)
		}
	}
}

func TestFilterEssentialEnv_Empty(t *testing.T) {
	got := filterEssentialEnv(nil)
	if len(got) != 0 {
		t.Errorf("filterEssentialEnv(nil) = %v, want empty", got)
	}
}

func TestMergeEnv(t *testing.T) {
	base := []string{"PATH=/usr/bin", "HOME=/home/user", "EXISTING=old"}
	resolved := map[string]string{"EXISTING": "new", "ADDED": "value"}

	got := mergeEnv(base, resolved)

	foundExisting := false
	foundAdded := false
	for _, e := range got {
		if e == "EXISTING=new" {
			foundExisting = true
		}
		if e == "EXISTING=old" {
			t.Error("old EXISTING value should be replaced")
		}
		if e == "ADDED=value" {
			foundAdded = true
		}
	}
	if !foundExisting {
		t.Error("EXISTING=new not found")
	}
	if !foundAdded {
		t.Error("ADDED=value not found")
	}
}

func TestMergeEnv_EmptyInputs(t *testing.T) {
	got := mergeEnv(nil, nil)
	if len(got) != 0 {
		t.Errorf("mergeEnv(nil, nil) = %v, want empty", got)
	}
}

func TestStringSetDiff(t *testing.T) {
	tests := []struct {
		name string
		a    []string
		b    []string
		want int
	}{
		{"disjoint", []string{"x", "y"}, []string{"a", "b"}, 2},
		{"overlap", []string{"x", "y", "z"}, []string{"y"}, 2},
		{"subset", []string{"x", "y"}, []string{"x", "y"}, 0},
		{"empty a", nil, []string{"x"}, 0},
		{"empty b", []string{"x"}, nil, 1},
		{"both empty", nil, nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stringSetDiff(tt.a, tt.b)
			if len(got) != tt.want {
				t.Errorf("stringSetDiff() = %v (len %d), want len %d", got, len(got), tt.want)
			}
		})
	}
}

func TestWrapTemplateError(t *testing.T) {
	t.Run("missing key with secret", func(t *testing.T) {
		err := fmt.Errorf("map has no entry for key \"missing\"")
		got := wrapTemplateError(err, "work", "work-secrets")
		if !strings.Contains(got.Error(), "secret key not found") {
			t.Errorf("wrapTemplateError() = %q, want 'secret key not found'", got)
		}
	})

	t.Run("missing key without secret", func(t *testing.T) {
		err := fmt.Errorf("map has no entry for key \"missing\"")
		got := wrapTemplateError(err, "work", "")
		if !strings.Contains(got.Error(), "no secret configured") {
			t.Errorf("wrapTemplateError() = %q, want 'no secret configured'", got)
		}
	})

	t.Run("nil pointer", func(t *testing.T) {
		err := fmt.Errorf("nil pointer evaluating")
		got := wrapTemplateError(err, "work", "work-secrets")
		if got == nil {
			t.Error("wrapTemplateError() should return non-nil error")
		}
	})
}

func TestApplyTrustGate(t *testing.T) {
	tmpDir := t.TempDir()
	aidePath := filepath.Join(tmpDir, ".aide.yaml")
	if err := os.WriteFile(aidePath, []byte("agent: claude\n"), 0o600); err != nil {
		t.Fatalf("writing temp .aide.yaml: %v", err)
	}
	absPath, _ := filepath.Abs(aidePath)

	t.Run("trusted", func(t *testing.T) {
		store := trust.NewStore(filepath.Join(t.TempDir(), "trust"))
		content, _ := os.ReadFile(aidePath)
		if err := store.Trust(absPath, content); err != nil {
			t.Fatalf("store.Trust: %v", err)
		}

		po := &config.ProjectOverride{Agent: "claude"}
		cfg := &config.Config{
			ProjectConfigPath: aidePath,
			ProjectOverride:   po,
		}
		l := &Launcher{TrustStore: store}
		trustInfo := l.applyTrustGate(cfg)

		if cfg.ProjectOverride == nil {
			t.Error("trusted file should keep ProjectOverride")
		}
		if trustInfo != nil {
			t.Error("trusted file should return nil TrustInfo")
		}
	})

	t.Run("denied", func(t *testing.T) {
		store := trust.NewStore(filepath.Join(t.TempDir(), "trust"))
		if err := store.Deny(absPath); err != nil {
			t.Fatalf("store.Deny: %v", err)
		}

		po := &config.ProjectOverride{Agent: "claude"}
		cfg := &config.Config{
			ProjectConfigPath: aidePath,
			ProjectOverride:   po,
		}
		l := &Launcher{TrustStore: store}
		trustInfo := l.applyTrustGate(cfg)

		if cfg.ProjectOverride != nil {
			t.Error("denied file should nil out ProjectOverride")
		}
		if trustInfo == nil {
			t.Error("denied file should return TrustInfo")
		}
		if trustInfo != nil && trustInfo.Status != "denied" {
			t.Errorf("expected status 'denied', got: %q", trustInfo.Status)
		}
	})

	t.Run("untrusted", func(t *testing.T) {
		store := trust.NewStore(filepath.Join(t.TempDir(), "trust"))
		// Don't trust or deny — file is untrusted by default

		po := &config.ProjectOverride{Agent: "claude"}
		cfg := &config.Config{
			ProjectConfigPath: aidePath,
			ProjectOverride:   po,
		}
		l := &Launcher{TrustStore: store}
		trustInfo := l.applyTrustGate(cfg)

		if cfg.ProjectOverride != nil {
			t.Error("untrusted file should nil out ProjectOverride")
		}
		if trustInfo == nil {
			t.Error("untrusted file should return TrustInfo")
		}
		if trustInfo != nil && trustInfo.Status != "untrusted" {
			t.Errorf("expected status 'untrusted', got: %q", trustInfo.Status)
		}
		if trustInfo != nil && trustInfo.Wants.Agent != "claude" {
			t.Errorf("expected agent 'claude' in TrustInfo, got: %q", trustInfo.Wants.Agent)
		}
	})
}

func TestBuildTrustInfo(t *testing.T) {
	homeDir := "/home/user"

	t.Run("with agent only", func(t *testing.T) {
		po := &config.ProjectOverride{Agent: "claude"}
		info := buildTrustInfo("/path/to/.aide.yaml", homeDir, po)

		if info == nil {
			t.Fatalf("expected TrustInfo, got nil")
		}
		if info.Status != "untrusted" {
			t.Errorf("expected status 'untrusted', got %q", info.Status)
		}
		if info.Wants.Agent != "claude" {
			t.Errorf("expected agent 'claude', got %q", info.Wants.Agent)
		}
	})

	t.Run("with capabilities and env", func(t *testing.T) {
		po := &config.ProjectOverride{
			Agent:        "claude",
			Capabilities: []string{"network", "filesystem"},
			Env:          map[string]string{"FOO": "bar", "BAZ": "qux"},
		}
		info := buildTrustInfo("/tmp/.aide.yaml", homeDir, po)

		if info == nil {
			t.Fatalf("expected TrustInfo, got nil")
		}
		if info.Status != "untrusted" {
			t.Errorf("expected status 'untrusted', got %q", info.Status)
		}
		if len(info.Wants.Capabilities) != 2 {
			t.Errorf("expected 2 capabilities, got %d", len(info.Wants.Capabilities))
		}
		if len(info.Wants.EnvVars) != 2 {
			t.Errorf("expected 2 env vars, got %d: %v", len(info.Wants.EnvVars), info.Wants.EnvVars)
		}
		// Check that env vars are sorted
		if info.Wants.EnvVars[0] != "BAZ" || info.Wants.EnvVars[1] != "FOO" {
			t.Errorf("expected env vars sorted [BAZ, FOO], got %v", info.Wants.EnvVars)
		}
	})

	t.Run("with sandbox overrides", func(t *testing.T) {
		po := &config.ProjectOverride{
			Agent: "claude",
			Sandbox: &config.SandboxPolicy{
				WritableExtra: []string{"/tmp", "/var/tmp"},
			},
		}
		info := buildTrustInfo("/path/.aide.yaml", homeDir, po)

		if info == nil {
			t.Fatalf("expected TrustInfo, got nil")
		}
		if len(info.Wants.Writable) != 2 {
			t.Errorf("expected 2 writable paths, got %d", len(info.Wants.Writable))
		}
	})

	t.Run("with nil ProjectOverride", func(t *testing.T) {
		info := buildTrustInfo("/path/.aide.yaml", homeDir, nil)

		if info == nil {
			t.Fatalf("expected TrustInfo, got nil")
		}
		if info.Status != "untrusted" {
			t.Errorf("expected status 'untrusted', got %q", info.Status)
		}
		if info.Wants.Agent != "" {
			t.Errorf("expected empty agent, got %q", info.Wants.Agent)
		}
	})
}

