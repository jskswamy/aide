package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/capability"
	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/consent"
)

// stubPrompter returns a canned decision without reading from any
// terminal. Used to simulate the consent prompt in an e2e flow.
type stubPrompter struct {
	decision capability.PromptDecision
	variants []string
	calls    int
}

func (p *stubPrompter) PromptVariantConsent(_ capability.PromptInput) capability.PromptResult {
	p.calls++
	return capability.PromptResult{Decision: p.decision, Variants: p.variants}
}

// TestPythonUV_EndToEnd walks the full stack for the canonical case
// that motivated this project: a Python project with uv.lock. We
// verify that (1) a non-interactive launch falls through to venv, (2)
// an AutoYes launch records a uv grant and applies uv's paths, (3) a
// subsequent call finds the grant and is silent, (4) the consent
// store exposes the recorded grant.
func TestPythonUV_EndToEnd(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "uv.lock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// Isolate the XDG root so the test can't pollute the developer's
	// real consent store.
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	cfg := &config.Config{}

	// Case 1: non-interactive, no AutoYes, no prompter — falls through to venv.
	// The stubPrompter zero value is intentional: PromptYes is iota 0 so the
	// zero value is a valid "Yes with no variants" stub, but this case
	// should never call the prompter anyway (Interactive=false).
	{
		store := consent.DefaultStore()
		stub := &stubPrompter{}
		_, overrides, _, err := ResolveCapabilitiesWithVariants(
			[]string{"python"}, cfg,
			VariantSelectionOptions{
				ProjectRoot: project,
				Consent:     store,
				Prompter:    stub,
				Interactive: false,
			},
		)
		if err != nil {
			t.Fatalf("non-interactive: %v", err)
		}
		if stub.calls != 0 {
			t.Errorf("non-interactive called prompter %d times; want 0", stub.calls)
		}
		// venv contributes VIRTUAL_ENV; uv paths must NOT be present.
		foundVirtualEnv := false
		for _, e := range overrides.EnvAllow {
			if e == "VIRTUAL_ENV" {
				foundVirtualEnv = true
			}
		}
		if !foundVirtualEnv {
			t.Errorf("venv default did not contribute VIRTUAL_ENV; EnvAllow=%v", overrides.EnvAllow)
		}
		for _, w := range overrides.WritableExtra {
			if strings.Contains(w, "uv") {
				t.Errorf("non-interactive fallback leaked uv path: %s", w)
			}
		}
	}

	// Case 2: AutoYes — auto-approve uv. Paths should include uv dirs;
	// consent store should record a grant.
	{
		store := consent.DefaultStore()
		stub := &stubPrompter{}
		_, overrides, _, err := ResolveCapabilitiesWithVariants(
			[]string{"python"}, cfg,
			VariantSelectionOptions{
				ProjectRoot: project,
				Consent:     store,
				Prompter:    stub,
				Interactive: true,
				AutoYes:     true,
			},
		)
		if err != nil {
			t.Fatalf("AutoYes: %v", err)
		}
		if stub.calls != 0 {
			t.Errorf("AutoYes called prompter %d times; want 0 (autoyes bypasses)", stub.calls)
		}
		foundUVPath := false
		for _, w := range overrides.WritableExtra {
			if w == "~/.local/share/uv" || w == "~/.cache/uv" {
				foundUVPath = true
			}
		}
		if !foundUVPath {
			t.Errorf("AutoYes did not merge uv paths; WritableExtra=%v", overrides.WritableExtra)
		}

		// Grant must be persisted.
		grants, err := store.List(project)
		if err != nil {
			t.Fatalf("consent.List: %v", err)
		}
		if len(grants) != 1 {
			t.Fatalf("expected 1 grant, got %d: %v", len(grants), grants)
		}
		g := grants[0]
		if g.Capability != "python" {
			t.Errorf("grant.Capability = %q, want python", g.Capability)
		}
		if len(g.Variants) != 1 || g.Variants[0] != "uv" {
			t.Errorf("grant.Variants = %v, want [uv]", g.Variants)
		}
	}

	// Case 3: re-run after AutoYes — stable consent, silent.
	{
		store := consent.DefaultStore()
		stub := &stubPrompter{}
		_, overrides, _, err := ResolveCapabilitiesWithVariants(
			[]string{"python"}, cfg,
			VariantSelectionOptions{
				ProjectRoot: project,
				Consent:     store,
				Prompter:    stub,
				Interactive: true,
			},
		)
		if err != nil {
			t.Fatalf("stable consent: %v", err)
		}
		if stub.calls != 0 {
			t.Errorf("stable-consent path called prompter %d times; want 0", stub.calls)
		}
		foundUVPath := false
		for _, w := range overrides.WritableExtra {
			if w == "~/.local/share/uv" {
				foundUVPath = true
			}
		}
		if !foundUVPath {
			t.Errorf("stable-consent did not apply uv path; WritableExtra=%v", overrides.WritableExtra)
		}
	}
}

// TestPythonUV_EvidenceChange_Reprompts verifies that when evidence
// changes after a grant, the consent store rejects the old grant
// (different digest) and the flow calls the prompter again.
func TestPythonUV_EvidenceChange_Reprompts(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "uv.lock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	cfg := &config.Config{}

	// First grant: AutoYes with uv.lock.
	store := consent.DefaultStore()
	yes := &stubPrompter{decision: capability.PromptYes, variants: []string{"uv"}}
	if _, _, _, err := ResolveCapabilitiesWithVariants(
		[]string{"python"}, cfg,
		VariantSelectionOptions{
			ProjectRoot: project, Consent: store,
			Prompter: yes, Interactive: true, AutoYes: true,
		},
	); err != nil {
		t.Fatal(err)
	}

	// Evidence changes: remove uv.lock, add environment.yml.
	if err := os.Remove(filepath.Join(project, "uv.lock")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "environment.yml"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	// Re-run interactively (no AutoYes). Prompter MUST be called.
	prompted := &stubPrompter{decision: capability.PromptYes, variants: []string{"conda"}}
	_, overrides, _, err := ResolveCapabilitiesWithVariants(
		[]string{"python"}, cfg,
		VariantSelectionOptions{
			ProjectRoot: project, Consent: store,
			Prompter: prompted, Interactive: true,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if prompted.calls != 1 {
		t.Errorf("evidence-change prompter calls = %d, want 1", prompted.calls)
	}
	foundConda := false
	for _, w := range overrides.WritableExtra {
		if w == "~/.conda" {
			foundConda = true
		}
	}
	if !foundConda {
		t.Errorf("conda path not merged after evidence change; WritableExtra=%v", overrides.WritableExtra)
	}
}

// TestPythonUV_CLIOverride_WinsOverYAMLAndDetection confirms the full
// precedence rule end-to-end: --variant wins over .aide.yaml pins and
// over auto-detection.
func TestPythonUV_CLIOverride_WinsOverYAMLAndDetection(t *testing.T) {
	project := t.TempDir()
	// uv.lock present → detection would normally pick uv.
	if err := os.WriteFile(filepath.Join(project, "uv.lock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	cfg := &config.Config{}
	store := consent.DefaultStore()

	// CLI overrides to pyenv; YAML pins conda; detection sees uv.
	_, overrides, _, err := ResolveCapabilitiesWithVariants(
		[]string{"python"}, cfg,
		VariantSelectionOptions{
			ProjectRoot:  project,
			CLIOverrides: map[string][]string{"python": {"pyenv"}},
			YAMLPins:     map[string][]string{"python": {"conda"}},
			Consent:      store,
			Interactive:  true,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	// pyenv contributes ~/.pyenv; conda contributes ~/.conda; uv
	// contributes ~/.local/share/uv. CLI override means pyenv should
	// be merged; the other two should not.
	foundPyenv, foundConda, foundUV := false, false, false
	for _, w := range overrides.WritableExtra {
		switch w {
		case "~/.pyenv":
			foundPyenv = true
		case "~/.conda", "~/miniconda3", "~/anaconda3":
			foundConda = true
		case "~/.local/share/uv", "~/.cache/uv":
			foundUV = true
		}
	}
	if !foundPyenv {
		t.Errorf("CLI-override pyenv path missing; WritableExtra=%v", overrides.WritableExtra)
	}
	if foundConda {
		t.Errorf("CLI override should have suppressed YAML conda pin")
	}
	if foundUV {
		t.Errorf("CLI override should have suppressed uv auto-detection")
	}
}
