package config

import (
	"reflect"
	"testing"
)

func boolPtrSL(b bool) *bool { return &b }

func TestResolveStatusline_NilInputsGivesDefaults(t *testing.T) {
	got := ResolveStatusline(nil, nil)
	wantOrder := []string{"sandbox", "network", "caps", "trust", "context"}
	if !reflect.DeepEqual(got.Order, wantOrder) {
		t.Errorf("Order = %v, want %v", got.Order, wantOrder)
	}
	if got.Sandbox == nil || got.Sandbox.On != "🔒" {
		t.Errorf("Sandbox.On = %q, want 🔒", got.Sandbox.On)
	}
	if got.Sandbox == nil || got.Sandbox.Off != "🔓" {
		t.Errorf("Sandbox.Off = %q, want 🔓", got.Sandbox.Off)
	}
	if got.Network == nil || got.Network.Outbound != "🌐" {
		t.Errorf("Network.Outbound = %q, want 🌐", got.Network.Outbound)
	}
	if got.Caps == nil || got.Caps.Icon != "⚡" {
		t.Errorf("Caps.Icon = %q, want ⚡", got.Caps.Icon)
	}
	if got.Trust == nil || got.Trust.Untrusted != "⚠️" {
		t.Errorf("Trust.Untrusted = %q, want ⚠️", got.Trust.Untrusted)
	}
	if got.Context == nil || got.Context.Icon != "📁" {
		t.Errorf("Context.Icon = %q, want 📁", got.Context.Icon)
	}
	if got.AutoApprove == nil || got.AutoApprove.Value != "🚨" {
		t.Errorf("AutoApprove.Value = %q, want 🚨", got.AutoApprove.Value)
	}
	if got.Gateway == nil || got.Gateway.Icon != "⇄" {
		t.Errorf("Gateway.Icon = %v, want ⇄", got.Gateway)
	}
	if got.Gateway == nil || got.Gateway.Disabled == nil || !*got.Gateway.Disabled {
		t.Errorf("Gateway.Disabled = %v, want true by default", got.Gateway)
	}
}

func TestResolveStatusline_ProjectOverridesModuleField(t *testing.T) {
	project := &StatuslineConfig{
		Trust: &ModuleConfig{Disabled: boolPtrSL(true)},
	}
	got := ResolveStatusline(nil, project)
	if got.Trust == nil || got.Trust.Disabled == nil || !*got.Trust.Disabled {
		t.Error("Trust.Disabled should be true after project override")
	}
	if got.Sandbox == nil || got.Sandbox.On != "🔒" {
		t.Errorf("Sandbox.On = %q, want default 🔒 (unchanged)", got.Sandbox.On)
	}
}

func TestResolveStatusline_ProjectReplacesOrderWholesale(t *testing.T) {
	project := &StatuslineConfig{Order: []string{"caps", "sandbox"}}
	got := ResolveStatusline(nil, project)
	want := []string{"caps", "sandbox"}
	if !reflect.DeepEqual(got.Order, want) {
		t.Errorf("Order = %v, want %v", got.Order, want)
	}
}

func TestResolveStatusline_GlobalThenProjectFieldMerge(t *testing.T) {
	global := &StatuslineConfig{
		Sandbox: &ModuleConfig{On: "G"},
		Network: &ModuleConfig{Outbound: "N"},
	}
	project := &StatuslineConfig{
		Sandbox: &ModuleConfig{On: "P"},
	}
	got := ResolveStatusline(global, project)
	if got.Sandbox.On != "P" {
		t.Errorf("Sandbox.On = %q, want P (project wins)", got.Sandbox.On)
	}
	if got.Network.Outbound != "N" {
		t.Errorf("Network.Outbound = %q, want N (global preserved)", got.Network.Outbound)
	}
}

func TestResolveStatusline_GlobalOrderPreservedWhenProjectEmpty(t *testing.T) {
	global := &StatuslineConfig{Order: []string{"trust", "sandbox"}}
	got := ResolveStatusline(global, nil)
	want := []string{"trust", "sandbox"}
	if !reflect.DeepEqual(got.Order, want) {
		t.Errorf("Order = %v, want %v", got.Order, want)
	}
}

func TestResolveStatusline_ProjectCanReenableDisabledModule(t *testing.T) {
	global := &StatuslineConfig{Trust: &ModuleConfig{Disabled: boolPtrSL(true)}}
	project := &StatuslineConfig{Trust: &ModuleConfig{Disabled: boolPtrSL(false)}}
	got := ResolveStatusline(global, project)
	if got.Trust == nil || got.Trust.Disabled == nil || *got.Trust.Disabled {
		t.Error("Trust.Disabled should be false (project re-enabled)")
	}
}

func TestResolveStatusline_SandboxUnmanagedDefault(t *testing.T) {
	got := ResolveStatusline(nil, nil)
	if got.Sandbox == nil || got.Sandbox.Unmanaged != "💀" {
		t.Errorf("Sandbox.Unmanaged = %q, want 💀", got.Sandbox.Unmanaged)
	}
}

func TestResolveStatusline_NetworkUnmanagedDefault(t *testing.T) {
	got := ResolveStatusline(nil, nil)
	if got.Network == nil || got.Network.Unmanaged != "🌫️" {
		t.Errorf("Network.Unmanaged = %q, want 🌫️", got.Network.Unmanaged)
	}
}

func TestResolveStatusline_ProjectOverridesUnmanaged(t *testing.T) {
	project := &StatuslineConfig{
		Sandbox: &ModuleConfig{Unmanaged: "🚫"},
	}
	got := ResolveStatusline(nil, project)
	if got.Sandbox.Unmanaged != "🚫" {
		t.Errorf("Sandbox.Unmanaged = %q, want 🚫", got.Sandbox.Unmanaged)
	}
	if got.Sandbox.On != "🔒" {
		t.Errorf("Sandbox.On = %q, want default 🔒 (unchanged)", got.Sandbox.On)
	}
}

func TestResolveStatusline_GatewayEnablementAndOrder(t *testing.T) {
	// 1. Enabling gateway via gateway.disabled: false appends "gateway" to Order.
	got := ResolveStatusline(&StatuslineConfig{
		Gateway: &ModuleConfig{Disabled: boolPtrSL(false)},
	}, nil)
	if got.Gateway == nil || got.Gateway.Disabled == nil || *got.Gateway.Disabled {
		t.Fatalf("Gateway.Disabled = %v, want false", got.Gateway)
	}
	wantOrder := []string{"sandbox", "network", "caps", "trust", "context", "gateway"}
	if !reflect.DeepEqual(got.Order, wantOrder) {
		t.Errorf("Order = %v, want %v", got.Order, wantOrder)
	}

	// 2. Listing "gateway" in statusline.order implicitly enables it when gateway.disabled is unset.
	gotOrder := ResolveStatusline(&StatuslineConfig{
		Order: []string{"context", "gateway"},
	}, nil)
	if gotOrder.Gateway == nil || gotOrder.Gateway.Disabled == nil || *gotOrder.Gateway.Disabled {
		t.Fatalf("Gateway.Disabled = %v, want false when listed in order", gotOrder.Gateway)
	}
	if !reflect.DeepEqual(gotOrder.Order, []string{"context", "gateway"}) {
		t.Errorf("Order = %v, want [context gateway]", gotOrder.Order)
	}

	// 3. Project layer can still explicitly disable gateway even if global order listed it.
	gotProjectOff := ResolveStatusline(
		&StatuslineConfig{Order: []string{"context", "gateway"}},
		&StatuslineConfig{Gateway: &ModuleConfig{Disabled: boolPtrSL(true)}},
	)
	if gotProjectOff.Gateway == nil || gotProjectOff.Gateway.Disabled == nil || !*gotProjectOff.Gateway.Disabled {
		t.Fatalf("Gateway.Disabled = %v, want true when project disables it", gotProjectOff.Gateway)
	}
}
