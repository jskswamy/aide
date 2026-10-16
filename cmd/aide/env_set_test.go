// cmd/aide/env_set_test.go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runEnvSet builds a fresh envSetCmd, redirects output, and runs it
// with the given args. It returns combined stdout+stderr and any error.
func runEnvSet(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := envSetCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

// projectTempDir creates a tempdir with an empty .aide/project.yaml and
// chdirs into it so cmdEnv resolves to a writable project override.
func projectTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
	t.Setenv("HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, ".aide"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestEnvSet_LiteralValue_WritesProjectOverride(t *testing.T) {
	projectTempDir(t)
	out, err := runEnvSet(t, "FOO", "bar")
	if err != nil {
		t.Fatalf("unexpected error: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Set FOO in project") {
		t.Errorf("expected success message, got: %s", out)
	}
}

func TestEnvSet_NoValueNoFlag_Errors(t *testing.T) {
	projectTempDir(t)
	_, err := runEnvSet(t, "FOO")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "must specify VALUE, --secret-key, or --pick") {
		t.Errorf("wrong error: %v", err)
	}
}

func TestEnvSet_LiteralAndSecretKey_Errors(t *testing.T) {
	projectTempDir(t)
	_, err := runEnvSet(t, "FOO", "bar", "--secret-key", "api_key")
	if err == nil || !strings.Contains(err.Error(), "cannot specify both") {
		t.Errorf("expected mutual-exclusion error, got: %v", err)
	}
}

func TestEnvSet_SecretKeyAndPick_Errors(t *testing.T) {
	projectTempDir(t)
	_, err := runEnvSet(t, "FOO", "--secret-key", "api_key", "--pick")
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("expected mutual-exclusion error, got: %v", err)
	}
}

func TestEnvSet_SecretKey_RequiresGlobal(t *testing.T) {
	projectTempDir(t)
	_, err := runEnvSet(t, "FOO", "--secret-key", "api_key")
	if err == nil || !strings.Contains(err.Error(), "--global") {
		t.Errorf("expected --global hint, got: %v", err)
	}
}

func TestEnvSet_FromSecret_UnknownFlag(t *testing.T) {
	projectTempDir(t)
	// Both space and = forms should fail since the flag is removed.
	for _, form := range [][]string{
		{"FOO", "--from-secret=api_key"},
		{"FOO", "--from-secret", "api_key"},
	} {
		_, err := runEnvSet(t, form...)
		if err == nil || !strings.Contains(err.Error(), "unknown flag") {
			t.Errorf("args %v: expected unknown-flag error, got: %v", form, err)
		}
	}
}

func TestEnvSet_SecretKey_SpaceForm_Parses(t *testing.T) {
	// The whole point of the redesign: space form must work.
	projectTempDir(t)
	_, err := runEnvSet(t, "FOO", "--secret-key", "api_key", "--global")
	// We don't expect success without a configured context+store, but
	// the error must NOT be "cannot specify both a value argument..."
	// (the old NoOptDefVal symptom).
	if err != nil && strings.Contains(err.Error(), "cannot specify both a value argument") {
		t.Errorf("space form still misparsed: %v", err)
	}
}

func TestEnvSet_SecretKey_NoStoreBound_Errors(t *testing.T) {
	projectTempDir(t)
	_, err := runEnvSet(t, "FOO", "--secret-key", "api_key", "--global")
	if err == nil || !strings.Contains(err.Error(), "no secret store bound") {
		t.Errorf("expected no-store-bound error, got: %v", err)
	}
}

func TestEnvSet_RefusesProviderSecretNames(t *testing.T) {
	for name, args := range map[string][]string{
		"project literal": {"ANTHROPIC_API_KEY", "sk-ant-xxx"},
		"global secret":   {"OPENAI_API_KEY", "--secret-key", "k", "--global"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := projectTempDir(t)
			out, err := runEnvSet(t, args...)
			if err == nil {
				t.Fatalf("expected refusal, got success: %s", out)
			}
			for _, w := range []string{args[0], "credentials:"} {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q lacks %q", err, w)
				}
			}
			if strings.Contains(err.Error(), "sk-ant-xxx") {
				t.Error("error echoed the value")
			}
			if _, statErr := os.Stat(filepath.Join(dir, ".aide.yaml")); statErr == nil {
				t.Error("refused command still wrote .aide.yaml")
			}
		})
	}
}

func TestEnvSet_ProviderSelectorsOnlyInUserConfig(t *testing.T) {
	dir := projectTempDir(t)
	_, err := runEnvSet(t, "CLAUDE_CODE_USE_BEDROCK", "1")
	if err == nil || !strings.Contains(err.Error(), "--global") {
		t.Fatalf("project-level selector error = %v, want a refusal pointing to --global", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".aide.yaml")); statErr == nil {
		t.Error("refused command still wrote .aide.yaml")
	}

	cfgDir := filepath.Join(dir, "xdg", "aide")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte("contexts:\n  work:\n    agent: claude\ndefault_context: work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := runEnvSet(t, "CLAUDE_CODE_USE_BEDROCK", "1", "--global"); err != nil {
		t.Fatalf("global selector refused: %v\n%s", err, out)
	}
}
