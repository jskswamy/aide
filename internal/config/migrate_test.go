package config

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

const legacyMigrateConfig = `# aide config
agents:
  claude:
    binary: claude
contexts:
  # work identity
  work:
    agent: claude
    secret: work
    profile: work # native dir name
    match:
      - remote: github.com/acme/*
    env:
      ANTHROPIC_API_KEY: "{{ .secrets.anthropic_key }}"
      GROQ_API_KEY: "{{ .secrets.groq }}"
      EDITOR: vim
  oss:
    agent: claude
    profile_dir: ~/.claude-oss
  personal:
    agent: claude
default_context: personal
never_allow_env: [FOO]
`

func migrate(t *testing.T, body string) *Migration {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	m, err := MigrateProfiles([]byte(body), MigrateOptions{Home: home})
	if err != nil {
		t.Fatalf("MigrateProfiles: %v", err)
	}
	return m
}

func decodeMigrated(t *testing.T, m *Migration) *Config {
	t.Helper()
	var cfg Config
	if err := yaml.Unmarshal(m.Output, &cfg); err != nil {
		t.Fatalf("migrated config does not parse: %v\n%s", err, m.Output)
	}
	return &cfg
}

func TestMigrateProfiles_RenamesKeysAndKeepsDirs(t *testing.T) {
	m := migrate(t, legacyMigrateConfig)
	if !m.Changed {
		t.Fatal("Changed = false for a legacy config")
	}
	out := string(m.Output)
	for _, gone := range []string{"contexts:", "default_context:", "profile: work", "profile_dir:"} {
		if strings.Contains(out, gone) {
			t.Errorf("migrated config still has %q:\n%s", gone, out)
		}
	}
	cfg := decodeMigrated(t, m)
	if !cfg.ProfileSchema || cfg.DefaultContext != "personal" {
		t.Fatalf("schema/default not migrated: schema=%v default=%q", cfg.ProfileSchema, cfg.DefaultContext)
	}
	if got := cfg.Contexts["work"].Profile; got != "work" {
		t.Errorf("work config_dir_name = %q, want work", got)
	}
	if got := cfg.Contexts["oss"].ProfileDir; got != "~/.claude-oss" {
		t.Errorf("oss config_dir = %q, want ~/.claude-oss", got)
	}
	if got := cfg.Contexts["work"].Match; len(got) != 1 || got[0].Remote != "github.com/acme/*" {
		t.Errorf("match rules not kept: %+v", got)
	}
	if len(cfg.NeverAllowEnv) != 1 || cfg.NeverAllowEnv[0] != "FOO" {
		t.Errorf("unrelated field lost: %+v", cfg.NeverAllowEnv)
	}
}

func TestMigrateProfiles_KeepsComments(t *testing.T) {
	out := string(migrate(t, legacyMigrateConfig).Output)
	for _, c := range []string{"# aide config", "# work identity", "# native dir name"} {
		if !strings.Contains(out, c) {
			t.Errorf("comment %q lost:\n%s", c, out)
		}
	}
}

func TestMigrateProfiles_LegacyCredentialEnvBecomesBindings(t *testing.T) {
	m := migrate(t, legacyMigrateConfig)
	cfg := decodeMigrated(t, m)
	work := cfg.Contexts["work"]
	if _, ok := work.Env["ANTHROPIC_API_KEY"]; ok {
		t.Errorf("ANTHROPIC_API_KEY still in env: %+v", work.Env)
	}
	if work.Env["EDITOR"] != "vim" {
		t.Errorf("ordinary env lost: %+v", work.Env)
	}
	a := work.Credentials["anthropic"]
	if a.Backend != "age" || a.Store != "work" || a.Key != "anthropic_key" {
		t.Errorf("anthropic binding = %+v", a)
	}
	g := work.Credentials["groq_api_key"]
	if g.Key != "groq" || len(g.Env) != 1 || g.Env[0] != "GROQ_API_KEY" || len(g.Harnesses) != 1 || g.Harnesses[0] != "claude" {
		t.Errorf("custom binding = %+v", g)
	}
	if strings.Join(work.DefaultCredentials, ",") != "anthropic,groq_api_key" {
		t.Errorf("default_credentials = %v", work.DefaultCredentials)
	}
	if len(m.Credentials) != 2 || m.Credentials[0].Profile != "work" || m.Credentials[0].EnvVar != "ANTHROPIC_API_KEY" {
		t.Errorf("credential report = %+v", m.Credentials)
	}
	if strings.Contains(string(m.Output), ".secrets.") {
		t.Errorf("secret template left behind:\n%s", m.Output)
	}
}

func TestMigrateProfiles_UntranslatableCredentialFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	_, err := MigrateProfiles([]byte(`contexts:
  work:
    agent: claude
    env:
      ANTHROPIC_API_KEY: sk-literal-value
`), MigrateOptions{Home: home})
	if !errors.Is(err, ErrLegacyCredentialEnv) {
		t.Fatalf("err = %v, want ErrLegacyCredentialEnv", err)
	}
	if strings.Contains(err.Error(), "sk-literal-value") {
		t.Errorf("error printed the value: %v", err)
	}
}

func TestMigrateProfiles_AlreadyMigratedIsUnchanged(t *testing.T) {
	body := `profiles:
  work:
    agent: claude
    config_dir_name: work
default_profile: work
`
	m := migrate(t, body)
	if m.Changed || string(m.Output) != body {
		t.Errorf("Changed = %v, output:\n%s", m.Changed, m.Output)
	}
}

func TestMigrateProfiles_MinimalConfigIsUnchanged(t *testing.T) {
	body := "agent: claude\n"
	m := migrate(t, body)
	if m.Changed || string(m.Output) != body {
		t.Errorf("minimal config changed:\n%s", m.Output)
	}
}

// YAML anchors, aliases and merge keys share nodes between profiles;
// editing them would drop or change another profile's settings, so the
// migration refuses instead (never silently, never a panic).
func TestMigrateProfiles_RefusesSharedYAMLNodes(t *testing.T) {
	cases := map[string]string{
		"env alias": `contexts:
  work:
    agent: claude
    secret: work
    env: &shared
      ANTHROPIC_API_KEY: "{{ .secrets.x }}"
      EDITOR: vim
  oss:
    agent: claude
    secret: work
    env: *shared
`,
		"merge key": `base: &base
  agent: claude
  secret: work
  env:
    ANTHROPIC_API_KEY: "{{ .secrets.x }}"
contexts:
  work:
    <<: *base
`,
		"entry alias": `contexts:
  work: &w
    agent: claude
  oss: *w
`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			_, err := MigrateProfiles([]byte(body), MigrateOptions{Home: home})
			if !errors.Is(err, ErrSharedYAML) {
				t.Fatalf("err = %v, want ErrSharedYAML", err)
			}
			if !strings.Contains(err.Error(), "by hand") {
				t.Errorf("error lacks the remedy: %v", err)
			}
		})
	}
}

// An already migrated file that uses anchors needs no edit, so it is
// left alone rather than refused.
func TestMigrateProfiles_AnchorsInMigratedFileWithNothingToDo(t *testing.T) {
	body := `profiles:
  work: &w
    agent: claude
    config_dir_name: work
`
	m := migrate(t, body)
	if m.Changed {
		t.Errorf("changed:\n%s", m.Output)
	}
}

// Every migrated profile keeps its effective env: except the moved
// credential entries.
func TestMigrateProfiles_EffectiveEnvKept(t *testing.T) {
	m := migrate(t, legacyMigrateConfig)
	var before Config
	if err := yaml.Unmarshal([]byte(legacyMigrateConfig), &before); err != nil {
		t.Fatal(err)
	}
	after := decodeMigrated(t, m)
	moved := map[string]bool{}
	for _, c := range m.Credentials {
		moved[c.Profile+"/"+c.EnvVar] = true
	}
	for name, ctx := range before.Contexts {
		for k, v := range ctx.Env {
			got, ok := after.Contexts[name].Env[k]
			if moved[name+"/"+k] {
				if ok {
					t.Errorf("%s: moved %s still in env", name, k)
				}
				continue
			}
			if !ok || got != v {
				t.Errorf("%s: env %s = %q (present %v), want %q", name, k, got, ok, v)
			}
		}
		if len(after.Contexts[name].Env) > len(ctx.Env) {
			t.Errorf("%s: env grew: %v", name, after.Contexts[name].Env)
		}
	}
}

func TestCheckEnvKept_RejectsDroppedVars(t *testing.T) {
	var before Config
	if err := yaml.Unmarshal([]byte("contexts:\n  work:\n    agent: claude\n    env: {EDITOR: vim, A: b}\n"), &before); err != nil {
		t.Fatal(err)
	}
	err := checkEnvKept(&before, []byte("profiles:\n  work:\n    agent: claude\n    env: {A: b}\n"), nil)
	if err == nil {
		t.Fatal("dropped EDITOR not detected")
	}
	if err := checkEnvKept(&before, []byte("profiles:\n  work:\n    agent: claude\n    env: {A: b, EDITOR: vim}\n"), nil); err != nil {
		t.Errorf("unchanged env rejected: %v", err)
	}
}

func TestMigrateProfiles_CustomCredentialWithoutKnownHarness(t *testing.T) {
	for name, agent := range map[string]string{"no agent": "", "unknown": "    agent: mytool\n"} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			body := "contexts:\n  work:\n" + agent + "    secret: work\n    env:\n      GROQ_API_KEY: \"{{ .secrets.groq }}\"\n"
			_, err := MigrateProfiles([]byte(body), MigrateOptions{Home: home})
			if !errors.Is(err, ErrLegacyCredentialEnv) || !strings.Contains(err.Error(), "not a known harness") {
				t.Fatalf("err = %v", err)
			}
			if strings.Contains(err.Error(), `"."`) {
				t.Errorf("confusing harness name in error: %v", err)
			}
		})
	}
}

func TestMigrateProfiles_EndpointMovesIntoTheKeysRoute(t *testing.T) {
	m := migrate(t, `profiles:
  work:
    agent: claude
    secret: work
    env:
      ANTHROPIC_AUTH_TOKEN: "{{ .secrets.or_key }}"
      ANTHROPIC_BASE_URL: https://openrouter.ai/api
      EDITOR: vim
`)
	work := decodeMigrated(t, m).Contexts["work"]
	if _, ok := work.Env["ANTHROPIC_BASE_URL"]; ok || work.Env["EDITOR"] != "vim" {
		t.Errorf("env = %v, want the endpoint moved and EDITOR kept", work.Env)
	}
	b := work.Credentials["anthropic_auth_token"]
	r := b.For["claude"]
	if r.Set["ANTHROPIC_BASE_URL"] != "https://openrouter.ai/api" || !slices.Equal(r.Env, []string{"ANTHROPIC_AUTH_TOKEN"}) ||
		len(b.Env) != 0 || len(b.Harnesses) != 0 {
		t.Errorf("binding = %+v, want the key and endpoint in for.claude", b)
	}
	if !strings.Contains(fmt.Sprint(m.Credentials), "ANTHROPIC_BASE_URL") {
		t.Errorf("report should name the moved endpoint: %+v", m.Credentials)
	}
}

func TestMigrateProfiles_TemplatedEndpointStops(t *testing.T) {
	_, err := MigrateProfiles([]byte(`profiles:
  work:
    agent: claude
    secret: work
    env:
      ANTHROPIC_API_KEY: "{{ .secrets.k }}"
      ANTHROPIC_BASE_URL: "{{ .secrets.url }}"
`), MigrateOptions{})
	if err == nil || !strings.Contains(err.Error(), "ANTHROPIC_BASE_URL") {
		t.Fatalf("error = %v, want a refusal naming the templated endpoint", err)
	}
}

func TestMigrateProfiles_ProducesSplitFilesPreservingComments(t *testing.T) {
	m := migrate(t, legacyMigrateConfig)
	if !m.SplitLayout {
		t.Fatal("expected SplitLayout = true for monolithic config")
	}
	for _, rel := range []string{"config.yaml", "sandbox.yaml", "profiles/work.yaml", "profiles/oss.yaml", "profiles/personal.yaml"} {
		if _, ok := m.SplitFiles[rel]; !ok {
			t.Errorf("SplitFiles missing %q; got keys %v", rel, slices.Sorted(maps.Keys(m.SplitFiles)))
		}
	}
	if _, ok := m.SplitFiles["provisioning.yaml"]; ok {
		t.Error("expected no provisioning.yaml when plugins/mcp_servers/hooks are empty")
	}
	coreStr := string(m.SplitFiles["config.yaml"])
	if !strings.Contains(coreStr, "# aide config") || !strings.Contains(coreStr, "default_profile: personal") {
		t.Errorf("config.yaml missing comment or default_profile:\n%s", coreStr)
	}
	for _, avoid := range []string{"profiles:", "contexts:", "never_allow_env:"} {
		if strings.Contains(coreStr, avoid) {
			t.Errorf("config.yaml still contains %q:\n%s", avoid, coreStr)
		}
	}
	workStr := string(m.SplitFiles["profiles/work.yaml"])
	if !strings.Contains(workStr, "# native dir name") || !strings.Contains(workStr, "config_dir_name: work") {
		t.Errorf("profiles/work.yaml missing comment or config_dir_name:\n%s", workStr)
	}
	sbxStr := string(m.SplitFiles["sandbox.yaml"])
	if !strings.Contains(sbxStr, "never_allow_env:") {
		t.Errorf("sandbox.yaml missing never_allow_env:\n%s", sbxStr)
	}

	// ApplyMigrationSplit writes the split layout and Load succeeds with LegacyMonolithic == false
	dir := t.TempDir()
	if err := ApplyMigrationSplit(dir, m); err != nil {
		t.Fatalf("ApplyMigrationSplit: %v", err)
	}
	loaded, err := Load(dir, "")
	if err != nil {
		t.Fatalf("Load after ApplyMigrationSplit: %v", err)
	}
	if loaded.LegacyMonolithic {
		t.Error("expected LegacyMonolithic = false after ApplyMigrationSplit")
	}
	if len(loaded.Contexts) != 3 || loaded.DefaultContext != "personal" {
		t.Errorf("loaded Contexts=%d DefaultContext=%q", len(loaded.Contexts), loaded.DefaultContext)
	}
}

func TestMigrateProfiles_InlineSectionsProduceSplitFiles(t *testing.T) {
	m := migrate(t, "default_profile: default\nprofiles:\n  default:\n    agent: claude\n    secret: personal\ncapabilities:\n  custom-cap:\n    description: test\nplugins:\n  steveyegge/beads:\n    - beads\n")
	if !m.SplitLayout || !m.NeedsWrite() {
		t.Fatal("expected SplitLayout = true and NeedsWrite = true for inline sections")
	}
	if _, ok := m.SplitFiles["profiles/default.yaml"]; !ok {
		t.Fatalf("expected profiles/default.yaml in SplitFiles, got %v", slices.Sorted(maps.Keys(m.SplitFiles)))
	}
	if _, ok := m.SplitFiles["sandbox.yaml"]; !ok {
		t.Errorf("expected sandbox.yaml in SplitFiles for config with capabilities")
	}
	if _, ok := m.SplitFiles["provisioning.yaml"]; !ok {
		t.Errorf("expected provisioning.yaml in SplitFiles for config with plugins")
	}
	preview := m.Preview()
	for _, want := range []string{"# === config.yaml ===", "# === sandbox.yaml ===", "# === provisioning.yaml ===", "# === profiles/default.yaml ==="} {
		if !strings.Contains(preview, want) {
			t.Errorf("Preview() missing %q:\n%s", want, preview)
		}
	}
}

func TestMigrateHelpers_PreviewInspectAndNudge(t *testing.T) {
	// Non-split Preview fallback
	plain := &Migration{Output: []byte("default_profile: work\n")}
	if plain.NeedsWrite() {
		t.Error("expected NeedsWrite=false for unchanged non-split migration")
	}
	if got := plain.Preview(); got != "default_profile: work" {
		t.Errorf("non-split Preview() = %q", got)
	}
	dir := t.TempDir()
	if err := ApplyMigrationSplit(dir, plain); err != nil {
		t.Fatalf("ApplyMigrationSplit(non-split): %v", err)
	}
	if err := ApplyMigrationSplit(dir, &Migration{SplitErr: ErrSharedYAML}); !errors.Is(err, ErrSharedYAML) {
		t.Fatalf("expected ErrSharedYAML, got %v", err)
	}

	// Monolithic profiles: config with provisioning section
	m := migrate(t, "default_profile: work\nplugins:\n  steveyegge/beads:\n    - beads\nprofiles:\n  work:\n    agent: opencode\n")
	if !m.SplitLayout || !m.NeedsWrite() {
		t.Fatal("expected SplitLayout=true for monolithic profiles config")
	}
	if _, ok := m.SplitFiles["provisioning.yaml"]; !ok {
		t.Error("expected provisioning.yaml in SplitFiles")
	}

	// InspectLoadedMigration on nil and loaded Config
	if InspectLoadedMigration(nil) {
		t.Error("InspectLoadedMigration(nil) = true")
	}
	cfg := &Config{Contexts: map[string]Context{"work": {Agent: "opencode"}}}
	if !InspectLoadedMigration(cfg) {
		t.Error("InspectLoadedMigration = false, want true")
	}

	// MaybeNudgeMigration cooldown and non-TTY checks
	home := t.TempDir()
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	var buf bytes.Buffer
	MaybeNudgeMigration(&Config{LegacyMonolithic: true}, &buf, false, home, now)
	if buf.Len() != 0 {
		t.Errorf("expected no nudge on non-TTY, got %q", buf.String())
	}
	MaybeNudgeMigration(&Config{LegacyMonolithic: true}, &buf, true, home, now)
	if !strings.Contains(buf.String(), "aide config migrate --write") {
		t.Errorf("expected nudge message, got %q", buf.String())
	}
	buf.Reset()
	MaybeNudgeMigration(&Config{LegacyMonolithic: true}, &buf, true, home, now.Add(24*time.Hour))
	if buf.Len() != 0 {
		t.Errorf("expected cooldown suppression, got %q", buf.String())
	}
}
