package launcher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/launcher/mocks"
	"github.com/jskswamy/aide/internal/sandbox"
	"go.uber.org/mock/gomock"
)

func TestLauncher_OMPProfileSetsConfigDirAndConfigRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("OMP_PROFILE", "leaked-profile")
	t.Setenv("PI_PROFILE", "leaked-pi-profile")
	t.Setenv("PI_CONFIG_FILES", "/tmp/leaked-overlay.yml")
	t.Setenv("OMP_AUTH_BROKER_TOKEN", "leaked-broker-token")
	t.Setenv("OMP_AUTH_BROKER_URL", "https://broker.example.com")

	env := launchCapturingEnv(t, `
agents:
  omp:
    binary: omp
profiles:
  work:
    agent: omp
    config_dir_name: work
    sandbox:
      clean_env: true
default_profile: work
`, "")

	if got, ok := envValue(env, "PI_CODING_AGENT_DIR"); !ok || got != filepath.Join(home, ".omp-work") {
		t.Errorf("PI_CODING_AGENT_DIR = %q (ok=%v), want %q", got, ok, filepath.Join(home, ".omp-work"))
	}
	if got, ok := envValue(env, "PI_CONFIG_DIR"); !ok || got != ".omp-work" {
		t.Errorf("PI_CONFIG_DIR = %q (ok=%v), want .omp-work", got, ok)
	}
	for _, scrubbed := range []string{
		"OMP_PROFILE",
		"PI_PROFILE",
		"PI_CONFIG_FILES",
		"OMP_AUTH_BROKER_TOKEN",
		"OMP_AUTH_BROKER_URL",
	} {
		if got, ok := envValue(env, scrubbed); ok {
			t.Errorf("%s leaked into omp profile launch: %q", scrubbed, got)
		}
	}
}

func TestLauncher_OMPDefaultProfileKeepsGlobalLayout(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	env := launchCapturingEnv(t, `
agents:
  omp:
    binary: omp
profiles:
  default:
    agent: omp
default_profile: default
`, "")

	if got, ok := envValue(env, "PI_CODING_AGENT_DIR"); ok {
		t.Errorf("PI_CODING_AGENT_DIR set on default profile: %q", got)
	}
	if got, ok := envValue(env, "PI_CONFIG_DIR"); ok {
		t.Errorf("PI_CONFIG_DIR set on default profile: %q", got)
	}
}

func TestLauncher_OMPProfileBlockedByNeverAllowEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfgDir := t.TempDir()
	projDir := t.TempDir()

	cfgYAML := `
never_allow_env:
  - PI_CONFIG_DIR
agents:
  omp:
    binary: omp
profiles:
  work:
    agent: omp
    config_dir_name: work
    sandbox: none
default_profile: work
`
	writeMinimalConfig(t, cfgDir, cfgYAML)
	ctrl := gomock.NewController(t)
	mockExec := mocks.NewMockExecer(ctrl)
	l := &Launcher{
		ConfigDir: cfgDir,
		Execer:    mockExec,
		LookPath: func(file string) (string, error) {
			return "/usr/local/bin/" + file, nil
		},
	}
	err := l.Launch(projDir, "", nil, false, false, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "never_allow_env") {
		t.Fatalf("Launch() error = %v, want never_allow_env error", err)
	}
}

func TestPrepareOMPSandboxDirs_LinuxCreatesIsolatedAndRuntimeDirs(t *testing.T) {
	home := t.TempDir()
	workDir := filepath.Join(home, ".omp-fresh")
	p := &sandbox.Policy{
		HomeDir: home,
		Env:     []string{"PI_CODING_AGENT_DIR=" + workDir},
	}
	if err := prepareOMPSandboxDirs(p, "linux"); err != nil {
		t.Fatalf("prepareOMPSandboxDirs: %v", err)
	}
	for _, want := range []string{
		workDir,
		filepath.Join(home, ".omp", "natives"),
		filepath.Join(home, ".omp", "logs"),
		filepath.Join(home, ".omp", "run"),
	} {
		if info, err := os.Stat(want); err != nil || !info.IsDir() {
			t.Errorf("expected directory %s to exist, stat err=%v", want, err)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".omp", "agent")); !os.IsNotExist(err) {
		t.Errorf("unexpected ~/.omp/agent created on profile launch: err=%v", err)
	}

	sshOverride := filepath.Join(home, ".ssh", "omp")
	if err := prepareOMPSandboxDirs(&sandbox.Policy{
		HomeDir: home,
		Env:     []string{"PI_CODING_AGENT_DIR=" + sshOverride},
	}, "linux"); err != nil {
		t.Fatalf("prepareOMPSandboxDirs(sensitive): %v", err)
	}
	if _, err := os.Stat(sshOverride); !os.IsNotExist(err) {
		t.Errorf("sensitive override %s must not be created", sshOverride)
	}

	darwinHome := t.TempDir()
	if err := prepareOMPSandboxDirs(&sandbox.Policy{HomeDir: darwinHome}, "darwin"); err != nil {
		t.Fatalf("prepareOMPSandboxDirs(darwin): %v", err)
	}
	if _, err := os.Stat(filepath.Join(darwinHome, ".omp")); !os.IsNotExist(err) {
		t.Error("prepareOMPSandboxDirs should be a no-op on darwin")
	}
}
