//go:build linux

package sandbox

import "testing"

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
	if containsInt(pp.AllowSet, 22) {
		t.Error("port 22 should be excluded from AllowSet (denied)")
	}
	if containsInt(pp.AllowSet, 80) {
		t.Error("port 80 should be excluded from AllowSet (denied)")
	}
	if !containsInt(pp.AllowSet, 443) {
		t.Error("port 443 should be in AllowSet (not denied)")
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
	for _, v := range list {
		if v == val {
			return true
		}
	}
	return false
}
