package gatewaymigrate

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/prefs"
)

func ctxWith(gws map[string]config.GatewayConfig) config.Context {
	return config.Context{ConvertedGateways: gws, Credentials: map[string]config.CredentialBinding{"github": {}}}
}

func TestTranslatePreference(t *testing.T) {
	ctx := ctxWith(map[string]config.GatewayConfig{
		"corp":       {Harnesses: []string{"claude", "pi"}},
		"corp-codex": {Harnesses: []string{"codex"}},
		"claude-gw":  {Harnesses: []string{"claude"}},
	})
	renames := map[string]map[string]string{"codex": {"corp": "corp-codex"}}
	tests := []struct {
		name       string
		rec        prefs.Preference
		want       map[string]string
		wantNotice string
	}{
		{"unset stays untouched", prefs.Preference{Harness: "pi"}, nil, ""},
		{"empty selection is none for the slot harnesses", prefs.Preference{LegacyCredentials: prefs.Selection()},
			map[string]string{"claude": "none", "codex": "none"}, ""},
		{"empty selection for pi sets nothing", prefs.Preference{Harness: "pi", LegacyCredentials: prefs.Selection()}, nil, ""},
		{"gateway follows each harness it serves", prefs.Preference{LegacyCredentials: prefs.Selection("corp")},
			map[string]string{"claude": "corp", "pi": "corp", "codex": "corp-codex"}, ""},
		{"record harness limits the translation", prefs.Preference{Harness: "codex", LegacyCredentials: prefs.Selection("corp")},
			map[string]string{"codex": "corp-codex"}, ""},
		{"plain credential selection is none without a notice", prefs.Preference{Harness: "claude", LegacyCredentials: prefs.Selection("github")},
			map[string]string{"claude": "none"}, ""},
		{"unknown credential is none with a notice", prefs.Preference{Harness: "claude", LegacyCredentials: prefs.Selection("anthropic")},
			map[string]string{"claude": "none"}, `"anthropic"`},
		{"gateway that no longer serves the harness is dropped", prefs.Preference{Harness: "codex", LegacyCredentials: prefs.Selection("claude-gw")},
			map[string]string{"codex": "none"}, "aide gateway add"},
		{"an existing choice wins", prefs.Preference{Harness: "claude", Gateway: map[string]string{"claude": "none"}, LegacyCredentials: prefs.Selection("corp")},
			map[string]string{"claude": "none"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, notices := TranslatePreference(tt.rec, ctx, renames)
			if len(got.Gateway) != len(tt.want) {
				t.Fatalf("Gateway = %v, want %v", got.Gateway, tt.want)
			}
			for h, v := range tt.want {
				if got.Gateway[h] != v {
					t.Errorf("Gateway[%s] = %q, want %q", h, got.Gateway[h], v)
				}
			}
			if tt.rec.LegacyCredentials != nil && got.LegacyCredentials != nil {
				t.Errorf("LegacyCredentials = %v, want it consumed", *got.LegacyCredentials)
			}
			if tt.wantNotice == "" && len(notices) != 0 || tt.wantNotice != "" && !slices.ContainsFunc(notices, func(n string) bool { return strings.Contains(n, tt.wantNotice) }) {
				t.Errorf("notices = %q, want one containing %q", notices, tt.wantNotice)
			}
		})
	}
}

const legacyProfile = `agent: claude
config_dir_name: work
credentials:
  github: {backend: age, store: work, key: gh, env: [GITHUB_TOKEN], harnesses: [claude]}
default_credentials: [corp]
gateways:
  corp:
    base_url: https://gw.example.com
    store: work
    key: corp_token
    harnesses: [claude, pi]
  alpha:
    base_url: https://alpha.example.com
    store: work
    key: alpha_token
    harnesses: [opencode, codex]
`

type fakeCaches map[string]string

func (f fakeCaches) DefaultModel(profile, gw string) string { return f[profile+"/"+gw] }

func (f fakeCaches) PinnedModel(_ config.Context, gw string) string { return f["pin/"+gw] }

func loadLegacy(t *testing.T, body string) (cfgDir string, cfg *config.Config) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	cfgDir = t.TempDir()
	if err := os.MkdirAll(filepath.Join(cfgDir, "profiles"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte("default_profile: work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "profiles", "work.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(cfgDir, "")
	if err != nil {
		t.Fatal(err)
	}
	return cfgDir, cfg
}

func TestPlanGatewayMigration(t *testing.T) {
	_, cfg := loadLegacy(t, legacyProfile)
	if !cfg.NeedsGatewayMigration {
		t.Fatal("legacy gateways must flag the config")
	}
	plan, err := PlanGatewayMigration(cfg, fakeCaches{"work/corp": "claude-sonnet-4-5"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Contexts["work"].Gateways["corp"].BaseURL == "" || !cfg.NeedsGatewayMigration {
		t.Error("planning changed the loaded config")
	}
	got := plan.Config.Contexts["work"]
	if plan.Config.NeedsGatewayMigration {
		t.Error("the planned config is still flagged")
	}
	if got.GatewayChoice["claude"] != "corp" || got.GatewayChoice["pi"] != "corp" || got.GatewayChoice["opencode"] != "alpha" || got.GatewayChoice["codex"] != "alpha-codex" {
		t.Errorf("gateway_choice = %v", got.GatewayChoice)
	}
	if len(got.DefaultCredentials) != 0 || got.Credentials["github"].Key != "gh" {
		t.Errorf("default_credentials = %v, credentials = %v; the gateway name goes, plain credentials stay", got.DefaultCredentials, got.Credentials)
	}
	if got.Gateways["corp"].API != "anthropic-messages" || got.Gateways["corp"].Model != "claude-sonnet-4-5" ||
		got.Gateways["alpha-codex"].API != "openai-responses" || got.Gateways["alpha-codex"].URL != "https://alpha.example.com/v1" {
		t.Errorf("gateways = %+v", got.Gateways)
	}
	var inherited bool
	for _, n := range plan.Notes {
		inherited = inherited || strings.Contains(n, "opencode now uses gateway \"alpha\", inherited from the old sorted gateway order")
		if strings.Contains(n, `claude now uses`) {
			t.Errorf("a choice named in default_credentials is not inherited: %s", n)
		}
	}
	if !inherited {
		t.Errorf("notes = %v, want opencode flagged as inherited", plan.Notes)
	}
}

func TestPlanNotesCodexLostWithAddCommand(t *testing.T) {
	// alpha claims codex, so corp (every harness by default) loses it.
	_, cfg := loadLegacy(t, strings.NewReplacer("default_credentials: [corp]\n", "", "    harnesses: [claude, pi]\n", "").Replace(legacyProfile))
	plan, _ := PlanGatewayMigration(cfg, nil)
	want := "aide gateway add corp-codex --url https://gw.example.com/v1 --api openai-responses --secret-store work --key corp_token --harnesses codex"
	if len(plan.Notes) == 0 || !strings.Contains(strings.Join(plan.Notes, "\n"), want) {
		t.Errorf("notes = %v, want %q", plan.Notes, want)
	}
}

func TestApplyBacksUpWritesAndIsIdempotent(t *testing.T) {
	cfgDir, cfg := loadLegacy(t, legacyProfile)
	state := t.TempDir()
	prefsPath := filepath.Join(state, "preferences.json")
	if err := os.WriteFile(prefsPath, []byte(`{"version":1,"profiles":{"work":{"github.com/o/r":{"harness":"pi","credentials":["corp"]}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := Migrate(cfg, nil, cfgDir, state)
	if err != nil || res.ReadOnly {
		t.Fatalf("Migrate() = %+v, %v", res, err)
	}
	for _, f := range []string{"config.yaml.bak", "profiles/work.yaml.bak", "preferences.json.bak"} {
		if _, err := os.Stat(filepath.Join(res.BackupDir, f)); err != nil {
			t.Errorf("missing backup %s: %v", f, err)
		}
	}
	if !strings.Contains(res.Summary, res.BackupDir) || !strings.Contains(res.Summary, "inherited") {
		t.Errorf("summary = %q", res.Summary)
	}
	if cfg.NeedsGatewayMigration || cfg.Contexts["work"].GatewayChoice["claude"] != "corp" {
		t.Error("Migrate must leave cfg as the migrated config")
	}
	loaded, err := config.Load(cfgDir, "")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.NeedsGatewayMigration || loaded.Contexts["work"].GatewayChoice["pi"] != "corp" || loaded.Contexts["work"].Gateways["alpha-codex"].API != "openai-responses" {
		t.Errorf("reloaded = %+v", loaded.Contexts["work"])
	}
	pf, err := prefs.Load(prefsPath)
	if err != nil {
		t.Fatal(err)
	}
	if p, _ := pf.Get(prefs.Key{Profile: "work", Remote: "github.com/o/r"}); p.LegacyCredentials != nil || p.Gateway["pi"] != "corp" {
		t.Errorf("preference = %+v, want translated to a pi choice", p)
	}
	// A second run changes nothing and backs up nothing.
	res2, err := Migrate(loaded, nil, cfgDir, state)
	if err != nil || res2.BackupDir != "" || res2.Summary != "" {
		t.Errorf("second Migrate() = %+v, %v", res2, err)
	}
}

func TestApplyReadOnlyConfigDirKeepsInMemoryTranslation(t *testing.T) {
	cfgDir, cfg := loadLegacy(t, legacyProfile)
	for _, d := range []string{filepath.Join(cfgDir, "profiles"), cfgDir} {
		if err := os.Chmod(d, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(d, 0o700) })
	}
	state := t.TempDir()
	res, err := Migrate(cfg, nil, cfgDir, state)
	if err != nil || !res.ReadOnly || !strings.Contains(res.Summary, "aide config migrate") || !strings.HasSuffix(res.Summary, "\n") {
		t.Fatalf("Migrate() = %+v, %v; want a read-only result with the hint", res, err)
	}
	if _, err := os.Stat(filepath.Join(state, "migrations")); !os.IsNotExist(err) {
		t.Errorf("a read-only dir left backups behind: %v", err)
	}
	if !cfg.NeedsGatewayMigration {
		t.Error("cfg must stay as loaded when nothing was written")
	}
	if b, _ := os.ReadFile(filepath.Join(cfgDir, "profiles", "work.yaml")); !strings.Contains(string(b), "base_url") {
		t.Error("the read-only config changed")
	}
}

// The model OpenCode pins to a gateway becomes its model, ahead of the
// catalogue default.
func TestPlanGatewayMigration_PinnedModelWins(t *testing.T) {
	_, cfg := loadLegacy(t, legacyProfile)
	plan, err := PlanGatewayMigration(cfg, fakeCaches{"work/alpha": "claude-sonnet-4-5", "pin/alpha": "claude-opus-4"})
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.Config.Contexts["work"].Gateways["alpha"].Model; got != "claude-opus-4" {
		t.Errorf("alpha model = %q, want the pinned claude-opus-4", got)
	}
}
