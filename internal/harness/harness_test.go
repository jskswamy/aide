package harness_test

import (
	"fmt"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/harness"
	"github.com/jskswamy/aide/pkg/seatbelt"
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
		{"antigravity", "agy"},
		{"agy", "agy"},
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
		{"opencode", "OPENCODE_CONFIG_DIR"},
		{"cursor-agent", ""},
		{"aider", ""},
		{"amp", ""},
		{"agy", ""},
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
		{"agy", []string{"--dangerously-skip-permissions"}, true},
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

func TestApplyYolo(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		// Claude: top-level flag prepended
		{"claude", []string{"--model", "opus"}, []string{"--dangerously-skip-permissions", "--model", "opus"}},
		{"claude", []string{"--dangerously-skip-permissions", "--model", "opus"}, []string{"--dangerously-skip-permissions", "--model", "opus"}},
		{"claude", nil, []string{"--dangerously-skip-permissions"}},

		// Pi: yolo needs no flag
		{"pi", []string{"--model", "opus"}, []string{"--model", "opus"}},

		// Aider: yolo unsupported
		{"aider", []string{"--model", "opus"}, []string{"--model", "opus"}},

		// OpenCode: root invocation (no args, flags, or directory) gets --auto prepended
		{"opencode", nil, []string{"--auto"}},
		{"opencode", []string{"-c"}, []string{"--auto", "-c"}},
		{"opencode", []string{"/path/to/project"}, []string{"--auto", "/path/to/project"}},

		// OpenCode: 'run' subcommand gets --auto inserted after 'run'
		{"opencode", []string{"run", "hello"}, []string{"run", "--auto", "hello"}},
		{"opencode", []string{"run", "-m", "model", "hello"}, []string{"run", "--auto", "-m", "model", "hello"}},
		{"opencode", []string{"run", "--auto", "hello"}, []string{"run", "--auto", "hello"}},

		// OpenCode: subcommands that reject --auto leave args unchanged
		{"opencode", []string{"models"}, []string{"models"}},
		{"opencode", []string{"auth", "list"}, []string{"auth", "list"}},
		{"opencode", []string{"mcp", "list"}, []string{"mcp", "list"}},
		{"opencode", []string{"stats"}, []string{"stats"}},
		{"opencode", []string{"upgrade"}, []string{"upgrade"}},
	}

	for _, tc := range tests {
		d, ok := harness.Lookup(tc.name)
		if !ok {
			t.Fatalf("harness %q not found", tc.name)
		}
		got := d.ApplyYolo(tc.input)
		if !slices.Equal(got, tc.expected) {
			t.Errorf("%s with %v: ApplyYolo() = %v, want %v", tc.name, tc.input, got, tc.expected)
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

func TestProfileConfigDir(t *testing.T) {
	home := "/home/u"
	if got := harness.ProfileConfigDir("claude", "work", "", home); got != "/home/u/.claude-work" {
		t.Errorf("derived dir = %q", got)
	}
	if got := harness.ProfileConfigDir("claude", "work", "~/x/y", home); got != "/home/u/x/y" {
		t.Errorf("override dir = %q", got)
	}
}

// pkg/seatbelt modules hard-code their config-dir variable because they
// cannot import internal/. This keeps them agreeing with the table: a
// module must grant the dir its harness's ProfileEnvKey points at.
func TestSandboxModulesHonorProfileEnvKey(t *testing.T) {
	for _, d := range harness.All() {
		if !d.SupportsProfiles() || !d.Launchable() {
			continue
		}
		home := t.TempDir()
		dir := filepath.Join(home, "sentinel-"+d.Name)
		ctx := &seatbelt.Context{
			HomeDir: home,
			GOOS:    runtime.GOOS,
			Env:     []string{d.ProfileEnvKey + "=" + dir},
		}
		if got := fmt.Sprint(d.Sandbox().Rules(ctx)); !strings.Contains(got, dir) {
			t.Errorf("%s module ignores %s=%s", d.Name, d.ProfileEnvKey, dir)
		}
	}
}

func TestDefaultDir(t *testing.T) {
	pi, _ := harness.Lookup("pi")
	if got, want := pi.DefaultDir("/home/u"), filepath.Join("/home/u", ".pi", "agent"); got != want {
		t.Errorf("pi DefaultDir = %q, want %q", got, want)
	}
	aider, _ := harness.Lookup("aider")
	if got := aider.DefaultDir("/home/u"); got != "" {
		t.Errorf("aider DefaultDir = %q, want empty (no known dir)", got)
	}
}
