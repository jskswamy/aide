package provisiontest

import (
	"errors"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
)

func TestFakeProvisionerRecordsCalls(t *testing.T) {
	f := &FakeProvisioner{AgentName: "x", SupportsPlug: true, SupportsMCPCfg: true}
	_ = f.InstallPlugin(provision.Context{}, provision.Plugin{Key: "a", Name: "a"})
	_ = f.UninstallPlugin(provision.Context{}, "b")
	_ = f.AddMarketplace(provision.Context{}, provision.Marketplace{Key: "k"})
	_ = f.RemoveMarketplace(provision.Context{}, "k")

	if got := f.Called; len(got) != 4 ||
		got[0] != "install:a:a" || got[1] != "uninstall:b" ||
		got[2] != "add-marketplace:k" || got[3] != "remove-marketplace:k" {
		t.Errorf("Called log = %v", got)
	}
	if len(f.InstallCalls) != 1 || f.InstallCalls[0].Key != "a" {
		t.Errorf("InstallCalls = %v", f.InstallCalls)
	}
	if len(f.UninstallCall) != 1 || f.UninstallCall[0] != "b" {
		t.Errorf("UninstallCall = %v", f.UninstallCall)
	}
	if len(f.AddedMarketplaces) != 1 || f.AddedMarketplaces[0].Key != "k" {
		t.Errorf("AddedMarketplaces = %v", f.AddedMarketplaces)
	}
	if len(f.RemoveMarketplaces) != 1 || f.RemoveMarketplaces[0] != "k" {
		t.Errorf("RemoveMarketplaces = %v", f.RemoveMarketplaces)
	}
}

func TestFakeProvisionerErrorInjection(t *testing.T) {
	want := errors.New("network down")
	f := &FakeProvisioner{InstallErr: want}
	got := f.InstallPlugin(provision.Context{}, provision.Plugin{})
	if !errors.Is(got, want) {
		t.Errorf("InstallPlugin err = %v, want %v", got, want)
	}
}

func TestFakeProvisionerWithMCPCLI(t *testing.T) {
	base := &FakeProvisioner{AgentName: "x"}
	f := &FakeProvisionerWithMCPCLI{
		FakeProvisioner: base,
		InstalledMCP: map[string]provision.MCPServer{
			"srv": {Key: "srv", Command: "cmd"},
		},
	}

	// InstalledMCPServers — normal path, returns only known servers.
	got, err := f.InstalledMCPServers(provision.Context{}, []string{"srv", "missing"})
	if err != nil {
		t.Fatalf("InstalledMCPServers: %v", err)
	}
	if len(got) != 1 || got["srv"].Key != "srv" {
		t.Errorf("InstalledMCPServers result = %v", got)
	}
	if len(f.InstalledMCPQuery) != 1 || len(f.InstalledMCPQuery[0]) != 2 {
		t.Errorf("InstalledMCPQuery = %v", f.InstalledMCPQuery)
	}

	// InstalledMCPServers — error path.
	wantErr := errors.New("mcp-down")
	f.InstalledMCPErr = wantErr
	if _, err := f.InstalledMCPServers(provision.Context{}, nil); !errors.Is(err, wantErr) {
		t.Errorf("expected InstalledMCPErr, got %v", err)
	}
	f.InstalledMCPErr = nil

	// InstallMCPServer.
	s := provision.MCPServer{Key: "new", Command: "run"}
	if err := f.InstallMCPServer(provision.Context{}, s); err != nil {
		t.Fatalf("InstallMCPServer: %v", err)
	}
	if len(f.InstallMCPCalls) != 1 || f.InstallMCPCalls[0].Key != "new" {
		t.Errorf("InstallMCPCalls = %v", f.InstallMCPCalls)
	}
	if len(f.Called) == 0 || f.Called[len(f.Called)-1] != "install-mcp:new" {
		t.Errorf("Called log = %v", f.Called)
	}

	// UninstallMCPServer.
	if err := f.UninstallMCPServer(provision.Context{}, "old"); err != nil {
		t.Fatalf("UninstallMCPServer: %v", err)
	}
	if len(f.UninstallMCPCalls) != 1 || f.UninstallMCPCalls[0] != "old" {
		t.Errorf("UninstallMCPCalls = %v", f.UninstallMCPCalls)
	}
}

func TestFakeProvisionerWithHookInstaller(t *testing.T) {
	hook1 := provision.Hook{Event: "stop", Matcher: "*", Command: "cmd1"}
	hook2 := provision.Hook{Event: "start", Matcher: "*", Command: "cmd2"}

	f := &FakeProvisionerWithHookInstaller{
		FakeProvisioner: &FakeProvisioner{},
		StoredHooks:     []provision.Hook{hook1},
	}

	// ReadHooks returns a copy of StoredHooks.
	got, err := f.ReadHooks(provision.Context{})
	if err != nil {
		t.Fatalf("ReadHooks: %v", err)
	}
	if len(got) != 1 || got[0].Command != "cmd1" {
		t.Errorf("ReadHooks = %v", got)
	}

	// WriteHooks — error path.
	wantErr := errors.New("write-fail")
	f.WriteHooksErr = wantErr
	if err := f.WriteHooks(provision.Context{}, nil, nil); !errors.Is(err, wantErr) {
		t.Errorf("expected WriteHooksErr, got %v", err)
	}
	f.WriteHooksErr = nil

	// WriteHooks — replaces prevManaged with desired.
	if err := f.WriteHooks(provision.Context{}, []provision.Hook{hook1}, []provision.Hook{hook2}); err != nil {
		t.Fatalf("WriteHooks: %v", err)
	}
	remaining, _ := f.ReadHooks(provision.Context{})
	if len(remaining) != 1 || remaining[0].Command != "cmd2" {
		t.Errorf("after WriteHooks, stored = %v, want [cmd2]", remaining)
	}
	if len(f.WriteHooksCalls) != 1 || len(f.WriteHooksCalls[0]) != 1 {
		t.Errorf("WriteHooksCalls = %v", f.WriteHooksCalls)
	}
}

func TestFakeProvisionerReset(t *testing.T) {
	f := &FakeProvisioner{AgentName: "agent", SupportsPlug: true, InstallErr: errors.New("x")}
	_ = f.InstallPlugin(provision.Context{}, provision.Plugin{Key: "k"})
	f.Reset()
	if len(f.Called) != 0 || len(f.InstallCalls) != 0 || f.InstallErr != nil {
		t.Errorf("Reset failed: %+v", f)
	}
	if f.AgentName != "agent" || !f.SupportsPlug {
		t.Errorf("Reset should preserve identity/capability fields, got %+v", f)
	}
}
