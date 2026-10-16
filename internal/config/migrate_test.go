package config

import (
	"errors"
	"strings"
	"testing"

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

func migrate(t *testing.T, body string, adopt ...string) *Migration {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	m, err := MigrateProfiles([]byte(body), MigrateOptions{Home: home, Adopt: adopt})
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

func TestMigrateProfiles_ListsGlobalLoginsWithoutChangingThem(t *testing.T) {
	m := migrate(t, legacyMigrateConfig)
	if len(m.GlobalLogins) != 1 {
		t.Fatalf("GlobalLogins = %+v, want personal only", m.GlobalLogins)
	}
	gl := m.GlobalLogins[0]
	if gl.Profile != "personal" || gl.Harness != "claude" || gl.Dir != "~/.claude" || gl.Adopted {
		t.Errorf("global login = %+v", gl)
	}
	personal := decodeMigrated(t, m).Contexts["personal"]
	if personal.SelectsConfigDir() {
		t.Errorf("personal changed without --adopt: %+v", personal)
	}
}

func TestMigrateProfiles_AdoptPinsTheGlobalDir(t *testing.T) {
	m := migrate(t, legacyMigrateConfig, "personal")
	personal := decodeMigrated(t, m).Contexts["personal"]
	if personal.Profile != "personal" || personal.ConfigDirs["claude"] != "~/.claude" {
		t.Errorf("adopted personal = %+v", personal)
	}
	if !m.GlobalLogins[0].Adopted {
		t.Errorf("report not marked adopted: %+v", m.GlobalLogins)
	}
}

func TestMigrateProfiles_AdoptRefusals(t *testing.T) {
	twoGlobal := `contexts:
  a:
    agent: claude
  b:
    agent: claude
  c:
    agent: claude
    profile: c
`
	cases := map[string][]string{
		"two on one dir": {"a", "b"},
		"isolated":       {"c"},
		"unknown":        {"nope"},
	}
	for name, adopt := range cases {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			_, err := MigrateProfiles([]byte(twoGlobal), MigrateOptions{Home: home, Adopt: adopt})
			if !errors.Is(err, ErrAdopt) {
				t.Fatalf("err = %v, want ErrAdopt", err)
			}
		})
	}
	m := migrate(t, twoGlobal)
	if len(m.GlobalLogins) != 2 {
		t.Errorf("GlobalLogins = %+v, want a and b", m.GlobalLogins)
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
