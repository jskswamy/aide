package consent

import (
	"errors"
	"testing"
)

func TestHash_OrderInsensitive(t *testing.T) {
	a := Hash("/p", "python", []string{"uv", "conda"}, "digest")
	b := Hash("/p", "python", []string{"conda", "uv"}, "digest")
	if a != b {
		t.Errorf("Hash order-sensitive: %s vs %s", a, b)
	}
}

func TestHash_ChangesOnScope(t *testing.T) {
	base := Hash("/p", "python", []string{"uv"}, "d")
	changes := []string{
		Hash("/q", "python", []string{"uv"}, "d"),
		Hash("/p", "node", []string{"uv"}, "d"),
		Hash("/p", "python", []string{"pyenv"}, "d"),
		Hash("/p", "python", []string{"uv"}, "other"),
	}
	for i, c := range changes {
		if c == base {
			t.Errorf("change %d did not alter Hash", i)
		}
	}
}

func TestStore_GrantCheckRoundTrip(t *testing.T) {
	s := NewStore(t.TempDir())
	ev := Evidence{
		Variants: []string{"uv"},
		Matches: []MarkerMatch{
			{Kind: "file", Target: "uv.lock", Matched: true},
		},
	}
	if s.Check("/proj", "python", ev) != NotGranted {
		t.Fatalf("Check before Grant != NotGranted")
	}
	err := s.Grant(Grant{
		ProjectRoot: "/proj",
		Capability:  "python",
		Variants:    []string{"uv"},
		Evidence:    ev,
		Summary:     "uv.lock",
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.Check("/proj", "python", ev) != Granted {
		t.Errorf("Check after Grant != Granted")
	}
}

func TestStore_Check_EvidenceDigestMismatch(t *testing.T) {
	s := NewStore(t.TempDir())
	ev1 := Evidence{Variants: []string{"uv"}, Matches: []MarkerMatch{{Kind: "file", Target: "uv.lock", Matched: true}}}
	ev2 := Evidence{Variants: []string{"uv"}, Matches: []MarkerMatch{{Kind: "contains", Target: "pyproject.toml:[tool.uv]", Matched: true}}}
	_ = s.Grant(Grant{ProjectRoot: "/p", Capability: "python", Variants: []string{"uv"}, Evidence: ev1})
	if s.Check("/p", "python", ev2) != NotGranted {
		t.Errorf("Check with different evidence digest returned Granted")
	}
}

func TestStore_Revoke_ClearsAllForCapability(t *testing.T) {
	s := NewStore(t.TempDir())
	evA := Evidence{Variants: []string{"uv"}, Matches: []MarkerMatch{{Kind: "file", Target: "uv.lock", Matched: true}}}
	evB := Evidence{Variants: []string{"conda"}, Matches: []MarkerMatch{{Kind: "file", Target: "environment.yml", Matched: true}}}
	_ = s.Grant(Grant{ProjectRoot: "/p", Capability: "python", Variants: []string{"uv"}, Evidence: evA})
	_ = s.Grant(Grant{ProjectRoot: "/p", Capability: "python", Variants: []string{"conda"}, Evidence: evB})
	if err := s.Revoke("/p", "python"); err != nil {
		t.Fatal(err)
	}
	if s.Check("/p", "python", evA) != NotGranted {
		t.Errorf("Revoke left uv grant behind")
	}
	if s.Check("/p", "python", evB) != NotGranted {
		t.Errorf("Revoke left conda grant behind")
	}
}

func TestStore_Revoke_IsIdempotent(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Revoke("/p", "python"); err != nil {
		t.Errorf("Revoke on empty store: %v", err)
	}
}

func TestStore_List_FiltersByProject(t *testing.T) {
	s := NewStore(t.TempDir())
	ev := Evidence{Variants: []string{"uv"}, Matches: []MarkerMatch{{Kind: "file", Target: "uv.lock", Matched: true}}}
	_ = s.Grant(Grant{ProjectRoot: "/a", Capability: "python", Variants: []string{"uv"}, Evidence: ev, Summary: "a"})
	_ = s.Grant(Grant{ProjectRoot: "/b", Capability: "python", Variants: []string{"uv"}, Evidence: ev, Summary: "b"})
	gs, err := s.List("/a")
	if err != nil {
		t.Fatal(err)
	}
	if len(gs) != 1 || gs[0].ProjectRoot != "/a" {
		t.Errorf("List(/a) = %v, want one grant for /a", gs)
	}
}

func TestGrant_RejectsNewlineInProjectRoot(t *testing.T) {
	s := NewStore(t.TempDir())
	err := s.Grant(Grant{
		ProjectRoot: "/proj\ncapability: evil",
		Capability:  "python",
		Variants:    []string{"uv"},
		Evidence:    Evidence{Variants: []string{"uv"}},
	})
	if err == nil {
		t.Fatalf("Grant with newline in ProjectRoot returned nil error; want ErrInvalidGrantField")
	}
	if !errors.Is(err, ErrInvalidGrantField) {
		t.Errorf("error not ErrInvalidGrantField: %v", err)
	}
}

func TestGrant_RejectsNewlineInCapability(t *testing.T) {
	s := NewStore(t.TempDir())
	err := s.Grant(Grant{
		ProjectRoot: "/proj",
		Capability:  "python\nextra",
		Variants:    []string{"uv"},
		Evidence:    Evidence{Variants: []string{"uv"}},
	})
	if !errors.Is(err, ErrInvalidGrantField) {
		t.Errorf("want ErrInvalidGrantField, got: %v", err)
	}
}

func TestGrant_RejectsNewlineInSummary(t *testing.T) {
	s := NewStore(t.TempDir())
	err := s.Grant(Grant{
		ProjectRoot: "/proj",
		Capability:  "python",
		Variants:    []string{"uv"},
		Evidence:    Evidence{Variants: []string{"uv"}},
		Summary:     "uv.lock\nproject: /evil",
	})
	if !errors.Is(err, ErrInvalidGrantField) {
		t.Errorf("want ErrInvalidGrantField, got: %v", err)
	}
}

func TestGrant_RejectsCommaOrNewlineInVariant(t *testing.T) {
	s := NewStore(t.TempDir())
	for _, bad := range []string{"uv,pyenv", "uv\nevil"} {
		err := s.Grant(Grant{
			ProjectRoot: "/proj",
			Capability:  "python",
			Variants:    []string{bad},
			Evidence:    Evidence{Variants: []string{bad}},
		})
		if !errors.Is(err, ErrInvalidGrantField) {
			t.Errorf("Variants[%q] — want ErrInvalidGrantField, got: %v", bad, err)
		}
	}
}

func TestList_PrefixIsolation_FooVsFoobar(t *testing.T) {
	s := NewStore(t.TempDir())
	ev := Evidence{Variants: []string{"uv"}, Matches: []MarkerMatch{{Kind: "file", Target: "uv.lock", Matched: true}}}
	_ = s.Grant(Grant{ProjectRoot: "/foo", Capability: "python", Variants: []string{"uv"}, Evidence: ev, Summary: "foo"})
	_ = s.Grant(Grant{ProjectRoot: "/foobar", Capability: "python", Variants: []string{"uv"}, Evidence: ev, Summary: "bar"})
	gs, err := s.List("/foo")
	if err != nil {
		t.Fatal(err)
	}
	if len(gs) != 1 || gs[0].ProjectRoot != "/foo" {
		t.Errorf("List(/foo) returned %v; want exactly one grant for /foo (not /foobar)", gs)
	}
}
