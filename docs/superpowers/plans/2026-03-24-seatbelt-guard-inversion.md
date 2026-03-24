# Seatbelt Guard Architecture Inversion — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Invert the seatbelt guard architecture from deny-broad/allow-narrow to allow-broad/deny-narrow, fixing the SSH keys guard bug and adding transparent guard diagnostics to the startup banner.

**Architecture:** Collapse three-tier RuleIntent (Setup/Restrict/Grant) to two tiers (Allow/Deny). Replace `[]Rule` return type with `GuardResult` carrying diagnostics. Add existence checks to all credential guards. Rewrite SSH keys guard with ReadDir + allowlist discovery. Redesign the banner to show per-guard status grouped by active/skipped/available.

**Tech Stack:** Go, macOS seatbelt (`sandbox-exec`), `fatih/color` for terminal output

**Spec:** `docs/superpowers/specs/2026-03-24-seatbelt-guard-inversion-design.md`

**Commit convention:** Use the `/commit` plugin for ALL commits in this plan. Do not use `git commit` directly. The `/commit` plugin provides atomic commit validation and style enforcement.

---

## File Structure

### Core (pkg/seatbelt/)

| File | Action | Responsibility |
|------|--------|----------------|
| `module.go` | Modify | RuleIntent constants, GuardResult/Override types, Module interface, rule constructors |
| `profile.go` | Modify | ProfileResult type, Render() returns ProfileResult, aggregates GuardResult |
| `render.go` | No change | Text rendering — intent values are transparent |
| `path.go` | No change | Path expression helpers |
| `path_helpers.go` | No change | ExistsOrUnderHome |
| `validation.go` | No change | ValidationResult |
| `doc.go` | Modify | Update usage example |

### Guards (pkg/seatbelt/guards/)

| File | Action | Responsibility |
|------|--------|----------------|
| `helpers.go` | Modify | DenyDir/DenyFile/AllowReadFile use new constructors, add pathExists/dirExists helpers |
| `guard_ssh_keys.go` | Rewrite | Discovery via ReadDir + allowlist |
| `guard_kubernetes.go` | Modify | Change to opt-in, add existence check |
| `guard_password_managers.go` | Modify | Per-tool existence checks |
| `guard_cloud.go` | Modify | Per-provider existence checks, populate Overrides |
| `guard_terraform.go` | Modify | Existence check, populate Overrides |
| `guard_vault.go` | Modify | Existence check |
| `guard_browsers.go` | Modify | Existence check |
| `guard_sensitive.go` | Modify | Per-guard existence checks (docker, github-cli, npm, netrc, vercel) |
| `guard_aide_secrets.go` | No change | Empty stub — `AideSecretsGuard` lives in `guard_password_managers.go` |
| `guard_base.go` | Modify | Return GuardResult (rules only, no diagnostics) |
| `guard_system_runtime.go` | Modify | Return GuardResult |
| `guard_network.go` | Modify | Return GuardResult |
| `guard_filesystem.go` | Modify | Return GuardResult |
| `guard_keychain.go` | Modify | Return GuardResult |
| `guard_node_toolchain.go` | Modify | Return GuardResult |
| `guard_nix_toolchain.go` | Modify | Return GuardResult |
| `guard_git_integration.go` | Modify | Return GuardResult |
| `guard_custom.go` | Modify | Return GuardResult, add existence checks |
| `registry.go` | No change | Guard registration/resolution unchanged |

### Modules (pkg/seatbelt/modules/)

| File | Action | Responsibility |
|------|--------|----------------|
| `helpers.go` | Modify | configDirRules uses AllowRule/SectionAllow, return GuardResult |
| `claude.go` | Modify | Return GuardResult |
| `aider.go` | Modify | Return GuardResult |
| `amp.go` | Modify | Return GuardResult |
| `codex.go` | Modify | Return GuardResult |
| `gemini.go` | Modify | Return GuardResult |
| `goose.go` | Modify | Return GuardResult |

### Banner (internal/ui/)

| File | Action | Responsibility |
|------|--------|----------------|
| `banner.go` | Modify | New SandboxInfo/GuardDisplay structs, grouped rendering |

### Callers

| File | Action | Responsibility |
|------|--------|----------------|
| `internal/sandbox/darwin.go` | Modify | Handle ProfileResult return, add EvaluateGuards export |
| `cmd/aide/commands.go` | Modify | Build new SandboxInfo from guard diagnostics |
| `internal/launcher/launcher.go` | Modify | Build new SandboxInfo from guard diagnostics |

---

## Task Breakdown

### Task 1: Core Domain Model — RuleIntent and GuardResult

**Files:**
- Modify: `pkg/seatbelt/module.go`
- Test: `pkg/seatbelt/module_test.go`

- [ ] **Step 1: Write failing tests for new RuleIntent constants**

In `pkg/seatbelt/module_test.go`, replace existing intent tests with tests for Allow/Deny:

```go
func TestAllowRule(t *testing.T) {
	r := AllowRule(`(allow file-read* (subpath "/usr"))`)
	if r.Intent() != Allow {
		t.Errorf("AllowRule intent = %d, want %d", r.Intent(), Allow)
	}
	if !strings.Contains(r.String(), "(allow file-read*") {
		t.Errorf("AllowRule text missing allow directive")
	}
}

func TestDenyRule(t *testing.T) {
	r := DenyRule(`(deny file-read-data (literal "/home/.ssh/id_rsa"))`)
	if r.Intent() != Deny {
		t.Errorf("DenyRule intent = %d, want %d", r.Intent(), Deny)
	}
	if !strings.Contains(r.String(), "(deny file-read-data") {
		t.Errorf("DenyRule text missing deny directive")
	}
}

func TestSectionAllow(t *testing.T) {
	r := SectionAllow("infrastructure")
	if r.Intent() != Allow {
		t.Errorf("SectionAllow intent = %d, want %d", r.Intent(), Allow)
	}
}

func TestSectionDeny(t *testing.T) {
	r := SectionDeny("credentials")
	if r.Intent() != Deny {
		t.Errorf("SectionDeny intent = %d, want %d", r.Intent(), Deny)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./pkg/seatbelt/ -run "TestAllowRule|TestDenyRule|TestSectionAllow|TestSectionDeny" -v`
Expected: FAIL — `AllowRule`, `DenyRule`, `SectionAllow`, `SectionDeny`, `Allow`, `Deny` undefined

- [ ] **Step 3: Implement new RuleIntent, GuardResult, Override, and constructors**

In `pkg/seatbelt/module.go`:

1. Replace the RuleIntent constants:
```go
// RuleIntent determines a rule's position in the rendered profile.
// The renderer stable-sorts rules by intent: allows first, then denies.
// Seatbelt uses deny-wins-over-allow semantics — deny rules always
// take precedence regardless of position. The sort order is for
// readability only.
type RuleIntent int

const (
	Allow RuleIntent = 100 // broad infrastructure + directory allows
	Deny  RuleIntent = 200 // narrow specific-file/path denials
)
```

2. Remove the old constants `Setup`, `Restrict`, `Grant`.

3. Add new types:
```go
// GuardResult holds rules and diagnostics from a guard evaluation.
type GuardResult struct {
	Name      string     // guard name, set by the profile builder from Module.Name()
	Rules     []Rule
	Protected []string   // paths being denied
	Allowed   []string   // paths explicitly allowed (exceptions)
	Skipped   []string   // "~/.config/op not found" etc.
	Overrides []Override // env var overrides detected
}

// Override records when an env var changed a guard's default path.
type Override struct {
	EnvVar      string // e.g. "KUBECONFIG"
	Value       string // e.g. "/custom/kubeconfig"
	DefaultPath string // e.g. "~/.kube/config"
}
```

4. Update the Module interface:
```go
type Module interface {
	Name() string
	Rules(ctx *Context) GuardResult
}
```

5. Replace rule constructors:
```go
// AllowRule creates a rule with Allow intent.
func AllowRule(text string) Rule { return Rule{intent: Allow, lines: text} }

// DenyRule creates a rule with Deny intent.
func DenyRule(text string) Rule { return Rule{intent: Deny, lines: text} }

// SectionAllow creates a section header comment with Allow intent.
func SectionAllow(name string) Rule {
	return Rule{intent: Allow, comment: "--- " + name + " ---"}
}

// SectionDeny creates a section header comment with Deny intent.
func SectionDeny(name string) Rule {
	return Rule{intent: Deny, comment: "--- " + name + " ---"}
}
```

6. Remove old constructors: `SetupRule`, `RestrictRule`, `GrantRule`, `SectionSetup`, `SectionRestrict`, `SectionGrant`.

7. Rename remaining convenience constructors to avoid name conflicts with the new `Allow`/`Deny` constants. The existing `Allow(operation)` and `Deny(operation)` functions create `(allow <op>)` and `(deny <op>)` rules — these conflict with the constant names:

```go
// AllowOp creates an (allow <operation>) rule with Allow intent.
func AllowOp(operation string) Rule {
	return Rule{intent: Allow, lines: "(allow " + operation + ")"}
}

// DenyOp creates a (deny <operation>) rule with Allow intent.
// Note: DenyOp has Allow intent because deny-ops in the Setup layer
// (e.g. deny default) are infrastructure rules, not credential guards.
func DenyOp(operation string) Rule {
	return Rule{intent: Allow, lines: "(deny " + operation + ")"}
}

func Comment(text string) Rule {
	return Rule{intent: Allow, comment: text}
}

func Section(name string) Rule {
	return Rule{intent: Allow, comment: "--- " + name + " ---"}
}

func Raw(text string) Rule {
	return Rule{intent: Allow, lines: text}
}
```

- [ ] **Step 4: Update old tests to use new API**

Remove tests for `SetupRule`, `RestrictRule`, `GrantRule`, `SectionSetup`, `SectionRestrict`, `SectionGrant`. Update any remaining tests that reference old constants.

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./pkg/seatbelt/ -v`
Expected: PASS — compilation may fail due to downstream consumers. That's expected; we fix those in subsequent tasks.

- [ ] **Step 6: Commit**

Stage `pkg/seatbelt/module.go` and `pkg/seatbelt/module_test.go`, then run `/commit`.

---

### Task 2: Profile Builder — ProfileResult and Render()

**Files:**
- Modify: `pkg/seatbelt/profile.go`
- Modify: `pkg/seatbelt/doc.go`
- Test: `pkg/seatbelt/profile_test.go`

- [ ] **Step 1: Write failing test for ProfileResult**

In `pkg/seatbelt/profile_test.go`, add:

```go
func TestRenderReturnsProfileResult(t *testing.T) {
	m := &testModule{
		name: "test-guard",
		result: GuardResult{
			Rules:     []Rule{AllowRule(`(allow file-read* (subpath "/usr"))`)},
			Protected: []string{"/home/.ssh/id_rsa"},
			Skipped:   []string{"~/.config/op not found"},
		},
	}
	p := New("/home/user").Use(m)
	result, err := p.Render()
	if err != nil {
		t.Fatal(err)
	}
	if result.Profile == "" {
		t.Error("ProfileResult.Profile is empty")
	}
	if len(result.Guards) != 1 {
		t.Fatalf("expected 1 guard result, got %d", len(result.Guards))
	}
	if result.Guards[0].Name != "test-guard" {
		t.Errorf("guard name = %q, want %q", result.Guards[0].Name, "test-guard")
	}
	if len(result.Guards[0].Protected) != 1 {
		t.Errorf("expected 1 protected path, got %d", len(result.Guards[0].Protected))
	}
}
```

Update the `testModule` helper to return `GuardResult`:

```go
type testModule struct {
	name   string
	result GuardResult
}

func (m *testModule) Name() string                  { return m.name }
func (m *testModule) Rules(ctx *Context) GuardResult { return m.result }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./pkg/seatbelt/ -run TestRenderReturnsProfileResult -v`
Expected: FAIL — `ProfileResult` undefined, `testModule` doesn't match interface

- [ ] **Step 3: Implement ProfileResult and update Render()**

In `pkg/seatbelt/profile.go`:

```go
// ProfileResult holds the rendered profile and per-guard diagnostics.
type ProfileResult struct {
	Profile string        // rendered seatbelt profile text
	Guards  []GuardResult // per-guard diagnostics for banner display
}

// Render generates the Seatbelt .sb profile and collects guard diagnostics.
// Rules from all modules are collected, stable-sorted by intent (Allow
// before Deny), then rendered. Guard diagnostics are aggregated for the
// caller to pass to the banner layer.
func (p *Profile) Render() (ProfileResult, error) {
	if len(p.modules) == 0 {
		return ProfileResult{}, nil
	}
	var allRules []taggedRule
	var guardResults []GuardResult
	for _, m := range p.modules {
		result := m.Rules(&p.ctx)
		result.Name = m.Name()
		guardResults = append(guardResults, result)
		for _, r := range result.Rules {
			allRules = append(allRules, taggedRule{module: m.Name(), rule: r})
		}
	}
	sort.SliceStable(allRules, func(i, j int) bool {
		return allRules[i].rule.intent < allRules[j].rule.intent
	})
	return ProfileResult{
		Profile: renderTaggedRules(allRules),
		Guards:  guardResults,
	}, nil
}
```

- [ ] **Step 4: Update existing profile tests**

All existing tests use `out, err := p.Render()` where `out` is a string. Update to:

```go
result, err := p.Render()
out := result.Profile
```

Update `testModule` to return `GuardResult` wrapping `[]Rule`.

- [ ] **Step 5: Update doc.go usage example**

Update the example in `doc.go` to show `ProfileResult`:

```go
//	result, err := profile.Render()
//	sbText := result.Profile
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./pkg/seatbelt/ -v`
Expected: PASS

- [ ] **Step 7: Commit**

Stage `pkg/seatbelt/profile.go`, `pkg/seatbelt/profile_test.go`, and `pkg/seatbelt/doc.go`, then run `/commit`.

---

### Task 3: Guard Helpers — Update Constructors and Add Existence Helpers

**Files:**
- Modify: `pkg/seatbelt/guards/helpers.go`
- Test: `pkg/seatbelt/guards/helpers_test.go`

- [ ] **Step 1: Write failing tests for updated helpers and new existence helpers**

In `helpers_test.go`, update existing tests to check for `Deny` intent instead of `Restrict`:

```go
func TestDenyDirIntent(t *testing.T) {
	rules := DenyDir("/home/user/.ssh")
	for _, r := range rules {
		if r.Intent() != seatbelt.Deny {
			t.Errorf("DenyDir rule intent = %d, want %d (Deny)", r.Intent(), seatbelt.Deny)
		}
	}
}

func TestDenyFileIntent(t *testing.T) {
	rules := DenyFile("/home/user/.ssh/id_rsa")
	for _, r := range rules {
		if r.Intent() != seatbelt.Deny {
			t.Errorf("DenyFile rule intent = %d, want %d (Deny)", r.Intent(), seatbelt.Deny)
		}
	}
}

func TestAllowReadFileIntent(t *testing.T) {
	r := AllowReadFile("/home/user/.ssh/known_hosts")
	if r.Intent() != seatbelt.Allow {
		t.Errorf("AllowReadFile intent = %d, want %d (Allow)", r.Intent(), seatbelt.Allow)
	}
}
```

Add tests for new existence helpers:

```go
func TestDirExists(t *testing.T) {
	// existing directory
	if !dirExists(t.TempDir()) {
		t.Error("dirExists should return true for existing directory")
	}
	// non-existent
	if dirExists("/nonexistent/path/xyz") {
		t.Error("dirExists should return false for non-existent path")
	}
	// file (not a directory)
	f := filepath.Join(t.TempDir(), "file.txt")
	os.WriteFile(f, []byte("x"), 0o644)
	if dirExists(f) {
		t.Error("dirExists should return false for a file")
	}
}

func TestPathExists(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "file.txt")
	os.WriteFile(f, []byte("x"), 0o644)
	if !pathExists(f) {
		t.Error("pathExists should return true for existing file")
	}
	if pathExists(filepath.Join(dir, "nope")) {
		t.Error("pathExists should return false for non-existent file")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./pkg/seatbelt/guards/ -run "TestDenyDirIntent|TestDenyFileIntent|TestAllowReadFileIntent|TestDirExists|TestPathExists" -v`
Expected: FAIL — wrong intent values, `dirExists`/`pathExists` undefined

- [ ] **Step 3: Update helpers and add existence helpers**

In `helpers.go`:

1. Update `DenyDir` to use `seatbelt.DenyRule`:
```go
func DenyDir(path string) []seatbelt.Rule {
	return []seatbelt.Rule{
		seatbelt.DenyRule(fmt.Sprintf(`(deny file-read-data (subpath "%s"))`, path)),
		seatbelt.DenyRule(fmt.Sprintf(`(deny file-write* (subpath "%s"))`, path)),
	}
}
```

2. Update `DenyFile` to use `seatbelt.DenyRule`:
```go
func DenyFile(path string) []seatbelt.Rule {
	return []seatbelt.Rule{
		seatbelt.DenyRule(fmt.Sprintf(`(deny file-read-data (literal "%s"))`, path)),
		seatbelt.DenyRule(fmt.Sprintf(`(deny file-write* (literal "%s"))`, path)),
	}
}
```

3. Update `AllowReadFile` to use `seatbelt.AllowRule`:
```go
func AllowReadFile(path string) seatbelt.Rule {
	return seatbelt.AllowRule(fmt.Sprintf(`(allow file-read* (literal "%s"))`, path))
}
```

4. Add existence helpers:
```go
// dirExists returns true if path exists and is a directory.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// pathExists returns true if path exists (file or directory).
func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
```

Add `"os"` to imports.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./pkg/seatbelt/guards/ -run "TestDenyDirIntent|TestDenyFileIntent|TestAllowReadFileIntent|TestDirExists|TestPathExists" -v`
Expected: PASS

- [ ] **Step 5: Commit**

Stage `pkg/seatbelt/guards/helpers.go` and `pkg/seatbelt/guards/helpers_test.go`, then run `/commit`.

---

### Task 4: Always-Guards — Mechanical Interface Update

Update all 8 always-guards to return `GuardResult` instead of `[]Rule`. These are mechanical changes — no logic changes, just wrapping the existing `[]Rule` in a `GuardResult`.

**Files:**
- Modify: `pkg/seatbelt/guards/guard_base.go`
- Modify: `pkg/seatbelt/guards/guard_system_runtime.go`
- Modify: `pkg/seatbelt/guards/guard_network.go`
- Modify: `pkg/seatbelt/guards/guard_filesystem.go`
- Modify: `pkg/seatbelt/guards/guard_keychain.go`
- Modify: `pkg/seatbelt/guards/guard_node_toolchain.go`
- Modify: `pkg/seatbelt/guards/guard_nix_toolchain.go`
- Modify: `pkg/seatbelt/guards/guard_git_integration.go`
- Test: `pkg/seatbelt/guards/base_test.go`
- Test: `pkg/seatbelt/guards/system_test.go`
- Test: `pkg/seatbelt/guards/network_test.go`
- Test: `pkg/seatbelt/guards/filesystem_test.go`
- Test: `pkg/seatbelt/guards/toolchain_test.go`

- [ ] **Step 1: Update each always-guard's Rules() signature**

For each guard, change from:
```go
func (g *baseGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
    return []seatbelt.Rule{...}
}
```
To:
```go
func (g *baseGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
    return seatbelt.GuardResult{
        Rules: []seatbelt.Rule{...},
    }
}
```

Also update rule constructors used within these guards:
- `seatbelt.SetupRule(...)` → `seatbelt.AllowRule(...)`
- `seatbelt.SectionSetup(...)` → `seatbelt.SectionAllow(...)`
- `seatbelt.Allow(...)` → `seatbelt.AllowOp(...)`
- `seatbelt.Deny(...)` → `seatbelt.DenyOp(...)`
- `seatbelt.RestrictRule(...)` → `seatbelt.DenyRule(...)`

The `filesystem` guard uses `seatbelt.RestrictRule` for `ExtraDenied` paths — change to `seatbelt.DenyRule`.

- [ ] **Step 2: Update tests to access GuardResult.Rules**

In each test file, change:
```go
rules := SomeGuard().Rules(ctx)
```
To:
```go
result := SomeGuard().Rules(ctx)
rules := result.Rules
```

- [ ] **Step 3: Run tests to verify they pass**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./pkg/seatbelt/guards/ -run "TestBase|TestSystem|TestNetwork|TestFilesystem|TestNode|TestNix|TestGit|TestKeychain" -v`
Expected: PASS

- [ ] **Step 4: Commit**

Stage all modified guard and test files in `pkg/seatbelt/guards/`, then run `/commit`.

---

### Task 5: Agent Modules — Mechanical Interface Update

Update all agent modules and their helpers to return `GuardResult`.

**Files:**
- Modify: `pkg/seatbelt/modules/helpers.go`
- Modify: `pkg/seatbelt/modules/claude.go`
- Modify: `pkg/seatbelt/modules/aider.go`
- Modify: `pkg/seatbelt/modules/amp.go`
- Modify: `pkg/seatbelt/modules/codex.go`
- Modify: `pkg/seatbelt/modules/gemini.go`
- Modify: `pkg/seatbelt/modules/goose.go`
- Test: `pkg/seatbelt/modules/agents_test.go`
- Test: `pkg/seatbelt/modules/claude_test.go`
- Test: `pkg/seatbelt/modules/helpers_test.go`

- [ ] **Step 1: Update modules/helpers.go**

Change `configDirRules` to use `AllowRule`/`SectionAllow`:
```go
func configDirRules(sectionName string, dirs []string) []seatbelt.Rule {
	if len(dirs) == 0 {
		return nil
	}
	rules := []seatbelt.Rule{
		seatbelt.SectionAllow(sectionName + " config"),
	}
	for _, dir := range dirs {
		rules = append(rules, seatbelt.AllowRule(fmt.Sprintf(
			`(allow file-read* file-write* (subpath %q))`, dir,
		)))
	}
	return rules
}
```

- [ ] **Step 2: Update each agent module's Rules() to return GuardResult**

For each agent (claude, aider, amp, codex, gemini, goose), change:
```go
func (m *claudeAgent) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
    var rules []seatbelt.Rule
    // ...
    return rules
}
```
To:
```go
func (m *claudeAgent) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
    var rules []seatbelt.Rule
    // ...
    return seatbelt.GuardResult{Rules: rules}
}
```

Also update any `seatbelt.GrantRule` → `seatbelt.AllowRule` and
`seatbelt.SectionGrant` → `seatbelt.SectionAllow` within the modules.

- [ ] **Step 3: Update module tests**

Change `rules := SomeAgent().Rules(ctx)` to `result := SomeAgent().Rules(ctx)` and then use `result.Rules` where rules were previously accessed.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./pkg/seatbelt/modules/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

Stage all modified files in `pkg/seatbelt/modules/`, then run `/commit`.

---

### Task 6: SSH Keys Guard — Full Rewrite with Discovery

**Files:**
- Rewrite: `pkg/seatbelt/guards/guard_ssh_keys.go`
- Rewrite: `pkg/seatbelt/guards/guard_ssh_keys_test.go`

- [ ] **Step 1: Write comprehensive tests for the new SSH keys guard**

In `guard_ssh_keys_test.go`:

```go
func TestSSHKeysGuard_DirectoryNotFound(t *testing.T) {
	ctx := &seatbelt.Context{HomeDir: t.TempDir(), GOOS: "darwin"}
	result := SSHKeysGuard().Rules(ctx)
	if len(result.Rules) != 0 {
		t.Errorf("expected 0 rules when .ssh missing, got %d", len(result.Rules))
	}
	if len(result.Skipped) != 1 {
		t.Fatalf("expected 1 skip message, got %d", len(result.Skipped))
	}
	if !strings.Contains(result.Skipped[0], ".ssh") {
		t.Errorf("skip message should mention .ssh, got %q", result.Skipped[0])
	}
}

func TestSSHKeysGuard_EmptyDirectory(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".ssh"), 0o700)
	ctx := &seatbelt.Context{HomeDir: home, GOOS: "darwin"}
	result := SSHKeysGuard().Rules(ctx)
	if len(result.Rules) != 0 {
		t.Errorf("expected 0 rules for empty .ssh, got %d", len(result.Rules))
	}
	if len(result.Protected) != 0 {
		t.Errorf("expected 0 protected, got %d", len(result.Protected))
	}
}

func TestSSHKeysGuard_OnlyPublicKeys(t *testing.T) {
	home := t.TempDir()
	sshDir := filepath.Join(home, ".ssh")
	os.MkdirAll(sshDir, 0o700)
	os.WriteFile(filepath.Join(sshDir, "id_rsa.pub"), []byte("pub"), 0o644)
	os.WriteFile(filepath.Join(sshDir, "id_ed25519.pub"), []byte("pub"), 0o644)

	ctx := &seatbelt.Context{HomeDir: home, GOOS: "darwin"}
	result := SSHKeysGuard().Rules(ctx)

	if len(result.Protected) != 0 {
		t.Errorf("expected 0 protected (only pub keys), got %d", len(result.Protected))
	}
	if len(result.Allowed) != 2 {
		t.Errorf("expected 2 allowed (.pub files), got %d", len(result.Allowed))
	}
}

func TestSSHKeysGuard_MixedFiles(t *testing.T) {
	home := t.TempDir()
	sshDir := filepath.Join(home, ".ssh")
	os.MkdirAll(sshDir, 0o700)

	// Safe files
	os.WriteFile(filepath.Join(sshDir, "known_hosts"), []byte("host"), 0o644)
	os.WriteFile(filepath.Join(sshDir, "known_hosts.old"), []byte("old"), 0o644)
	os.WriteFile(filepath.Join(sshDir, "config"), []byte("cfg"), 0o644)
	os.WriteFile(filepath.Join(sshDir, "authorized_keys"), []byte("ak"), 0o644)
	os.WriteFile(filepath.Join(sshDir, "id_rsa.pub"), []byte("pub"), 0o644)
	os.WriteFile(filepath.Join(sshDir, "environment"), []byte("env"), 0o644)

	// Private keys (should be denied)
	os.WriteFile(filepath.Join(sshDir, "id_rsa"), []byte("key"), 0o600)
	os.WriteFile(filepath.Join(sshDir, "id_ed25519"), []byte("key"), 0o600)
	os.WriteFile(filepath.Join(sshDir, "my-deploy-key"), []byte("key"), 0o600)

	// Subdirectory (should be skipped)
	os.MkdirAll(filepath.Join(sshDir, "sockets"), 0o700)

	ctx := &seatbelt.Context{HomeDir: home, GOOS: "darwin"}
	result := SSHKeysGuard().Rules(ctx)

	if len(result.Protected) != 3 {
		t.Errorf("expected 3 protected (private keys), got %d: %v",
			len(result.Protected), result.Protected)
	}
	if len(result.Allowed) != 6 {
		t.Errorf("expected 6 allowed (safe files), got %d: %v",
			len(result.Allowed), result.Allowed)
	}

	// Verify deny rules exist for each protected path
	ruleText := ""
	for _, r := range result.Rules {
		ruleText += r.String() + "\n"
	}
	for _, p := range result.Protected {
		if !strings.Contains(ruleText, p) {
			t.Errorf("missing deny rule for protected path %q", p)
		}
	}
	// Verify allow rules exist for each allowed path
	for _, p := range result.Allowed {
		if !strings.Contains(ruleText, p) {
			t.Errorf("missing allow rule for allowed path %q", p)
		}
	}
}

func TestSSHKeysGuard_SymlinkToFile(t *testing.T) {
	home := t.TempDir()
	sshDir := filepath.Join(home, ".ssh")
	os.MkdirAll(sshDir, 0o700)

	// Create a key file and symlink to it
	os.WriteFile(filepath.Join(sshDir, "actual-key"), []byte("key"), 0o600)
	os.Symlink(
		filepath.Join(sshDir, "actual-key"),
		filepath.Join(sshDir, "link-to-key"),
	)

	ctx := &seatbelt.Context{HomeDir: home, GOOS: "darwin"}
	result := SSHKeysGuard().Rules(ctx)

	// Both actual-key and link-to-key should be denied
	if len(result.Protected) != 2 {
		t.Errorf("expected 2 protected (key + symlink), got %d: %v",
			len(result.Protected), result.Protected)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./pkg/seatbelt/guards/ -run "TestSSHKeysGuard_" -v`
Expected: FAIL — tests fail because current implementation returns different structure

- [ ] **Step 3: Rewrite the SSH keys guard**

In `guard_ssh_keys.go`:

```go
package guards

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

// sshSafeFiles are filenames known to be safe (not private key material).
var sshSafeFiles = map[string]bool{
	"known_hosts":     true,
	"known_hosts.old": true, // backup from ssh-keygen -R
	"config":          true,
	"authorized_keys": true,
	"environment":     true,
}

type sshKeysGuard struct{}

// SSHKeysGuard returns a Guard that discovers SSH files and denies access
// to private keys while allowing known-safe files.
func SSHKeysGuard() seatbelt.Guard { return &sshKeysGuard{} }

func (g *sshKeysGuard) Name() string        { return "ssh-keys" }
func (g *sshKeysGuard) Type() string        { return "default" }
func (g *sshKeysGuard) Description() string {
	return "Blocks access to SSH private keys; allows known_hosts and config"
}

func (g *sshKeysGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	result := seatbelt.GuardResult{}
	sshDir := ctx.HomePath(".ssh")

	if !dirExists(sshDir) {
		result.Skipped = append(result.Skipped,
			fmt.Sprintf("%s not found, SSH key protection skipped", sshDir))
		return result
	}

	entries, err := os.ReadDir(sshDir)
	if err != nil {
		result.Skipped = append(result.Skipped,
			fmt.Sprintf("%s unreadable, SSH key protection skipped", sshDir))
		return result
	}

	var allowRules []seatbelt.Rule
	var denyRules []seatbelt.Rule

	for _, entry := range entries {
		// Skip directories
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		fullPath := filepath.Join(sshDir, name)

		if isSafeSSHFile(name) {
			allowRules = append(allowRules,
				seatbelt.AllowRule(fmt.Sprintf(
					`(allow file-read* (literal "%s"))`, fullPath)))
			result.Allowed = append(result.Allowed, fullPath)
		} else {
			denyRules = append(denyRules, DenyFile(fullPath)...)
			result.Protected = append(result.Protected, fullPath)
		}
	}

	// Emit allow rules first, then deny rules
	if len(allowRules) > 0 {
		result.Rules = append(result.Rules,
			seatbelt.SectionAllow("SSH safe files (allow)"))
		result.Rules = append(result.Rules, allowRules...)
	}
	if len(denyRules) > 0 {
		result.Rules = append(result.Rules,
			seatbelt.SectionDeny("SSH private keys (deny)"))
		result.Rules = append(result.Rules, denyRules...)
	}

	// Allow directory listing metadata
	if len(allowRules) > 0 || len(denyRules) > 0 {
		result.Rules = append(result.Rules,
			seatbelt.AllowRule(fmt.Sprintf(
				`(allow file-read-metadata (literal "%s"))`, sshDir)))
	}

	return result
}

// isSafeSSHFile returns true if the filename matches the safe-file allowlist.
func isSafeSSHFile(name string) bool {
	if sshSafeFiles[name] {
		return true
	}
	if strings.HasSuffix(name, ".pub") {
		return true
	}
	return false
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./pkg/seatbelt/guards/ -run "TestSSHKeysGuard" -v`
Expected: PASS

- [ ] **Step 5: Commit**

Stage `pkg/seatbelt/guards/guard_ssh_keys.go` and `pkg/seatbelt/guards/guard_ssh_keys_test.go`, then run `/commit`.

---

### Task 7: Credential Guards — Existence Checks and GuardResult

Update all credential guards with existence checks and GuardResult return.

**Files:**
- Modify: `pkg/seatbelt/guards/guard_password_managers.go`
- Modify: `pkg/seatbelt/guards/guard_cloud.go`
- Modify: `pkg/seatbelt/guards/guard_kubernetes.go`
- Modify: `pkg/seatbelt/guards/guard_terraform.go`
- Modify: `pkg/seatbelt/guards/guard_vault.go`
- Modify: `pkg/seatbelt/guards/guard_browsers.go`
- Modify: `pkg/seatbelt/guards/guard_sensitive.go`
- Test: `pkg/seatbelt/guards/guard_password_managers_test.go`
- Test: `pkg/seatbelt/guards/guard_cloud_test.go` (includes vault, terraform, kubernetes tests)
- Test: `pkg/seatbelt/guards/guard_browsers_test.go`
- Test: `pkg/seatbelt/guards/guard_sensitive_test.go`

Note: `AideSecretsGuard` lives in `guard_password_managers.go`, not `guard_aide_secrets.go` (which is an empty stub). Update it alongside `PasswordManagersGuard`. Vault and terraform guard tests live in `guard_cloud_test.go`.

- [ ] **Step 1: Write tests for existence-check behavior**

For each credential guard, add a test case with a clean temp home directory where no credential paths exist. Verify `result.Skipped` is populated and `result.Rules` is empty.

Example for PasswordManagersGuard:
```go
func TestPasswordManagers_AllSkipped(t *testing.T) {
	ctx := &seatbelt.Context{HomeDir: t.TempDir(), GOOS: "darwin"}
	result := PasswordManagersGuard().Rules(ctx)
	if len(result.Rules) != 0 {
		t.Errorf("expected 0 rules when no password managers installed, got %d", len(result.Rules))
	}
	if len(result.Skipped) == 0 {
		t.Error("expected skip messages when no password managers found")
	}
}
```

Example for CloudAWSGuard with env override:
```go
func TestCloudAWS_Override(t *testing.T) {
	home := t.TempDir()
	customPath := filepath.Join(home, "custom-aws-creds")
	os.WriteFile(customPath, []byte("creds"), 0o600)
	ctx := &seatbelt.Context{
		HomeDir: home,
		GOOS:    "darwin",
		Env:     []string{fmt.Sprintf("AWS_SHARED_CREDENTIALS_FILE=%s", customPath)},
	}
	result := CloudAWSGuard().Rules(ctx)
	if len(result.Overrides) == 0 {
		t.Error("expected override to be recorded")
	}
	if result.Overrides[0].EnvVar != "AWS_SHARED_CREDENTIALS_FILE" {
		t.Errorf("override env var = %q, want AWS_SHARED_CREDENTIALS_FILE",
			result.Overrides[0].EnvVar)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./pkg/seatbelt/guards/ -run "AllSkipped|Override" -v`
Expected: FAIL

- [ ] **Step 3: Update password-managers guard**

In `guard_password_managers.go`, update `PasswordManagersGuard.Rules()`:
- Check each tool directory with `dirExists`/`pathExists` before emitting deny rules
- Add to `result.Protected` when denying, `result.Skipped` when missing
- Replace `RestrictRule`/`SectionRestrict` with `DenyRule`/`SectionDeny`

Also update `AideSecretsGuard` in the same file (`guard_password_managers.go` — NOT `guard_aide_secrets.go` which is an empty stub).

- [ ] **Step 4: Update cloud guards**

In `guard_cloud.go`, update each cloud guard:
- Check if default path exists before denying
- When env override is detected and the override path exists, record in `result.Overrides`
- Replace `RestrictRule`/`SectionRestrict` with `DenyRule`/`SectionDeny`
- Add to `result.Protected`/`result.Skipped` as appropriate

- [ ] **Step 5: Update kubernetes guard**

In `guard_kubernetes.go`:
- Change `Type()` from `"default"` to `"opt-in"`
- Add existence check for kubeconfig path
- Return `GuardResult` with proper diagnostics

- [ ] **Step 6: Update remaining credential guards**

Update `guard_terraform.go`, `guard_vault.go`, `guard_browsers.go`, `guard_sensitive.go` with the same pattern:
- Existence check per path
- Return `GuardResult`
- Populate `Protected`/`Skipped`/`Overrides`
- Use `DenyRule`/`SectionDeny` constructors

- [ ] **Step 7: Update existing tests**

Update all existing credential guard tests to access `result.Rules` instead of raw `rules`. Add assertions for `result.Protected` containing expected paths.

- [ ] **Step 8: Run all guard tests**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./pkg/seatbelt/guards/ -v`
Expected: PASS

- [ ] **Step 9: Commit**

Stage all modified credential guard and test files, then run `/commit`.

---

### Task 8: Custom Guard — Interface Update

**Files:**
- Modify: `pkg/seatbelt/guards/guard_custom.go`
- Test: `pkg/seatbelt/guards/guard_custom_test.go`

- [ ] **Step 1: Update custom guard to return GuardResult**

In `guard_custom.go`, update `Rules()` to return `seatbelt.GuardResult`, add existence checks for configured paths, populate `Protected`/`Skipped`/`Overrides`. Replace `RestrictRule`/`GrantRule` with `DenyRule`/`AllowRule`.

- [ ] **Step 2: Update custom guard tests**

Access `result.Rules` instead of raw slice. Add test for missing paths producing skip messages.

- [ ] **Step 3: Run tests**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./pkg/seatbelt/guards/ -run TestCustom -v`
Expected: PASS

- [ ] **Step 4: Commit**

Stage `pkg/seatbelt/guards/guard_custom.go` and `pkg/seatbelt/guards/guard_custom_test.go`, then run `/commit`.

---

### Task 9: Caller Update — darwin.go and EvaluateGuards

**Files:**
- Modify: `internal/sandbox/darwin.go`

- [ ] **Step 1: Update generateSeatbeltProfile to handle ProfileResult**

Change `generateSeatbeltProfile` to return `(string, error)` still (callers only need the profile string):

```go
func generateSeatbeltProfile(policy Policy) (string, error) {
	// ... existing code ...
	result, err := p.Render()
	if err != nil {
		return "", err
	}
	return result.Profile, nil
}
```

- [ ] **Step 2: Add EvaluateGuards export for banner consumption**

The banner needs `GuardResult` data but doesn't need the rendered profile. Add a new exported function that runs guard evaluation and returns diagnostics:

```go
// EvaluateGuards runs all guards from the policy and returns their diagnostics
// without rendering a full profile. Used by the banner layer to show guard status.
func EvaluateGuards(policy *Policy) []seatbelt.GuardResult {
	if policy == nil {
		return nil
	}
	homeDir, _ := os.UserHomeDir()
	activeGuards := guards.ResolveActiveGuards(policy.Guards)

	ctx := &seatbelt.Context{
		HomeDir:     homeDir,
		ProjectRoot: policy.ProjectRoot,
		TempDir:     policy.TempDir,
		RuntimeDir:  policy.RuntimeDir,
		Env:         policy.Env,
		GOOS:        runtime.GOOS,
		Network:     string(policy.Network),
		AllowPorts:  policy.AllowPorts,
		DenyPorts:   policy.DenyPorts,
		ExtraDenied: policy.ExtraDenied,
	}

	var results []seatbelt.GuardResult
	for _, g := range activeGuards {
		result := g.Rules(ctx)
		result.Name = g.Name()
		results = append(results, result)
	}
	if policy.AgentModule != nil {
		result := policy.AgentModule.Rules(ctx)
		result.Name = policy.AgentModule.Name()
		results = append(results, result)
	}
	return results
}
```

Also add a helper to get available (opt-in) guard names that aren't in the policy:

```go
// AvailableGuardNames returns opt-in guard names not included in the active list.
func AvailableGuardNames(activeNames []string) []string {
	active := make(map[string]bool)
	for _, n := range activeNames {
		active[n] = true
	}
	var available []string
	for _, g := range guards.AllGuards() {
		if g.Type() == "opt-in" && !active[g.Name()] {
			available = append(available, g.Name())
		}
	}
	return available
}
```

- [ ] **Step 3: Run full test suite**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./... 2>&1 | tail -30`
Expected: PASS across all packages.

- [ ] **Step 4: Commit**

Stage `internal/sandbox/darwin.go`, then run `/commit`.

---

### Task 10: Banner Redesign — Data Model and Rendering

**Files:**
- Modify: `internal/ui/banner.go`
- Test: `internal/ui/banner_test.go`

- [ ] **Step 1: Write tests for new banner rendering**

In `banner_test.go`, add tests for the new grouped guard display:

```go
func TestRenderCompact_GuardGroups(t *testing.T) {
	var buf bytes.Buffer
	data := &BannerData{
		ContextName: "test-project",
		AgentName:   "claude",
		Sandbox: &SandboxInfo{
			Network: "outbound only",
			Active: []GuardDisplay{
				{
					Name:      "ssh-keys",
					Protected: []string{"~/.ssh/id_rsa", "~/.ssh/id_ed25519"},
					Allowed:   []string{"~/.ssh/known_hosts", "~/.ssh/config"},
				},
			},
			Skipped: []GuardDisplay{
				{Name: "kubernetes", Reason: "~/.kube not found"},
			},
			Available: []string{"docker", "github-cli"},
		},
	}
	RenderCompact(&buf, data)
	out := buf.String()

	// Active guard with details
	if !strings.Contains(out, "ssh-keys") {
		t.Error("missing active guard name")
	}
	if !strings.Contains(out, "id_rsa") {
		t.Error("missing protected path")
	}
	if !strings.Contains(out, "known_hosts") {
		t.Error("missing allowed path")
	}
	// Skipped guard
	if !strings.Contains(out, "kubernetes") {
		t.Error("missing skipped guard")
	}
	if !strings.Contains(out, "~/.kube not found") {
		t.Error("missing skip reason")
	}
	// Available guards
	if !strings.Contains(out, "docker") {
		t.Error("missing available guard")
	}
	// Hint line
	if !strings.Contains(out, "aide sandbox") {
		t.Error("missing hint line")
	}
}

func TestRenderCompact_ListTruncation(t *testing.T) {
	var buf bytes.Buffer
	data := &BannerData{
		Sandbox: &SandboxInfo{
			Network: "outbound only",
			Active: []GuardDisplay{
				{
					Name: "password-managers",
					Protected: []string{
						"~/.config/op", "~/.password-store",
						"~/.gnupg/private-keys-v1.d",
						"~/.config/Bitwarden CLI",
						"~/.local/share/gopass",
					},
				},
			},
		},
	}
	RenderCompact(&buf, data)
	out := buf.String()
	if !strings.Contains(out, "(+2 more)") {
		t.Errorf("expected truncation marker, got:\n%s", out)
	}
}
```

Add similar tests for `RenderBoxed` and `RenderClean`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./internal/ui/ -run "TestRenderCompact_Guard|TestRenderCompact_List" -v`
Expected: FAIL — new `SandboxInfo` fields don't exist

- [ ] **Step 3: Implement new SandboxInfo and GuardDisplay types**

In `banner.go`, replace the old `SandboxInfo`:

```go
// SandboxInfo describes sandbox configuration for display.
type SandboxInfo struct {
	Disabled  bool
	Network   string           // "outbound only", "unrestricted", "none"
	Ports     string           // "all" or "443, 53"
	Active    []GuardDisplay
	Skipped   []GuardDisplay
	Available []string         // opt-in guard names not enabled
}

// GuardDisplay holds per-guard information for banner rendering.
type GuardDisplay struct {
	Name      string
	Protected []string
	Allowed   []string
	Overrides []GuardOverride
	Reason    string           // for skipped: "~/.kube not found"
}

// GuardOverride records an env var override for display.
type GuardOverride struct {
	EnvVar      string
	Value       string
	DefaultPath string
}
```

Note: `GuardOverride` is a UI-layer copy of `seatbelt.Override` to avoid importing `pkg/seatbelt` into `internal/ui`. The caller maps between them.

- [ ] **Step 4: Implement guard rendering helpers**

```go
// truncateList caps a list at maxItems and appends "(+N more)" if truncated.
func truncateList(items []string, maxItems int) string {
	if len(items) <= maxItems {
		return strings.Join(items, ", ")
	}
	shown := strings.Join(items[:maxItems], ", ")
	return fmt.Sprintf("%s (+%d more)", shown, len(items)-maxItems)
}

// renderGuardSection renders the grouped guard display for all banner styles.
func renderGuardSection(w io.Writer, info *SandboxInfo, prefix string) {
	// Active guards
	for _, g := range info.Active {
		boldGreen.Fprintf(w, "%s✓ %s\n", prefix, g.Name)
		if len(g.Protected) > 0 {
			fmt.Fprintf(w, "%s    denied:  %s\n", prefix, truncateList(g.Protected, 3))
		}
		if len(g.Allowed) > 0 {
			fmt.Fprintf(w, "%s    allowed: %s\n", prefix, truncateList(g.Allowed, 3))
		}
		for _, o := range g.Overrides {
			fmt.Fprintf(w, "%s    override: %s → %s (default: %s)\n",
				prefix, o.EnvVar, o.Value, o.DefaultPath)
		}
	}

	// Blank line between groups
	if len(info.Active) > 0 && (len(info.Skipped) > 0 || len(info.Available) > 0) {
		fmt.Fprintln(w)
	}

	// Skipped guards
	for _, g := range info.Skipped {
		yellow.Fprintf(w, "%s⊘ %s", prefix, g.Name)
		fmt.Fprintf(w, " — %s\n", g.Reason)
	}

	// Blank line
	if len(info.Skipped) > 0 && len(info.Available) > 0 {
		fmt.Fprintln(w)
	}

	// Available guards
	if len(info.Available) > 0 {
		dim.Fprintf(w, "%s○ %s — available (opt-in)\n",
			prefix, strings.Join(info.Available, ", "))
	}

	// Hint line
	needsHint := len(info.Skipped) > 0 || len(info.Available) > 0
	for _, g := range info.Active {
		if len(g.Protected) > 3 || len(g.Allowed) > 3 {
			needsHint = true
		}
	}
	if needsHint {
		fmt.Fprintln(w)
		dim.Fprintf(w, "%srun `aide sandbox` for full details\n", prefix)
	}
}
```

- [ ] **Step 5: Update RenderCompact to use new guard section**

Replace the old `sandboxCountsLine`/`sandboxDeniedLine`/`sandboxProtectingLine` calls with the new `renderGuardSection`. Remove old helper functions.

Update the sandbox section in `RenderCompact`:
```go
if data.Sandbox != nil {
    fmt.Fprintf(w, "   🛡 Sandbox\n")
    if !data.Sandbox.Disabled {
        fmt.Fprintf(w, "         network: %s\n", data.Sandbox.Network)
        if data.Sandbox.Ports != "" && data.Sandbox.Ports != "all" {
            fmt.Fprintf(w, "         ports: %s\n", data.Sandbox.Ports)
        }
        fmt.Fprintln(w)
        renderGuardSection(w, data.Sandbox, "     ")
    } else {
        fmt.Fprintf(w, "         disabled\n")
    }
}
```

- [ ] **Step 6: Update RenderBoxed and RenderClean similarly**

Same pattern, adjust prefix for box-drawing characters or clean indentation.

- [ ] **Step 7: Remove old helper functions**

Remove `sandboxSummary`, `sandboxDeniedLine`, `sandboxCountsLine`, `sandboxProtectingLine` and the old `SandboxInfo` fields they depended on.

- [ ] **Step 8: Update existing banner tests**

Update any existing tests that create `SandboxInfo` with old fields to use the new struct.

- [ ] **Step 9: Run tests**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./internal/ui/ -v`
Expected: PASS

- [ ] **Step 10: Commit**

Stage `internal/ui/banner.go` and `internal/ui/banner_test.go`, then run `/commit`.

---

### Task 11: Integration — Wire GuardResult to Banner

There are two places where `SandboxInfo` is constructed from a `Policy`:
1. `cmd/aide/commands.go:292` — the `aide which` command
2. `internal/launcher/launcher.go:399` — the launcher banner

Both currently build the old `SandboxInfo` with `GuardCount`, `Denied`, `Guards`, `Protecting`. These must be updated to call `sandbox.EvaluateGuards()` (from Task 9) and map results into the new `SandboxInfo`.

**Files:**
- Modify: `cmd/aide/commands.go`
- Modify: `internal/launcher/launcher.go`

- [ ] **Step 1: Add a shared helper to convert GuardResult to SandboxInfo**

Place this mapping function directly at each call site (`commands.go` and `launcher.go`) rather than in `internal/sandbox/`, because adding a `ui` import to `internal/sandbox` risks creating an import cycle. Both call sites already import `internal/ui` and `internal/sandbox`.

```go
// GuardsToSandboxInfo maps guard evaluation results into banner display data.
func GuardsToSandboxInfo(
	guardResults []seatbelt.GuardResult,
	availableNames []string,
	network string,
	ports string,
) *ui.SandboxInfo {
	info := &ui.SandboxInfo{
		Network:   networkDisplayName(network),
		Ports:     ports,
		Available: availableNames,
	}
	for _, g := range guardResults {
		if len(g.Rules) > 0 {
			display := ui.GuardDisplay{
				Name:      g.Name,
				Protected: g.Protected,
				Allowed:   g.Allowed,
			}
			for _, o := range g.Overrides {
				display.Overrides = append(display.Overrides, ui.GuardOverride{
					EnvVar:      o.EnvVar,
					Value:       o.Value,
					DefaultPath: o.DefaultPath,
				})
			}
			info.Active = append(info.Active, display)
		} else if len(g.Skipped) > 0 {
			info.Skipped = append(info.Skipped, ui.GuardDisplay{
				Name:   g.Name,
				Reason: strings.Join(g.Skipped, "; "),
			})
		}
	}
	return info
}

// networkDisplayName converts raw network mode to user-friendly display.
func networkDisplayName(mode string) string {
	switch mode {
	case "outbound":
		return "outbound only"
	case "none":
		return "none"
	case "unrestricted":
		return "unrestricted"
	default:
		return mode
	}
}
```

- [ ] **Step 2: Update cmd/aide/commands.go**

Replace the old `SandboxInfo` construction (around line 292):

```go
// Old:
si := &ui.SandboxInfo{
    Network:    string(policy.Network),
    GuardCount: len(policy.Guards),
    Denied:     policy.ExtraDenied,
}

// New:
guardResults := sandbox.EvaluateGuards(policy)
availableNames := sandbox.AvailableGuardNames(policy.Guards)
portsStr := "all"
if len(policy.AllowPorts) > 0 {
    portStrs := make([]string, len(policy.AllowPorts))
    for i, p := range policy.AllowPorts {
        portStrs[i] = strconv.Itoa(p)
    }
    portsStr = strings.Join(portStrs, ", ")
}
si := sandbox.GuardsToSandboxInfo(guardResults, availableNames,
    string(policy.Network), portsStr)
```

Remove old field references (`GuardCount`, `Denied`, `Guards`, `Protecting`).

- [ ] **Step 3: Update internal/launcher/launcher.go**

Same pattern as Step 2 — replace old `SandboxInfo` construction around line 399 with `EvaluateGuards` + `GuardsToSandboxInfo`.

- [ ] **Step 4: Run full test suite**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./... 2>&1 | tail -30`
Expected: PASS

- [ ] **Step 5: Commit**

Stage `cmd/aide/commands.go` and `internal/launcher/launcher.go`, then run `/commit`.

---

### Task 12: Regression Verification

- [ ] **Step 1: Verify no old constructors remain**

Run:
```bash
cd /Users/subramk/source/github.com/jskswamy/aide
grep -rn "SetupRule\|RestrictRule\|GrantRule\|SectionSetup\|SectionRestrict\|SectionGrant" pkg/ internal/ --include="*.go" | grep -v "_test.go" | grep -v "test_"
```
Expected: No matches

- [ ] **Step 2: Verify no old RuleIntent constants remain**

Run:
```bash
grep -rn "Setup\s*RuleIntent\|Restrict\s*RuleIntent\|Grant\s*RuleIntent" pkg/ --include="*.go"
```
Expected: No matches

- [ ] **Step 3: Run full test suite one final time**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./... -count=1`
Expected: All packages PASS

- [ ] **Step 4: Verify the generated seatbelt profile structure**

Write a quick integration test or use an existing one to render a full profile and verify:
- All `(deny ...)` rules appear after `(allow ...)` rules
- SSH guard uses literal paths (not subpath) for deny rules
- No `SectionRestrict` or `SectionGrant` comments in output

- [ ] **Step 5: Commit any final fixes**

If any issues were found in steps 1-4, fix and commit.
