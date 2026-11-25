package doctor

import (
	"bytes"
	"strings"
	"testing"
)

func TestNewReport_CountsOrdersAndExitCode(t *testing.T) {
	dir := &Directory{Profile: "work", Agent: "claude"}
	r := NewReport("dev", dir, []Finding{
		{ID: "config.no-match-rules", Subject: "old", Profile: "old", Severity: SeverityWarning, Message: "old never activates"},
		{ID: "capability.suggested", Subject: "go", Profile: "work", Severity: SeverityTip, Message: "go.mod found"},
		{ID: "config.unknown-agent", Subject: "work", Profile: "work", Severity: SeverityError, Message: "unknown agent"},
	})
	if r.Counts != (Counts{Errors: 1, Warnings: 1, Tips: 1}) {
		t.Errorf("counts = %+v", r.Counts)
	}
	var ids []string
	for _, f := range r.Findings {
		ids = append(ids, f.ID)
	}
	// This directory's findings first, worst first; then other profiles.
	want := "config.unknown-agent capability.suggested config.no-match-rules"
	if strings.Join(ids, " ") != want {
		t.Errorf("order = %v, want %s", ids, want)
	}
	if !r.HasErrors() {
		t.Error("HasErrors = false with an error finding")
	}
	if NewReport("dev", dir, nil).HasErrors() {
		t.Error("HasErrors = true with no findings")
	}
}

func TestReport_RenderText(t *testing.T) {
	dir := &Directory{Profile: "work", MatchReason: "path ~/src", Agent: "claude", Network: "outbound",
		Capabilities: []DirCapability{{Name: "python", Variants: []string{"uv"}}, {Name: "go"}},
		Guards:       []string{"base", "network"},
	}
	r := NewReport("dev", dir, []Finding{
		{ID: "config.unknown-agent", Subject: "work", Profile: "work", Severity: SeverityError, Message: "unknown agent", Fix: Fix{Steps: []string{"Add the agent"}}},
		{ID: "config.no-match-rules", Subject: "old", Profile: "old", Severity: SeverityWarning, Message: "old never activates"},
	})
	var b bytes.Buffer
	if err := r.RenderText(&b); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		"1 error, 1 warning, 0 tips",
		"work (path ~/src)", "claude", "python (uv), go", "2 guards", "outbound",
		"This directory", "✗ unknown agent", "Add the agent",
		"Other profiles", "⚠ old never activates",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("text output missing %q:\n%s", want, out)
		}
	}
}

func TestReport_RenderTextWithoutDirectory(t *testing.T) {
	r := NewReport("dev", nil, []Finding{{ID: "config.load-failed", Severity: SeverityError, Message: "config.yaml: bad"}})
	var b bytes.Buffer
	if err := r.RenderText(&b); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "config.yaml: bad") || strings.Contains(b.String(), "Profile") {
		t.Errorf("unexpected output:\n%s", b.String())
	}
}
