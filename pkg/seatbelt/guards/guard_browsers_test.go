package guards_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

func TestGuard_Browsers_Metadata(t *testing.T) {
	g := guards.BrowsersGuard()
	if g.Name() != "browsers" {
		t.Errorf("expected Name() = %q, got %q", "browsers", g.Name())
	}
	if g.Type() != "default" {
		t.Errorf("expected Type() = %q, got %q", "default", g.Type())
	}
	if g.Description() == "" {
		t.Error("expected non-empty Description()")
	}
}

func TestGuard_Browsers_DarwinPaths(t *testing.T) {
	home := t.TempDir()
	appSupport := filepath.Join(home, "Library/Application Support")

	darwinBrowsers := []string{
		"Google/Chrome",
		"Google/Chrome Canary",
		"Firefox",
		"Safari",
		"BraveSoftware/Brave-Browser",
		"Microsoft Edge",
		"Arc",
		"Chromium",
	}
	for _, b := range darwinBrowsers {
		if err := os.MkdirAll(filepath.Join(appSupport, b), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	g := guards.BrowsersGuard()
	ctx := &seatbelt.Context{
		HomeDir: home,
		GOOS:    "darwin",
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	checkPaths := []string{
		"Google/Chrome",
		"Firefox",
		"Safari",
		"BraveSoftware/Brave-Browser",
		"Microsoft Edge",
		"Arc",
		"Chromium",
	}
	for _, want := range checkPaths {
		if !strings.Contains(output, want) {
			t.Errorf("darwin: expected output to contain %q", want)
		}
	}

	// Linux-specific paths should NOT appear
	linuxOnlyPaths := []string{
		".mozilla",
		"google-chrome",
		"microsoft-edge",
		"snap/chromium",
	}
	for _, bad := range linuxOnlyPaths {
		if strings.Contains(output, bad) {
			t.Errorf("darwin: output should NOT contain linux-only path %q", bad)
		}
	}

	if len(result.Protected) == 0 {
		t.Error("expected Protected to be populated")
	}
}

func TestGuard_Browsers_LinuxPaths(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(home, ".config")
	mozillaDir := filepath.Join(home, ".mozilla")
	snapDir := filepath.Join(home, "snap")

	linuxBrowsers := []struct {
		base string
		name string
	}{
		{configDir, "google-chrome"},
		{configDir, "google-chrome-beta"},
		{mozillaDir, "firefox"},
		{configDir, "BraveSoftware/Brave-Browser"},
		{configDir, "microsoft-edge"},
		{configDir, "chromium"},
		{snapDir, "chromium"},
	}
	for _, b := range linuxBrowsers {
		if err := os.MkdirAll(filepath.Join(b.base, b.name), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	g := guards.BrowsersGuard()
	ctx := &seatbelt.Context{
		HomeDir: home,
		GOOS:    "linux",
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	linuxPaths := []string{
		"google-chrome",
		"firefox",
		"BraveSoftware",
		"microsoft-edge",
		"chromium",
	}
	for _, want := range linuxPaths {
		if !strings.Contains(output, want) {
			t.Errorf("linux: expected output to contain %q", want)
		}
	}

	// Safari is macOS-only and should NOT appear on Linux
	if strings.Contains(output, "Safari") {
		t.Error("linux: output should NOT contain Safari (macOS only)")
	}

	if len(result.Protected) == 0 {
		t.Error("expected Protected to be populated")
	}
}

func TestGuard_Browsers_AllSkipped(t *testing.T) {
	ctx := &seatbelt.Context{HomeDir: t.TempDir(), GOOS: "darwin"}
	result := guards.BrowsersGuard().Rules(ctx)
	if len(result.Rules) != 0 {
		t.Errorf("expected 0 rules when paths missing, got %d", len(result.Rules))
	}
	if len(result.Skipped) == 0 {
		t.Error("expected skip messages")
	}
}
