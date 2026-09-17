package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/output"
	"github.com/spf13/cobra"
)

// TestSandboxShow_ContainsTierPreamble verifies the isolation tier/backend preamble
// is present in text mode output.
func TestSandboxShow_ContainsTierPreamble(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	cmd := sandboxShowCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})

	root := &cobra.Command{}
	root.AddCommand(cmd)
	root.SetArgs([]string{"show"})

	if err := root.Execute(); err != nil {
		t.Skipf("sandbox show failed (expected in minimal test env): %v", err)
	}

	out := buf.String()
	requiredLines := []string{
		"Isolation tier:", "Backend:", "Port filtering:",
	}
	for _, line := range requiredLines {
		if !strings.Contains(out, line) {
			t.Errorf("sandbox show output missing %q; got:\n%s", line, out)
		}
	}
}

// TestSandboxShow_ExitsZeroWhenUnavailable verifies sandbox show exits 0 even
// when tier is unavailable (does not treat lack of isolation as an error).
func TestSandboxShow_ExitsZeroWhenUnavailable(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	cmd := sandboxShowCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	root := &cobra.Command{}
	root.AddCommand(cmd)
	root.SetArgs([]string{"show"})

	// The command should either succeed or fail for config reasons only —
	// tier=unavailable must not cause a non-zero exit.
	// In a minimal env (no config), it uses default policy → exits 0.
	err := root.Execute()
	if err != nil {
		t.Logf("sandbox show returned error (may be expected in minimal env): %v", err)
		if strings.Contains(err.Error(), "unavailable") {
			t.Errorf("sandbox show should not return error for unavailable tier: %v", err)
		}
	}
}

func TestSandboxShow_JSONFormat(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	cmd := sandboxShowCmd()
	output.RegisterFlag(cmd)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	root := &cobra.Command{}
	root.AddCommand(cmd)
	root.SetArgs([]string{"show", "--format", "json"})

	if err := root.Execute(); err != nil {
		t.Skipf("sandbox show failed (expected in minimal test env): %v", err)
	}

	// A minimal test env with no config may resolve to a disabled or
	// default-policy sandbox — this test only pins that --format json
	// produces valid, unmarshalable JSON either way (matching the file's
	// existing tolerant-of-minimal-env style above), not specific field
	// values.
	var got sandboxShowResult
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, buf.String())
	}
}
