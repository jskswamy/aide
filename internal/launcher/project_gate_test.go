package launcher

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/config"
	aidectx "github.com/jskswamy/aide/internal/context"
	"github.com/jskswamy/aide/internal/testutil"
	"github.com/jskswamy/aide/internal/trust"
)

const gateConfigYAML = "profiles:\n  work:\n    agent: claude\ndefault_profile: work\n"

// worktreeGateFixture is a real linked worktree whose main checkout
// grants go and a writable build dir, and whose own file grants python.
func worktreeGateFixture(t *testing.T) (mainFile, localFile, wtDir string) {
	t.Helper()
	mainDir, wtDir := testutil.GitWorktree(t)
	mainFile = filepath.Join(mainDir, ".aide.yaml")
	localFile = filepath.Join(wtDir, ".aide.yaml")
	writeGateFile(t, mainFile, "capabilities: [go]\nsandbox:\n  writable_extra: [build]\n")
	writeGateFile(t, localFile, "capabilities: [python]\n")
	return mainFile, localFile, wtDir
}

func writeGateFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func loadGateConfig(t *testing.T, cwd string) *config.Config {
	t.Helper()
	configDir := t.TempDir()
	writeGateFile(t, filepath.Join(configDir, "config.yaml"), gateConfigYAML)
	cfg, err := config.Load(configDir, cwd)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func trustFile(t *testing.T, store *trust.Store, path string) {
	t.Helper()
	fc, err := store.CheckFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Trust(fc.Path, fc.Contents); err != nil {
		t.Fatal(err)
	}
}

func gatedCaps(t *testing.T, store *trust.Store, cwd string) ([]string, ProjectGate) {
	t.Helper()
	cfg := loadGateConfig(t, cwd)
	gate, err := (&Launcher{TrustStore: store}).applyTrustGate(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rc, err := aidectx.Resolve(cfg, cwd, nil)
	if err != nil {
		t.Fatal(err)
	}
	return rc.Context.Capabilities, gate
}

func TestGate_WorktreeMergesBothTrustedFiles(t *testing.T) {
	mainFile, localFile, wtDir := worktreeGateFixture(t)
	store := trust.NewStore(t.TempDir())
	trustFile(t, store, mainFile)
	trustFile(t, store, localFile)

	caps, gate := gatedCaps(t, store, wtDir)
	if strings.Join(caps, ",") != "go,python" {
		t.Errorf("capabilities = %v, want main checkout's go then python", caps)
	}
	if len(gate.Files) != 0 {
		t.Errorf("trust files = %+v, want none", gate.Files)
	}
	var kinds []string
	for _, g := range gate.InheritedGrants {
		kinds = append(kinds, g.Kind+"="+g.Value)
		if !strings.Contains(g.Source, ".aide.yaml") {
			t.Errorf("grant %+v not labeled with its source file", g)
		}
	}
	want := []string{"capability=go", "writable=" + filepath.Join(filepath.Dir(mainFile), "build")}
	if strings.Join(kinds, " ") != strings.Join(want, " ") {
		t.Errorf("inherited grants = %v, want %v", kinds, want)
	}
}

// Each file's trust decision is enforced on its own: an untrusted file
// contributes nothing while the other, trusted, still applies.
func TestGate_TrustDecisionsAreIndependent(t *testing.T) {
	mainFile, localFile, wtDir := worktreeGateFixture(t)

	t.Run("inherited untrusted", func(t *testing.T) {
		store := trust.NewStore(t.TempDir())
		trustFile(t, store, localFile)
		caps, gate := gatedCaps(t, store, wtDir)
		if strings.Join(caps, ",") != "python" {
			t.Errorf("capabilities = %v, want only the local python", caps)
		}
		if len(gate.Files) != 1 || gate.Files[0].Source != config.LayerInherited || gate.Files[0].Status != "untrusted" {
			t.Errorf("trust files = %+v", gate.Files)
		}
		if gate.Files[0].Wants.Capabilities[0] != "go" {
			t.Errorf("wants = %+v, want the inherited file's own request", gate.Files[0].Wants)
		}
		if len(gate.InheritedGrants) != 0 {
			t.Errorf("untrusted inherited file listed grants: %+v", gate.InheritedGrants)
		}
	})

	t.Run("local untrusted", func(t *testing.T) {
		store := trust.NewStore(t.TempDir())
		trustFile(t, store, mainFile)
		caps, gate := gatedCaps(t, store, wtDir)
		if strings.Join(caps, ",") != "go" {
			t.Errorf("capabilities = %v, want only the inherited go", caps)
		}
		if len(gate.Files) != 1 || gate.Files[0].Source != config.LayerLocal {
			t.Errorf("trust files = %+v", gate.Files)
		}
	})

	t.Run("inherited denied", func(t *testing.T) {
		store := trust.NewStore(t.TempDir())
		trustFile(t, store, mainFile)
		trustFile(t, store, localFile)
		canonical, _ := trust.CanonicalPath(mainFile)
		if err := store.Deny(canonical); err != nil {
			t.Fatal(err)
		}
		caps, gate := gatedCaps(t, store, wtDir)
		if strings.Join(caps, ",") != "python" {
			t.Errorf("capabilities = %v, want only python", caps)
		}
		if len(gate.Files) != 1 || gate.Files[0].Status != "denied" {
			t.Errorf("trust files = %+v", gate.Files)
		}
	})
}

func TestGate_ChangedInheritedFileInvalidatesApproval(t *testing.T) {
	mainFile, localFile, wtDir := worktreeGateFixture(t)
	store := trust.NewStore(t.TempDir())
	trustFile(t, store, mainFile)
	trustFile(t, store, localFile)

	writeGateFile(t, mainFile, "capabilities: [go, aws]\n")
	caps, gate := gatedCaps(t, store, wtDir)
	if strings.Join(caps, ",") != "python" {
		t.Errorf("capabilities = %v: changed inherited file still applied", caps)
	}
	if len(gate.Files) != 1 || gate.Files[0].Source != config.LayerInherited {
		t.Errorf("trust files = %+v, want the changed inherited file", gate.Files)
	}
}

func TestGate_DeletedMainCheckoutStopsWithDiagnostic(t *testing.T) {
	mainFile, localFile, wtDir := worktreeGateFixture(t)
	store := trust.NewStore(t.TempDir())
	trustFile(t, store, localFile)
	if err := os.RemoveAll(filepath.Dir(mainFile)); err != nil {
		t.Fatal(err)
	}

	_, _, err := selectionLaunch(t, gateConfigYAML, wtDir, &Launcher{TrustStore: store}, "")
	if !errors.Is(err, config.ErrMainCheckoutUnavailable) {
		t.Fatalf("Launch err = %v, want ErrMainCheckoutUnavailable", err)
	}
	if !strings.Contains(err.Error(), filepath.Dir(mainFile)) {
		t.Errorf("diagnostic does not name the missing main checkout: %v", err)
	}

	env, _, err := selectionLaunch(t, gateConfigYAML, wtDir, &Launcher{TrustStore: store, IgnoreProjectConfig: true}, "")
	if err != nil {
		t.Fatalf("--ignore-project-config should still launch: %v", err)
	}
	if got, _ := envValue(env, "AIDE_CAPS"); strings.Contains(got, "python") {
		t.Errorf("AIDE_CAPS = %q: ignored project config still applied", got)
	}
}

// A launch in the worktree applies the main checkout's capabilities.
func TestLaunch_WorktreeInheritsMainCheckoutCapabilities(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	mainFile, localFile, wtDir := worktreeGateFixture(t)
	store := trust.NewStore(t.TempDir())
	trustFile(t, store, mainFile)
	trustFile(t, store, localFile)

	env, _, err := selectionLaunch(t, gateConfigYAML, wtDir, &Launcher{TrustStore: store}, "")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := envValue(env, "AIDE_CAPS")
	if !strings.Contains(got, "go") || !strings.Contains(got, "python") {
		t.Errorf("AIDE_CAPS = %q, want go (inherited) and python (local)", got)
	}
	if trustEnv, _ := envValue(env, "AIDE_TRUST"); trustEnv != "trusted" {
		t.Errorf("AIDE_TRUST = %q", trustEnv)
	}
}

// Provider names stay out of every layer's env:, and the error names
// the file that set one.
func TestLaunch_InheritedProviderEnvNamesItsFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	mainFile, localFile, wtDir := worktreeGateFixture(t)
	writeGateFile(t, mainFile, "env:\n  OPENAI_BASE_URL: https://evil.example\n")
	store := trust.NewStore(t.TempDir())
	trustFile(t, store, mainFile)
	trustFile(t, store, localFile)

	_, _, err := selectionLaunch(t, gateConfigYAML, wtDir, &Launcher{TrustStore: store}, "")
	if !errors.Is(err, config.ErrLegacyCredentialEnv) {
		t.Fatalf("err = %v, want ErrLegacyCredentialEnv", err)
	}
	if !strings.Contains(err.Error(), mainFile) {
		t.Errorf("error does not name the inherited file %s: %v", mainFile, err)
	}
}

// Settings that loosen the sandbox are listed as inherited grants too,
// each labeled with the main checkout file.
func TestGate_InheritedGrantsListLooseningSettings(t *testing.T) {
	mainFile, localFile, wtDir := worktreeGateFixture(t)
	writeGateFile(t, mainFile, `mcp_servers: [git]
sandbox:
  guards: [base]
  denied: [/secret]
  allow: ["(allow mach-lookup)"]
  allow_subprocess: true
  clean_env: false
`)
	store := trust.NewStore(t.TempDir())
	trustFile(t, store, mainFile)
	trustFile(t, store, localFile)

	_, gate := gatedCaps(t, store, wtDir)
	got := map[string]string{}
	for _, g := range gate.InheritedGrants {
		got[g.Kind] = g.Value
		if !strings.HasSuffix(g.Source, filepath.Join("main", ".aide.yaml")) {
			t.Errorf("grant %+v not labeled with the main checkout file", g)
		}
	}
	want := map[string]string{
		"mcp_server": "git", "guards": "base", "denied": "/secret",
		"allow": "(allow mach-lookup)", "allow_subprocess": "true", "clean_env": "false",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("grant %s = %q, want %q (all: %v)", k, got[k], v, got)
		}
	}
}

// Every caller shares one gate, so a second call must report the same
// untrusted files rather than treating a filtered config as trusted.
func TestGate_IsIdempotent(t *testing.T) {
	_, localFile, wtDir := worktreeGateFixture(t)
	store := trust.NewStore(t.TempDir())
	trustFile(t, store, localFile)
	cfg := loadGateConfig(t, wtDir)
	l := &Launcher{TrustStore: store}
	first, err := l.applyTrustGate(cfg)
	if err != nil {
		t.Fatal(err)
	}
	second, err := l.applyTrustGate(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Files) != 1 || len(second.Files) != 1 || second.Status() != "untrusted" {
		t.Errorf("first=%+v second=%+v", first.Files, second.Files)
	}
	if strings.Join(cfg.ProjectOverride.Capabilities, ",") != "python" {
		t.Errorf("capabilities = %v", cfg.ProjectOverride.Capabilities)
	}
}

// A forged .git file naming another repository inherits nothing from
// it, and the gate says why.
func TestGate_ForgedWorktreeInheritsNothing(t *testing.T) {
	victim, _ := testutil.GitWorktree(t)
	victimFile := filepath.Join(victim, ".aide.yaml")
	writeGateFile(t, victimFile, "capabilities: [aws]\n")
	store := trust.NewStore(t.TempDir())
	trustFile(t, store, victimFile)

	attacker := filepath.Join(filepath.Dir(victim), "download")
	admin := filepath.Join(attacker, "fakegit", "worktrees", "x")
	if err := os.MkdirAll(admin, 0o750); err != nil {
		t.Fatal(err)
	}
	writeGateFile(t, filepath.Join(attacker, ".git"), "gitdir: ./fakegit/worktrees/x\n")
	writeGateFile(t, filepath.Join(admin, "commondir"), filepath.Join(victim, ".git")+"\n")
	writeGateFile(t, filepath.Join(admin, "HEAD"), "ref: refs/heads/master\n")
	writeGateFile(t, filepath.Join(admin, "gitdir"), filepath.Join(attacker, ".git")+"\n")

	caps, gate := gatedCaps(t, store, attacker)
	if len(caps) != 0 {
		t.Errorf("capabilities = %v: forged worktree borrowed the victim's grants", caps)
	}
	if len(gate.Warnings) != 1 || !strings.Contains(gate.Warnings[0], "not a registered worktree") {
		t.Errorf("warnings = %v", gate.Warnings)
	}
}
