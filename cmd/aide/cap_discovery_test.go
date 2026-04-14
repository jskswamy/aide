package main

import (
	"bytes"
	"strings"
	"testing"
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
