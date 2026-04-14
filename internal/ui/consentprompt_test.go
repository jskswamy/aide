package ui

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/capability"
	"github.com/jskswamy/aide/internal/consent"
)

func TestRenderPrompt_FirstTime_OmitsPrevious(t *testing.T) {
	in := capability.PromptInput{
		Capability: "python",
		DetectedVariants: []capability.Variant{
			{
				Name:     "uv",
				Writable: []string{"~/.local/share/uv"},
				EnvAllow: []string{"UV_CACHE_DIR"},
				Markers:  []capability.Marker{{File: "uv.lock"}},
			},
		},
		PreviousVariants: nil,
		Evidence: consent.Evidence{
			Variants: []string{"uv"},
			Matches:  []consent.MarkerMatch{{Kind: "file", Target: "uv.lock", Matched: true}},
		},
	}
	out := RenderPrompt(in)
	if strings.Contains(out, "Previously:") {
		t.Errorf("first-time prompt should not contain 'Previously:', got:\n%s", out)
	}
	if !strings.Contains(out, "Detected:") {
		t.Errorf("missing 'Detected:' line")
	}
	if !strings.Contains(out, "uv") {
		t.Errorf("missing variant 'uv'")
	}
	if !strings.Contains(out, "~/.local/share/uv") {
		t.Errorf("missing granted path")
	}
	if !strings.Contains(out, "UV_CACHE_DIR") {
		t.Errorf("missing env var line")
	}
}

func TestRenderPrompt_Change_IncludesPrevious(t *testing.T) {
	in := capability.PromptInput{
		Capability: "python",
		DetectedVariants: []capability.Variant{
			{
				Name:     "conda",
				Writable: []string{"~/.conda"},
				Markers:  []capability.Marker{{File: "environment.yml"}},
			},
		},
		PreviousVariants: []string{"uv"},
		Evidence:         consent.Evidence{Variants: []string{"conda"}},
	}
	out := RenderPrompt(in)
	if !strings.Contains(out, "Previously: uv") {
		t.Errorf("missing 'Previously: uv', got:\n%s", out)
	}
}

func TestRenderPrompt_MultiVariant_ListsAllTogether(t *testing.T) {
	in := capability.PromptInput{
		Capability: "node",
		DetectedVariants: []capability.Variant{
			{Name: "pnpm", Writable: []string{"~/.local/share/pnpm"}, Markers: []capability.Marker{{File: "pnpm-lock.yaml"}}},
			{Name: "corepack", Writable: []string{"~/.cache/node/corepack"}},
		},
		Evidence: consent.Evidence{Variants: []string{"pnpm", "corepack"}},
	}
	out := RenderPrompt(in)
	if !strings.Contains(out, "pnpm") || !strings.Contains(out, "corepack") {
		t.Errorf("multi-variant render missing a name; got:\n%s", out)
	}
	if !strings.Contains(out, "pnpm-lock.yaml") {
		t.Errorf("missing marker summary")
	}
}

func TestTTYPrompter_Yes(t *testing.T) {
	in := bytes.NewBufferString("Y\n")
	var out bytes.Buffer
	p := NewTTYPrompter(in, &out)
	res := p.PromptVariantConsent(capability.PromptInput{
		Capability:       "python",
		DetectedVariants: []capability.Variant{{Name: "uv"}},
	})
	if res.Decision != capability.PromptYes {
		t.Errorf("decision = %v, want PromptYes", res.Decision)
	}
	if len(res.Variants) != 1 || res.Variants[0] != "uv" {
		t.Errorf("Variants = %v, want [uv]", res.Variants)
	}
}

func TestTTYPrompter_No(t *testing.T) {
	p := NewTTYPrompter(bytes.NewBufferString("N\n"), io.Discard)
	res := p.PromptVariantConsent(capability.PromptInput{
		Capability:       "python",
		DetectedVariants: []capability.Variant{{Name: "uv"}},
	})
	if res.Decision != capability.PromptNo {
		t.Errorf("decision = %v, want PromptNo", res.Decision)
	}
}

func TestTTYPrompter_Skip(t *testing.T) {
	p := NewTTYPrompter(bytes.NewBufferString("S\n"), io.Discard)
	res := p.PromptVariantConsent(capability.PromptInput{
		Capability:       "python",
		DetectedVariants: []capability.Variant{{Name: "uv"}},
	})
	if res.Decision != capability.PromptSkip {
		t.Errorf("decision = %v, want PromptSkip", res.Decision)
	}
}

func TestTTYPrompter_DetailsThenYes(t *testing.T) {
	in := bytes.NewBufferString("D\nY\n")
	var out bytes.Buffer
	p := NewTTYPrompter(in, &out)
	res := p.PromptVariantConsent(capability.PromptInput{
		Capability: "python",
		DetectedVariants: []capability.Variant{
			{
				Name:        "uv",
				Description: "uv desc",
				Writable:    []string{"~/.local/share/uv"},
				Markers:     []capability.Marker{{File: "uv.lock"}},
			},
		},
	})
	if res.Decision != capability.PromptYes {
		t.Errorf("decision = %v, want PromptYes after D->Y", res.Decision)
	}
	rendered := out.String()
	if !strings.Contains(rendered, "uv desc") {
		t.Errorf("Details expansion missing variant description; out:\n%s", rendered)
	}
}

func TestTTYPrompter_CustomizeSelectsSubset(t *testing.T) {
	in := bytes.NewBufferString("C\ny\nn\n")
	p := NewTTYPrompter(in, io.Discard)
	res := p.PromptVariantConsent(capability.PromptInput{
		Capability:       "python",
		DetectedVariants: []capability.Variant{{Name: "uv"}, {Name: "corepack"}},
	})
	if res.Decision != capability.PromptYes {
		t.Errorf("decision = %v, want PromptYes (from Customize)", res.Decision)
	}
	if len(res.Variants) != 1 || res.Variants[0] != "uv" {
		t.Errorf("Variants = %v, want [uv]", res.Variants)
	}
}

func TestTTYPrompter_CustomizeAllNoBecomesPromptNo(t *testing.T) {
	in := bytes.NewBufferString("C\nn\nn\n")
	p := NewTTYPrompter(in, io.Discard)
	res := p.PromptVariantConsent(capability.PromptInput{
		Capability:       "python",
		DetectedVariants: []capability.Variant{{Name: "uv"}, {Name: "corepack"}},
	})
	if res.Decision != capability.PromptNo {
		t.Errorf("decision = %v, want PromptNo when all variants refused", res.Decision)
	}
}

func TestTTYPrompter_UnknownAnswerRetries(t *testing.T) {
	in := bytes.NewBufferString("?\nY\n")
	var out bytes.Buffer
	p := NewTTYPrompter(in, &out)
	res := p.PromptVariantConsent(capability.PromptInput{
		Capability:       "python",
		DetectedVariants: []capability.Variant{{Name: "uv"}},
	})
	if res.Decision != capability.PromptYes {
		t.Errorf("decision = %v, want PromptYes after retry", res.Decision)
	}
	if !strings.Contains(out.String(), "please answer") {
		t.Errorf("retry message missing; out:\n%s", out.String())
	}
}

func TestTTYPrompter_EOFTreatedAsNo(t *testing.T) {
	p := NewTTYPrompter(bytes.NewBufferString(""), io.Discard)
	res := p.PromptVariantConsent(capability.PromptInput{
		Capability:       "python",
		DetectedVariants: []capability.Variant{{Name: "uv"}},
	})
	if res.Decision != capability.PromptNo {
		t.Errorf("decision = %v, want PromptNo on EOF", res.Decision)
	}
}
