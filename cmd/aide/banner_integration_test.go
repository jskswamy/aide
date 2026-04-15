package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fatih/color"
)

// TestBannerIntegration_PythonUVRendersVariantProvenanceAndFreshMarker
// is a documented end-to-end shape for the banner variant + provenance
// feature. It is currently skipped because cmd/aide does not yet have a
// runAide-style test harness; once such a harness lands the body below
// should be enabled.
//
// What it would assert:
//
//  1. Create a temp project with uv.lock so DetectEvidence fires for
//     the python uv variant.
//  2. Set XDG_DATA_HOME to a temp dir so the consent store is empty
//     (so the launch records a fresh grant).
//  3. Invoke aide with --info-style=clean --yes so the prompter
//     auto-approves the uv variant without TTY interaction.
//  4. Capture stdout; assert it contains:
//     - "python[uv]"  (variant suffix)
//     - "🆕"           (fresh-grant marker)
//     - "(detected)"   (provenance tag)
//
// Once a runAide harness exists, replace the t.Skip and the placeholder
// blocks below with real invocation and stdout capture.
func TestBannerIntegration_PythonUVRendersVariantProvenanceAndFreshMarker(t *testing.T) {
	t.Skip("integration wiring spec — enable once cmd/aide has a runAide test harness; banner variant + provenance feature is unit-tested at the ui package layer")

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "uv.lock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)

	var buf bytes.Buffer
	prev := color.NoColor
	color.NoColor = true
	defer func() { color.NoColor = prev }()

	// Future invocation shape:
	//
	//   err := runAide(&buf, dir, []string{"--info-style=clean", "--yes", "status"})
	//   if err != nil { t.Fatal(err) }
	//
	// Then assert the rendered output contains the expected tokens:
	//
	//   for _, want := range []string{"python[uv]", "🆕", "(detected)"} {
	//       if !strings.Contains(buf.String(), want) {
	//           t.Errorf("banner missing %q; got:\n%s", want, buf.String())
	//       }
	//   }

	// Keep imports referenced so the file still compiles when the
	// skip is removed and only some lines are uncommented.
	_ = buf
	_ = strings.Contains
}
