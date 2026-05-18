package provision_test

import (
	"testing"

	"github.com/jskswamy/aide/internal/provision"
)

func TestComputePlanInstall(t *testing.T) {
	desired := provision.Desired{
		Plugins: map[string]provision.Plugin{
			"linear": {Key: "linear", Source: "marketplace", Name: "linear"},
		},
	}
	installed := provision.Installed{}
	managed := provision.ContextState{}
	plan := provision.ComputePlan(provision.Context{Name: "work"}, desired, installed, managed)
	if len(plan.Ops) != 1 || plan.Ops[0].OpKind != provision.OpInstall {
		t.Fatalf("expected one install op, got %+v", plan.Ops)
	}
	if plan.Ops[0].Plugin == nil || plan.Ops[0].Plugin.Key != "linear" {
		t.Errorf("op.Plugin = %+v", plan.Ops[0].Plugin)
	}
}

func TestComputePlanUninstallWhenManagedButNotDesired(t *testing.T) {
	desired := provision.Desired{}
	installed := provision.Installed{Plugins: []string{"old-tool"}}
	managed := provision.ContextState{
		Plugins: map[string]provision.ManagedItem{"old-tool": {}},
	}
	plan := provision.ComputePlan(provision.Context{Name: "work"}, desired, installed, managed)
	if len(plan.Ops) != 1 || plan.Ops[0].OpKind != provision.OpUninstall {
		t.Fatalf("expected one uninstall op, got %+v", plan.Ops)
	}
}

func TestComputePlanIgnoreWhenInstalledButNotManaged(t *testing.T) {
	desired := provision.Desired{}
	installed := provision.Installed{Plugins: []string{"manual-tool"}}
	managed := provision.ContextState{}
	plan := provision.ComputePlan(provision.Context{Name: "work"}, desired, installed, managed)
	if len(plan.Ops) != 1 || plan.Ops[0].OpKind != provision.OpIgnore {
		t.Fatalf("expected one ignore op, got %+v", plan.Ops)
	}
}

func TestComputePlanIgnoreWhenMarketplaceInstalledButNotDeclared(t *testing.T) {
	desired := provision.Desired{}
	installed := provision.Installed{
		Marketplaces: map[string]provision.Marketplace{
			"foo/manual-marketplace": {
				Key:    "foo/manual-marketplace",
				Source: "github:foo/manual-marketplace",
				Name:   "manual-marketplace",
			},
		},
	}
	managed := provision.ContextState{}
	plan := provision.ComputePlan(provision.Context{Name: "test"}, desired, installed, managed)
	if len(plan.Ops) != 1 {
		t.Fatalf("expected one op, got %+v", plan.Ops)
	}
	if plan.Ops[0].OpKind != provision.OpIgnore {
		t.Errorf("op kind = %v, want OpIgnore", plan.Ops[0].OpKind)
	}
	if plan.Ops[0].Kind != provision.KindMarketplace {
		t.Errorf("kind = %v, want KindMarketplace", plan.Ops[0].Kind)
	}
	if plan.Ops[0].Name != "foo/manual-marketplace" {
		t.Errorf("name = %q", plan.Ops[0].Name)
	}
}

func TestComputePlanMCPUpdate(t *testing.T) {
	desired := provision.Desired{
		MCPServers: map[string]provision.MCPServer{
			"postgres": {Key: "postgres", Command: "postgres-mcp", Args: []string{"--port", "9090"}},
		},
	}
	installed := provision.Installed{
		MCPServers: map[string]provision.MCPServer{
			"postgres": {Key: "postgres", Command: "postgres-mcp", Args: []string{"--port", "5432"}},
		},
	}
	managed := provision.ContextState{
		MCPServers: map[string]provision.ManagedItem{"postgres": {}},
	}
	plan := provision.ComputePlan(provision.Context{Name: "work"}, desired, installed, managed)
	if len(plan.Ops) != 1 || plan.Ops[0].OpKind != provision.OpUpdate {
		t.Fatalf("expected one update op, got %+v", plan.Ops)
	}
	if plan.Ops[0].OldMCP == nil || plan.Ops[0].OldMCP.Args[1] != "5432" {
		t.Errorf("OldMCP not captured: %+v", plan.Ops[0].OldMCP)
	}
}

func TestComputePlanInstallsMarketplaceFirst(t *testing.T) {
	desired := provision.Desired{
		Marketplaces: map[string]provision.Marketplace{
			"steveyegge/beads": {Key: "steveyegge/beads", Source: "github:steveyegge/beads"},
		},
		Plugins: map[string]provision.Plugin{
			"beads": {Key: "beads", Name: "beads@steveyegge/beads", Source: "marketplace"},
		},
	}
	installed := provision.Installed{}
	managed := provision.ContextState{}
	plan := provision.ComputePlan(provision.Context{Name: "test"}, desired, installed, managed)

	// First op should be marketplace add, then plugin install.
	if len(plan.Ops) < 2 {
		t.Fatalf("expected at least 2 ops, got %d", len(plan.Ops))
	}
	if plan.Ops[0].Kind != provision.KindMarketplace || plan.Ops[0].OpKind != provision.OpInstall {
		t.Errorf("first op = %+v, want install marketplace", plan.Ops[0])
	}
	if plan.Ops[1].Kind != provision.KindPlugin {
		t.Errorf("second op kind = %v, want plugin", plan.Ops[1].Kind)
	}
}
