package guards_test

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

func systemRuntimeOutput() string {
	ctx := &seatbelt.Context{
		HomeDir:         "/Users/testuser",
		AllowSubprocess: true,
	}
	g := guards.SystemRuntimeGuard()
	result := g.Rules(ctx)
	return renderTestRules(result.Rules)
}

func TestGuard_SystemRuntime_Metadata(t *testing.T) {
	g := guards.SystemRuntimeGuard()

	if g.Name() != "system-runtime" {
		t.Errorf("expected Name() = %q, got %q", "system-runtime", g.Name())
	}
	if g.Type() != "always" {
		t.Errorf("expected Type() = %q, got %q", "always", g.Type())
	}
	if g.Description() == "" {
		t.Error("expected non-empty Description()")
	}
}

func TestSystemRuntime_MachServices(t *testing.T) {
	output := systemRuntimeOutput()

	services := []string{
		"com.apple.logd",
		"com.apple.trustd.agent",
		"com.apple.dnssd.service",
		"com.apple.coreservices.launchservicesd",
	}
	for _, svc := range services {
		if !strings.Contains(output, svc) {
			t.Errorf("expected output to contain mach service %q", svc)
		}
	}
}

func TestSystemRuntime_ProcessRules(t *testing.T) {
	output := systemRuntimeOutput()

	for _, rule := range []string{
		"(allow process-exec)",
		"(allow pseudo-tty)",
		"(allow system-socket)",
	} {
		if !strings.Contains(output, rule) {
			t.Errorf("expected output to contain %q", rule)
		}
	}
}

func TestSystemRuntime_TempDirs(t *testing.T) {
	output := systemRuntimeOutput()

	for _, path := range []string{
		`(subpath "/private/tmp")`,
		`(subpath "/private/var/folders")`,
	} {
		if !strings.Contains(output, path) {
			t.Errorf("expected output to contain %q", path)
		}
	}
}

func TestSystemRuntime_DeviceNodes(t *testing.T) {
	output := systemRuntimeOutput()

	for _, path := range []string{
		`(literal "/dev/null")`,
		`(literal "/dev/tty")`,
		`(literal "/dev/ptmx")`,
	} {
		if !strings.Contains(output, path) {
			t.Errorf("expected output to contain %q", path)
		}
	}
}

func TestSystemRuntime_SystemPaths(t *testing.T) {
	output := systemRuntimeOutput()

	for _, path := range []string{
		`(subpath "/usr")`,
		`(subpath "/bin")`,
		`(subpath "/System")`,
		`(subpath "/Library")`,
	} {
		if !strings.Contains(output, path) {
			t.Errorf("expected output to contain %q", path)
		}
	}
}

func TestSystemRuntime_AllowSubprocess_True(t *testing.T) {
	g := guards.SystemRuntimeGuard()
	ctx := &seatbelt.Context{
		HomeDir:         "/Users/testuser",
		AllowSubprocess: true,
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, "(allow process-exec)") {
		t.Error("expected process-exec when AllowSubprocess=true")
	}
	if !strings.Contains(output, "(allow process-fork)") {
		t.Error("expected process-fork when AllowSubprocess=true")
	}
}

func TestSystemRuntime_AllowSubprocess_False(t *testing.T) {
	g := guards.SystemRuntimeGuard()
	ctx := &seatbelt.Context{
		HomeDir:         "/Users/testuser",
		AllowSubprocess: false,
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, "(allow process-exec)") {
		t.Error("expected process-exec even when AllowSubprocess=false")
	}
	if !strings.Contains(output, "(deny process-fork)") {
		t.Error("expected deny process-fork when AllowSubprocess=false")
	}
	if strings.Contains(output, "(allow process-fork)") {
		t.Error("should NOT have allow process-fork when AllowSubprocess=false")
	}
}

func TestSystemRuntime_NoHomeRules(t *testing.T) {
	// Home metadata and user preferences are now handled by the filesystem guard.
	output := systemRuntimeOutput()

	homePatterns := []string{
		"/Users/testuser/.config",
		"/Users/testuser/.cache",
		"/Users/testuser/.local",
		"GlobalPreferences",
		".CFUserTextEncoding",
	}
	for _, p := range homePatterns {
		if strings.Contains(output, p) {
			t.Errorf("system-runtime should NOT contain home path %q (moved to filesystem guard)", p)
		}
	}
}

func TestSystemRuntime_BroadSystemReads(t *testing.T) {
	g := guards.SystemRuntimeGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser", AllowSubprocess: true}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	// Should have broad system directory allows
	broadPaths := []string{
		`(subpath "/usr")`, `(subpath "/bin")`, `(subpath "/sbin")`,
		`(subpath "/opt")`, `(subpath "/System")`, `(subpath "/Library")`,
		`(subpath "/nix")`, `(subpath "/private")`, `(subpath "/Applications")`,
		`(subpath "/run")`, `(subpath "/dev")`, `(subpath "/tmp")`, `(subpath "/var")`,
	}
	for _, p := range broadPaths {
		if !strings.Contains(output, p) {
			t.Errorf("expected broad system read for %s", p)
		}
	}

	// Should NOT have the old granular paths
	oldPaths := []string{
		`(subpath "/System/Library")`,     // was specific, now just /System
		`(subpath "/Library/Apple")`,       // was specific, now just /Library
		`(subpath "/Library/Frameworks")`,  // was specific, now just /Library
	}
	for _, p := range oldPaths {
		if strings.Contains(output, p) {
			t.Errorf("should not have old granular path %s (replaced by broad /System, /Library)", p)
		}
	}

	// Non-read rules should still be present
	if !strings.Contains(output, "(allow process-exec)") {
		t.Error("expected process-exec rule")
	}
	if !strings.Contains(output, "(allow mach-lookup") {
		t.Error("expected mach-lookup rules")
	}
}
