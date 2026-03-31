package guards_test

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

func TestXcodeSimulator_Metadata(t *testing.T) {
	g := guards.XcodeSimulatorGuard()
	if g.Name() != "xcode-simulator" {
		t.Errorf("expected name %q, got %q", "xcode-simulator", g.Name())
	}
	if g.Type() != "opt-in" {
		t.Errorf("expected type %q, got %q", "opt-in", g.Type())
	}
}

func TestXcodeSimulator_HomePaths(t *testing.T) {
	g := guards.XcodeSimulatorGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/test"}
	result := g.Rules(ctx)
	profile := rulesToString(result.Rules)

	expectedPaths := []string{
		"/Users/test/Library/Preferences",
		"/Users/test/Library/Developer",
		"/Users/test/Library/Caches/com.apple.dt.Xcode",
		"/Users/test/Library/org.swift.swiftpm",
		"/Users/test/.swiftpm",
		"/Users/test/.CFUserTextEncoding",
	}
	for _, p := range expectedPaths {
		if !strings.Contains(profile, p) {
			t.Errorf("expected path %q in profile", p)
		}
	}
}

func TestXcodeSimulator_NoIPCRules(t *testing.T) {
	g := guards.XcodeSimulatorGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/test"}
	result := g.Rules(ctx)
	profile := rulesToString(result.Rules)

	ipcOps := []string{
		"(allow mach-lookup)",
		"(allow iokit-open)",
		"(allow signal)",
		"(allow job-creation)",
		"(allow distributed-notification-post)",
		"(allow system-fsctl)",
	}
	for _, op := range ipcOps {
		if strings.Contains(profile, op) {
			t.Errorf("xcode-simulator should not contain IPC rule %q", op)
		}
	}
}

func TestXcodeSimulator_NilContext(t *testing.T) {
	g := guards.XcodeSimulatorGuard()
	result := g.Rules(nil)
	if len(result.Rules) != 0 {
		t.Error("expected no rules with nil context")
	}
}

func TestXcodeSimulator_EmptyHomeDir(t *testing.T) {
	g := guards.XcodeSimulatorGuard()
	ctx := &seatbelt.Context{HomeDir: ""}
	result := g.Rules(ctx)
	if len(result.Rules) != 0 {
		t.Error("expected no rules with empty HomeDir")
	}
}
