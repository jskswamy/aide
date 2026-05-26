//go:build linux

package sandbox

import (
	"slices"
	"testing"
)

func TestDerivePortPolicy_Unrestricted(t *testing.T) {
	pp := DerivePortPolicy(Policy{}, true)
	if pp.Mode != "unrestricted" {
		t.Errorf("Mode = %q, want unrestricted", pp.Mode)
	}
	if len(pp.AllowSet) != 0 {
		t.Errorf("AllowSet = %v, want empty for unrestricted", pp.AllowSet)
	}
	if !pp.Enforceable {
		t.Error("unrestricted should always be enforceable")
	}
}

func TestDerivePortPolicy_AllowOnly(t *testing.T) {
	policy := Policy{AllowPorts: []int{443, 80}}
	pp := DerivePortPolicy(policy, true)

	if pp.Mode != "allow_only" {
		t.Errorf("Mode = %q, want allow_only", pp.Mode)
	}
	if !containsInt(pp.AllowSet, 443) || !containsInt(pp.AllowSet, 80) {
		t.Errorf("AllowSet = %v, want [80 443]", pp.AllowSet)
	}
	if !pp.Enforceable {
		t.Error("allow_only on ABI4 should be enforceable")
	}
}

func TestDerivePortPolicy_AllowOnly_NotEnforceableOnABI3(t *testing.T) {
	policy := Policy{AllowPorts: []int{443}}
	pp := DerivePortPolicy(policy, false) // landlockABI4=false

	if pp.Enforceable {
		t.Error("allow_only without ABI4 must not be enforceable")
	}
}

func TestDerivePortPolicy_DenyComplement(t *testing.T) {
	policy := Policy{DenyPorts: []int{22, 80}}
	pp := DerivePortPolicy(policy, true)

	if pp.Mode != "deny_complement" {
		t.Errorf("Mode = %q, want deny_complement", pp.Mode)
	}
	// Denied ports must be absent.
	if containsInt(pp.AllowSet, 22) {
		t.Error("port 22 should be excluded from AllowSet (denied)")
	}
	if containsInt(pp.AllowSet, 80) {
		t.Error("port 80 should be excluded from AllowSet (denied)")
	}
	// Well-known ports NOT in CommonPorts must be present (previously missing).
	for _, port := range []int{443, 5173, 8888, 9090, 1234, 65535} {
		if !containsInt(pp.AllowSet, port) {
			t.Errorf("port %d should be in AllowSet (not denied)", port)
		}
	}
	// AllowSet is the full range (1–65535) minus 2 denied ports.
	wantLen := 65535 - 2
	if len(pp.AllowSet) != wantLen {
		t.Errorf("AllowSet len = %d, want %d (full range minus 2 denied ports)", len(pp.AllowSet), wantLen)
	}
}

func TestDerivePortPolicy_AllowIntersectDeny(t *testing.T) {
	policy := Policy{AllowPorts: []int{443, 80, 22}, DenyPorts: []int{22}}
	pp := DerivePortPolicy(policy, true)

	if pp.Mode != "allow_intersect_deny" {
		t.Errorf("Mode = %q, want allow_intersect_deny", pp.Mode)
	}
	if containsInt(pp.AllowSet, 22) {
		t.Error("port 22 should be excluded (in DenyPorts)")
	}
	if !containsInt(pp.AllowSet, 443) || !containsInt(pp.AllowSet, 80) {
		t.Errorf("AllowSet = %v, should contain 443 and 80", pp.AllowSet)
	}
}

func TestValidatePortRange_ValidPorts(t *testing.T) {
	if err := ValidatePortRange([]int{0, 1, 443, 65535}); err != nil {
		t.Errorf("valid ports returned error: %v", err)
	}
}

func TestValidatePortRange_InvalidPort(t *testing.T) {
	if err := ValidatePortRange([]int{443, 65536}); err == nil {
		t.Error("expected error for port 65536")
	}
	if err := ValidatePortRange([]int{-1}); err == nil {
		t.Error("expected error for port -1")
	}
}

func containsInt(list []int, val int) bool {
	return slices.Contains(list, val)
}
