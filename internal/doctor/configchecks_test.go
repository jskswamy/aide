package doctor

import (
	"errors"
	"testing"

	"github.com/jskswamy/aide/internal/config"
)

func noBinaries(string) (string, error) { return "", errors.New("not found") }

func TestConfigChecks_ReportsEachProblemOnce(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // secret stores resolve under it; none exist
	cfg := &config.Config{
		DefaultContext: "work",
		Agents:         map[string]config.AgentDef{"claude": {Binary: "claude"}},
		Contexts: map[string]config.Context{
			"work":        {Agent: "claude"},
			"stray":       {Agent: "nobody", Match: []config.MatchRule{{Path: "/a"}}},
			"lost-secret": {Agent: "claude", Secret: "gone", Match: []config.MatchRule{{Path: "/b"}}},
			"bad-sandbox": {Agent: "claude", Sandbox: &config.SandboxRef{ProfileName: "nope"}, Match: []config.MatchRule{{Path: "/c"}}},
			"bad-tmpl":    {Agent: "claude", Env: map[string]string{"X": "{{ .bad"}, Match: []config.MatchRule{{Path: "/d"}}},
			"no-store":    {Agent: "claude", Env: map[string]string{"T": "{{ .secrets.t }}"}, Match: []config.MatchRule{{Path: "/e"}}},
			"unbound":     {Agent: "claude"},
		},
	}
	got := map[string]Finding{}
	for _, f := range ConfigChecks(cfg, noBinaries) {
		key := f.ID + " " + f.Subject
		if _, dup := got[key]; dup {
			t.Errorf("duplicate finding %s", key)
		}
		got[key] = f
	}
	want := map[string]Severity{
		"config.unknown-agent stray":             SeverityError,
		"config.secret-missing lost-secret":      SeverityError,
		"config.sandbox-ref-invalid bad-sandbox": SeverityError,
		"config.template-invalid bad-tmpl.X":     SeverityError,
		"config.secret-without-store no-store.T": SeverityError,
		"config.no-match-rules unbound":          SeverityWarning,
		"config.agent-not-on-path claude":        SeverityWarning,
	}
	for key, sev := range want {
		f, ok := got[key]
		if !ok {
			t.Errorf("missing finding %s", key)
			continue
		}
		if f.Severity != sev {
			t.Errorf("%s: severity %s, want %s", key, f.Severity, sev)
		}
		if f.Message == "" || len(f.Fix.Steps) == 0 {
			t.Errorf("%s: want a message and written steps, got %+v", key, f)
		}
	}
	if len(got) != len(want) {
		t.Errorf("got %d findings, want %d: %v", len(got), len(want), got)
	}
}

func TestConfigChecks_CleanConfigHasNoFindings(t *testing.T) {
	cfg := &config.Config{
		DefaultContext: "work",
		Agents:         map[string]config.AgentDef{"claude": {Binary: "claude"}},
		Contexts:       map[string]config.Context{"work": {Agent: "claude"}},
	}
	found := func(string) (string, error) { return "/bin/claude", nil }
	if f := ConfigChecks(cfg, found); len(f) != 0 {
		t.Errorf("clean config: got %v", f)
	}
}
