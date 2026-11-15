package launcher

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/config"
	aidectx "github.com/jskswamy/aide/internal/context"
	"github.com/jskswamy/aide/internal/prefs"
	"github.com/jskswamy/aide/internal/ui"
)

// choiceCtx is a profile with gateways a (claude, pi), b (claude) and
// c (codex) in converted form and no legacy gateways.
func choiceCtx(choice map[string]string) config.Context {
	return config.Context{
		GatewayChoice: choice,
		ConvertedGateways: map[string]config.GatewayConfig{
			"a": {URL: "https://a.example.com", API: "anthropic-messages", Store: "s", Key: "k", Harnesses: []string{"claude", "pi"}},
			"b": {URL: "https://b.example.com", API: "anthropic-messages", NoAuth: true, Harnesses: []string{"claude"}},
			"c": {URL: "https://c.example.com/v1", API: "openai-responses", Store: "s", Key: "k", Harnesses: []string{"codex"}},
		},
	}
}

func TestResolveChoice_Precedence(t *testing.T) {
	saved := func(h, v string) prefs.Preference { return prefs.Preference{Gateway: map[string]string{h: v}} }
	tests := []struct {
		name       string
		ctx        config.Context
		harness    string
		in         SelectionInputs
		want       Choice
		wantNotice string
		wantErr    string
	}{
		{"flag beats repo and profile", choiceCtx(map[string]string{"claude": "a"}), "claude",
			SelectionInputs{Credentials: []string{"b"}, CredentialsSet: true, Saved: saved("claude", "a")}, Choice{Value: "b", Source: SourceFlag}, "", ""},
		{"flag none", choiceCtx(nil), "claude", SelectionInputs{Credentials: []string{"none"}, CredentialsSet: true}, Choice{Value: "none", Source: SourceFlag}, "", ""},
		{"empty flag selection is none", choiceCtx(nil), "claude", SelectionInputs{CredentialsSet: true}, Choice{Value: "none", Source: SourceFlag}, "", ""},
		{"auth login is none", choiceCtx(nil), "claude", SelectionInputs{Auth: AuthLogin}, Choice{Value: "none", Source: SourceFlag}, "", ""},
		{"flag names a gateway that does not serve the harness", choiceCtx(nil), "claude",
			SelectionInputs{Credentials: []string{"c"}, CredentialsSet: true}, Choice{}, "", "does not serve claude"},
		{"flag names a preset that is not added", choiceCtx(nil), "claude",
			SelectionInputs{Credentials: []string{"openrouter"}, CredentialsSet: true}, Choice{}, "", "aide gateway add openrouter"},
		{"repo beats profile", choiceCtx(map[string]string{"claude": "a"}), "claude",
			SelectionInputs{Saved: saved("claude", "b")}, Choice{Value: "b", Source: SourceRepo}, "", ""},
		{"v1 repo preference is translated", choiceCtx(map[string]string{"claude": "a"}), "claude",
			SelectionInputs{Saved: prefs.Preference{LegacyCredentials: prefs.Selection("b")}}, Choice{Value: "b", Source: SourceRepo}, "", ""},
		{"profile choice", choiceCtx(map[string]string{"pi": "a"}), "pi", SelectionInputs{}, Choice{Value: "a", Source: SourceProfile}, "", ""},
		{"stale repo choice falls to the profile", choiceCtx(map[string]string{"claude": "a"}), "claude",
			SelectionInputs{Saved: saved("claude", "gone")}, Choice{Value: "a", Source: SourceProfile}, `"gone"`, ""},
		{"stale profile choice is unset", choiceCtx(map[string]string{"claude": "c"}), "claude", SelectionInputs{},
			Choice{Source: SourceUnset}, `"c"`, ""},
		{"no candidates", choiceCtx(nil), "omp", SelectionInputs{}, Choice{Value: "none", Source: SourceNoCandidates}, "", ""},
		{"unset slot stays unset in a preview", choiceCtx(nil), "claude", SelectionInputs{}, Choice{Source: SourceUnset}, "", ""},
		{"harness without a gateway reach", choiceCtx(nil), "gemini", SelectionInputs{}, Choice{}, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, notices, err := ResolveChoice(&config.Config{}, "work", tt.ctx, tt.harness, tt.in, nil)
			if (err != nil) != (tt.wantErr != "") || err != nil && !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want %q", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("Choice = %+v, want %+v", got, tt.want)
			}
			if (tt.wantNotice == "") != (len(notices) == 0) || tt.wantNotice != "" && !strings.Contains(strings.Join(notices, "\n"), tt.wantNotice) {
				t.Errorf("notices = %q, want one with %q", notices, tt.wantNotice)
			}
		})
	}
}

func TestResolveChoice_LegacyDerivedLosesToExplicitChoice(t *testing.T) {
	ctx := config.Context{Gateways: map[string]config.GatewayConfig{
		"corp": {BaseURL: "https://corp.example.com", Store: "s", Key: "k", Harnesses: []string{"claude"}},
	}}
	ctx.ConvertedGateways, _ = config.ConvertLegacyGateways(ctx)
	got, _, _ := ResolveChoice(&config.Config{}, "work", ctx, "claude", SelectionInputs{}, nil)
	if got.Value != "corp" || !got.Derived || got.Source != SourceProfile {
		t.Errorf("legacy choice = %+v, want derived corp", got)
	}
	ctx.GatewayChoice = map[string]string{"claude": "none"}
	got, _, _ = ResolveChoice(&config.Config{}, "work", ctx, "claude", SelectionInputs{}, nil)
	if got.Value != "none" || got.Derived {
		t.Errorf("explicit choice = %+v, want none, not derived", got)
	}
}

func TestResolveChoice_Asking(t *testing.T) {
	var asked []string
	ask := func(h, _ string, options []ui.PickerOption) (string, ui.Remember, error) {
		asked = append(asked, h+":"+options[0].Name)
		return "a", ui.RememberRepo, nil
	}
	got, _, err := ResolveChoice(&config.Config{}, "work", choiceCtx(nil), "claude", SelectionInputs{}, ask)
	if err != nil || got != (Choice{Value: "a", Source: SourcePicker, Saved: ui.RememberRepo}) || !slices.Equal(asked, []string{"claude:a"}) {
		t.Errorf("picked = %+v, %v, asked %v", got, err, asked)
	}

	asked = nil
	got, _, _ = ResolveChoice(&config.Config{}, "work", choiceCtx(nil), "pi", SelectionInputs{}, ask)
	if len(asked) != 0 || got != (Choice{Source: SourceUnset}) {
		t.Errorf("multi harness asked %v and got %+v, want it to start on its own default", asked, got)
	}

	_, _, err = ResolveChoice(&config.Config{}, "work", choiceCtx(nil), "claude", SelectionInputs{},
		func(string, string, []ui.PickerOption) (string, ui.Remember, error) { return "", "", ErrNoTerminal })
	var ee interface{ ExitCode() int }
	if !errors.As(err, &ee) || ee.ExitCode() != 78 {
		t.Fatalf("error = %v, want exit code 78", err)
	}
	for _, want := range []string{"a, b", "aide gateway use <name|none> --harness claude", "--gateway <name|none>"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
}

func TestPlanProfileCredentials_InjectsByReach(t *testing.T) {
	cfg := &config.Config{}
	plan := func(agent string, choice map[string]string) CredentialPlan {
		ctx := choiceCtx(choice)
		ctx.Agent = agent
		sel := Selection{}
		_, _, p, err := PlanProfileCredentials(cfg, &aidectx.ResolvedContext{Name: "work", Context: ctx}, SelectionInputs{}, &sel)
		if err != nil {
			t.Fatalf("%s: %v", agent, err)
		}
		return p
	}
	names := func(p CredentialPlan) []string {
		var out []string
		for _, b := range p.Bindings {
			out = append(out, b.Name)
		}
		return out
	}

	if got := names(plan("claude", map[string]string{"claude": "a"})); !slices.Equal(got, []string{"a"}) {
		t.Errorf("slot harness injects %v, want only the chosen gateway", got)
	}
	if got := names(plan("claude", map[string]string{"claude": "none"})); len(got) != 0 {
		t.Errorf("none injects %v", got)
	}
	if got := names(plan("claude", nil)); len(got) != 0 {
		t.Errorf("an unset preview injects %v", got)
	}
	p := plan("pi", map[string]string{"pi": "a"})
	if got := p.Bindings; len(got) != 1 || got[0].Env[0] != "AIDE_GW_A_KEY" || got[0].Optional {
		t.Errorf("pi plan = %+v, want a's key as AIDE_GW_A_KEY", got)
	}
	cp := plan("codex", map[string]string{"codex": "c"})
	if len(cp.Bindings) != 1 || cp.Bindings[0].Env[0] != "AIDE_GW_C_KEY" {
		t.Errorf("codex plan = %+v", cp.Bindings)
	}
	cl := plan("claude", map[string]string{"claude": "b"})
	if len(cl.Bindings) != 1 || cl.Bindings[0].Set["ANTHROPIC_AUTH_TOKEN"] != config.KeylessPlaceholder {
		t.Errorf("keyless claude plan = %+v", cl.Bindings)
	}
}

func TestResolveCredentialEnv_WithholdsOptionalGateway(t *testing.T) {
	plan := CredentialPlan{Bindings: []PlannedCredential{
		{Name: "a", Env: []string{"AIDE_GW_A_KEY"}, Store: "ok", Key: "k"},
		{Name: "b", Env: []string{"AIDE_GW_B_KEY"}, Store: "broken", Key: "k", Optional: true},
	}}
	load := func(path string) (map[string]string, error) {
		if strings.HasSuffix(path, "broken") || strings.HasSuffix(path, "broken.enc.yaml") {
			return nil, errors.New("cannot open")
		}
		return map[string]string{"k": "secret"}, nil
	}
	env, withheld, err := resolveCredentialEnv(plan, load)
	if err != nil || env["AIDE_GW_A_KEY"] != "secret" || env["AIDE_GW_B_KEY"] != "" || withheld["b"] == nil {
		t.Errorf("env = %v, withheld = %v, err = %v", env, withheld, err)
	}
	plan.Bindings[0].Store = "broken"
	if _, _, err := resolveCredentialEnv(plan, load); err == nil {
		t.Error("a required gateway whose store fails must stop the launch")
	}
}

func TestChoicePrompter_RemembersWhereTheUserSays(t *testing.T) {
	configDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(configDir, "profiles"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"config.yaml": "default_profile: work\n", "profiles/work.yaml": "agent: claude\n"} {
		if err := os.WriteFile(filepath.Join(configDir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	prefsPath := filepath.Join(t.TempDir(), "preferences.json")
	options := []ui.PickerOption{{Name: "a", URL: "https://a.example.com"}}
	ask := func(answers string) (*Launcher, string, ui.Remember, error) {
		var stderr bytes.Buffer
		l := &Launcher{ConfigDir: configDir, PrefsPath: prefsPath, Interactive: true, StdinIsTTY: func() bool { return true },
			Stdin: strings.NewReader(answers), Stderr: &stderr}
		v, saved, err := l.choicePrompter(t.TempDir(), t.TempDir())("claude", "work", options)
		return l, v, saved, err
	}

	if _, v, saved, err := ask("1\n1\n"); err != nil || v != "a" || saved != "" {
		t.Errorf("once = %q, %q, %v", v, saved, err)
	}
	if _, v, saved, err := ask("1\n2\n"); err != nil || v != "a" || saved != ui.RememberProfile {
		t.Fatalf("profile (no repo remote) = %q, %q, %v", v, saved, err)
	}
	fresh, err := config.Load(configDir, "")
	if err != nil || fresh.Contexts["work"].GatewayChoice["claude"] != "a" {
		t.Errorf("profile gateway_choice = %v, %v; want claude: a saved", fresh.Contexts["work"].GatewayChoice, err)
	}

	l := &Launcher{Interactive: false}
	if _, _, err := l.choicePrompter("", "")("claude", "work", options); !errors.Is(err, ErrNoTerminal) {
		t.Errorf("off a terminal error = %v, want ErrNoTerminal", err)
	}
}

func TestRememberChoice_RepoPreference(t *testing.T) {
	prefsPath := filepath.Join(t.TempDir(), "preferences.json")
	l := &Launcher{PrefsPath: prefsPath}
	key := prefs.Key{Profile: "work", Remote: "github.com/o/r"}
	if err := l.rememberChoice(ui.RememberRepo, key, "claude", "a", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	f, err := prefs.Load(prefsPath)
	if err != nil {
		t.Fatal(err)
	}
	if p, _ := f.Get(key); p.Gateway["claude"] != "a" {
		t.Errorf("saved preference = %+v", p)
	}
}

func TestLaunch_GatewayChoiceDelivery(t *testing.T) {
	fakeGatewayStore(t)
	gs := newGatewayServer(t, `{"data":[{"id":"m"}]}`)
	cfg := func(extra string) string {
		return fmt.Sprintf(`
profiles:
  work:
    agent: pi
    config_dir_name: work
    gateways:
      alpha:
        base_url: %[1]s
        store: work
        key: corp_token
        model: alpha-model
      beta:
        base_url: %[1]s
        store: work
        key: beta_token
        harnesses: [claude, pi]
%[2]sdefault_profile: work
`, gs.URL, extra)
	}
	has := func(env []string, k string) bool { _, ok := envValue(env, k); return ok }

	t.Run("pi injects every marked gateway and starts on the choice", func(t *testing.T) {
		env, args, stderr := launchGatewayArgs(t, t.TempDir(), cfg("    gateway_choice:\n      pi: alpha\n"), "pi", "")
		if !has(env, "AIDE_GW_ALPHA_KEY") || !has(env, "AIDE_GW_BETA_KEY") {
			t.Errorf("env lacks a marked gateway's key; stderr:\n%s", stderr)
		}
		if !slices.Contains(args, "aide-alpha") || !slices.Contains(args, "alpha-model") {
			t.Errorf("pi args = %v, want --provider aide-alpha --model alpha-model", args)
		}
	})
	t.Run("an explicit choice beats the legacy rule", func(t *testing.T) {
		env, _, _ := launchGatewayArgs(t, t.TempDir(), cfg("    gateway_choice:\n      claude: beta\n"), "claude", "")
		if got, _ := envValue(env, "ANTHROPIC_BASE_URL"); got == "" {
			t.Error("claude was not routed to beta")
		}
		env, _, _ = launchGatewayArgs(t, t.TempDir(), cfg("    gateway_choice:\n      claude: none\n"), "claude", "")
		if has(env, "ANTHROPIC_BASE_URL") || has(env, "ANTHROPIC_AUTH_TOKEN") {
			t.Error("claude none still injected a gateway")
		}
	})
	t.Run("codex starts on its split gateway", func(t *testing.T) {
		env, args, _ := launchGatewayArgs(t, t.TempDir(), cfg(""), "codex", "")
		if !slices.Contains(args, "model_provider=aide-alpha-codex") {
			t.Errorf("codex args = %v, want the split gateway as model_provider", args)
		}
		if !has(env, "AIDE_GW_ALPHA_CODEX_KEY") || has(env, "AIDE_GW_BETA_KEY") {
			t.Errorf("codex injects the wrong keys: %v", env)
		}
		if data, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".codex-work", "config.toml")); err != nil || !strings.Contains(string(data), "AIDE_GW_ALPHA_CODEX_KEY") {
			t.Errorf("codex config.toml = %s, %v", data, err)
		}
	})
}

func TestMergeChoiceEnv(t *testing.T) {
	const key = "OPENCODE_CONFIG_CONTENT"
	choice := map[string]string{key: `{"model":"aide-a/m"}`}
	tests := []struct {
		name     string
		env      []string
		explicit bool
		blocked  []string
		want     string // "" means the variable is absent
		stderr   string
	}{
		{"unset", []string{"A=1"}, false, nil, `{"model":"aide-a/m"}`, ""},
		{"blocked", []string{"A=1"}, true, []string{key}, "", "never_allow_env"},
		{"flag replaces only model", []string{key + `={"model":"x","theme":"t"}`}, true, nil, `{"model":"aide-a/m","theme":"t"}`, "replaces"},
		{"saved choice yields", []string{key + `={"model":"x"}`}, false, nil, `{"model":"x"}`, "keeps"},
		{"invalid json", []string{key + "=nope"}, true, nil, "nope", "not a JSON object"},
		{"non-object", []string{key + "=[1]"}, true, nil, "[1]", "not a JSON object"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			l := &Launcher{Stderr: &stderr}
			got := envSliceToMap(l.mergeChoiceEnv(tt.env, choice, tt.explicit, tt.blocked))[key]
			if got != tt.want {
				t.Errorf("%s = %q, want %q", key, got, tt.want)
			}
			if !strings.Contains(stderr.String(), tt.stderr) || (tt.stderr == "" && stderr.Len() > 0) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.stderr)
			}
		})
	}
}

func TestLaunch_OpenCodeStartsOnChoice(t *testing.T) {
	fakeGatewayStore(t)
	gs := newGatewayServer(t, `{"data":[{"id":"m"}]}`)
	cfg := fmt.Sprintf(`
profiles:
  work:
    agent: opencode
    config_dir_name: work
    gateways:
      alpha:
        base_url: %s
        store: work
        key: corp_token
        model: alpha-model
        harnesses: [opencode]
    gateway_choice:
      opencode: alpha
default_profile: work
`, gs.URL)
	env, args, stderr := launchGatewayArgs(t, t.TempDir(), cfg, "opencode", "")
	if got, _ := envValue(env, "OPENCODE_CONFIG_CONTENT"); got != `{"model":"aide-alpha/alpha-model"}` {
		t.Errorf("OPENCODE_CONFIG_CONTENT = %q; stderr:\n%s", got, stderr)
	}
	if !slices.Contains(args, "--standalone") {
		t.Errorf("opencode args = %v, want --standalone so the choice env reaches the session", args)
	}
	if slices.Contains(args, "aide-alpha") {
		t.Errorf("opencode args = %v, want no startup flags", args)
	}
}

func TestResolveChoice_SavedNonGatewaySelectionIsNone(t *testing.T) {
	ctx := config.Context{Gateways: map[string]config.GatewayConfig{
		"corp": {BaseURL: "https://corp.example.com", Store: "s", Key: "k", Harnesses: []string{"claude"}},
	}}
	ctx.ConvertedGateways, _ = config.ConvertLegacyGateways(ctx)
	ctx.Agent = "claude"
	ctx.Credentials = map[string]config.CredentialBinding{"anthropic": {Store: "s", Key: "ak"}}
	in := SelectionInputs{Saved: prefs.Preference{Harness: "claude", LegacyCredentials: prefs.Selection("anthropic")}}
	got, _, err := ResolveChoice(&config.Config{}, "work", ctx, "claude", in, nil)
	if err != nil || got.Value != "none" || got.Source != SourceRepo {
		t.Fatalf("choice = %+v, %v, want repo none", got, err)
	}
	sel := Selection{}
	if _, _, _, err := PlanProfileCredentials(&config.Config{}, &aidectx.ResolvedContext{Name: "work", Context: ctx}, in, &sel); err != nil {
		t.Errorf("PlanProfileCredentials: %v", err)
	}
}
