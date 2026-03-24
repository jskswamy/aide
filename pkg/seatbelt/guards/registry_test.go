package guards_test

import (
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

func TestRegistry_AllGuards(t *testing.T) {
	guards := guards.AllGuards()
	if len(guards) != 28 {
		t.Errorf("expected 28 guards, got %d", len(guards))
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
	g, ok := guards.GuardByName("ssh-keys")
	if !ok {
		t.Fatal("expected to find guard ssh-keys")
	}
	if g.Name() != "ssh-keys" {
		t.Errorf("expected name %q, got %q", "ssh-keys", g.Name())
	}

	_, ok = guards.GuardByName("does-not-exist")
	if ok {
		t.Error("expected not found for unknown guard name")
	}
}

func TestRegistry_GuardsByType(t *testing.T) {
	always := guards.ByType("always")
	if len(always) != 7 {
		t.Errorf("expected 7 always guards, got %d", len(always))
	}
	for _, g := range always {
		if g.Type() != "always" {
			t.Errorf("guard %q has type %q, expected always", g.Name(), g.Type())
		}
	}

	defaults := guards.ByType("default")
	if len(defaults) != 20 {
		t.Errorf("expected 20 default guards, got %d", len(defaults))
	}
	for _, g := range defaults {
		if g.Type() != "default" {
			t.Errorf("guard %q has type %q, expected default", g.Name(), g.Type())
		}
	}

	optIn := guards.ByType("opt-in")
	if len(optIn) != 1 {
		t.Errorf("expected 1 opt-in guards, got %d", len(optIn))
	}
	for _, g := range optIn {
		if g.Type() != "opt-in" {
			t.Errorf("guard %q has type %q, expected opt-in", g.Name(), g.Type())
		}
	}
}

func TestRegistry_ExpandGuardName_Cloud(t *testing.T) {
	names := guards.ExpandGuardName("cloud")
	if len(names) != 5 {
		t.Errorf("expected 5 cloud guard names, got %d", len(names))
	}
	want := map[string]bool{
		"cloud-aws":          true,
		"cloud-gcp":          true,
		"cloud-azure":        true,
		"cloud-digitalocean": true,
		"cloud-oci":          true,
	}
	for _, n := range names {
		if !want[n] {
			t.Errorf("unexpected cloud guard name %q", n)
		}
		delete(want, n)
	}
	for n := range want {
		t.Errorf("missing cloud guard name %q", n)
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
	names := guards.ExpandGuardName("ssh-keys")
	if len(names) != 1 || names[0] != "ssh-keys" {
		t.Errorf("expected [\"ssh-keys\"], got %v", names)
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

	// Should not include opt-in guards.
	optIn := guards.ByType("opt-in")
	optInSet := make(map[string]bool, len(optIn))
	for _, g := range optIn {
		optInSet[g.Name()] = true
	}
	for _, n := range names {
		if optInSet[n] {
			t.Errorf("opt-in guard %q should not be in DefaultGuardNames", n)
		}
	}
}

func TestRegistry_ResolveActiveGuards(t *testing.T) {
	// docker is now "default", so use vercel (still opt-in) for testing type ordering
	names := []string{"vercel", "base", "ssh-keys"}
	guards := guards.ResolveActiveGuards(names)

	if len(guards) != 3 {
		t.Fatalf("expected 3 guards, got %d", len(guards))
	}

	// Should be ordered: always (base) → default (ssh-keys) → opt-in (vercel)
	if guards[0].Name() != "base" {
		t.Errorf("expected guards[0] = base (always), got %q", guards[0].Name())
	}
	if guards[1].Name() != "ssh-keys" {
		t.Errorf("expected guards[1] = ssh-keys (default), got %q", guards[1].Name())
	}
	if guards[2].Name() != "vercel" {
		t.Errorf("expected guards[2] = vercel (opt-in), got %q", guards[2].Name())
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
	result := guards.ResolveActiveGuards([]string{"ssh-keys", "base", "ssh-keys", "base"})
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
