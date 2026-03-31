package guards_test

import (
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

func TestRegistry_AllGuards(t *testing.T) {
	guards := guards.AllGuards()
	if len(guards) != 14 {
		t.Errorf("expected 14 guards, got %d", len(guards))
	}

	// Verify each guard has a non-empty name and type.
	for _, g := range guards {
		if g.Name() == "" {
			t.Error("guard with empty name found")
		}
		if g.Type() == "" {
			t.Errorf("guard %q has empty type", g.Name())
		}
	}
}

func TestRegistry_GuardByName(t *testing.T) {
	g, ok := guards.GuardByName("aide-secrets")
	if !ok {
		t.Fatal("expected to find guard aide-secrets")
	}
	if g.Name() != "aide-secrets" {
		t.Errorf("expected name %q, got %q", "aide-secrets", g.Name())
	}

	_, ok = guards.GuardByName("does-not-exist")
	if ok {
		t.Error("expected not found for unknown guard name")
	}
}

func TestRegistry_GuardsByType(t *testing.T) {
	always := guards.ByType("always")
	if len(always) != 8 {
		t.Errorf("expected 8 always guards, got %d", len(always))
	}
	for _, g := range always {
		if g.Type() != "always" {
			t.Errorf("guard %q has type %q, expected always", g.Name(), g.Type())
		}
	}

	defaults := guards.ByType("default")
	if len(defaults) != 3 {
		t.Errorf("expected 3 default guards, got %d", len(defaults))
	}
	for _, g := range defaults {
		if g.Type() != "default" {
			t.Errorf("guard %q has type %q, expected default", g.Name(), g.Type())
		}
	}

	optIn := guards.ByType("opt-in")
	if len(optIn) != 3 {
		t.Errorf("expected 3 opt-in guards, got %d", len(optIn))
	}
}

func TestRegistry_ExpandGuardName_AllDefault(t *testing.T) {
	names := guards.ExpandGuardName("all-default")
	defaults := guards.ByType("default")
	if len(names) != len(defaults) {
		t.Errorf("expected %d names for all-default, got %d", len(defaults), len(names))
	}
	wantSet := make(map[string]bool, len(defaults))
	for _, g := range defaults {
		wantSet[g.Name()] = true
	}
	for _, n := range names {
		if !wantSet[n] {
			t.Errorf("unexpected name in all-default expansion: %q", n)
		}
	}
}

func TestRegistry_ExpandGuardName_Regular(t *testing.T) {
	names := guards.ExpandGuardName("aide-secrets")
	if len(names) != 1 || names[0] != "aide-secrets" {
		t.Errorf("expected [\"aide-secrets\"], got %v", names)
	}
}

func TestRegistry_DefaultGuardNames(t *testing.T) {
	names := guards.DefaultGuardNames()

	// Should include always and default guards.
	always := guards.ByType("always")
	defaults := guards.ByType("default")
	expected := len(always) + len(defaults)
	if len(names) != expected {
		t.Errorf("expected %d default guard names, got %d", expected, len(names))
	}
}

func TestRegistry_ResolveActiveGuards(t *testing.T) {
	names := []string{"aide-secrets", "base"}
	guards := guards.ResolveActiveGuards(names)

	if len(guards) != 2 {
		t.Fatalf("expected 2 guards, got %d", len(guards))
	}

	// Should be ordered: always (base) → default (aide-secrets)
	if guards[0].Name() != "base" {
		t.Errorf("expected guards[0] = base (always), got %q", guards[0].Name())
	}
	if guards[1].Name() != "aide-secrets" {
		t.Errorf("expected guards[1] = aide-secrets (default), got %q", guards[1].Name())
	}
}

func TestRegistry_ResolveActiveGuards_SkipsUnknown(t *testing.T) {
	names := []string{"unknown-guard", "base", "another-unknown"}
	guards := guards.ResolveActiveGuards(names)

	if len(guards) != 1 {
		t.Fatalf("expected 1 guard (only base), got %d", len(guards))
	}
	if guards[0].Name() != "base" {
		t.Errorf("expected guards[0] = base, got %q", guards[0].Name())
	}
}

func TestRegistry_ResolveActiveGuards_Deduplication(t *testing.T) {
	result := guards.ResolveActiveGuards([]string{"aide-secrets", "base", "aide-secrets", "base"})
	if len(result) != 2 {
		t.Errorf("expected 2 unique guards after dedup, got %d", len(result))
	}
}

func TestRegistry_ExpandGuardName_Unknown(t *testing.T) {
	names := guards.ExpandGuardName("totally-unknown")
	if len(names) != 1 || names[0] != "totally-unknown" {
		t.Errorf("expected unknown name passed through, got %v", names)
	}
}
