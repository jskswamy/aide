package launcher

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/launcher/mocks"
)

// A launch upgrades legacy gateways on disk, with backups and a summary,
// and goes on with the migrated config.
func TestLaunchMigratesLegacyGateways(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "")
	cfgDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cfgDir, "profiles"), 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(rel, body string) {
		if err := os.WriteFile(filepath.Join(cfgDir, rel), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("config.yaml", "agents:\n  claude: {binary: claude}\ndefault_profile: work\n")
	write("profiles/work.yaml", "agent: claude\nconfig_dir_name: work\ngateways:\n  corp:\n    base_url: http://127.0.0.1:1\n    harnesses: [claude]\n")

	mockExec := mocks.NewMockExecer(gomock.NewController(t))
	mockExec.EXPECT().Exec(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).MaxTimes(1)
	var stderr bytes.Buffer
	l := &Launcher{Execer: mockExec, ConfigDir: cfgDir, Stderr: &stderr, LookPath: okLookPath}
	if err := l.Launch(t.TempDir(), "", nil, false, false, nil, nil); err != nil {
		t.Fatalf("Launch() = %v\n%s", err, stderr.String())
	}
	if !strings.Contains(stderr.String(), "migrated gateway config") {
		t.Errorf("stderr lacks the migration summary:\n%s", stderr.String())
	}
	cfg, err := config.Load(cfgDir, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.NeedsGatewayMigration || cfg.Contexts["work"].GatewayChoice["claude"] != "corp" {
		t.Errorf("work after launch = %+v", cfg.Contexts["work"])
	}
	backups, _ := filepath.Glob(filepath.Join(home, ".local", "state", "aide", "migrations", "*", "profiles", "work.yaml.bak"))
	if len(backups) != 1 {
		t.Errorf("backups = %v, want the legacy profile kept once", backups)
	}
}
