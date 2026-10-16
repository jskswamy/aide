package provision_test

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
)

func TestExecRunnerSuccess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires POSIX /usr/bin/true")
	}
	r := provision.ExecRunner{}
	_, _, code, err := r.Run(context.Background(), nil, "/usr/bin/true")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
}

func TestExecRunnerNonZeroExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires POSIX /usr/bin/false")
	}
	r := provision.ExecRunner{}
	_, _, code, err := r.Run(context.Background(), nil, "/usr/bin/false")
	if err != nil {
		t.Fatalf("err should be nil for non-zero exit, got %v", err)
	}
	if code == 0 {
		t.Errorf("exit = %d, want non-zero", code)
	}
}

func TestExecRunnerCapturesStdout(t *testing.T) {
	r := provision.ExecRunner{}
	out, _, code, err := r.Run(context.Background(), nil, "echo", "hello")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if code != 0 {
		t.Errorf("exit = %d", code)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("stdout = %q, want contains 'hello'", out)
	}
}

func TestExecRunnerMissingBinary(t *testing.T) {
	r := provision.ExecRunner{}
	_, _, _, err := r.Run(context.Background(), nil, "/no/such/binary-aide-test")
	if err == nil {
		t.Fatal("expected err for missing binary")
	}
}

func TestExecRunnerScrubsInheritedCredentials(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires POSIX env")
	}
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-shell")
	t.Setenv("OPENAI_API_KEY", "sk-openai-shell")
	t.Setenv("CODEX_API_KEY", "codex-shell")
	t.Setenv("SOPS_AGE_KEY", "AGE-SECRET-KEY-SHELL")
	t.Setenv("SOPS_AGE_KEY_FILE", "/shell/age.txt")
	t.Setenv("AIDE_RUNNER_KEEP", "kept")
	r := provision.ExecRunner{}
	out, _, code, err := r.Run(context.Background(), map[string]string{"CLAUDE_CONFIG_DIR": "/p"}, "env")
	if err != nil || code != 0 {
		t.Fatalf("env: code=%d err=%v", code, err)
	}
	for _, k := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "CODEX_API_KEY", "SOPS_AGE_KEY", "SOPS_AGE_KEY_FILE"} {
		if strings.Contains(out, k+"=") {
			t.Errorf("%s reached the provision subprocess", k)
		}
	}
	for _, want := range []string{"AIDE_RUNNER_KEEP=kept", "CLAUDE_CONFIG_DIR=/p"} {
		if !strings.Contains(out, want) {
			t.Errorf("subprocess env missing %s", want)
		}
	}
}

func TestExecRunnerScrubsConfiguredCustomNames(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires POSIX env")
	}
	t.Cleanup(func() { provision.SetScrubEnv(nil) })
	provision.SetScrubEnv([]string{"MY_PROVIDER_TOKEN", "CORP_LLM_*"})
	t.Setenv("MY_PROVIDER_TOKEN", "custom-shell")
	t.Setenv("CORP_LLM_KEY", "corp-shell")
	t.Setenv("AIDE_RUNNER_KEEP", "kept")
	out, _, code, err := provision.ExecRunner{}.Run(context.Background(), nil, "env")
	if err != nil || code != 0 {
		t.Fatalf("env: code=%d err=%v", code, err)
	}
	for _, k := range []string{"MY_PROVIDER_TOKEN", "CORP_LLM_KEY"} {
		if strings.Contains(out, k+"=") {
			t.Errorf("%s reached the provision subprocess", k)
		}
	}
	if !strings.Contains(out, "AIDE_RUNNER_KEEP=kept") {
		t.Error("unrelated variable dropped")
	}
}
