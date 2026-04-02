package launcher

import (
	"bytes"
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

func TestRedactValue(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"short", "short***"},
		{"12345678", "12345678***"},
		{"123456789", "12345678***"},
		{"sk-ant-api03-very-long-key", "sk-ant-a***"},
		{"", "***"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := redactValue(tt.input)
			if got != tt.want {
				t.Errorf("redactValue(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
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

func TestNetworkDisplayName(t *testing.T) {
	tests := []struct {
		mode string
		want string
	}{
		{"outbound", "outbound only"},
		{"none", "none"},
		{"unrestricted", "unrestricted"},
		{"", ""},          // empty string passes through as-is (default case)
		{"custom", "custom"}, // unknown mode passes through
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			got := networkDisplayName(tt.mode)
			if got != tt.want {
				t.Errorf("networkDisplayName(%q) = %q, want %q", tt.mode, got, tt.want)
			}
		})
	}
}

func TestClassifyEnvSource(t *testing.T) {
	tests := []struct {
		name      string
		val       string
		wantSrc   string
		wantKey   string
	}{
		{
			name:    "dot secrets pattern",
			val:     "{{ .secrets.MY_KEY }}",
			wantSrc: "secrets.MY_KEY",
			wantKey: "MY_KEY",
		},
		{
			name:    "index secrets pattern",
			val:     `{{ index .secrets "db_pass" }}`,
			wantSrc: "secrets.db_pass",
			wantKey: "db_pass",
		},
		{
			name:    "secrets without extractable key",
			val:     "{{ index .secrets }}",
			wantSrc: "secret",
			wantKey: "",
		},
		{
			name:    "project_root template",
			val:     "{{ .project_root }}/bin",
			wantSrc: "project_root",
			wantKey: "",
		},
		{
			name:    "runtime_dir template",
			val:     "{{ .runtime_dir }}",
			wantSrc: "runtime_dir",
			wantKey: "",
		},
		{
			name:    "generic template",
			val:     "{{ .something_else }}",
			wantSrc: "template",
			wantKey: "",
		},
		{
			name:    "literal value",
			val:     "just-a-plain-value",
			wantSrc: "literal",
			wantKey: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSrc, gotKey := classifyEnvSource(tt.val)
			if gotSrc != tt.wantSrc {
				t.Errorf("classifyEnvSource(%q) source = %q, want %q", tt.val, gotSrc, tt.wantSrc)
			}
			if gotKey != tt.wantKey {
				t.Errorf("classifyEnvSource(%q) key = %q, want %q", tt.val, gotKey, tt.wantKey)
			}
		})
	}
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
		l.applyTrustGate(cfg)

		if cfg.ProjectOverride == nil {
			t.Error("trusted file should keep ProjectOverride")
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
		l.applyTrustGate(cfg)

		if cfg.ProjectOverride != nil {
			t.Error("denied file should nil out ProjectOverride")
		}
	})

	t.Run("untrusted", func(t *testing.T) {
		store := trust.NewStore(filepath.Join(t.TempDir(), "trust"))
		// Don't trust or deny — file is untrusted by default

		var stderrBuf bytes.Buffer
		po := &config.ProjectOverride{Agent: "claude"}
		cfg := &config.Config{
			ProjectConfigPath: aidePath,
			ProjectOverride:   po,
		}
		l := &Launcher{TrustStore: store, Stderr: &stderrBuf}
		l.applyTrustGate(cfg)

		if cfg.ProjectOverride != nil {
			t.Error("untrusted file should nil out ProjectOverride")
		}
		if !strings.Contains(stderrBuf.String(), "not trusted") {
			t.Errorf("expected 'not trusted' warning, got: %q", stderrBuf.String())
		}
	})
}

func TestPrintUntrustedWarning(t *testing.T) {
	t.Run("with agent only", func(t *testing.T) {
		var buf bytes.Buffer
		po := &config.ProjectOverride{Agent: "claude"}
		printUntrustedWarning(&buf, "/path/to/.aide.yaml", po)

		output := buf.String()
		if !strings.Contains(output, "not trusted") {
			t.Errorf("expected 'not trusted' in output, got %q", output)
		}
		if !strings.Contains(output, "claude") {
			t.Errorf("expected agent name 'claude' in output, got %q", output)
		}
		if !strings.Contains(output, "aide trust") {
			t.Errorf("expected 'aide trust' hint in output, got %q", output)
		}
	})

	t.Run("with capabilities and env", func(t *testing.T) {
		var buf bytes.Buffer
		po := &config.ProjectOverride{
			Agent:        "claude",
			Capabilities: []string{"network", "filesystem"},
			Env:          map[string]string{"FOO": "bar", "BAZ": "qux"},
		}
		printUntrustedWarning(&buf, "/tmp/.aide.yaml", po)

		output := buf.String()
		if !strings.Contains(output, "network") {
			t.Errorf("expected capabilities in output, got %q", output)
		}
		if !strings.Contains(output, "2 configured") {
			t.Errorf("expected env count in output, got %q", output)
		}
	})
}

