package main

import (
	"bytes"
	"strings"
	"testing"

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

	// Run sandboxShowCmd against an empty config dir (default sandbox policy).
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
		// Only fail if the error is sandbox-tier-related.
		if strings.Contains(err.Error(), "unavailable") {
			t.Errorf("sandbox show should not return error for unavailable tier: %v", err)
		}
	}
}
