package guards_test

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

func TestPermissiveIPC_Metadata(t *testing.T) {
	g := guards.PermissiveIPCGuard()
	if g.Name() != "permissive-ipc" {
		t.Errorf("expected name %q, got %q", "permissive-ipc", g.Name())
	}
	if g.Type() != "opt-in" {
		t.Errorf("expected type %q, got %q", "opt-in", g.Type())
	}
}

func TestPermissiveIPC_AllowsMachLookup(t *testing.T) {
	g := guards.PermissiveIPCGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/test"}
	result := g.Rules(ctx)
	profile := rulesToString(result.Rules)
	if !strings.Contains(profile, "(allow mach-lookup)") {
		t.Error("expected unrestricted mach-lookup")
	}
}

func TestPermissiveIPC_AllowsIOKit(t *testing.T) {
	g := guards.PermissiveIPCGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/test"}
	result := g.Rules(ctx)
	profile := rulesToString(result.Rules)
	if !strings.Contains(profile, "(allow iokit-open)") {
		t.Error("expected unrestricted iokit-open")
	}
}

func TestPermissiveIPC_AllowsSignals(t *testing.T) {
	g := guards.PermissiveIPCGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/test"}
	result := g.Rules(ctx)
	profile := rulesToString(result.Rules)
	if !strings.Contains(profile, "(allow signal)") {
		t.Error("expected unrestricted signal")
	}
}

func TestPermissiveIPC_AllowsJobCreation(t *testing.T) {
	g := guards.PermissiveIPCGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/test"}
	result := g.Rules(ctx)
	profile := rulesToString(result.Rules)
	if !strings.Contains(profile, "(allow job-creation)") {
		t.Error("expected job-creation")
	}
}

func TestPermissiveIPC_AllowsDistributedNotifications(t *testing.T) {
	g := guards.PermissiveIPCGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/test"}
	result := g.Rules(ctx)
	profile := rulesToString(result.Rules)
	if !strings.Contains(profile, "(allow distributed-notification-post)") {
		t.Error("expected distributed-notification-post")
	}
}

func TestPermissiveIPC_AllowsPreferenceReadWrite(t *testing.T) {
	g := guards.PermissiveIPCGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/test"}
	result := g.Rules(ctx)
	profile := rulesToString(result.Rules)
	if !strings.Contains(profile, "(allow user-preference-read)") {
		t.Error("expected user-preference-read")
	}
	if !strings.Contains(profile, "(allow user-preference-write)") {
		t.Error("expected user-preference-write")
	}
}

func TestPermissiveIPC_AllowsFsctl(t *testing.T) {
	g := guards.PermissiveIPCGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/test"}
	result := g.Rules(ctx)
	profile := rulesToString(result.Rules)
	if !strings.Contains(profile, "(allow system-fsctl)") {
		t.Error("expected system-fsctl")
	}
}

func TestPermissiveIPC_NoFilesystemRules(t *testing.T) {
	g := guards.PermissiveIPCGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/test"}
	result := g.Rules(ctx)
	profile := rulesToString(result.Rules)
	if strings.Contains(profile, "file-read") || strings.Contains(profile, "file-write") {
		t.Error("permissive-ipc should not contain filesystem rules")
	}
}

func TestPermissiveIPC_NilContext(t *testing.T) {
	g := guards.PermissiveIPCGuard()
	result := g.Rules(nil)
	if len(result.Rules) == 0 {
		t.Error("permissive-ipc should still emit rules with nil context")
	}
}

// rulesToString renders rules to a string for testing.
func rulesToString(rules []seatbelt.Rule) string {
	var sb strings.Builder
	for _, r := range rules {
		sb.WriteString(r.String())
		sb.WriteString("\n")
	}
	return sb.String()
}
