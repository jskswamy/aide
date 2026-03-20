package modules_test

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/modules"
)

func systemRuntimeOutput() string {
	ctx := &seatbelt.Context{
		HomeDir: "/Users/testuser",
	}
	m := modules.SystemRuntime()
	return renderTestRules(m.Rules(ctx))
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
