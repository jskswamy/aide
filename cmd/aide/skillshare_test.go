package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/secrets"
)

const skillshareAideConfig = `profiles:
  work:
    agent: claude
    config_dir_name: work
    credentials:
      anthropic:
        backend: age
        store: test-secrets
        key: anthropic_api_key
    default_credentials: [anthropic]
    env:
      CONTEXT7_TOKEN: "%s"
  personal:
    agent: claude
default_profile: personal
`

const skillshareUserConfig = `# my skillshare config
sources:
  skills: ~/.config/skillshare/skills
targets:
  claude:
    path: ~/.claude/skills
`

// skillshareFixture writes an aide config whose work profile binds a
// credential from the repo's fixture store and carries another of its
// values as a literal env: entry, and a Skillshare config. It returns
// the loaded config, home, the Skillshare config path and every value
// in the fixture store.
func skillshareFixture(t *testing.T) (*config.Config, string, string, []string) {
	t.Helper()
	td := repoTestdataDir(t)
	store, err := secrets.DecryptSecretsFile(filepath.Join(td, "test-secrets.enc.yaml"),
		&secrets.AgeIdentity{Source: secrets.SourceEnvKeyFile, KeyData: filepath.Join(td, "age-key.txt")})
	if err != nil {
		t.Fatal(err)
	}
	var values []string
	for _, v := range store {
		values = append(values, v)
	}

	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	cfgDir := filepath.Join(home, ".config", "aide")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	aideCfg := strings.Replace(skillshareAideConfig, "%s", store["context7_token"], 1)
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(aideCfg), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(cfgDir, home)
	if err != nil {
		t.Fatal(err)
	}
	ssPath := filepath.Join(home, ".config", "skillshare", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(ssPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ssPath, []byte(skillshareUserConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfg, home, ssPath, values
}

func TestSkillshareTargets_PreviewWritesNothing(t *testing.T) {
	cfg, home, ssPath, _ := skillshareFixture(t)
	var out bytes.Buffer

	if err := runSkillshareTargets(&out, cfg, home, ssPath, false); err != nil {
		t.Fatal(err)
	}

	got, _ := os.ReadFile(ssPath)
	if string(got) != skillshareUserConfig {
		t.Errorf("preview changed %s:\n%s", ssPath, got)
	}
	for _, want := range []string{
		"aide-work-claude:\n", "config_dir: ~/.claude-work",
		"+ aide-work-codex",
		"personal: no config_dir_name, config_dir or config_dirs",
		"Nothing written",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("preview lacks %q:\n%s", want, out.String())
		}
	}
	if _, err := os.Stat(ssPath + ".bak"); err == nil {
		t.Error("preview wrote a backup")
	}
}

func TestSkillshareTargets_WriteMergesWithBackupThenIsIdempotent(t *testing.T) {
	cfg, home, ssPath, _ := skillshareFixture(t)
	var out bytes.Buffer

	if err := runSkillshareTargets(&out, cfg, home, ssPath, true); err != nil {
		t.Fatal(err)
	}

	got, _ := os.ReadFile(ssPath)
	for _, want := range []string{"# my skillshare config", "path: ~/.claude/skills", "aide-work-pi:", "config_dir: ~/.pi-work"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("written config lacks %q:\n%s", want, got)
		}
	}
	bak, err := os.ReadFile(ssPath + ".bak")
	if err != nil || string(bak) != skillshareUserConfig {
		t.Errorf("backup = %q, %v", bak, err)
	}
	if !strings.Contains(out.String(), "skillshare sync") {
		t.Errorf("output does not tell the user to run skillshare sync:\n%s", out.String())
	}

	out.Reset()
	if err := runSkillshareTargets(&out, cfg, home, ssPath, true); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(ssPath)
	if string(again) != string(got) {
		t.Errorf("second write changed the file:\n%s", again)
	}
	if !strings.Contains(out.String(), "up to date") {
		t.Errorf("second run output:\n%s", out.String())
	}
	if extra, _ := filepath.Glob(ssPath + ".bak-*"); len(extra) != 0 {
		t.Errorf("second run made another backup: %v", extra)
	}
}

func TestSkillshareTargets_NoPlaintextCredential(t *testing.T) {
	cfg, home, ssPath, values := skillshareFixture(t)
	var preview, written bytes.Buffer

	if err := runSkillshareTargets(&preview, cfg, home, ssPath, false); err != nil {
		t.Fatal(err)
	}
	if err := runSkillshareTargets(&written, cfg, home, ssPath, true); err != nil {
		t.Fatal(err)
	}

	file, _ := os.ReadFile(ssPath)
	for _, v := range values {
		for name, text := range map[string]string{"preview": preview.String(), "write output": written.String(), "config": string(file)} {
			if strings.Contains(text, v) {
				t.Errorf("%s contains a credential value from the fixture store", name)
			}
		}
	}
}

func TestSkillshareTargets_WriteNeedsSkillshareConfig(t *testing.T) {
	cfg, home, ssPath, _ := skillshareFixture(t)
	if err := os.Remove(ssPath); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer

	err := runSkillshareTargets(&out, cfg, home, ssPath, true)

	if err == nil || !strings.Contains(err.Error(), "skillshare init") {
		t.Errorf("err = %v, want a pointer to skillshare init", err)
	}
	if _, statErr := os.Stat(ssPath); statErr == nil {
		t.Error("created a Skillshare config")
	}
}
