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
		`(subpath "/Library/Apple")`,      // was specific, now just /Library
		`(subpath "/Library/Frameworks")`, // was specific, now just /Library
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

// codebase-memory-mcp opens each ancestor of its cache dir for reading,
// which needs file-read-data on the directory holding $HOME. The grant
// must stay a literal: a subpath would expose every account's files.
func TestSystemRuntime_HomeParentIsOpenableAsALiteral(t *testing.T) {
	for _, tc := range []struct {
		name, home string
		want       []string
	}{
		{"users", "/Users/testuser", []string{`(literal "/Users")`}},
		{"home outside /Users", "/Volumes/ext/jane", []string{`(literal "/Users")`, `(literal "/Volumes/ext")`}},
		{"path with a space", "/Volumes/My Disk/jane", []string{`(literal "/Volumes/My Disk")`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := &seatbelt.Context{HomeDir: tc.home, AllowSubprocess: true}
			out := renderTestRules(guards.SystemRuntimeGuard().Rules(ctx).Rules)
			for _, want := range tc.want {
				if !fileReadDataRuleHas(out, want) {
					t.Errorf("no (allow file-read-data ...) rule contains %s in:\n%s", want, out)
				}
			}
			for _, bad := range []string{`(subpath "/Users")`, `(subpath "/Volumes")`} {
				if strings.Contains(out, bad) {
					t.Errorf("policy must not grant %s", bad)
				}
			}
		})
	}
}

func TestSystemRuntime_NoExtraGrantWhenHomeParentIsRoot(t *testing.T) {
	ctx := &seatbelt.Context{HomeDir: "/root", AllowSubprocess: true}
	out := renderTestRules(guards.SystemRuntimeGuard().Rules(ctx).Rules)
	if strings.Contains(out, `(literal "")`) || strings.Count(out, `(literal "/")`) != 2 {
		t.Errorf("a home directly under / must add no parent literal (/ is already granted):\n%s", out)
	}
}

// fileReadDataRuleHas reports whether any "(allow file-read-data ...)"
// rule in the rendered policy mentions needle.
func fileReadDataRuleHas(policy, needle string) bool {
	for _, block := range strings.Split(policy, "(allow ")[1:] {
		end := strings.Index(block, "\n)")
		if end < 0 {
			end = len(block)
		}
		if strings.HasPrefix(block, "file-read-data") && strings.Contains(block[:end], needle) {
			return true
		}
	}
	return false
}
