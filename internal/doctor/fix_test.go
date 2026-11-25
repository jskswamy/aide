package doctor

import (
	"slices"
	"testing"
)

func TestSuggestedCapability_CommandOnlyForTrustedLocalFile(t *testing.T) {
	opens := []string{"~/go"}
	for _, tc := range []struct {
		name    string
		local   *ProjectFile
		command bool
	}{
		{"trusted local file", &ProjectFile{Layer: "local", Path: "~/repo/.aide.yaml", State: "trusted"}, true},
		{"untrusted local file", &ProjectFile{Layer: "local", Path: "~/repo/.aide.yaml", State: "untrusted"}, false},
		{"no local file", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := SuggestedCapability("go", "go.mod", opens, "work", tc.local)
			if f.ID != "capability.suggested" || f.Severity != SeverityTip || !f.WidensAccess {
				t.Fatalf("finding = %+v", f)
			}
			if tc.command {
				if !slices.Equal(f.Fix.Argv, []string{"aide", "cap", "enable", "go"}) {
					t.Errorf("argv = %v", f.Fix.Argv)
				}
				if !slices.ContainsFunc(f.Fix.SideEffects, func(s string) bool { return s == "re-approves ~/repo/.aide.yaml after writing it" }) {
					t.Errorf("side effects = %v, want the re-approval named", f.Fix.SideEffects)
				}
				return
			}
			if len(f.Fix.Argv) != 0 || !slices.Contains(f.Fix.Steps, "Run: aide trust") {
				t.Errorf("fix = %+v, want steps ending in aide trust and no command", f.Fix)
			}
		})
	}
}
