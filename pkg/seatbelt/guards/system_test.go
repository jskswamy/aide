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
		`(subpath "/System/Library")`,
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

func TestSystemRuntime_HomeMetadata(t *testing.T) {
	output := systemRuntimeOutput()

	for _, path := range []string{
		`(literal "/Users/testuser/.config")`,
		`(literal "/Users/testuser/.cache")`,
	} {
		if !strings.Contains(output, path) {
			t.Errorf("expected output to contain %q", path)
		}
	}
}
