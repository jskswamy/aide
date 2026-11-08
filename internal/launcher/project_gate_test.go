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

// A registered worktree whose main checkout .aide.yaml is a symlink to
// another, trusted file inherits nothing from it: the link needs its
// own approval, and the trust prompt shows where it points.
func TestGate_SymlinkedInheritedFileIsUntrusted(t *testing.T) {
	mainFile, localFile, wtDir := worktreeGateFixture(t)
	victim := filepath.Join(t.TempDir(), ".aide.yaml")
	writeGateFile(t, victim, "capabilities: [aws]\nsandbox: false\n")
	store := trust.NewStore(t.TempDir())
	trustFile(t, store, victim)
	trustFile(t, store, localFile)
	if err := os.Remove(mainFile); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, mainFile); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	caps, gate := gatedCaps(t, store, wtDir)
	if strings.Join(caps, ",") != "python" {
		t.Errorf("capabilities = %v: symlinked inherited file borrowed the target's approval", caps)
	}
	if len(gate.Files) != 1 || gate.Files[0].Source != config.LayerInherited || gate.Files[0].Target == "" {
		t.Errorf("trust files = %+v, want the inherited link with its target", gate.Files)
	}
}

// A checkout reached through a symlinked parent directory keeps one
// identity: approval given through the real path holds.
func TestGate_SymlinkedCheckoutDirKeepsOneIdentity(t *testing.T) {
	_, localFile, wtDir := worktreeGateFixture(t)
	store := trust.NewStore(t.TempDir())
	trustFile(t, store, localFile)
	link := filepath.Join(t.TempDir(), "wt-link")
	if err := os.Symlink(wtDir, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	caps, gate := gatedCaps(t, store, link)
	if strings.Join(caps, ",") != "python" {
		t.Errorf("capabilities = %v via symlinked dir", caps)
	}
	for _, f := range gate.Files {
		if f.Source == config.LayerLocal {
			t.Errorf("local file untrusted via symlinked dir: %+v", f)
		}
	}
}

func TestProjectGate_InteractiveTrust_YesPersistsAndMerges(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cwd := t.TempDir()
	aideFile := filepath.Join(cwd, ".aide.yaml")
	writeGateFile(t, aideFile, "agent: pi\ncapabilities: [go]\n")
	store := trust.NewStore(t.TempDir())

	l := &Launcher{
		TrustStore:  store,
		Interactive: true,
		Stdin:       strings.NewReader("y\n"),
	}
	env, stderr, err := selectionLaunch(t, selectionConfig, cwd, l, "")
	if err != nil {
		t.Fatalf("Launch error: %v", err)
	}
	if !strings.Contains(stderr, "[aide] Untrusted .aide.yaml at") {
		t.Errorf("stderr missing trust prompt header:\n%s", stderr)
	}
	if !strings.Contains(stderr, "wants: pi agent · go capability") {
		t.Errorf("stderr missing wants summary:\n%s", stderr)
	}
	if !strings.Contains(stderr, "Trusted: ") {
		t.Errorf("stderr missing Trusted confirmation:\n%s", stderr)
	}
	if got, _ := envValue(env, "AIDE_AGENT"); got != "pi" {
		t.Errorf("AIDE_AGENT = %q, want pi from trusted .aide.yaml", got)
	}
	if got, _ := envValue(env, "AIDE_CAPS"); !strings.Contains(got, "go") {
		t.Errorf("AIDE_CAPS = %q, want go capability from trusted .aide.yaml", got)
	}
	if got, _ := envValue(env, "AIDE_TRUST"); got != "trusted" {
		t.Errorf("AIDE_TRUST = %q, want trusted", got)
	}
	fc, err := store.CheckFile(aideFile)
	if err != nil {
		t.Fatal(err)
	}
	if fc.Status != trust.Trusted {
		t.Errorf("store status = %v, want Trusted", fc.Status)
	}
}

func TestProjectGate_InteractiveTrust_SkipAndEmptyDefault(t *testing.T) {
	for _, input := range []string{"s\n", "\n", ""} {
		t.Run("input_"+strings.TrimSpace(input), func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			cwd := t.TempDir()
			aideFile := filepath.Join(cwd, ".aide.yaml")
			writeGateFile(t, aideFile, "agent: pi\ncapabilities: [go]\n")
			store := trust.NewStore(t.TempDir())

			l := &Launcher{
				TrustStore:  store,
				Interactive: true,
				Stdin:       strings.NewReader(input),
			}
			env, stderr, err := selectionLaunch(t, selectionConfig, cwd, l, "")
			if err != nil {
				t.Fatalf("Launch error: %v", err)
			}
			if !strings.Contains(stderr, "[aide] Untrusted .aide.yaml at") {
				t.Errorf("stderr missing trust prompt:\n%s", stderr)
			}
			if got, _ := envValue(env, "AIDE_AGENT"); got != "claude" {
				t.Errorf("AIDE_AGENT = %q, want profile default claude when skipped", got)
			}
			if got, _ := envValue(env, "AIDE_TRUST"); got != "untrusted" {
				t.Errorf("AIDE_TRUST = %q, want untrusted when skipped", got)
			}
			fc, err := store.CheckFile(aideFile)
			if err != nil {
				t.Fatal(err)
			}
			if fc.Status != trust.Untrusted {
				t.Errorf("store status = %v, want Untrusted (skip must not persist)", fc.Status)
			}

			// Calling applyTrustGate again on the same Launcher after ResolveSelection
			// must not prompt a second time.
			cfg := loadGateConfig(t, cwd)
			gate, err := l.applyTrustGate(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if len(gate.Files) != 1 || gate.Files[0].Status != "untrusted" {
				t.Errorf("second gate files = %+v, want 1 untrusted", gate.Files)
			}
		})
	}
}

func TestProjectGate_InteractiveTrust_NoRepromptAfterResolveSelection(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cwd := t.TempDir()
	aideFile := filepath.Join(cwd, ".aide.yaml")
	writeGateFile(t, aideFile, "capabilities: [go]\n")
	store := trust.NewStore(t.TempDir())

	var stderr strings.Builder
	l := &Launcher{
		TrustStore:         store,
		Interactive:        true,
		promptProjectTrust: true,
		Stdin:              strings.NewReader("s\n"),
		Stderr:             &stderr,
		LookPath:           okLookPath,
	}
	cfg := loadGateConfig(t, cwd)
	res, err := l.ResolveSelection(cfg, cwd, t.TempDir(), SelectionInputs{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Gate.Files) != 1 || res.Gate.Files[0].Status != "untrusted" {
		t.Fatalf("gate files = %+v, want 1 untrusted", res.Gate.Files)
	}
	firstPromptCount := strings.Count(stderr.String(), "[aide] Untrusted .aide.yaml")
	if firstPromptCount != 1 {
		t.Fatalf("prompt count = %d, want 1", firstPromptCount)
	}

	// Reloading config and calling applyTrustGate again must not prompt again.
	reloadedCfg := loadGateConfig(t, cwd)
	gate2, err := l.applyTrustGate(reloadedCfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(gate2.Files) != 1 || gate2.Files[0].Status != "untrusted" {
		t.Errorf("reloaded gate files = %+v, want 1 untrusted", gate2.Files)
	}
	if got := strings.Count(stderr.String(), "[aide] Untrusted .aide.yaml"); got != 1 {
		t.Errorf("prompt count after reload = %d, want still 1", got)
	}
}

func TestProjectGate_InteractiveTrust_DenyPersistsAndDoesNotReprompt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cwd := t.TempDir()
	aideFile := filepath.Join(cwd, ".aide.yaml")
	writeGateFile(t, aideFile, "agent: pi\ncapabilities: [go]\n")
	store := trust.NewStore(t.TempDir())

	l1 := &Launcher{
		TrustStore:  store,
		Interactive: true,
		Stdin:       strings.NewReader("d\n"),
	}
	env, stderr, err := selectionLaunch(t, selectionConfig, cwd, l1, "")
	if err != nil {
		t.Fatalf("Launch error: %v", err)
	}
	if !strings.Contains(stderr, "Denied: ") || !strings.Contains(stderr, "run 'aide trust ") {
		t.Errorf("stderr missing Denied confirmation:\n%s", stderr)
	}
	if got, _ := envValue(env, "AIDE_AGENT"); got != "claude" {
		t.Errorf("AIDE_AGENT = %q, want claude when denied", got)
	}
	fc, err := store.CheckFile(aideFile)
	if err != nil {
		t.Fatal(err)
	}
	if fc.Status != trust.Denied {
		t.Errorf("store status = %v, want Denied", fc.Status)
	}

	// A second interactive launch must not prompt for the denied file.
	l2 := &Launcher{
		TrustStore:  store,
		Interactive: true,
		Stdin:       strings.NewReader("y\n"),
	}
	_, stderr2, err := selectionLaunch(t, selectionConfig, cwd, l2, "")
	if err != nil {
		t.Fatalf("second Launch error: %v", err)
	}
	if strings.Contains(stderr2, "[aide] Untrusted .aide.yaml") {
		t.Errorf("denied file should not re-prompt on subsequent launch:\n%s", stderr2)
	}
}

func TestProjectGate_InteractiveTrust_InvalidInputRetries(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cwd := t.TempDir()
	aideFile := filepath.Join(cwd, ".aide.yaml")
	writeGateFile(t, aideFile, "{}\n")
	store := trust.NewStore(t.TempDir())

	l := &Launcher{
		TrustStore:  store,
		Interactive: true,
		Stdin:       strings.NewReader("maybe\ny\n"),
	}
	_, stderr, err := selectionLaunch(t, selectionConfig, cwd, l, "")
	if err != nil {
		t.Fatalf("Launch error: %v", err)
	}
	if !strings.Contains(stderr, "wants: (no special grants)") {
		t.Errorf("stderr missing empty wants placeholder:\n%s", stderr)
	}
	if !strings.Contains(stderr, "please answer y (trust), s (skip), or d (deny)") {
		t.Errorf("stderr missing retry message:\n%s", stderr)
	}
	if !strings.Contains(stderr, "Trusted: ") {
		t.Errorf("stderr missing Trusted confirmation after retry:\n%s", stderr)
	}
	fc, err := store.CheckFile(aideFile)
	if err != nil {
		t.Fatal(err)
	}
	if fc.Status != trust.Trusted {
		t.Errorf("store status = %v, want Trusted", fc.Status)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("unexpected Stdin read in non-interactive/preview mode")
}

func TestProjectGate_NonInteractiveAndPreviewNeverPrompt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cwd := t.TempDir()
	aideFile := filepath.Join(cwd, ".aide.yaml")
	writeGateFile(t, aideFile, "agent: pi\ncapabilities: [go]\n")
	store := trust.NewStore(t.TempDir())

	// 1. GateProjectConfig preview never prompts.
	cfg := loadGateConfig(t, cwd)
	gate, err := GateProjectConfig(cfg)
	if err != nil {
		t.Fatalf("GateProjectConfig error: %v", err)
	}
	if len(gate.Files) != 1 || gate.Files[0].Status != "untrusted" {
		t.Errorf("GateProjectConfig files = %+v, want 1 untrusted", gate.Files)
	}

	// 2. Non-interactive launch never reads Stdin or prompts.
	l := &Launcher{
		TrustStore:  store,
		Interactive: false,
		Stdin:       failingReader{},
	}
	env, stderr, err := selectionLaunch(t, selectionConfig, cwd, l, "")
	if err != nil {
		t.Fatalf("non-interactive Launch error: %v", err)
	}
	if strings.Contains(stderr, "[aide] Untrusted .aide.yaml") {
		t.Errorf("non-interactive launch should not prompt:\n%s", stderr)
	}
	if got, _ := envValue(env, "AIDE_AGENT"); got != "claude" {
		t.Errorf("AIDE_AGENT = %q, want claude", got)
	}
}

func TestProjectGate_InteractiveTrust_InheritedAndSymlinkOriginNotes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	mainFile, localFile, wtDir := worktreeGateFixture(t)
	target := filepath.Join(t.TempDir(), "real-local.aide.yaml")
	writeGateFile(t, target, "capabilities: [python]\n")
	if err := os.Remove(localFile); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, localFile); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	store := trust.NewStore(t.TempDir())

	// Answer "y" to inherited mainFile and "s" to symlinked localFile.
	l := &Launcher{
		TrustStore:  store,
		Interactive: true,
		Stdin:       strings.NewReader("y\ns\n"),
	}
	env, stderr, err := selectionLaunch(t, gateConfigYAML, wtDir, l, "")
	if err != nil {
		t.Fatalf("Launch error: %v", err)
	}
	if !strings.Contains(stderr, "(inherited from main checkout)") {
		t.Errorf("stderr missing inherited origin note:\n%s", stderr)
	}
	if !strings.Contains(stderr, "(symlink to ") {
		t.Errorf("stderr missing symlink origin note:\n%s", stderr)
	}
	if got, _ := envValue(env, "AIDE_CAPS"); !strings.Contains(got, "go") || strings.Contains(got, "python") {
		t.Errorf("AIDE_CAPS = %q, want go (trusted) and not python (skipped)", got)
	}
	fcMain, err := store.CheckFile(mainFile)
	if err != nil {
		t.Fatal(err)
	}
	if fcMain.Status != trust.Trusted {
		t.Errorf("mainFile status = %v, want Trusted", fcMain.Status)
	}
}
