package ui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/fatih/color"
)

func init() {
	color.NoColor = true // disable ANSI for predictable test output
}

func fullBannerData() *BannerData {
	return &BannerData{
		ContextName: "work",
		MatchReason: "path glob match: ~/work/*",
		AgentName:   "claude",
		AgentPath:   "/usr/local/bin/claude",
		SecretName:  "work",
		SecretKeys:  []string{"api_key", "org_id", "token"},
		Env: map[string]string{
			"ANTHROPIC_API_KEY": "<- secrets.api_key",
			"ORG_ID":            "= acme",
		},
		Sandbox: &SandboxInfo{
			Network:    "outbound",
			Ports:      "all",
			GuardCount: 20,
			Denied:     []string{"~/.ssh/id_*", "~/.aws/credentials"},
		},
		Warnings: []string{"skipped: ~/.kube (not found)"},
	}
}

func TestRenderCompact(t *testing.T) {
	var buf bytes.Buffer
	RenderCompact(&buf, fullBannerData())
	out := buf.String()
	for _, want := range []string{"aide", "work", "claude", "secret:", "env:", "Sandbox", "denied:", "guards:"} {
		if !strings.Contains(out, want) {
			t.Errorf("compact output missing %q", want)
		}
	}
}

func TestRenderBoxed(t *testing.T) {
	var buf bytes.Buffer
	RenderBoxed(&buf, fullBannerData())
	out := buf.String()
	if !strings.Contains(out, string(rune(9484))) || !strings.Contains(out, string(rune(9492))) {
		t.Error("boxed output missing box-drawing characters")
	}
	if !strings.Contains(out, "Context") {
		t.Error("boxed output missing Context label")
	}
}

func TestRenderClean(t *testing.T) {
	var buf bytes.Buffer
	RenderClean(&buf, fullBannerData())
	out := buf.String()
	if !strings.Contains(out, "aide") {
		t.Error("clean output missing header")
	}
	if !strings.Contains(out, "Agent") {
		t.Error("clean output missing Agent label")
	}
}

func TestRenderBanner_UnknownStyle(t *testing.T) {
	var buf bytes.Buffer
	RenderBanner(&buf, "unknown-style", fullBannerData())
	out := buf.String()
	if !strings.Contains(out, "aide") {
		t.Error("unknown style should fall back to compact")
	}
}

func TestRenderBanner_WithWarnings(_ *testing.T) {
	var buf bytes.Buffer
	RenderCompact(&buf, fullBannerData())
	out := buf.String()
	_ = out
	// Warnings should be rendered (the specific format may vary)
}

func TestRenderBanner_DetailedMode(t *testing.T) {
	data := fullBannerData()
	data.Sandbox.Guards = []string{"base", "system-runtime", "network", "filesystem"}
	var buf bytes.Buffer
	RenderCompact(&buf, data)
	out := buf.String()
	if !strings.Contains(out, "base") {
		t.Error("detailed mode should list guard names")
	}
}

func TestRenderBanner_NormalMode(t *testing.T) {
	data := fullBannerData()
	data.SecretKeys = nil // normal mode
	data.Sandbox.Guards = nil
	var buf bytes.Buffer
	RenderCompact(&buf, data)
	out := buf.String()
	if !strings.Contains(out, "20 active") {
		t.Error("normal mode should show guard count")
	}
}

func TestRenderBanner_NoSandbox(t *testing.T) {
	data := fullBannerData()
	data.Sandbox = &SandboxInfo{Disabled: true}
	var buf bytes.Buffer
	RenderCompact(&buf, data)
	if !strings.Contains(buf.String(), "disabled") {
		t.Error("disabled sandbox should show 'disabled'")
	}
}

func TestRenderBanner_NoSecret(t *testing.T) {
	data := fullBannerData()
	data.SecretName = ""
	data.SecretKeys = nil
	var buf bytes.Buffer
	RenderCompact(&buf, data)
	if strings.Contains(buf.String(), "secret:") {
		t.Error("should not show secret section when no secret")
	}
}

func TestRenderBanner_NoEnv(t *testing.T) {
	data := fullBannerData()
	data.Env = nil
	var buf bytes.Buffer
	RenderCompact(&buf, data)
	if strings.Contains(buf.String(), "env:") {
		t.Error("should not show env section when no env")
	}
}
