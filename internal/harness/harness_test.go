package harness_test

import (
	"slices"
	"testing"

	"github.com/jskswamy/aide/internal/harness"
)

func TestAllIsSortedAndNamesAreUnique(t *testing.T) {
	all := harness.All()
	if len(all) == 0 {
		t.Fatal("no descriptors")
	}
	seen := map[string]string{}
	for i, d := range all {
		if i > 0 && all[i-1].Name >= d.Name {
			t.Errorf("All() not sorted: %q before %q", all[i-1].Name, d.Name)
		}
		for _, n := range d.Names() {
			if owner, dup := seen[n]; dup {
				t.Errorf("name %q claimed by both %q and %q", n, owner, d.Name)
			}
			seen[n] = d.Name
		}
	}
}

func TestLookupResolvesNamesAndAliases(t *testing.T) {
	tests := []struct{ in, want string }{
		{"claude", "claude"},
		{"cursor-agent", "cursor-agent"},
		{"cursor", "cursor-agent"},
		{"hermes", "hermes"},
	}
	for _, tc := range tests {
		d, ok := harness.Lookup(tc.in)
		if !ok || d.Name != tc.want {
			t.Errorf("Lookup(%q) = %q, %v; want %q", tc.in, d.Name, ok, tc.want)
		}
	}
	if _, ok := harness.Lookup("vim"); ok {
		t.Error("Lookup(vim) found a descriptor")
	}
}

func TestProfileSupport(t *testing.T) {
	tests := []struct {
		name   string
		envKey string
	}{
		{"claude", "CLAUDE_CONFIG_DIR"},
		{"codex", "CODEX_HOME"},
		{"pi", "PI_CODING_AGENT_DIR"},
		{"copilot", "COPILOT_HOME"},
		{"gemini", "GEMINI_HOME"},
		{"opencode", ""},
		{"cursor-agent", ""},
		{"aider", ""},
		{"amp", ""},
		{"goose", ""},
		{"hermes", ""},
	}
	for _, tc := range tests {
		d, ok := harness.Lookup(tc.name)
		if !ok {
			t.Fatalf("no descriptor for %q", tc.name)
		}
		if d.ProfileEnvKey != tc.envKey {
			t.Errorf("%s: ProfileEnvKey = %q, want %q", tc.name, d.ProfileEnvKey, tc.envKey)
		}
		if d.SupportsProfiles() != (tc.envKey != "") {
			t.Errorf("%s: SupportsProfiles() = %v", tc.name, d.SupportsProfiles())
		}
	}
}

// The profile key is one of the harness's config-dir redirects, so a
// scrub list derived from ConfigEnvKeys always covers it.
func TestProfileEnvKeyIsAConfigEnvKey(t *testing.T) {
	for _, d := range harness.All() {
		if d.ProfileEnvKey != "" && !slices.Contains(d.ConfigEnvKeys, d.ProfileEnvKey) {
			t.Errorf("%s: ProfileEnvKey %q missing from ConfigEnvKeys %v", d.Name, d.ProfileEnvKey, d.ConfigEnvKeys)
		}
		if d.ProfileEnvKey != "" && d.DefaultConfigDir == "" {
			t.Errorf("%s: profile-capable harness has no DefaultConfigDir", d.Name)
		}
	}
}

func TestYoloArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		ok   bool
	}{
		{"claude", []string{"--dangerously-skip-permissions"}, true},
		{"pi", nil, true},
		{"aider", nil, false},
	}
	for _, tc := range tests {
		d, _ := harness.Lookup(tc.name)
		args, ok := d.YoloArgs()
		if ok != tc.ok || !slices.Equal(args, tc.args) {
			t.Errorf("%s: YoloArgs() = %v, %v; want %v, %v", tc.name, args, ok, tc.args, tc.ok)
		}
	}
}

func TestAllReturnsACopy(t *testing.T) {
	all := harness.All()
	all[0].Name = "mutated"
	all[0].Aliases = append(all[0].Aliases, "x")
	if harness.All()[0].Name == "mutated" {
		t.Error("All() exposes the registry for mutation")
	}
}
