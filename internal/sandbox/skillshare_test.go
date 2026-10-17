package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

// skillshareFixture is a home with every Skillshare dir, a config whose
// sources: list one safe path and several the capability must refuse,
// and the profile dirs those refusals protect.
type skillshareFixture struct {
	home    string
	granted []string // readable through the capability
	hidden  []string // never granted by it
}

func newSkillshareFixture(t *testing.T) skillshareFixture {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	for _, k := range []string{"XDG_CONFIG_HOME", "XDG_STATE_HOME", "SKILLSHARE_CONFIG"} {
		t.Setenv(k, "")
	}
	f := skillshareFixture{home: home}
	for _, rel := range []string{".config/skillshare/skills", ".config/skillshare/agents", ".local/state/skillshare/plugins", "src/skills"} {
		f.granted = append(f.granted, filepath.Join(home, rel))
	}
	for _, rel := range []string{
		".config/skillshare/config.yaml", ".local/state/skillshare/hooks", ".local/state/skillshare/mcp",
		".local/state/skillshare/logs", ".local/share/skillshare", ".cache/skillshare",
		".ssh", ".config", ".claude-personal", ".claude",
	} {
		f.hidden = append(f.hidden, filepath.Join(home, rel))
	}
	for _, p := range append(append([]string{}, f.granted...), f.hidden...) {
		if filepath.Ext(p) == ".yaml" {
			continue
		}
		if err := os.MkdirAll(p, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	sources := "sources:\n  skills: ~/src/skills\n  extra: [~, /, ~/.ssh, ~/.config, ~/.claude-personal, ~/.claude]\n"
	if err := os.WriteFile(filepath.Join(home, ".config/skillshare/config.yaml"), []byte(sources), 0o600); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f skillshareFixture) config() *config.Config {
	return &config.Config{Contexts: map[string]config.Context{
		"personal": {Agent: "claude", Profile: "personal"},
	}}
}

// policy is the launch policy for capNames, built the way the launcher
// builds it.
func (f skillshareFixture) policy(t *testing.T, capNames []string) *Policy {
	t.Helper()
	_, overrides, err := ResolveCapabilities(capNames, f.config())
	if err != nil {
		t.Fatal(err)
	}
	var sbCfg *config.SandboxPolicy
	ApplyOverrides(&sbCfg, overrides)
	policy, _, err := PolicyFromConfig(sbCfg, Paths{ProjectRoot: t.TempDir(), RuntimeDir: t.TempDir(), TempDir: t.TempDir(), HomeDir: f.home})
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

func seatbeltProfile(t *testing.T, policy *Policy) string {
	t.Helper()
	p := seatbelt.New(policy.HomeDir).WithContext(func(c *seatbelt.Context) { *c = *policy.ToSeatbeltContext() })
	for _, g := range guards.ResolveActiveGuards(policy.Guards) {
		p.Use(g)
	}
	res, err := p.Render()
	if err != nil {
		t.Fatal(err)
	}
	return res.Profile
}

// Landlock: the skillshare capability makes Skillshare's skills, agents,
// plugins and safe sources readable and not writable, and nothing else
// of Skillshare's (or the sources it refuses) readable.
func TestSkillshareCapability_LandlockReadOnlyGrants(t *testing.T) {
	f := newSkillshareFixture(t)

	gps := DeriveGrantedPathSet(*f.policy(t, []string{"skillshare"}))

	for _, p := range f.granted {
		if !containsPath(gps.Readable, p) {
			t.Errorf("%s not readable: %v", p, gps.Readable)
		}
		for _, w := range gps.Writable {
			if w == p || strings.HasPrefix(p, w+string(filepath.Separator)) {
				t.Errorf("%s writable through %s", p, w)
			}
		}
	}
	for _, p := range f.hidden {
		for _, r := range append(append([]string{}, gps.Readable...), gps.Writable...) {
			if r == p || strings.HasPrefix(p, r+string(filepath.Separator)) {
				t.Errorf("%s granted through %s", p, r)
			}
		}
	}
	for _, r := range gps.Readable {
		if r == "/" || r == f.home {
			t.Errorf("readable grant of %s", r)
		}
	}
}

// Seatbelt: the same grants, as file-read* rules only.
func TestSkillshareCapability_SeatbeltReadOnlyGrants(t *testing.T) {
	f := newSkillshareFixture(t)

	profile := seatbeltProfile(t, f.policy(t, []string{"skillshare"}))

	for _, p := range f.granted {
		if !strings.Contains(profile, `(allow file-read* (subpath "`+p+`"))`) {
			t.Errorf("profile does not grant read of %s", p)
		}
	}
	for _, rule := range strings.Split(profile, "(allow") {
		if !strings.Contains(rule, "file-write") {
			continue
		}
		for _, p := range f.granted {
			if strings.Contains(rule, `"`+p+`"`) || strings.Contains(rule, "skillshare") {
				t.Errorf("profile grants a write to %s: %s", p, rule)
			}
		}
	}
	for _, p := range f.hidden {
		if strings.Contains(profile, `"`+p+`"`) {
			t.Errorf("profile names %s", p)
		}
	}
}

// Without the capability (disabled_capabilities or --without dropped
// it), nothing of Skillshare's is granted on either backend.
func TestSkillshareCapability_AbsentGrantsNothing(t *testing.T) {
	f := newSkillshareFixture(t)
	policy := f.policy(t, nil)

	gps := DeriveGrantedPathSet(*policy)
	for _, p := range f.granted {
		for _, r := range append(append([]string{}, gps.Readable...), gps.Writable...) {
			if r == p || strings.HasPrefix(p, r+string(filepath.Separator)) {
				t.Errorf("%s granted through %s without the capability", p, r)
			}
		}
	}
	profile := seatbeltProfile(t, policy)
	if strings.Contains(profile, "skillshare") || strings.Contains(profile, filepath.Join(f.home, "src")) {
		t.Error("seatbelt profile grants Skillshare paths without the capability")
	}
}
