package provision_test

import (
	"errors"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/provision/provisiontest"
)

// fakeMCPHandler implements provision.MCPHandler for testing ReadInstalledMCP.
type fakeMCPHandler struct {
	servers map[string]provision.MCPServer
	err     error
}

func (f *fakeMCPHandler) Read(_ string) (map[string]provision.MCPServer, map[string]bool, error) {
	if f.err != nil {
		return nil, nil, f.err
	}
	out := make(map[string]provision.MCPServer, len(f.servers))
	for k, v := range f.servers {
		out[k] = v
	}
	return out, nil, nil
}

func (f *fakeMCPHandler) Write(_ string, _ map[string]provision.MCPServer) error {
	return nil
}

// ---- MCPQueryNames ----

func TestMCPQueryNames_NilInputs(t *testing.T) {
	if got := provision.MCPQueryNames(nil, nil); got != nil {
		t.Errorf("MCPQueryNames(nil,nil) = %v, want nil", got)
	}
}

func TestMCPQueryNames_DesiredOnly(t *testing.T) {
	desired := map[string]provision.MCPServer{"b": {Key: "b"}, "a": {Key: "a"}}
	got := provision.MCPQueryNames(desired, nil)
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("MCPQueryNames sorted = %v, want [a b]", got)
	}
}

func TestMCPQueryNames_ManagedOnly(t *testing.T) {
	managed := map[string]provision.ManagedItem{"c": {}}
	got := provision.MCPQueryNames(nil, managed)
	if len(got) != 1 || got[0] != "c" {
		t.Errorf("MCPQueryNames from managed = %v, want [c]", got)
	}
}

func TestMCPQueryNames_Deduplication(t *testing.T) {
	got := provision.MCPQueryNames(
		map[string]provision.MCPServer{"x": {}, "y": {}},
		map[string]provision.ManagedItem{"x": {}, "z": {}},
	)
	if len(got) != 3 {
		t.Errorf("MCPQueryNames dedup len = %d, want 3; got %v", len(got), got)
	}
}

// ---- ReadInstalledMCP ----

func TestReadInstalledMCP_NilProvisioner(t *testing.T) {
	got, err := provision.ReadInstalledMCP(nil, provision.Context{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map, got %v", got)
	}
}

func TestReadInstalledMCP_NoMCPSupport(t *testing.T) {
	fp := &provisiontest.FakeProvisioner{SupportsMCPCfg: false}
	got, err := provision.ReadInstalledMCP(fp, provision.Context{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map, got %v", got)
	}
}

func TestReadInstalledMCP_MCPInstaller_EmptyNames(t *testing.T) {
	fp := &provisiontest.FakeProvisionerWithMCPCLI{
		FakeProvisioner: &provisiontest.FakeProvisioner{SupportsMCPCfg: true},
	}
	got, err := provision.ReadInstalledMCP(fp, provision.Context{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map for empty names, got %v", got)
	}
}

func TestReadInstalledMCP_MCPInstaller_WithNames(t *testing.T) {
	fp := &provisiontest.FakeProvisionerWithMCPCLI{
		FakeProvisioner: &provisiontest.FakeProvisioner{SupportsMCPCfg: true},
		InstalledMCP:    map[string]provision.MCPServer{"srv": {Key: "srv"}},
	}
	got, err := provision.ReadInstalledMCP(fp, provision.Context{}, []string{"srv", "missing"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["srv"]; !ok {
		t.Errorf("expected srv in result, got %v", got)
	}
	if _, ok := got["missing"]; ok {
		t.Error("missing should not appear in result")
	}
}

func TestReadInstalledMCP_MCPInstaller_Error(t *testing.T) {
	wantErr := errors.New("cli-down")
	fp := &provisiontest.FakeProvisionerWithMCPCLI{
		FakeProvisioner: &provisiontest.FakeProvisioner{SupportsMCPCfg: true},
		InstalledMCPErr: wantErr,
	}
	_, err := provision.ReadInstalledMCP(fp, provision.Context{}, []string{"srv"})
	if !errors.Is(err, wantErr) {
		t.Errorf("expected InstalledMCPErr, got %v", err)
	}
}

func TestReadInstalledMCP_MCPHandler_NilHandler(t *testing.T) {
	fp := &provisiontest.FakeProvisioner{SupportsMCPCfg: true}
	got, err := provision.ReadInstalledMCP(fp, provision.Context{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map for nil handler, got %v", got)
	}
}

func TestReadInstalledMCP_MCPHandler_WithServers(t *testing.T) {
	handler := &fakeMCPHandler{
		servers: map[string]provision.MCPServer{"tool": {Key: "tool", Command: "run-tool"}},
	}
	fp := &provisiontest.FakeProvisioner{
		SupportsMCPCfg:  true,
		MCPHandlerValue: handler,
	}
	got, err := provision.ReadInstalledMCP(fp, provision.Context{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s, ok := got["tool"]; !ok || s.Command != "run-tool" {
		t.Errorf("expected tool in result, got %v", got)
	}
}

func TestReadInstalledMCP_MCPHandler_Error(t *testing.T) {
	wantErr := errors.New("read-error")
	handler := &fakeMCPHandler{err: wantErr}
	fp := &provisiontest.FakeProvisioner{
		SupportsMCPCfg:  true,
		MCPHandlerValue: handler,
	}
	_, err := provision.ReadInstalledMCP(fp, provision.Context{}, nil)
	if !errors.Is(err, wantErr) {
		t.Errorf("expected read error, got %v", err)
	}
}
