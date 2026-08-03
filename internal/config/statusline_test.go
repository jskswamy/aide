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
