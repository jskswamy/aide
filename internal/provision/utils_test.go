package provision_test

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/provision"
)

func TestDriftMessage(t *testing.T) {
	tests := []struct {
		kind     provision.DriftKind
		ctxName  string
		wantHas  string
		wantSize int
	}{
		{provision.DriftNone, "alpha", "", 0},
		{provision.DriftConfigChanged, "alpha", "config changed", 1},
		{provision.DriftNeverSynced, "beta", "never synced", 1},
		// any out-of-range kind falls through to ""
		{provision.DriftKind(99), "x", "", 0},
	}
	for _, tc := range tests {
		got := provision.DriftMessage(tc.kind, tc.ctxName)
		if tc.wantSize == 0 {
			if got != "" {
				t.Errorf("kind=%v: expected empty, got %q", tc.kind, got)
			}
			continue
		}
		if !strings.Contains(got, tc.wantHas) {
			t.Errorf("kind=%v: %q must contain %q", tc.kind, got, tc.wantHas)
		}
		if !strings.Contains(got, tc.ctxName) {
			t.Errorf("kind=%v: %q must contain context name %q", tc.kind, got, tc.ctxName)
		}
	}
}

func TestKindString(t *testing.T) {
	cases := map[provision.Kind]string{
		provision.KindPlugin:      "plugin",
		provision.KindMCP:         "mcp",
		provision.KindMarketplace: "marketplace",
		provision.Kind(99):        "unknown",
	}
	for k, want := range cases {
		if got := k.String(); got != want {
			t.Errorf("Kind(%d).String() = %q, want %q", k, got, want)
		}
	}
}

func TestPlanHasMutations(t *testing.T) {
	empty := provision.Plan{}
	if empty.HasMutations() {
		t.Error("empty plan should have no mutations")
	}
	ignoresOnly := provision.Plan{Ops: []provision.Op{
		{OpKind: provision.OpIgnore},
		{OpKind: provision.OpIgnore},
	}}
	if ignoresOnly.HasMutations() {
		t.Error("plan with only ignores should report no mutations")
	}
	withMutation := provision.Plan{Ops: []provision.Op{
		{OpKind: provision.OpIgnore},
		{OpKind: provision.OpInstall},
	}}
	if !withMutation.HasMutations() {
		t.Error("plan with one install must report mutations")
	}
}

func TestJournalLen(t *testing.T) {
	j := &provision.Journal{}
	if got := j.Len(); got != 0 {
		t.Errorf("empty journal Len = %d, want 0", got)
	}
	j.Record(func() error { return nil })
	j.Record(func() error { return nil })
	if got := j.Len(); got != 2 {
		t.Errorf("after 2 records Len = %d, want 2", got)
	}
}

func TestResolveContextPopulatesFields(t *testing.T) {
	got, err := provision.ResolveContext("work",
		config.Context{Agent: "claude"},
		"/home/u",
		"/p/root",
		map[string]string{"K": "v"},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "work" || got.Agent != "claude" || got.HomeDir != "/home/u" || got.ProjectRoot != "/p/root" || got.Env["K"] != "v" {
		t.Errorf("ResolveContext fields: %+v", got)
	}
}

func TestDefaultStatePath(t *testing.T) {
	got := provision.DefaultStatePath("/home/u")
	want := "/home/u/.local/state/aide/managed.json"
	if got != want {
		t.Errorf("DefaultStatePath = %q, want %q", got, want)
	}
}
