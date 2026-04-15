package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jskswamy/aide/internal/consent"
)

// runCapCmd builds a fresh `cap` cobra command, runs it with args, and
// returns combined stdout/stderr. The working directory is redirected
// to a tempdir so config.Load does not pick up a user's local
// .aide.yaml and pollute results.
func runCapCmd(t *testing.T, args ...string) string {
	t.Helper()
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
