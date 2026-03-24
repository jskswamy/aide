# Yolo Mode Configuration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add persistent yolo mode configuration with two-level override (preferences + context) and `--no-yolo` CLI flag.

**Architecture:** `Yolo *bool` field added to `Preferences`, `Context`, `Config` (minimal), and `ProjectOverride`. A `ResolveYolo()` function implements the precedence chain: `--no-yolo` > `--yolo` > context > preferences > false. Warning messages move from `main.go` into the launcher with source attribution. Banner displays yolo status.

**Tech Stack:** Go, cobra (CLI), gopkg.in/yaml.v3 (config), fatih/color (banner)

**Spec:** `docs/superpowers/specs/2026-03-24-yolo-config-design.md`

---

### Task 1: Add `Yolo *bool` to config schema and `ResolveYolo` function

**Files:**
- Modify: `internal/config/schema.go:244-248` (Preferences struct)
- Modify: `internal/config/schema.go:46-54` (Context struct)
- Modify: `internal/config/schema.go:19-25` (Config minimal fields)
- Modify: `internal/config/schema.go:234-241` (ProjectOverride struct)
- Test: `internal/config/schema_test.go`

- [ ] **Step 1: Write failing tests for ResolveYolo**

In `internal/config/schema_test.go`, add:

```go
func boolPtr(b bool) *bool { return &b }

func TestResolveYolo_NoYoloFlagWins(t *testing.T) {
	got := config.ResolveYolo(true, true, boolPtr(true), boolPtr(true))
	if got {
		t.Error("--no-yolo should win over everything")
	}
}

func TestResolveYolo_YoloFlagWinsOverConfig(t *testing.T) {
	got := config.ResolveYolo(true, false, boolPtr(false), boolPtr(false))
	if !got {
		t.Error("--yolo flag should win over config")
	}
}

func TestResolveYolo_ContextWinsOverPreferences(t *testing.T) {
	got := config.ResolveYolo(false, false, boolPtr(true), boolPtr(false))
	if !got {
		t.Error("context yolo should win over preferences")
	}
}

func TestResolveYolo_PreferencesUsedWhenContextNil(t *testing.T) {
	got := config.ResolveYolo(false, false, nil, boolPtr(true))
	if !got {
		t.Error("preferences yolo should be used when context is nil")
	}
}

func TestResolveYolo_DefaultFalse(t *testing.T) {
	got := config.ResolveYolo(false, false, nil, nil)
	if got {
		t.Error("default should be false")
	}
}

func TestResolveYolo_ContextExplicitFalse(t *testing.T) {
	got := config.ResolveYolo(false, false, boolPtr(false), boolPtr(true))
	if got {
		t.Error("context yolo=false should override preferences yolo=true")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./internal/config/ -run TestResolveYolo -v`
Expected: FAIL — `ResolveYolo` not defined

- [ ] **Step 3: Add Yolo field to all four structs and implement ResolveYolo**

In `internal/config/schema.go`:

Add `Yolo *bool` to `Preferences` (after line 247):
```go
Yolo       *bool  `yaml:"yolo,omitempty"`
```

Add `Yolo *bool` to `Context` (after line 53):
```go
Yolo               *bool                `yaml:"yolo,omitempty"`
```

Add `Yolo *bool` to `Config` minimal fields (after line 25):
```go
Yolo        *bool             `yaml:"yolo,omitempty"`
```

Add `Yolo *bool` to `ProjectOverride` (after line 240):
```go
Yolo        *bool             `yaml:"yolo,omitempty"`
```

Add `ResolveYolo` function (after `ResolvePreferences`, around line 282):
```go
// ResolveYolo determines the effective yolo setting.
// cliYolo and cliNoYolo represent the --yolo and --no-yolo CLI flags.
// contextYolo is from the resolved context, globalYolo from preferences.
func ResolveYolo(cliYolo, cliNoYolo bool, contextYolo, globalYolo *bool) bool {
	if cliNoYolo {
		return false
	}
	if cliYolo {
		return true
	}
	if contextYolo != nil {
		return *contextYolo
	}
	if globalYolo != nil {
		return *globalYolo
	}
	return false
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./internal/config/ -run TestResolveYolo -v`
Expected: PASS (all 6 tests)

- [ ] **Step 5: Write YAML parsing tests for yolo in config**

In `internal/config/schema_test.go`, add:

```go
func TestConfig_YoloInMinimalFormat(t *testing.T) {
	input := `
agent: claude
yolo: true
`
	var cfg config.Config
	if err := yaml.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.Yolo == nil || !*cfg.Yolo {
		t.Error("expected Yolo to be true in minimal config")
	}
}

func TestConfig_YoloInContext(t *testing.T) {
	input := `
agents:
  claude:
    binary: claude
contexts:
  work:
    agent: claude
    yolo: true
`
	var cfg config.Config
	if err := yaml.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	ctx := cfg.Contexts["work"]
	if ctx.Yolo == nil || !*ctx.Yolo {
		t.Error("expected Yolo to be true on context")
	}
}

func TestConfig_YoloInPreferences(t *testing.T) {
	input := `
agents:
  claude:
    binary: claude
contexts:
  work:
    agent: claude
preferences:
  yolo: true
`
	var cfg config.Config
	if err := yaml.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.Preferences == nil || cfg.Preferences.Yolo == nil || !*cfg.Preferences.Yolo {
		t.Error("expected Yolo to be true in preferences")
	}
}

func TestConfig_YoloInProjectOverride(t *testing.T) {
	input := `
agent: claude
yolo: false
`
	var po config.ProjectOverride
	if err := yaml.Unmarshal([]byte(input), &po); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if po.Yolo == nil || *po.Yolo {
		t.Error("expected Yolo to be false in project override")
	}
}
```

- [ ] **Step 6: Run all config tests**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./internal/config/ -v`
Expected: PASS

- [ ] **Step 7: Commit**

```
Add yolo field to config schema and ResolveYolo function

Add Yolo *bool to Preferences, Context, Config (minimal), and
ProjectOverride structs. Implement ResolveYolo with precedence:
--no-yolo > --yolo > context > preferences > false.
```

---

### Task 2: Wire yolo through resolver (minimal promotion + project override)

**Files:**
- Modify: `internal/context/resolver.go:48-53` (minimal config promotion)
- Modify: `internal/context/resolver.go:115-146` (applyProjectOverride)
- Test: `internal/context/resolver_test.go`

- [ ] **Step 1: Write failing test for minimal config yolo promotion**

In `internal/context/resolver_test.go`, add:

```go
func boolPtr(b bool) *bool { return &b }

func TestResolve_MinimalConfig_PromotesYolo(t *testing.T) {
	cfg := &config.Config{
		Agent: "claude",
		Yolo:  boolPtr(true),
	}
	rc, err := Resolve(cfg, "/tmp", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rc.Context.Yolo == nil || !*rc.Context.Yolo {
		t.Error("expected yolo to be promoted from minimal config to context")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./internal/context/ -run TestResolve_MinimalConfig_PromotesYolo -v`
Expected: FAIL — Context.Yolo has no field (or is nil)

- [ ] **Step 3: Add Yolo to minimal config promotion in resolver**

In `internal/context/resolver.go`, modify the synthetic context creation (line 48-53) to include Yolo:

```go
ctx := config.Context{
	Agent:      cfg.Agent,
	Env:        cfg.Env,
	Secret:     cfg.Secret,
	MCPServers: cfg.MCPServers,
	Sandbox:    config.SandboxPolicyToRef(cfg.Sandbox),
	Yolo:       cfg.Yolo,
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./internal/context/ -run TestResolve_MinimalConfig_PromotesYolo -v`
Expected: PASS

- [ ] **Step 5: Write failing test for project override yolo merge**

In `internal/context/resolver_test.go`, add:

```go
func TestResolve_ProjectOverride_MergesYolo(t *testing.T) {
	cfg := &config.Config{
		Agent: "claude",
		Yolo:  boolPtr(true),
		ProjectOverride: &config.ProjectOverride{
			Yolo: boolPtr(false),
		},
	}
	rc, err := Resolve(cfg, "/tmp", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rc.Context.Yolo == nil || *rc.Context.Yolo {
		t.Error("expected project override yolo=false to override config yolo=true")
	}
}

func TestResolve_ProjectOverride_NilYoloPreservesContext(t *testing.T) {
	cfg := &config.Config{
		Agent: "claude",
		Yolo:  boolPtr(true),
		ProjectOverride: &config.ProjectOverride{
			Agent: "codex",
			// Yolo not set — should preserve context value
		},
	}
	rc, err := Resolve(cfg, "/tmp", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rc.Context.Yolo == nil || !*rc.Context.Yolo {
		t.Error("expected context yolo=true to be preserved when override is nil")
	}
}
```

- [ ] **Step 6: Run tests to verify they fail**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./internal/context/ -run TestResolve_ProjectOverride -v`
Expected: FAIL — first test passes (yolo not merged so stays true), second passes

- [ ] **Step 7: Add yolo merge to applyProjectOverride**

In `internal/context/resolver.go`, add after the preferences merge (line 144), before the MatchReason update:

```go
if po.Yolo != nil {
	rc.Context.Yolo = po.Yolo
}
```

- [ ] **Step 8: Run all resolver tests**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./internal/context/ -v`
Expected: PASS

- [ ] **Step 9: Commit**

```
Wire yolo through resolver for minimal promotion and overrides

Promote cfg.Yolo to synthetic context in minimal config path.
Merge ProjectOverride.Yolo in applyProjectOverride, preserving
context value when override is nil.
```

---

### Task 3: Add `--no-yolo` CLI flag and wire to launcher

**Files:**
- Modify: `cmd/aide/main.go:18-74`
- Modify: `internal/launcher/launcher.go:40-47` (Launcher struct)

- [ ] **Step 1: Add NoYolo field to Launcher struct**

In `internal/launcher/launcher.go`, add after the `Yolo` field (line 45):

```go
NoYolo    bool         // from CLI --no-yolo flag
```

- [ ] **Step 2: Add `--no-yolo` flag and wire both flags in main.go**

In `cmd/aide/main.go`:

Add `noYolo` variable (after line 21):
```go
var noYolo bool
```

Remove the yolo warning block (lines 34-39, the `if yolo { ... }` block).

Update launcher construction (around line 46-49) to pass both flags:
```go
l := &launcher.Launcher{
	Execer: &launcher.SyscallExecer{},
	Yolo:   yolo,
	NoYolo: noYolo,
}
```

Add the `--no-yolo` flag registration (after line 66):
```go
rootCmd.Flags().BoolVar(&noYolo, "no-yolo", false, "Disable yolo mode (overrides config and --yolo flag)")
```

- [ ] **Step 3: Verify build compiles**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go build ./cmd/aide/`
Expected: Success (no errors)

- [ ] **Step 4: Commit**

```
Add --no-yolo CLI flag and wire to launcher

Add NoYolo field to Launcher struct and --no-yolo flag to cobra.
Remove yolo warning from main.go (moves to launcher in next task).
```

---

### Task 4: Config-aware yolo resolution in launch path + warning messages

**Files:**
- Modify: `internal/launcher/launcher.go:109-116` (yolo injection in Launch)
- Test: `internal/launcher/launcher_test.go`

- [ ] **Step 1: Write failing test for config-resolved yolo in launch path**

In `internal/launcher/launcher_test.go`, find the existing `TestLauncher_YoloInjectsFlag` test pattern and add a new test. The test needs a config file with `preferences.yolo: true` and should verify the yolo flag is injected even without `l.Yolo = true`.

```go
func TestLauncher_YoloFromConfig(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("sandbox tests only run on darwin")
	}
	mock := &mockExecer{}
	configDir := t.TempDir()

	// Write config with preferences.yolo: true
	configContent := `
agents:
  claude:
    binary: claude
contexts:
  test:
    agent: claude
    match:
      - path: "*"
preferences:
  yolo: true
`
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(configContent), 0o644); err != nil {
		t.Fatal(err)
	}

	l := &Launcher{
		Execer:    mock,
		ConfigDir: configDir,
		LookPath:  mockLookPath(map[string]string{"claude": "/usr/local/bin/claude"}),
		Yolo:      false, // not from CLI
		Stderr:    io.Discard,
	}
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	err := l.Launch(t.TempDir(), "", nil, false, false)
	if err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	// Verify yolo flag was injected from config
	_, innerArgs := unwrapSandbox(t, mock.binary, mock.args)
	found := false
	for _, arg := range innerArgs {
		if arg == "--dangerously-skip-permissions" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected --dangerously-skip-permissions from config yolo, args: %v", innerArgs)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./internal/launcher/ -run TestLauncher_YoloFromConfig -v`
Expected: FAIL — yolo flag not injected (only CLI flag checked)

- [ ] **Step 3: Replace yolo injection in Launch() with config-aware resolution**

In `internal/launcher/launcher.go`, replace lines 109-116 (the `if l.Yolo { ... }` block) with:

```go
// 5b. Resolve effective yolo from CLI flags + config (DD-25)
var globalYolo *bool
if cfg.Preferences != nil {
	globalYolo = cfg.Preferences.Yolo
}
effectiveYolo := config.ResolveYolo(l.Yolo, l.NoYolo, rc.Context.Yolo, globalYolo)

if effectiveYolo {
	yoloArgs, err := YoloArgs(agentName)
	if err != nil {
		return err
	}
	extraArgs = append(yoloArgs, extraArgs...)

	source := yoloSource(l.Yolo, rc.Context.Yolo, globalYolo)
	fmt.Fprintf(l.stderr(), "\033[1;33mWARNING:\033[0m yolo mode enabled (%s)\n", source)
	fmt.Fprintln(l.stderr(), "  Agent permission checks are disabled.")
	fmt.Fprintln(l.stderr(), "  OS sandbox is active (use `aide sandbox show` to inspect).")
	fmt.Fprintln(l.stderr())
}
```

Add the `yoloSource` helper at the bottom of `launcher.go`:

```go
// yoloSource returns a human-readable description of why yolo is active.
func yoloSource(cliYolo bool, contextYolo, globalYolo *bool) string {
	if cliYolo {
		return "--yolo flag"
	}
	if contextYolo != nil && *contextYolo {
		return "context config"
	}
	if globalYolo != nil && *globalYolo {
		return "preferences config"
	}
	return "config"
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./internal/launcher/ -run TestLauncher_Yolo -v`
Expected: PASS (both old and new yolo tests)

- [ ] **Step 5: Write test for --no-yolo overriding config**

```go
func TestLauncher_NoYoloOverridesConfig(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("sandbox tests only run on darwin")
	}
	mock := &mockExecer{}
	configDir := t.TempDir()

	configContent := `
agents:
  claude:
    binary: claude
contexts:
  test:
    agent: claude
    match:
      - path: "*"
preferences:
  yolo: true
`
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(configContent), 0o644); err != nil {
		t.Fatal(err)
	}

	l := &Launcher{
		Execer:    mock,
		ConfigDir: configDir,
		LookPath:  mockLookPath(map[string]string{"claude": "/usr/local/bin/claude"}),
		Yolo:      false,
		NoYolo:    true, // --no-yolo should override config
		Stderr:    io.Discard,
	}
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	err := l.Launch(t.TempDir(), "", nil, false, false)
	if err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	_, innerArgs := unwrapSandbox(t, mock.binary, mock.args)
	for _, arg := range innerArgs {
		if arg == "--dangerously-skip-permissions" {
			t.Error("--no-yolo should prevent yolo flag injection")
		}
	}
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./internal/launcher/ -run TestLauncher_NoYoloOverridesConfig -v`
Expected: PASS

- [ ] **Step 7: Write tests for yoloSource helper**

In `internal/launcher/launcher_test.go`, add:

```go
func boolPtr(b bool) *bool { return &b }

func TestYoloSource_CLIFlag(t *testing.T) {
	if got := yoloSource(true, nil, nil); got != "--yolo flag" {
		t.Errorf("yoloSource(cli=true) = %q, want %q", got, "--yolo flag")
	}
}

func TestYoloSource_ContextConfig(t *testing.T) {
	if got := yoloSource(false, boolPtr(true), nil); got != "context config" {
		t.Errorf("yoloSource(context=true) = %q, want %q", got, "context config")
	}
}

func TestYoloSource_PreferencesConfig(t *testing.T) {
	if got := yoloSource(false, nil, boolPtr(true)); got != "preferences config" {
		t.Errorf("yoloSource(prefs=true) = %q, want %q", got, "preferences config")
	}
}

func TestYoloSource_CLIWinsOverContext(t *testing.T) {
	if got := yoloSource(true, boolPtr(true), boolPtr(true)); got != "--yolo flag" {
		t.Errorf("yoloSource(all=true) = %q, want %q", got, "--yolo flag")
	}
}
```

- [ ] **Step 8: Run yoloSource tests**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./internal/launcher/ -run TestYoloSource -v`
Expected: PASS

- [ ] **Step 9: Write test for warning message content with source attribution**

In `internal/launcher/launcher_test.go`, add:

```go
func TestLauncher_YoloWarningShowsSource(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("sandbox tests only run on darwin")
	}
	mock := &mockExecer{}
	configDir := t.TempDir()

	configContent := `
agents:
  claude:
    binary: claude
contexts:
  test:
    agent: claude
    match:
      - path: "*"
preferences:
  yolo: true
`
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(configContent), 0o644); err != nil {
		t.Fatal(err)
	}

	var stderrBuf bytes.Buffer
	l := &Launcher{
		Execer:    mock,
		ConfigDir: configDir,
		LookPath:  mockLookPath(map[string]string{"claude": "/usr/local/bin/claude"}),
		Stderr:    &stderrBuf,
	}
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	err := l.Launch(t.TempDir(), "", nil, false, false)
	if err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	output := stderrBuf.String()
	if !strings.Contains(output, "yolo mode enabled") {
		t.Error("expected yolo warning in stderr")
	}
	if !strings.Contains(output, "preferences config") {
		t.Errorf("expected source 'preferences config' in warning, got: %s", output)
	}
}
```

- [ ] **Step 10: Run warning test**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./internal/launcher/ -run TestLauncher_YoloWarningShowsSource -v`
Expected: PASS

- [ ] **Step 11: Write test for unsupported agent with config yolo**

In `internal/launcher/launcher_test.go`, add:

```go
func TestLauncher_YoloFromConfig_UnsupportedAgent(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("sandbox tests only run on darwin")
	}
	mock := &mockExecer{}
	configDir := t.TempDir()

	configContent := `
agents:
  aider:
    binary: aider
contexts:
  test:
    agent: aider
    match:
      - path: "*"
    yolo: true
`
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(configContent), 0o644); err != nil {
		t.Fatal(err)
	}

	l := &Launcher{
		Execer:    mock,
		ConfigDir: configDir,
		LookPath:  mockLookPath(map[string]string{"aider": "/usr/local/bin/aider"}),
		Stderr:    io.Discard,
	}
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	err := l.Launch(t.TempDir(), "", nil, false, false)
	if err == nil {
		t.Fatal("expected error for unsupported yolo agent")
	}
	if !strings.Contains(err.Error(), "--yolo not supported") {
		t.Errorf("expected '--yolo not supported' error, got: %v", err)
	}
}
```

- [ ] **Step 12: Run unsupported agent test**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./internal/launcher/ -run TestLauncher_YoloFromConfig_UnsupportedAgent -v`
Expected: PASS (YoloArgs returns error for "aider")

- [ ] **Step 13: Write test for multi-context yolo override**

In `internal/context/resolver_test.go`, add:

```go
func TestResolve_MultiContext_YoloOverridePerContext(t *testing.T) {
	cfg := &config.Config{
		Agents: map[string]config.AgentDef{
			"claude": {Binary: "claude"},
		},
		Contexts: map[string]config.Context{
			"personal": {
				Match: []config.MatchRule{{Remote: "github.com/myuser/*"}},
				Agent: "claude",
				Yolo:  boolPtr(true),
			},
			"work": {
				Match: []config.MatchRule{{Remote: "github.com/company/*"}},
				Agent: "claude",
				// Yolo not set — should be nil
			},
		},
		Preferences: &config.Preferences{
			Yolo: boolPtr(false),
		},
	}

	// Personal context should have yolo=true
	rc, err := Resolve(cfg, "/tmp", "github.com/myuser/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rc.Context.Yolo == nil || !*rc.Context.Yolo {
		t.Error("personal context should have yolo=true")
	}

	// Work context should have yolo=nil (falls through to preferences=false)
	rc2, err := Resolve(cfg, "/tmp", "github.com/company/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rc2.Context.Yolo != nil {
		t.Errorf("work context should have yolo=nil (unset), got %v", *rc2.Context.Yolo)
	}
}
```

- [ ] **Step 14: Run multi-context test**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./internal/context/ -run TestResolve_MultiContext_YoloOverridePerContext -v`
Expected: PASS

- [ ] **Step 15: Commit**

```
Resolve yolo from config in launch path with source attribution

Replace CLI-only yolo check with ResolveYolo that merges CLI flags,
context config, and preferences. Add yoloSource helper for warning
messages that attribute the yolo source. Cover unsupported agent,
warning content, and multi-context override scenarios.
```

---

### Task 5: Update passthrough path to respect `--no-yolo`

**Files:**
- Modify: `internal/launcher/passthrough.go:114-120` (execAgent yolo check)
- Test: `internal/launcher/passthrough_test.go`

- [ ] **Step 1: Write failing test for --no-yolo in passthrough**

In `internal/launcher/passthrough_test.go`, add:

```go
func TestPassthrough_NoYoloOverridesYolo(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("sandbox tests only run on darwin")
	}
	mock := &mockExecer{}
	l := &Launcher{
		Execer:   mock,
		LookPath: mockLookPath(map[string]string{"claude": "/usr/local/bin/claude"}),
		Yolo:     true,
		NoYolo:   true,
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	err := l.Passthrough(t.TempDir(), "", nil)
	if err != nil {
		t.Fatalf("Passthrough failed: %v", err)
	}

	_, innerArgs := unwrapSandbox(t, mock.binary, mock.args)
	for _, arg := range innerArgs {
		if arg == "--dangerously-skip-permissions" {
			t.Error("--no-yolo should prevent yolo flag injection in passthrough")
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./internal/launcher/ -run TestPassthrough_NoYoloOverridesYolo -v`
Expected: FAIL — yolo flag still injected because `execAgent` only checks `l.Yolo`

- [ ] **Step 3: Update execAgent to check NoYolo**

In `internal/launcher/passthrough.go`, change the yolo check in `execAgent` (line 114) from:

```go
if l.Yolo {
```

to:

```go
if l.Yolo && !l.NoYolo {
```

Also add the passthrough-specific warning after the yolo args injection (before the sandbox setup):

```go
if l.Yolo && !l.NoYolo {
	yoloArgs, err := YoloArgs(name)
	if err != nil {
		return err
	}
	extraArgs = append(yoloArgs, extraArgs...)

	fmt.Fprintln(l.stderr(), "\033[1;33mWARNING:\033[0m yolo mode enabled (--yolo flag)")
	fmt.Fprintln(l.stderr(), "  Agent permission checks are disabled.")
	fmt.Fprintln(l.stderr(), "  OS sandbox is active with default policy (use `aide sandbox show` to inspect).")
	fmt.Fprintln(l.stderr())
}
```

- [ ] **Step 4: Run all passthrough tests**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./internal/launcher/ -run TestPassthrough -v`
Expected: PASS

- [ ] **Step 5: Commit**

```
Respect --no-yolo in passthrough path

Add NoYolo check to execAgent so --no-yolo overrides --yolo in
passthrough mode. Add yolo warning with source attribution.
```

---

### Task 6: Add yolo to banner display

**Files:**
- Modify: `internal/ui/banner.go:21-32` (BannerData struct)
- Modify: `internal/ui/banner.go:183-225` (RenderCompact)
- Modify: `internal/ui/banner.go:228-285` (RenderBoxed)
- Modify: `internal/ui/banner.go:288+` (RenderClean)
- Modify: `internal/launcher/launcher.go:356-439` (buildBannerData)
- Test: `internal/ui/banner_test.go`

- [ ] **Step 1: Write failing test for yolo in banner**

In `internal/ui/banner_test.go`, add:

```go
func TestRenderCompact_ShowsYolo(t *testing.T) {
	data := fullBannerData()
	data.Yolo = true
	var buf bytes.Buffer
	RenderCompact(&buf, data)
	out := buf.String()
	if !strings.Contains(out, "yolo") {
		t.Error("compact banner should show yolo indicator when active")
	}
}

func TestRenderCompact_HidesYoloWhenFalse(t *testing.T) {
	data := fullBannerData()
	data.Yolo = false
	var buf bytes.Buffer
	RenderCompact(&buf, data)
	out := buf.String()
	if strings.Contains(out, "yolo") {
		t.Error("compact banner should not show yolo when inactive")
	}
}

func TestRenderBoxed_ShowsYolo(t *testing.T) {
	data := fullBannerData()
	data.Yolo = true
	var buf bytes.Buffer
	RenderBoxed(&buf, data)
	out := buf.String()
	if !strings.Contains(out, "yolo") {
		t.Error("boxed banner should show yolo indicator when active")
	}
}

func TestRenderClean_ShowsYolo(t *testing.T) {
	data := fullBannerData()
	data.Yolo = true
	var buf bytes.Buffer
	RenderClean(&buf, data)
	out := buf.String()
	if !strings.Contains(out, "yolo") {
		t.Error("clean banner should show yolo indicator when active")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./internal/ui/ -run TestRender.*Yolo -v`
Expected: FAIL — BannerData has no Yolo field

- [ ] **Step 3: Add Yolo field to BannerData and render it**

In `internal/ui/banner.go`, add to `BannerData` struct (after `Warnings` field, around line 31):

```go
Yolo        bool
```

In `RenderCompact()`, add after the warnings loop (before the closing brace, around line 225):

```go
if data.Yolo {
	yellow.Fprintf(w, "   ⚡ yolo: agent permission checks disabled\n")
}
```

In `RenderBoxed()`, add after the warnings loop (before the closing border, around line 283):

```go
if data.Yolo {
	fmt.Fprintf(w, "│ ")
	yellow.Fprintf(w, "⚡ yolo: agent permission checks disabled\n")
}
```

In `RenderClean()`, add at the end of the function:

```go
if data.Yolo {
	fmt.Fprintf(w, "  ")
	yellow.Fprintf(w, "yolo      ")
	fmt.Fprintf(w, "agent permission checks disabled\n")
}
```

- [ ] **Step 4: Run banner tests**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./internal/ui/ -v`
Expected: PASS

- [ ] **Step 5: Wire effectiveYolo to banner in launcher**

In `internal/launcher/launcher.go`, in `buildBannerData` function (around line 366), the function signature doesn't currently accept yolo state. The simplest approach: set `data.Yolo` in `Launch()` after calling `buildBannerData`:

In `Launch()`, after `bannerData := l.buildBannerData(...)` (around line 233), add:

```go
bannerData.Yolo = effectiveYolo
```

Note: `effectiveYolo` was computed earlier in step 5b. If it's not in scope at the banner section, store it in a variable earlier that's accessible here.

For the passthrough path in `execAgent`, set `bannerData.Yolo` similarly:

```go
bannerData.Yolo = l.Yolo && !l.NoYolo
```

- [ ] **Step 6: Run full test suite**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./... 2>&1 | tail -20`
Expected: PASS (or only pre-existing failures)

- [ ] **Step 7: Commit**

```
Display yolo status in startup banner

Add Yolo field to BannerData and render it in all three banner
styles (compact, boxed, clean). Wire effectiveYolo from launcher
to banner data in both launch and passthrough paths.
```

---

### Task 7: Update documentation

**Files:**
- Modify: `docs/configuration.md`
- Modify: `docs/sandbox.md`
- Modify: `docs/cli-reference.md`
- Modify: `docs/contexts.md`
- Modify: `docs/getting-started.md`

- [ ] **Step 1: Update docs/configuration.md**

Add `yolo` to the preferences schema section and to the context fields. Show examples of global and per-context yolo configuration. Reference the resolution order (CLI > context > preferences > false).

- [ ] **Step 2: Update docs/cli-reference.md**

Add `--no-yolo` flag to the root command flags table. Update `--yolo` description to mention it can also be set via config.

- [ ] **Step 3: Update docs/sandbox.md**

Add a section explaining the relationship between yolo mode and sandbox guards. Key point: yolo disables agent-level permission prompts but the OS sandbox with guards remains active.

- [ ] **Step 4: Update docs/contexts.md**

Add per-context `yolo` field to the context schema reference. Show example of overriding global yolo per context.

- [ ] **Step 5: Update docs/getting-started.md**

Add a brief mention of `yolo: true` in the quickstart config example as an optional field.

- [ ] **Step 6: Commit**

```
Update documentation for yolo config support

Add yolo field documentation to configuration, CLI reference,
sandbox, contexts, and getting-started guides.
```

---

### Task 8: Run full test suite and lint

- [ ] **Step 1: Run all tests**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./...`
Expected: PASS

- [ ] **Step 2: Run linter**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && golangci-lint run ./...`
Expected: No new violations

- [ ] **Step 3: Build binary and smoke test**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go build -o /tmp/aide-test ./cmd/aide/ && /tmp/aide-test --help`
Expected: Help output shows both `--yolo` and `--no-yolo` flags

- [ ] **Step 4: Fix any issues found and commit**

Only if needed. Run tests again after fixes.
