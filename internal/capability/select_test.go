package capability

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jskswamy/aide/internal/consent"
)

type fakePrompter struct {
	returnedVariants []string
	called           int
}

func (f *fakePrompter) PromptVariantConsent(_ PromptInput) PromptResult {
	f.called++
	return PromptResult{
		Decision: PromptYes,
		Variants: f.returnedVariants,
	}
}

type fakePrompterNo struct{}

func (f *fakePrompterNo) PromptVariantConsent(_ PromptInput) PromptResult {
	return PromptResult{Decision: PromptNo}
}

type fakePrompterSkip struct{}

func (f *fakePrompterSkip) PromptVariantConsent(_ PromptInput) PromptResult {
	return PromptResult{Decision: PromptSkip}
}

func newTestCap() Capability {
	return Capability{
		Name: "python",
		Variants: []Variant{
			{Name: "uv", Markers: []Marker{{File: "uv.lock"}}},
			{Name: "pyenv", Markers: []Marker{{File: ".python-version"}}},
			{Name: "venv"},
		},
		DefaultVariants: []string{"venv"},
	}
}

// State A: first-time detection, prompter says Yes -> grant recorded, silent on repeat.
func TestSelect_StateA_FirstTime_PromptsAndGrants(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "uv.lock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	c := newTestCap()
	cstore := consent.NewStore(t.TempDir())
	p := &fakePrompter{returnedVariants: []string{"uv"}}

	got, prov, err := SelectVariants(SelectInput{
		Capability: c,
		ProjectRoot: dir,
		Consent:     cstore,
		Prompter:    p,
		Interactive: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "uv" {
		t.Errorf("selected = %v, want [uv]", got)
	}
	if prov.Reason != "consent:granted" {
		t.Errorf("Provenance.Reason = %q, want consent:granted", prov.Reason)
	}
	if p.called != 1 {
		t.Errorf("prompter call count = %d, want 1", p.called)
	}

	// Second call with same project must be silent and return consent:stable.
	p.called = 0
	got2, prov2, _ := SelectVariants(SelectInput{
		Capability: c, ProjectRoot: dir, Consent: cstore,
		Prompter: p, Interactive: true,
	})
	if p.called != 0 {
		t.Errorf("second call prompted again; should be silent")
	}
	if prov2.Reason != "consent:stable" {
		t.Errorf("second Provenance.Reason = %q, want consent:stable", prov2.Reason)
	}
	if len(got2) != 1 || got2[0].Name != "uv" {
		t.Errorf("second call selected = %v, want [uv]", got2)
	}
}

// State D: YAML pin bypasses consent flow entirely.
func TestSelect_StateD_YAMLPinBypassesConsent(t *testing.T) {
	c := newTestCap()
	cstore := consent.NewStore(t.TempDir())
	p := &fakePrompter{}
	got, prov, _ := SelectVariants(SelectInput{
		Capability: c, ProjectRoot: t.TempDir(),
		YAMLPins: []string{"uv"},
		Consent:  cstore, Prompter: p, Interactive: true,
	})
	if len(got) != 1 || got[0].Name != "uv" {
		t.Errorf("yaml pin not honored; got %v", got)
	}
	if p.called != 0 {
		t.Errorf("prompter called despite yaml pin")
	}
	if prov.Reason != "yaml-pin" {
		t.Errorf("Reason = %q, want yaml-pin", prov.Reason)
	}
}

// State E: CLI override wins over YAML pin and consent.
func TestSelect_StateE_CLIOverrideWins(t *testing.T) {
	c := newTestCap()
	cstore := consent.NewStore(t.TempDir())
	p := &fakePrompter{}
	got, prov, _ := SelectVariants(SelectInput{
		Capability: c, ProjectRoot: t.TempDir(),
		Overrides: []string{"pyenv"},
		YAMLPins:  []string{"uv"},
		Consent:   cstore, Prompter: p, Interactive: true,
	})
	if len(got) != 1 || got[0].Name != "pyenv" {
		t.Errorf("override not honored; got %v", got)
	}
	if p.called != 0 {
		t.Errorf("prompter called despite override")
	}
	if prov.Reason != "cli-override" {
		t.Errorf("Reason = %q, want cli-override", prov.Reason)
	}
}

// State C: evidence changes -> re-prompt.
func TestSelect_StateC_EvidenceChanged_Reprompts(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "uv.lock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	c := newTestCap()
	cstore := consent.NewStore(t.TempDir())
	p := &fakePrompter{returnedVariants: []string{"uv"}}

	if _, _, err := (SelectVariants(SelectInput{
		Capability: c, ProjectRoot: dir, Consent: cstore,
		Prompter: p, Interactive: true,
	})); err != nil {
		t.Fatal(err)
	}
	if p.called != 1 {
		t.Fatalf("initial prompt count = %d", p.called)
	}

	// Change evidence: remove uv.lock, add .python-version.
	if err := os.Remove(filepath.Join(dir, "uv.lock")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".python-version"), []byte("3.11"), 0o600); err != nil {
		t.Fatal(err)
	}
	p.returnedVariants = []string{"pyenv"}
	p.called = 0

	got, prov, _ := SelectVariants(SelectInput{
		Capability: c, ProjectRoot: dir, Consent: cstore,
		Prompter: p, Interactive: true,
	})
	if p.called != 1 {
		t.Errorf("evidence change did not re-prompt; called=%d", p.called)
	}
	if len(got) != 1 || got[0].Name != "pyenv" {
		t.Errorf("selected = %v, want [pyenv]", got)
	}
	if prov.Reason != "consent:granted" {
		t.Errorf("Reason = %q, want consent:granted", prov.Reason)
	}
}

// Non-interactive contexts always fall through to DefaultVariants.
func TestSelect_NonInteractive_FallsThroughToDefault(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "uv.lock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	c := newTestCap()
	cstore := consent.NewStore(t.TempDir())
	got, prov, err := SelectVariants(SelectInput{
		Capability: c, ProjectRoot: dir, Consent: cstore,
		Prompter: nil, Interactive: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "venv" {
		t.Errorf("fallback = %v, want [venv]", got)
	}
	if prov.Reason != "default:non-interactive" {
		t.Errorf("Reason = %q, want default:non-interactive", prov.Reason)
	}
}

func TestSelect_PromptNoKeepsDefault(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "uv.lock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	c := newTestCap()
	cstore := consent.NewStore(t.TempDir())
	got, prov, _ := SelectVariants(SelectInput{
		Capability: c, ProjectRoot: dir, Consent: cstore,
		Prompter: &fakePrompterNo{}, Interactive: true,
	})
	if len(got) != 1 || got[0].Name != "venv" {
		t.Errorf("got %v, want [venv] (No answer falls to default)", got)
	}
	if prov.Reason != "default:declined" {
		t.Errorf("Reason = %q, want default:declined", prov.Reason)
	}
}

func TestSelect_PromptSkipKeepsDefault(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "uv.lock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	c := newTestCap()
	cstore := consent.NewStore(t.TempDir())
	got, prov, _ := SelectVariants(SelectInput{
		Capability: c, ProjectRoot: dir, Consent: cstore,
		Prompter: &fakePrompterSkip{}, Interactive: true,
	})
	if len(got) != 1 || got[0].Name != "venv" {
		t.Errorf("got %v, want [venv] (Skip answer falls to default)", got)
	}
	if prov.Reason != "default:skipped" {
		t.Errorf("Reason = %q, want default:skipped", prov.Reason)
	}
}

// No markers fire -> DefaultVariants + no-evidence reason.
func TestSelect_NoEvidence_UsesDefault(t *testing.T) {
	dir := t.TempDir() // no marker files
	c := newTestCap()
	cstore := consent.NewStore(t.TempDir())
	got, prov, _ := SelectVariants(SelectInput{
		Capability: c, ProjectRoot: dir, Consent: cstore,
		Prompter: &fakePrompter{}, Interactive: true,
	})
	if len(got) != 1 || got[0].Name != "venv" {
		t.Errorf("got %v, want [venv]", got)
	}
	if prov.Reason != "default:no-evidence" {
		t.Errorf("Reason = %q, want default:no-evidence", prov.Reason)
	}
}

// AutoYes bypasses the prompter and records the full detected set.
func TestSelect_AutoYes_BypassesPrompt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "uv.lock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	c := newTestCap()
	cstore := consent.NewStore(t.TempDir())
	p := &fakePrompter{returnedVariants: []string{"uv"}}

	got, prov, _ := SelectVariants(SelectInput{
		Capability: c, ProjectRoot: dir, Consent: cstore,
		Prompter: p, Interactive: true, AutoYes: true,
	})
	if p.called != 0 {
		t.Errorf("AutoYes still called prompter: %d", p.called)
	}
	if len(got) != 1 || got[0].Name != "uv" {
		t.Errorf("AutoYes selected %v, want [uv]", got)
	}
	if prov.Reason != "consent:granted" {
		t.Errorf("Reason = %q, want consent:granted", prov.Reason)
	}
}

// Unknown variant in CLI override returns UnknownVariantError.
func TestSelect_UnknownVariant_InOverrides_ReturnsError(t *testing.T) {
	c := newTestCap()
	cstore := consent.NewStore(t.TempDir())
	_, _, err := SelectVariants(SelectInput{
		Capability: c, ProjectRoot: t.TempDir(),
		Overrides: []string{"nosuch"},
		Consent:   cstore, Interactive: true,
	})
	if err == nil {
		t.Fatal("want error, got nil")
	}
	var uve *UnknownVariantError
	if !errors.As(err, &uve) {
		t.Errorf("err = %v, want *UnknownVariantError", err)
	}
}
