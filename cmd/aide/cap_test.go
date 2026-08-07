package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jskswamy/aide/internal/consent"
	"github.com/jskswamy/aide/internal/testutil"
)

// runCapCmd builds a fresh `cap` cobra command, runs it with args, and
// returns combined stdout/stderr. The working directory is redirected
// to a tempdir so config.Load does not pick up a user's local
// .aide.yaml and pollute results.
func runCapCmd(t *testing.T, args ...string) string {
	t.Helper()
	// Isolate from the user's real config (which may still be in v1 shape).
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Chdir(t.TempDir())
	var buf bytes.Buffer
	cmd := capCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("cap %v: %v\nout: %s", args, err, buf.String())
	}
	return buf.String()
}

func TestCapList_ShowsVariantHintForPython(t *testing.T) {
	out := runCapCmd(t, "list")
	if !strings.Contains(out, "python") {
		t.Fatalf("cap list missing python:\n%s", out)
	}
	if !strings.Contains(out, "5 variants") {
		t.Errorf("cap list missing variant count hint for python; got:\n%s", out)
	}
	for _, v := range []string{"uv", "pyenv", "conda", "poetry", "venv"} {
		if !strings.Contains(out, v) {
			t.Errorf("variant hint missing %q in output:\n%s", v, out)
		}
	}
}

func TestCapShow_ListsPythonVariants(t *testing.T) {
	out := runCapCmd(t, "show", "python")
	for _, v := range []string{"uv", "pyenv", "conda", "poetry", "venv"} {
		if !strings.Contains(out, v) {
			t.Errorf("cap show python missing variant %q; got:\n%s", v, out)
		}
	}
	if !strings.Contains(out, "uv.lock") || !strings.Contains(out, ".python-version") {
		t.Errorf("cap show python missing marker summaries; got:\n%s", out)
	}
}

// TestCapShow_DisplaysResolvedSymlinkTarget pins the AIDE-46h diagnostic UX.
// When a custom capability declares a symlinked path, `aide cap show` must
// surface BOTH the declared path and the EvalSymlinks-resolved target on
// the same line, so the user can audit what the sandbox actually grants
// before launching a session.
func TestCapShow_DisplaysResolvedSymlinkTarget(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	linkPath, target := testutil.MakeSymlinkedFile(t, home, ".config/foo/config.yaml", "real/config.yaml")

	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	aideCfg := filepath.Join(configHome, "aide", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(aideCfg), 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := "version: 2\ncapabilities:\n  custom-foo:\n    description: \"foo bar\"\n    readable:\n      - " + linkPath + "\n"
	if err := os.WriteFile(aideCfg, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())

	var buf bytes.Buffer
	cmd := capCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"show", "custom-foo"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("cap show: %v\nout: %s", err, buf.String())
	}
	out := buf.String()

	if !strings.Contains(out, linkPath) {
		t.Errorf("show output must list declared path %q; got:\n%s", linkPath, out)
	}
	if !strings.Contains(out, target) {
		t.Errorf("show output must surface resolved target %q (the path the sandbox actually matches against); got:\n%s", target, out)
	}
}

// TestCapShow_WarnsOnOutsideHomeResolution pins the warning marker for
// resolved targets that fall outside $HOME. Today the sandbox silently
// drops outside-$HOME widenings (a safety floor); the show output must
// make this visible so the user understands why their declared path
// will EPERM at runtime, and points them at the AIDE-mu8 escape hatch.
func TestCapShow_WarnsOnOutsideHomeResolution(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	t.Setenv("HOME", home)
	linkPath, outside := testutil.MakeSymlinkedFile(t, tmp, "home/escape-link", "outside-home/secret.txt")

	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	aideCfg := filepath.Join(configHome, "aide", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(aideCfg), 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := "version: 2\ncapabilities:\n  out-of-home:\n    description: \"escapes\"\n    readable:\n      - " + linkPath + "\n"
	if err := os.WriteFile(aideCfg, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())

	var buf bytes.Buffer
	cmd := capCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"show", "out-of-home"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("cap show: %v\nout: %s", err, buf.String())
	}
	out := buf.String()

	if !strings.Contains(out, outside) {
		t.Errorf("show output must surface the outside-$HOME resolved target %q; got:\n%s", outside, out)
	}
	if !strings.Contains(strings.ToLower(out), "outside") {
		t.Errorf("show output must warn that resolved target is outside $HOME; got:\n%s", out)
	}
}

func TestCapVariants_FlatList(t *testing.T) {
	out := runCapCmd(t, "variants")
	wants := []string{"python/uv", "python/pyenv", "python/conda", "python/poetry", "python/venv"}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("cap variants missing %q; got:\n%s", w, out)
		}
	}
}

// runCapConsent isolates the test to a fresh XDG root and executes the
// given "consent <...>" subcommand, returning combined stdout+stderr.
func runCapConsent(t *testing.T, projectDir string, args ...string) string {
	t.Helper()
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Chdir(projectDir)

	var buf bytes.Buffer
	cmd := capCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(append([]string{"consent"}, args...))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("cap consent %v: %v\nout: %s", args, err, buf.String())
	}
	return buf.String()
}

func seedConsent(t *testing.T, _, project string) {
	t.Helper()
	// approvalstore.DefaultRoot looks under XDG_DATA_HOME/aide
	store := consent.DefaultStore()
	err := store.Grant(consent.Grant{
		ProjectRoot: project,
		Capability:  "python",
		Variants:    []string{"uv"},
		Evidence: consent.Evidence{
			Variants: []string{"uv"},
			Matches: []consent.MarkerMatch{
				{Kind: "file", Target: "uv.lock", Matched: true},
			},
		},
		Summary:     "uv.lock",
		ConfirmedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("seed consent: %v", err)
	}
}

func TestCapConsentList_EmptyStore(t *testing.T) {
	project := t.TempDir()
	out := runCapConsent(t, project, "list")
	if !strings.Contains(out, "no consents") {
		t.Errorf("expected 'no consents' marker in empty list; got:\n%s", out)
	}
}

func TestCapConsentList_ShowsGrant(t *testing.T) {
	project := t.TempDir()
	// set XDG_DATA_HOME then seed + list in the same test env.
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Chdir(project)
	seedConsent(t, xdg, project)

	var buf bytes.Buffer
	cmd := capCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"consent", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("cap consent list: %v\nout: %s", err, buf.String())
	}
	out := buf.String()
	if !strings.Contains(out, "python") {
		t.Errorf("list missing 'python'; got:\n%s", out)
	}
	if !strings.Contains(out, "uv") {
		t.Errorf("list missing 'uv' variant; got:\n%s", out)
	}
}

func TestCapConsentRevoke_ClearsGrant(t *testing.T) {
	project := t.TempDir()
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Chdir(project)
	seedConsent(t, xdg, project)

	// Verify seed worked.
	list1 := listConsents(t)
	if !strings.Contains(list1, "python") {
		t.Fatalf("seed didn't produce a visible grant; got:\n%s", list1)
	}

	// Revoke
	var buf bytes.Buffer
	cmd := capCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"consent", "revoke", "python"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("cap consent revoke: %v\nout: %s", err, buf.String())
	}
	if !strings.Contains(buf.String(), "revoked") {
		t.Errorf("revoke output missing 'revoked' confirmation; got:\n%s", buf.String())
	}

	// Verify list now empty.
	list2 := listConsents(t)
	if !strings.Contains(list2, "no consents") {
		t.Errorf("after revoke, list did not report empty; got:\n%s", list2)
	}
}

func listConsents(t *testing.T) string {
	t.Helper()
	var buf bytes.Buffer
	cmd := capCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"consent", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("cap consent list: %v", err)
	}
	return buf.String()
}

func TestCapConsentList_ProjectFlag(t *testing.T) {
	other := t.TempDir()
	self := t.TempDir()
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Chdir(self)

	// Seed a grant belonging to the OTHER project.
	store := consent.DefaultStore()
	_ = store.Grant(consent.Grant{
		ProjectRoot: other,
		Capability:  "python",
		Variants:    []string{"uv"},
		Evidence:    consent.Evidence{Variants: []string{"uv"}, Matches: []consent.MarkerMatch{{Kind: "file", Target: "uv.lock", Matched: true}}},
		Summary:     "uv.lock",
	})

	// list without --project uses cwd (self) → should report empty.
	var bufSelf bytes.Buffer
	cmd := capCmd()
	cmd.SetOut(&bufSelf)
	cmd.SetErr(&bufSelf)
	cmd.SetArgs([]string{"consent", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("%v", err)
	}
	if !strings.Contains(bufSelf.String(), "no consents") {
		t.Errorf("cwd list not empty; got:\n%s", bufSelf.String())
	}

	// list --project <other> should see the grant.
	var bufOther bytes.Buffer
	cmd2 := capCmd()
	cmd2.SetOut(&bufOther)
	cmd2.SetErr(&bufOther)
	cmd2.SetArgs([]string{"consent", "list", "--project", other})
	if err := cmd2.Execute(); err != nil {
		t.Fatalf("%v", err)
	}
	if !strings.Contains(bufOther.String(), "python") {
		t.Errorf("list --project %s missing python; got:\n%s", other, bufOther.String())
	}

	// Ensure unused imports stay silent.
	_ = os.DirFS
	_ = filepath.Join
}

func TestCapAudit_ReflectsProjectOverrideDisabledCapability(t *testing.T) {
	dir := isolatedConfigDir(t)

	configYAML := `default_context: work
contexts:
  work:
    agent: claude
    capabilities: [ssh]
`
	if err := os.WriteFile(filepath.Join(dir, "xdg", "aide", "config.yaml"), []byte(configYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".aide.yaml"), []byte("disabled_capabilities: [ssh]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := runCapCmdInPlace(t, "audit")

	if strings.Contains(out, "ssh") {
		t.Errorf("expected ssh to be excluded by project override disabled_capabilities, got:\n%s", out)
	}
	if !strings.Contains(out, `Context "work" has no capabilities enabled.`) {
		t.Errorf("expected 'no capabilities enabled' message once ssh is disabled, got:\n%s", out)
	}
}

// runCapCmdInPlace builds a fresh `cap` cobra command and runs it in the
// CURRENT working directory (unlike runCapCmd, which chdirs to a fresh
// tempdir). Callers must have already set up an isolated cwd/config
// (e.g. via isolatedConfigDir) before calling this.
func runCapCmdInPlace(t *testing.T, args ...string) string {
	t.Helper()
	var buf bytes.Buffer
	cmd := capCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("cap %v: %v\nout: %s", args, err, buf.String())
	}
	return buf.String()
}

func TestCapListStatus_Precedence(t *testing.T) {
	enabled := map[string]bool{"clipboard": true}
	disabled := map[string]bool{"ssh": true}
	suggested := map[string]bool{"go": true}

	cases := []struct {
		name string
		want string
	}{
		{"clipboard", "enabled"},
		{"ssh", "disabled"},
		{"go", "suggested"},
		{"docker", "-"},
	}
	for _, c := range cases {
		got := capListStatus(c.name, enabled, disabled, suggested)
		if got != c.want {
			t.Errorf("capListStatus(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestCapList_ShowsStatusColumn(t *testing.T) {
	dir := isolatedConfigDir(t)

	configYAML := `default_context: work
contexts:
  work:
    agent: claude
    capabilities: [clipboard]
`
	if err := os.WriteFile(filepath.Join(dir, "xdg", "aide", "config.yaml"), []byte(configYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".aide.yaml"), []byte("disabled_capabilities: [docker]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := runCapCmdInPlace(t, "list")

	lines := strings.Split(out, "\n")
	if !strings.HasPrefix(lines[0], "NAME") {
		t.Fatalf("expected header line starting with NAME, got %q", lines[0])
	}
	header := lines[0]
	nameIdx := strings.Index(header, "NAME")
	statusIdx := strings.Index(header, "STATUS")
	sourceIdx := strings.Index(header, "SOURCE")
	if !(nameIdx < statusIdx && statusIdx < sourceIdx) {
		t.Fatalf("expected column order NAME < STATUS < SOURCE, got header: %q", header)
	}

	findRow := func(capName string) string {
		for _, l := range lines {
			fields := strings.Fields(l)
			if len(fields) > 0 && fields[0] == capName {
				return l
			}
		}
		t.Fatalf("no row found for capability %q in output:\n%s", capName, out)
		return ""
	}

	clipboardRow := findRow("clipboard")
	if !strings.Contains(clipboardRow, "enabled") {
		t.Errorf("expected clipboard row to show 'enabled', got: %q", clipboardRow)
	}
	dockerRow := findRow("docker")
	if !strings.Contains(dockerRow, "disabled") {
		t.Errorf("expected docker row to show 'disabled', got: %q", dockerRow)
	}
	goRow := findRow("go")
	if !strings.Contains(goRow, "suggested") {
		t.Errorf("expected go row to show 'suggested' (go.mod present), got: %q", goRow)
	}
	awsRow := findRow("aws")
	fields := strings.Fields(awsRow)
	if len(fields) < 2 || fields[1] != "-" {
		t.Errorf("expected aws row status to be '-', got: %q", awsRow)
	}
}

func TestCapList_ContextFlagSkipsProjectOverride(t *testing.T) {
	dir := isolatedConfigDir(t)

	configYAML := `default_context: other
contexts:
  other:
    agent: claude
    capabilities: []
  work:
    agent: claude
    capabilities: [clipboard]
`
	if err := os.WriteFile(filepath.Join(dir, "xdg", "aide", "config.yaml"), []byte(configYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".aide.yaml"), []byte("disabled_capabilities: [clipboard]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := runCapCmdInPlace(t, "list", "--context", "work")

	lines := strings.Split(out, "\n")
	for _, l := range lines {
		f := strings.Fields(l)
		if len(f) > 0 && f[0] == "clipboard" {
			if len(f) < 2 || f[1] != "enabled" {
				t.Errorf("expected clipboard status 'enabled' for --context work (project override should not apply), got: %q", l)
			}
			return
		}
	}
	t.Fatalf("no clipboard row found in output:\n%s", out)
}
