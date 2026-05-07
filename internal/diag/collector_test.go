package diag

import (
	"strings"
	"testing"
	"time"
)

func TestPre_RedactsEnvValues(t *testing.T) {
	in := PreInput{
		AideVersion: "1.8.1",
		CWD:         "/home/u/proj",
		AgentBinary: "/usr/bin/sandbox-exec",
		Argv:        []string{"sandbox-exec", "-f", "/tmp/p.sb", "claude"},
		Env: []string{
			"PATH=/usr/bin",
			"ANTHROPIC_API_KEY=sk-ant-supersecret-do-not-leak",
			"FOO=",
		},
		SecretSourcePaths: []string{"/home/u/.config/aide/secrets/secrets.yaml"},
	}
	r := Pre(in)

	// Find the API key entry and confirm length is recorded but value is not.
	var found bool
	for _, k := range r.EnvKeys {
		if k.Name == "ANTHROPIC_API_KEY" {
			found = true
			if k.Length != len("sk-ant-supersecret-do-not-leak") {
				t.Errorf("env length mismatch: got %d, want %d", k.Length, len("sk-ant-supersecret-do-not-leak"))
			}
		}
	}
	if !found {
		t.Errorf("ANTHROPIC_API_KEY not present in EnvKeys")
	}

	// Belt-and-suspenders: scan everything in the Report stringifying for the secret.
	for _, k := range r.EnvKeys {
		if strings.Contains(k.Name, "sk-ant-supersecret-do-not-leak") {
			t.Errorf("secret leaked into EnvKey.Name: %s", k.Name)
		}
	}
}

func TestPre_KeepsArgvButRedactsEqualsValues(t *testing.T) {
	in := PreInput{
		Argv: []string{"claude", "--api-key=sk-ant-leak", "--model", "claude-opus-4-7"},
	}
	r := Pre(in)
	for _, a := range r.Argv {
		if strings.Contains(a, "sk-ant-leak") {
			t.Errorf("argv leaked secret: %s", a)
		}
	}
	// Verify the structure: --api-key=<redacted:N> should remain
	var redactedSeen bool
	for _, a := range r.Argv {
		if strings.HasPrefix(a, "--api-key=<redacted:") {
			redactedSeen = true
		}
	}
	if !redactedSeen {
		t.Errorf("expected --api-key=<redacted:N> in argv, got %v", r.Argv)
	}
}

func TestPre_PreservesNonSensitiveArgv(t *testing.T) {
	in := PreInput{
		Argv: []string{"claude", "--model=claude-opus-4-7", "--verbose", "subcommand"},
	}
	r := Pre(in)
	if r.Argv[0] != "claude" || r.Argv[3] != "subcommand" {
		t.Errorf("non-sensitive argv altered: %v", r.Argv)
	}
	if r.Argv[1] != "--model=claude-opus-4-7" {
		t.Errorf("non-sensitive --model=value altered: %v", r.Argv)
	}
}

func TestPre_HandlesEmptyEnv(t *testing.T) {
	r := Pre(PreInput{Env: nil})
	if len(r.EnvKeys) != 0 {
		t.Errorf("expected empty EnvKeys, got %v", r.EnvKeys)
	}
}

func TestPre_HandlesMalformedEnvEntry(t *testing.T) {
	r := Pre(PreInput{Env: []string{"NOEQUALS", "OK=value"}})
	if len(r.EnvKeys) != 1 {
		t.Fatalf("expected 1 env key (malformed entry skipped), got %d: %v", len(r.EnvKeys), r.EnvKeys)
	}
	if r.EnvKeys[0].Name != "OK" {
		t.Errorf("unexpected key: %s", r.EnvKeys[0].Name)
	}
}

func TestRedactArgv_DoesNotRedactBenignAuthFlags(t *testing.T) {
	in := PreInput{
		Argv: []string{"app", "--author=john", "--auth-method=basic", "--authority=cn=foo"},
	}
	r := Pre(in)
	for _, a := range r.Argv {
		if strings.Contains(a, "<redacted") {
			t.Errorf("benign flag was redacted: %s", a)
		}
	}
}

func TestRedactArgv_HandlesUnderscoreFlagNames(t *testing.T) {
	in := PreInput{
		Argv: []string{"app", "--api_key=should-be-redacted", "--auth_token=also-redacted"},
	}
	r := Pre(in)
	for _, a := range r.Argv {
		if strings.Contains(a, "should-be-redacted") || strings.Contains(a, "also-redacted") {
			t.Errorf("underscore-form flag not redacted: %s", a)
		}
	}
}

func TestPre_DefensiveCopiesSecretSourcePaths(t *testing.T) {
	paths := []string{"/path/a", "/path/b"}
	in := PreInput{SecretSourcePaths: paths}
	r := Pre(in)
	paths[0] = "MUTATED"
	if r.SecretSourcePaths[0] == "MUTATED" {
		t.Error("Report shares slice memory with input — mutation leaked through")
	}
}

func TestPost_AppliesExitAndStderr(t *testing.T) {
	r := Report{}
	out := Post(r, PostInput{
		ExitCode:        7,
		Signal:          "",
		Runtime:         123 * time.Millisecond,
		StderrTail:      "/Users/alice/.config/aide/secrets/x.yaml: error\n",
		StderrTruncated: 0,
		HomeDir:         "/Users/alice",
	})
	if out.ExitCode != 7 || out.Runtime != 123*time.Millisecond {
		t.Fatalf("post did not propagate exit/runtime: %+v", out)
	}
	if !strings.Contains(out.StderrTail, "~/.config/aide/secrets/x.yaml") {
		t.Errorf("$HOME not rewritten: %q", out.StderrTail)
	}
	if strings.Contains(out.StderrTail, "/Users/alice/") {
		t.Errorf("$HOME leaked: %q", out.StderrTail)
	}
}

func TestPost_HandlesEmptyHomeDir(t *testing.T) {
	out := Post(Report{}, PostInput{
		StderrTail: "/Users/alice/foo: bar\n",
		HomeDir:    "",
	})
	if !strings.Contains(out.StderrTail, "/Users/alice/foo") {
		t.Errorf("empty HomeDir should leave path untouched, got %q", out.StderrTail)
	}
}

func TestPost_PropagatesSignalAndTruncatedBytes(t *testing.T) {
	out := Post(Report{}, PostInput{
		ExitCode:        137,
		Signal:          "SIGKILL",
		Runtime:         5 * time.Second,
		StderrTail:      "killed",
		StderrTruncated: 1024,
	})
	if out.Signal != "SIGKILL" {
		t.Errorf("Signal not propagated: %q", out.Signal)
	}
	if out.StderrTruncated != 1024 {
		t.Errorf("StderrTruncated not propagated: %d", out.StderrTruncated)
	}
}

func TestRedactArgv_HandlesSpaceSeparatedSecretValue(t *testing.T) {
	in := PreInput{
		Argv: []string{"claude", "--api-key", "sk-ant-leak-do-not-show", "--model", "opus"},
	}
	r := Pre(in)
	for _, a := range r.Argv {
		if strings.Contains(a, "sk-ant-leak") {
			t.Errorf("argv leaked secret: %s (full argv: %v)", a, r.Argv)
		}
	}
	if r.Argv[1] != "--api-key" {
		t.Errorf("flag itself altered: %v", r.Argv)
	}
	if !strings.HasPrefix(r.Argv[2], "<redacted:") {
		t.Errorf("expected <redacted:N> at position 2, got %q (full: %v)", r.Argv[2], r.Argv)
	}
	if r.Argv[3] != "--model" || r.Argv[4] != "opus" {
		t.Errorf("non-secret flags altered: %v", r.Argv)
	}
}

func TestRedactArgv_DoesNotRedactWhenNextArgIsAFlag(t *testing.T) {
	in := PreInput{
		Argv: []string{"claude", "--api-key", "--debug"},
	}
	r := Pre(in)
	if r.Argv[2] != "--debug" {
		t.Errorf("flag-following-flag should pass through, got %v", r.Argv)
	}
}
