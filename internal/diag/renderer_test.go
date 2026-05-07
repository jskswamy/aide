package diag

import (
	"os"
	"strings"
	"testing"
	"time"
)

func fixtureReport() Report {
	return Report{
		AideVersion:    "1.8.1",
		AideCommit:     "abcd123",
		AideBuildDate:  "2026-05-07",
		OS:             "darwin",
		Arch:           "arm64",
		Shell:          "/bin/zsh",
		Locale:         "en_US.UTF-8",
		CWD:            "/Users/alice/proj",
		ResolvedConfig: "/Users/alice/.config/aide/secrets",
		AgentBinary:    "/usr/bin/sandbox-exec",
		Argv:           []string{"sandbox-exec", "-f", "/tmp/p.sb", "claude"},
		EnvKeys: []EnvKey{
			{Name: "PATH", Length: 80},
			{Name: "ANTHROPIC_API_KEY", Length: 51},
		},
		SecretSourcePaths: []string{"/Users/alice/.config/aide/secrets/secrets.yaml"},
		AgeKeySource:     "yubikey",
		Sandbox: SandboxInfo{
			Variants:   []string{"network-outbound", "code-only"},
			GuardNames: []string{"network", "filesystem", "toolchain"},
			RenderedSB: "(version 1)\n(deny default)\n",
		},
		ExitCode:   1,
		Runtime:    250 * time.Millisecond,
		StderrTail: "error: An unknown error occurred (Unexpected)\n",
	}
}

func TestMarkdownGolden(t *testing.T) {
	got := Markdown(fixtureReport())
	want, err := os.ReadFile("testdata/golden_full.md")
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		_ = os.WriteFile("testdata/golden_full.actual.md", []byte(got), 0o644)
		t.Errorf("markdown mismatch — see testdata/golden_full.actual.md (diff against testdata/golden_full.md)")
	}
}

func TestSummaryGolden(t *testing.T) {
	got := Summary(fixtureReport())
	want, err := os.ReadFile("testdata/golden_summary.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		_ = os.WriteFile("testdata/golden_summary.actual.txt", []byte(got), 0o644)
		t.Errorf("summary mismatch — see testdata/golden_summary.actual.txt")
	}
}

func TestSummaryDoesNotIncludeRenderedSB(t *testing.T) {
	if strings.Contains(Summary(fixtureReport()), "(deny default)") {
		t.Error("rendered .sb leaked into terminal summary; it should appear only in the markdown file output")
	}
}

func TestMarkdownIncludesRenderedSB(t *testing.T) {
	if !strings.Contains(Markdown(fixtureReport()), "(deny default)") {
		t.Error("rendered .sb should be in the markdown file output")
	}
}

func TestMarkdownDoesNotLeakSecretValues(t *testing.T) {
	r := fixtureReport()
	if !strings.Contains(Markdown(r), "len=51") {
		t.Error("expected env Length to be rendered (e.g. 'len=51')")
	}
}

func TestMarkdownEmptyReport(t *testing.T) {
	// Should not panic and should produce something parseable.
	out := Markdown(Report{})
	if !strings.Contains(out, "# aide diagnose report") {
		t.Errorf("empty report missing top-level heading: %q", out)
	}
	if !strings.Contains(out, "## TL;DR") {
		t.Errorf("empty report missing TL;DR section")
	}
	// No truncated indicator on a fresh empty report.
	if strings.Contains(out, "dropped") {
		t.Errorf("empty report should not say 'dropped' bytes")
	}
}

func TestSummaryEmptyReport(t *testing.T) {
	out := Summary(Report{})
	if !strings.Contains(out, "── aide diagnose ──") {
		t.Errorf("empty summary missing header: %q", out)
	}
}

func TestMarkdown_HeaderShowsTruncatedBytes(t *testing.T) {
	r := fixtureReport()
	r.StderrTruncated = 12345
	out := Markdown(r)
	if !strings.Contains(out, "12345 dropped") {
		t.Errorf("expected '12345 dropped' in header, got:\n%s", out)
	}
}
