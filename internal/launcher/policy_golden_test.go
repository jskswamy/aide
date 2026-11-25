package launcher

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/jskswamy/aide/internal/launcher/mocks"
	"github.com/jskswamy/aide/internal/sandbox"
	"github.com/jskswamy/aide/internal/testutil"
)

// The launch policy goldens lock the sandbox policy Launch builds, so
// moving its capability and sandbox step can be checked byte for byte.
// Regenerate with go test ./internal/launcher/ -run Golden -update.

const goldenPolicyConfig = `
profiles:
  work:
    agent: claude
    capabilities: [go]
    sandbox:
      readable_extra: ["~/data"]
      writable_extra: ["~/out"]
      network:
        mode: outbound
        allow_ports: [443]
        deny_ports: [25]
default_profile: work
`

func TestGolden_LaunchPolicy(t *testing.T) {
	for _, tc := range []struct {
		name         string
		unrestricted bool
	}{
		{name: "capabilities"},
		{name: "unrestricted", unrestricted: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			// Host XDG dirs would add the user's Skillshare paths and
			// create runtime dirs outside the test tree.
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
			t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
			mkGoldenDirs(t, home)
			configDir, cwd := t.TempDir(), t.TempDir()
			writeMinimalConfig(t, configDir, goldenPolicyConfig)
			mockExec := mocks.NewMockExecer(gomock.NewController(t))
			mockExec.EXPECT().Exec(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
			var got *sandbox.Policy
			l := &Launcher{
				ConfigDir: configDir, Execer: mockExec, LookPath: okLookPath,
				Stderr: &strings.Builder{}, UnrestrictedNetwork: tc.unrestricted,
				PolicyHook: func(p sandbox.Policy) { got = &p },
			}
			if err := l.Launch(cwd, "", nil, false, false, nil, nil); err != nil {
				t.Fatal(err)
			}
			if got == nil {
				t.Fatal("PolicyHook was not called")
			}
			if tc.unrestricted && (got.Network != "unrestricted" || len(got.AllowPorts) != 0 || len(got.DenyPorts) != 0) {
				t.Errorf("-N: network = %q, allow = %v, deny = %v; want unrestricted with no port rules", got.Network, got.AllowPorts, got.DenyPorts)
			}
			// Host- and run-specific fields are left out.
			p := *got
			p.Env, p.AgentModule, p.RuntimeDir = nil, nil, ""
			p.TempDir = filepath.Clean(p.TempDir) // some runners end os.TempDir() in a slash
			b, err := json.MarshalIndent(p, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			out := testutil.NormalizeGolden(t, string(b)+"\n", map[string]string{home: "$HOME", cwd: "$CWD", os.TempDir(): "$TMP"})
			testutil.AssertGolden(t, "launch_policy_"+tc.name+".json", out)
		})
	}
}

// mkGoldenDirs creates the paths the golden fixtures grant, since the
// policy drops paths that do not exist.
func mkGoldenDirs(t *testing.T, home string) {
	t.Helper()
	for _, d := range []string{"data", "out", "go"} {
		if err := os.MkdirAll(filepath.Join(home, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}
