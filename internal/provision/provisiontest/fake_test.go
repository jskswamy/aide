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
