//go:build linux

package launcher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jskswamy/aide/internal/gateway"
)

// TestLauncher_GatewayUnisolatedProfileWritesToLinuxSandboxRedirect pins
// that launching Claude on a profile whose Profile stays empty (because
// another harness's env var is set) on Linux materializes
// gateway-models.json into ~/.config/aide/claude/cache/ — the same
// CLAUDE_CONFIG_DIR the Linux sandbox injects at launch.
func TestLauncher_GatewayUnisolatedProfileWritesToLinuxSandboxRedirect(t *testing.T) {
	fakeGatewayStore(t)
	unsetEnvForTest(t, "CLAUDE_CONFIG_DIR")
	gs := newGatewayServer(t, `{"data":[]}`)
	home := t.TempDir()

	cat := &gateway.CachedCatalogue{
		Profile:   "default",
		Gateway:   "corp",
		BaseURL:   gs.URL,
		FetchedAt: time.Now(),
		Models:    []gateway.Model{{ID: "corp-model", DisplayName: "corp-model"}},
	}
	if err := gateway.WriteCache(home, cat); err != nil {
		t.Fatal(err)
	}

	cfg := `
profiles:
  default:
    agent: claude
    env:
      CODEX_HOME: ~/.codex-custom
    gateways:
      corp:
        base_url: ` + gs.URL + `
        store: default
        key: corp_token
default_profile: default
`
	env, _ := launchGateway(t, home, cfg, "claude", "")
	wantDir := filepath.Join(home, ".config", "aide", "claude")
	if got, _ := envValue(env, "CLAUDE_CONFIG_DIR"); got != wantDir {
		t.Fatalf("CLAUDE_CONFIG_DIR = %q, want %q", got, wantDir)
	}
	wantFile := filepath.Join(wantDir, "cache", "gateway-models.json")
	data, err := os.ReadFile(wantFile)
	if err != nil {
		t.Fatalf("expected gateway-models.json in redirected dir %q: %v", wantFile, err)
	}
	if !strings.Contains(string(data), "corp-model") {
		t.Errorf("gateway-models.json = %s, want corp-model", data)
	}
}
