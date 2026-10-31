package launcher

import (
	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/launcher/mocks"
	"github.com/jskswamy/aide/internal/piinstall/testfixture"
	"github.com/jskswamy/aide/internal/sandbox"
	"go.uber.org/mock/gomock"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

func TestPrepareAgentSandboxPiManaged(t *testing.T) {
	f := testfixture.New(t)
	work := filepath.Join(f.Home, ".pi-work")
	p := sandbox.DefaultPolicy(sandbox.Paths{HomeDir: f.Home, ProjectRoot: t.TempDir(), RuntimeDir: t.TempDir(), TempDir: t.TempDir()}, []string{"HOME=" + f.Home, "PI_CODING_AGENT_DIR=" + work})
	p.AgentModule = ResolveAgentModule("pi")
	cfg := &config.Config{Contexts: map[string]config.Context{"work": {Profile: "work", Agent: "pi"}}}
	if err := PrepareAgentSandbox(&p, cfg, "pi", f.Launcher); err != nil {
		t.Fatal(err)
	}
	r := p.AgentModule.Rules(p.ToSeatbeltContext())
	if !slices.Contains(r.Writable, f.Root) {
		t.Errorf("missing install grant: %v", r.Writable)
	}
	if len(p.ExtraWritable) > 0 || len(p.ExtraReadable) > 0 {
		t.Fatal("automatic grants used opt-out channels")
	}
}

func TestPrepareAgentSandboxRejectsPrivateProfile(t *testing.T) {
	f := testfixture.New(t)
	p := sandbox.DefaultPolicy(sandbox.Paths{HomeDir: f.Home}, []string{"HOME=" + f.Home, "PI_CODING_AGENT_DIR=" + filepath.Join(f.Home, ".pi-work")})
	p.AgentModule = ResolveAgentModule("pi")
	cfg := &config.Config{Contexts: map[string]config.Context{"personal": {Profile: "personal", ProfileDir: filepath.Join(f.Root, "other-profile")}}}
	before := p
	if err := PrepareAgentSandbox(&p, cfg, "pi", f.Launcher); err == nil {
		t.Fatal("private profile exposed")
	}
	if !reflect.DeepEqual(p, before) {
		t.Fatal("failed preparation mutated policy")
	}
}

func TestPrepareAgentSandboxNonPiUnchanged(t *testing.T) {
	p := sandbox.Policy{}
	if err := PrepareAgentSandbox(&p, nil, "claude", "/missing"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p, sandbox.Policy{}) {
		t.Fatal("non-Pi changed")
	}
}

func TestLaunchPiManagedRejectedBeforeExec(t *testing.T) {
	f := testfixture.New(t)
	t.Setenv("HOME", f.Home)
	testfixture.Write(t, filepath.Join(f.Root, "managed-install.json"), "{", 0600)
	dir := t.TempDir()
	writeMinimalConfig(t, dir, `profiles:
  work:
    agent: pi
    config_dir_name: work
default_profile: work
`)
	l := &Launcher{ConfigDir: dir, Execer: mocks.NewMockExecer(gomock.NewController(t)), LookPath: func(string) (string, error) { return f.Launcher, nil }}
	if err := l.Launch(t.TempDir(), "pi", nil, false, false, nil, nil); err == nil {
		t.Fatal("unsafe candidate reached execution")
	}
}

func TestLaunchPiManagedScrubsSessionRedirect(t *testing.T) {
	f := testfixture.New(t)
	t.Setenv("HOME", f.Home)
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", "/other/sessions")
	// Existing launch capture exercises profile scrubbing; a managed module is
	// also prepared directly above, without ever running a developer's Pi.
	env, _ := launchCapturingEnvAndStderr(t, `profiles:
  work:
    agent: pi
    config_dir_name: work
default_profile: work
`, "pi")
	for _, v := range env {
		if len(v) >= len("PI_CODING_AGENT_SESSION_DIR=") && v[:len("PI_CODING_AGENT_SESSION_DIR=")] == "PI_CODING_AGENT_SESSION_DIR=" {
			t.Fatal("session redirect leaked")
		}
	}
}
