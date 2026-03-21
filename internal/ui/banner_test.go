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
			"ANTHROPIC_API_KEY": "← secrets.api_key",
			"ORG_ID":            "= acme",
		},
		Sandbox: &SandboxInfo{
			Network:       "outbound",
			Ports:         "all",
			WritableCount: 3,
			ReadableCount: 2,
			Denied:        []string{"~/.ssh/id_*", "~/.aws/credentials"},
		},
		Warnings: []string{"skipped: ~/.kube (not found)"},
	}
}

func TestRenderCompact(t *testing.T) {
	var buf bytes.Buffer
	RenderCompact(&buf, fullBannerData())
	out := buf.String()
	for _, want := range []string{"aide", "work", "claude", "secret:", "env:", "sandbox:", "denied:", "writable:"} {
		if !strings.Contains(out, want) {
			t.Errorf("compact output missing %q", want)
		}
	}
}

func TestRenderBoxed(t *testing.T) {
	var buf bytes.Buffer
	RenderBoxed(&buf, fullBannerData())
	out := buf.String()
	if !strings.Contains(out, "┌") || !strings.Contains(out, "└") {
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
	if !strings.Contains(out, "aide · context: work") {
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
	// Should fall back to compact (has emoji prefix)
	if !strings.Contains(out, "🔧") {
		t.Error("unknown style should fall back to compact")
	}
}

func TestRenderBanner_WithWarnings(t *testing.T) {
	var buf bytes.Buffer
	RenderCompact(&buf, fullBannerData())
	if !strings.Contains(buf.String(), "⚠") {
		t.Error("warnings should show ⚠ marker")
	}
}

func TestRenderBanner_DetailedMode(t *testing.T) {
	data := fullBannerData()
	data.Sandbox.Writable = []string{"/project", "/tmp", "/run/aide"}
	data.Sandbox.Readable = []string{"/home/user"}
	var buf bytes.Buffer
	RenderCompact(&buf, data)
	out := buf.String()
	if !strings.Contains(out, "/project") {
		t.Error("detailed mode should list writable paths")
	}
}

func TestRenderBanner_NormalMode(t *testing.T) {
	data := fullBannerData()
	data.SecretKeys = nil // normal mode
	data.Sandbox.Writable = nil
	var buf bytes.Buffer
	RenderCompact(&buf, data)
	out := buf.String()
	if !strings.Contains(out, "3 paths") {
		t.Error("normal mode should show writable count")
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
