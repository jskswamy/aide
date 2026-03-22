package modules_test

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/modules"
)

func TestGuard_Browsers_Metadata(t *testing.T) {
	g := modules.BrowsersGuard()
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
	g := modules.BrowsersGuard()
	ctx := &seatbelt.Context{
		HomeDir: "/Users/testuser",
		GOOS:    "darwin",
	}
	output := renderTestRules(g.Rules(ctx))

	darwinPaths := []string{
		"Google/Chrome",
		"Firefox",
		"Safari",
		"BraveSoftware/Brave-Browser",
		"Microsoft Edge",
		"Arc",
		"Chromium",
	}
	for _, want := range darwinPaths {
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
}

func TestGuard_Browsers_LinuxPaths(t *testing.T) {
	g := modules.BrowsersGuard()
	ctx := &seatbelt.Context{
		HomeDir: "/home/testuser",
		GOOS:    "linux",
	}
	output := renderTestRules(g.Rules(ctx))

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
}
