# aide context UX Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace `aide context add` / `add-match` with intent-named `aide context bind` and `aide context create`, and turn the launcher's "no context matched" failure into a guided empty-state flow (TTY interactive, non-TTY hard error with concrete next-command hints).

**Architecture:** Two new cobra commands (`bind`, `create`) live in `cmd/aide/context.go` alongside their existing siblings. A small helper in `cmd/aide/match_rule.go` resolves the auto-detected match (git remote when available, else folder path), reused by both new commands and `aide use`. The launcher gains an `empty_state.go` that handles the TTY prompt and non-TTY error path, called from `Launch` only when `aidectx.Resolve` errors AND `cfg.DefaultContext == ""`. To keep the spec's promise that choices `[1]` and `[2]` continue the launch inline (no re-run required), the launcher accepts a small `EmptyStateActions` interface that `main()` wires up with cmd-layer implementations of `bind` and `create`. After the action runs, the launcher reloads config and re-resolves transparently. Existing `add` and `add-match` commands are deleted outright (clean break — no aliases).

**Tech Stack:** Go, cobra/pflag, `golang.org/x/term` for TTY detection, existing `internal/context` resolver, existing `internal/launcher` framework.

---

## Spec

See `docs/superpowers/specs/2026-05-01-context-ux-redesign.md`.

## File Map

- **Create:** `cmd/aide/match_rule.go` — `autoDetectMatchRule(cwd string) (config.MatchRule, string)` returning the rule plus a human-readable description ("by remote ..." / "by path ..."). Reused by `bind`, `create`, and the empty-state `[2]` path.
- **Modify:** `cmd/aide/context.go` — delete `contextAddCmd` and `contextAddMatchCmd`; add `contextBindCmd` and `contextCreateCmd`; update `contextCmd()` to register the new ones.
- **Create:** `cmd/aide/context_bind_test.go` — cobra-level parsing/validation tests for `bind`.
- **Create:** `cmd/aide/context_create_test.go` — cobra-level parsing/validation tests for `create`.
- **Create:** `internal/launcher/empty_state.go` — `handleEmptyState(...)` returning the chosen `*ResolvedContext` (or error / cancel). Defines the `EmptyStateActions` interface that the launcher caller implements.
- **Create:** `internal/launcher/empty_state_test.go` — TTY and non-TTY paths with fake reader/writer and a fake `EmptyStateActions`.
- **Modify:** `internal/launcher/launcher.go` — add `EmptyStateActions` field on `Launcher`; call `handleEmptyState` from `Launch` when `Resolve` fails AND `cfg.DefaultContext == ""`; reload config and re-resolve after `[1]`/`[2]` actions complete.
- **Modify:** `cmd/aide/main.go` — wire a concrete `EmptyStateActions` implementation when constructing `Launcher`.
- **Create:** `cmd/aide/empty_state_actions.go` — adapter that calls `runCreateWizard` and `contextBindCmd().RunE` from launcher.
- **Modify:** `cmd/aide/status.go` — strip the `aide use --context myproject # Add CWD match` example from the `useCmd` help block (the recommended path is now `aide context bind <name>`); update the line at status.go:175 the same way.
- **Modify:** `docs/cli-reference.md`, `docs/environment.md` — add bind/create rows; remove add/add-match references.
- **Create:** `docs/getting-started.md` — first-run / empty-state walkthrough.

---

## Task 1: Match-rule auto-detect helper

A small pure function used by both new commands and the empty-state path. Pure function = trivial to test.

**Files:**
- Create: `cmd/aide/match_rule.go`
- Create: `cmd/aide/match_rule_test.go`

- [ ] **Step 1: Write the failing test**

Create `cmd/aide/match_rule_test.go`:

```go
// cmd/aide/match_rule_test.go
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/jskswamy/aide/internal/config"
)

func TestAutoDetectMatchRule_NonGitFolder_PathRule(t *testing.T) {
	dir := t.TempDir()
	rule, desc := autoDetectMatchRule(dir)
	if rule.Path != dir || rule.Remote != "" {
		t.Errorf("non-git folder: got %+v, want Path=%s", rule, dir)
	}
	if desc == "" {
		t.Error("description must be non-empty")
	}
}

func TestAutoDetectMatchRule_GitRepoWithRemote_RemoteRule(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "git", "init")
	mustRun(t, dir, "git", "remote", "add", "origin", "git@example.com:foo/bar.git")
	rule, _ := autoDetectMatchRule(dir)
	if rule.Remote != "git@example.com:foo/bar.git" {
		t.Errorf("git repo with remote: got %+v, want Remote=git@example.com:foo/bar.git", rule)
	}
	if rule.Path != "" {
		t.Errorf("git repo with remote should not set Path: got %q", rule.Path)
	}
}

func TestAutoDetectMatchRule_GitRepoNoRemote_PathRule(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "git", "init")
	rule, _ := autoDetectMatchRule(dir)
	if rule.Path != dir {
		t.Errorf("git repo no remote: got %+v, want Path=%s", rule, dir)
	}
}

func mustRun(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=t@t",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, string(out))
	}
}

// Unused-import shield: keep filepath/config referenced even if a future test
// shrinks; harmless during plan execution.
var _ = filepath.Join
var _ = config.MatchRule{}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./cmd/aide/ -run TestAutoDetectMatchRule -v
```
Expected: FAIL — "undefined: autoDetectMatchRule".

- [ ] **Step 3: Implement the helper**

Create `cmd/aide/match_rule.go`:

```go
// cmd/aide/match_rule.go
package main

import (
	"fmt"

	"github.com/jskswamy/aide/internal/config"
	aidectx "github.com/jskswamy/aide/internal/context"
)

// autoDetectMatchRule returns the match rule that best identifies the
// given folder. If the folder is inside a git repo with an "origin"
// remote, match by remote URL (durable across worktrees and fresh
// checkouts). Otherwise match by exact folder path.
//
// The second return value is a human-readable description suitable for
// inclusion in user-facing output, e.g. "by remote git@…/foo.git" or
// "by path /Users/x/work/foo".
func autoDetectMatchRule(cwd string) (config.MatchRule, string) {
	if remote := aidectx.DetectRemote(cwd, "origin"); remote != "" {
		return config.MatchRule{Remote: remote}, fmt.Sprintf("by remote %s", remote)
	}
	return config.MatchRule{Path: cwd}, fmt.Sprintf("by path %s", cwd)
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./cmd/aide/ -run TestAutoDetectMatchRule -v
```
Expected: all 3 PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/aide/match_rule.go cmd/aide/match_rule_test.go
__GIT_COMMIT_PLUGIN__=1 git commit -m "Add autoDetectMatchRule helper for context bind/create"
```

---

## Task 2: `aide context bind`

Replaces `add-match`. Attaches cwd to an existing context. Auto-detects the match rule unless `--path` or `--remote` is passed. Hybrid TTY behavior when the named context doesn't exist.

**Files:**
- Modify: `cmd/aide/context.go` — add `contextBindCmd`, register it
- Create: `cmd/aide/context_bind_test.go`

- [ ] **Step 1: Write the failing tests**

Create `cmd/aide/context_bind_test.go`:

```go
// cmd/aide/context_bind_test.go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func runContextBind(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := contextBindCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

// isolatedConfigDir builds a tempdir with HOME/XDG redirected so the
// tests cannot read the developer's real config. Returns the dir.
func isolatedConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
	t.Setenv("HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "xdg", "aide"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// writeConfig writes a minimal config.yaml with the given context names
// (each with a no-op match rule so they are not "minimal config").
func writeConfig(t *testing.T, dir string, contexts ...string) {
	t.Helper()
	var b strings.Builder
	b.WriteString("contexts:\n")
	for _, c := range contexts {
		b.WriteString("  ")
		b.WriteString(c)
		b.WriteString(":\n")
		b.WriteString("    agent: claude\n")
		b.WriteString("    match:\n")
		b.WriteString("      - path: /never/matches/anything\n")
	}
	path := filepath.Join(dir, "xdg", "aide", "config.yaml")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestContextBind_ExistingContext_AppendsMatch(t *testing.T) {
	dir := isolatedConfigDir(t)
	writeConfig(t, dir, "work")
	out, err := runContextBind(t, "work")
	if err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out)
	}
	if !strings.Contains(out, `Bound this folder to context "work"`) {
		t.Errorf("expected success message, got: %s", out)
	}
}

func TestContextBind_MissingContext_NonTTY_Errors(t *testing.T) {
	isolatedConfigDir(t) // no contexts written
	_, err := runContextBind(t, "ghost")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, `not found`) || !strings.Contains(msg, "aide context create ghost") {
		t.Errorf("expected not-found error pointing at create, got: %v", err)
	}
}

func TestContextBind_PathFlag_ForcesPathRule(t *testing.T) {
	dir := isolatedConfigDir(t)
	writeConfig(t, dir, "work")
	out, err := runContextBind(t, "work", "--path")
	if err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out)
	}
	if !strings.Contains(out, "by path") {
		t.Errorf("--path should produce path-based match: %s", out)
	}
}

func TestContextBind_RemoteFlag_OutsideGitRepo_Errors(t *testing.T) {
	isolatedConfigDir(t) // tempdir is not a git repo
	writeConfig(t, isolatedConfigDirReuse(t), "work") // sub-helper not needed; test isolation is per-test
	// The test above already isolated; we only care about the error.
	_, err := runContextBind(t, "work", "--remote")
	if err == nil || !strings.Contains(err.Error(), "not a git repo") {
		t.Errorf("expected git-repo error, got: %v", err)
	}
}

// Helper used by the remote-flag test to avoid re-isolating.
func isolatedConfigDirReuse(t *testing.T) string {
	t.Helper()
	cwd, _ := os.Getwd()
	return cwd
}

func TestContextBind_PathAndRemote_MutualExclusive(t *testing.T) {
	dir := isolatedConfigDir(t)
	writeConfig(t, dir, "work")
	_, err := runContextBind(t, "work", "--path", "--remote")
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("expected mutual-exclusion error, got: %v", err)
	}
}

// Unused-import shield (cobra import survives even if some tests collapse).
var _ = cobra.Command{}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./cmd/aide/ -run TestContextBind -v
```
Expected: every test fails with `undefined: contextBindCmd`.

- [ ] **Step 3: Implement contextBindCmd**

In `cmd/aide/context.go`, add (and register in `contextCmd()`):

```go
func contextBindCmd() *cobra.Command {
	var (
		forcePath   bool
		forceRemote bool
	)

	cmd := &cobra.Command{
		Use:   "bind [name]",
		Short: "Attach this folder to an existing context",
		Long: `Attach the current folder to an existing context.

Examples:
  aide context bind work               # auto-detect: git remote if repo, else folder path
  aide context bind work --path        # force exact folder path match
  aide context bind work --remote      # force git remote match (errors if not a git repo)
  aide context bind                    # interactive picker over existing contexts`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if forcePath && forceRemote {
				return fmt.Errorf("--path and --remote are mutually exclusive")
			}

			out := cmd.OutOrStdout()
			reader := bufio.NewReader(os.Stdin)

			env, err := cmdEnv(cmd)
			if err != nil {
				return err
			}
			cwd := env.CWD()
			cfg := env.Config()

			var name string
			if len(args) == 1 {
				name = args[0]
			} else {
				picked, err := pickExistingContext(out, reader, cfg)
				if err != nil {
					return err
				}
				name = picked
			}

			ctx, ok := cfg.Contexts[name]
			if !ok {
				// TTY: offer to create. Non-TTY: hard error.
				if isStdinTTY() {
					fmt.Fprintf(out, "Context %q doesn't exist. Create it now? [y/N]: ", name)
					ans, _ := reader.ReadString('\n')
					if strings.EqualFold(strings.TrimSpace(ans), "y") {
						return runCreateWizard(cmd, name, createOptions{here: tristateYes})
					}
				}
				return fmt.Errorf("context %q not found.\nRun: aide context create %s", name, name)
			}

			var rule config.MatchRule
			var desc string
			switch {
			case forceRemote:
				remote := aidectx.DetectRemote(cwd, "origin")
				if remote == "" {
					return fmt.Errorf("--remote requires the current folder to be a git repo with an 'origin' remote (not a git repo or no origin)")
				}
				rule = config.MatchRule{Remote: remote}
				desc = fmt.Sprintf("by remote %s", remote)
			case forcePath:
				rule = config.MatchRule{Path: cwd}
				desc = fmt.Sprintf("by path %s", cwd)
			default:
				rule, desc = autoDetectMatchRule(cwd)
			}

			ctx.Match = append(ctx.Match, rule)
			cfg.Contexts[name] = ctx
			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}
			fmt.Fprintf(out, "Bound this folder to context %q (matched %s)\n", name, desc)
			return nil
		},
	}

	cmd.Flags().BoolVar(&forcePath, "path", false, "Force exact folder path match")
	cmd.Flags().BoolVar(&forceRemote, "remote", false, "Force git remote match (errors if not a git repo)")
	return cmd
}
```

This references three helpers that don't exist yet: `pickExistingContext`, `runCreateWizard` (Task 3), `isStdinTTY` (Task 4 — actually we need it now too). Add these stubs in this same task to keep `bind` self-contained.

Add `isStdinTTY` once at the bottom of `cmd/aide/context.go`:

```go
// isStdinTTY reports whether stdin is connected to a terminal. Used by
// commands that need to choose between interactive prompting and a
// non-TTY hard error.
func isStdinTTY() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}
```

Add the import `"golang.org/x/term"` to `cmd/aide/context.go`.

Add `pickExistingContext` (also in `cmd/aide/context.go`):

```go
// pickExistingContext shows a numbered menu of existing contexts and
// returns the chosen name. Returns an error in non-TTY mode (the
// caller is expected to require a positional name in that case).
func pickExistingContext(out io.Writer, reader *bufio.Reader, cfg *config.Config) (string, error) {
	if !isStdinTTY() {
		return "", fmt.Errorf("a context name is required in non-interactive mode")
	}
	if len(cfg.Contexts) == 0 {
		return "", fmt.Errorf("no contexts configured. Run: aide context create <name>")
	}
	names := make([]string, 0, len(cfg.Contexts))
	for n := range cfg.Contexts {
		names = append(names, n)
	}
	sort.Strings(names)

	fmt.Fprintln(out, "Existing contexts:")
	for i, n := range names {
		fmt.Fprintf(out, "  [%d] %s\n", i+1, n)
	}
	fmt.Fprint(out, "Choose [1]: ")
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("reading selection: %w", err)
	}
	input = strings.TrimSpace(input)
	choice := 1
	if input != "" {
		n, err := strconv.Atoi(input)
		if err != nil || n < 1 || n > len(names) {
			return "", fmt.Errorf("invalid selection: %q", input)
		}
		choice = n
	}
	return names[choice-1], nil
}
```

(`strconv` is already imported.)

Stub `runCreateWizard` — Task 3 fully implements it. For now:

```go
type createTristate int

const (
	tristateUnset createTristate = iota
	tristateYes
	tristateNo
)

type createOptions struct {
	agent  string
	secret string
	here   createTristate
}

// runCreateWizard creates a new context and (optionally) binds cwd.
// Implemented in Task 3.
func runCreateWizard(cmd *cobra.Command, prefilledName string, opts createOptions) error {
	return fmt.Errorf("runCreateWizard not implemented yet")
}
```

In `contextCmd()`, register the new command and DELETE the old `add-match` registration (and the function it pointed at):

```go
// Replace these two lines in contextCmd():
//   cmd.AddCommand(contextAddCmd())
//   cmd.AddCommand(contextAddMatchCmd())
// With:
cmd.AddCommand(contextBindCmd())
// (contextCreateCmd registered in Task 3)
```

Delete the now-unused `contextAddCmd` and `contextAddMatchCmd` functions from `cmd/aide/context.go`. The shared `askMatchRule` helper in `cmd/aide/commands.go` is still used by `contextSetSecretCmd` callers via `aide use` (status.go). Keep it.

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./cmd/aide/ -run TestContextBind -v
```
Expected: all PASS.

- [ ] **Step 5: Verify the rest of the suite still builds**

```bash
go build ./... && go test ./...
```
Expected: green. (Some other tests may break because we deleted `contextAddCmd` / `contextAddMatchCmd`. Fix any test that referenced those by name — the test should be deleted or migrated to the new commands. Don't loosen it.)

- [ ] **Step 6: Commit**

```bash
git add cmd/aide/context.go cmd/aide/context_bind_test.go
__GIT_COMMIT_PLUGIN__=1 git commit -m "Add aide context bind, replacing add-match"
```

---

## Task 3: `aide context create`

Replaces `add`. Interactive wizard for new contexts; flags for non-interactive use; auto-detects single agent on PATH; binds cwd by default in TTY.

**Files:**
- Modify: `cmd/aide/context.go` — add `contextCreateCmd`; replace the stub `runCreateWizard`; register the new command; delete `contextAddCmd`.
- Create: `cmd/aide/context_create_test.go`

- [ ] **Step 1: Write the failing tests**

Create `cmd/aide/context_create_test.go`:

```go
// cmd/aide/context_create_test.go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runContextCreate(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := contextCreateCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	return buf.String(), cmd.Execute()
}

func TestContextCreate_FullyScripted_NoHere(t *testing.T) {
	dir := isolatedConfigDir(t)
	out, err := runContextCreate(t, "work", "--agent", "claude", "--no-here")
	if err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out)
	}
	if !strings.Contains(out, `Created context "work"`) {
		t.Errorf("expected create message, got: %s", out)
	}
	// Verify the config file was written and the context has no match rules.
	cfgBytes, err := os.ReadFile(filepath.Join(dir, "xdg", "aide", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfgBytes), "work:") {
		t.Errorf("config did not include new context: %s", cfgBytes)
	}
	if strings.Contains(string(cfgBytes), "match:") {
		t.Errorf("--no-here should produce no match rules: %s", cfgBytes)
	}
}

func TestContextCreate_FullyScripted_WithHere(t *testing.T) {
	dir := isolatedConfigDir(t)
	out, err := runContextCreate(t, "work", "--agent", "claude", "--here")
	if err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Bound this folder") {
		t.Errorf("--here should bind cwd: %s", out)
	}
	cfgBytes, _ := os.ReadFile(filepath.Join(dir, "xdg", "aide", "config.yaml"))
	if !strings.Contains(string(cfgBytes), "match:") {
		t.Errorf("--here should produce a match rule: %s", cfgBytes)
	}
}

func TestContextCreate_NonTTY_NoName_Errors(t *testing.T) {
	isolatedConfigDir(t)
	_, err := runContextCreate(t, "--agent", "claude", "--no-here")
	if err == nil || !strings.Contains(err.Error(), "name is required") {
		t.Errorf("expected name-required error in non-TTY, got: %v", err)
	}
}

func TestContextCreate_NonTTY_NoAgent_Errors(t *testing.T) {
	isolatedConfigDir(t)
	// We do not stub agent autodetect; in CI no claude/codex/etc is on PATH,
	// so no agent will be auto-picked. The test expects a clear error.
	_, err := runContextCreate(t, "work", "--no-here")
	if err == nil {
		t.Fatal("expected an error when no agent can be resolved")
	}
	if !strings.Contains(err.Error(), "agent") {
		t.Errorf("error must mention agent: %v", err)
	}
}

func TestContextCreate_DuplicateName_Errors(t *testing.T) {
	dir := isolatedConfigDir(t)
	writeConfig(t, dir, "work")
	_, err := runContextCreate(t, "work", "--agent", "claude", "--no-here")
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected duplicate error, got: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./cmd/aide/ -run TestContextCreate -v
```
Expected: every test fails with `undefined: contextCreateCmd` (or similar).

- [ ] **Step 3: Implement contextCreateCmd and runCreateWizard**

In `cmd/aide/context.go`, add:

```go
func contextCreateCmd() *cobra.Command {
	var (
		agent  string
		secret string
		here   bool
		noHere bool
	)

	cmd := &cobra.Command{
		Use:   "create [name]",
		Short: "Create a new context",
		Long: `Create a new context.

Examples:
  aide context create                                              # interactive wizard
  aide context create work                                         # name pre-filled
  aide context create work --agent claude --secret-store firmus --here
  aide context create work --agent claude --no-here                # skip cwd binding`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if here && noHere {
				return fmt.Errorf("--here and --no-here are mutually exclusive")
			}

			name := ""
			if len(args) == 1 {
				name = args[0]
			}

			opts := createOptions{
				agent:  agent,
				secret: secret,
				here:   tristateUnset,
			}
			if here {
				opts.here = tristateYes
			} else if noHere {
				opts.here = tristateNo
			}

			return runCreateWizard(cmd, name, opts)
		},
	}

	cmd.Flags().StringVar(&agent, "agent", "", "Agent name (skips agent prompt)")
	cmd.Flags().StringVar(&secret, "secret-store", "", "Secret store name (skips secret prompt)")
	cmd.Flags().BoolVar(&here, "here", false, "Bind this folder as a match rule")
	cmd.Flags().BoolVar(&noHere, "no-here", false, "Skip cwd binding")
	return cmd
}
```

Replace the stub `runCreateWizard` with the real implementation:

```go
// runCreateWizard creates a new context, optionally binding cwd. The
// wizard fills in any missing fields by prompting in TTY mode, or by
// returning a helpful error in non-TTY mode.
func runCreateWizard(cmd *cobra.Command, prefilledName string, opts createOptions) error {
	out := cmd.OutOrStdout()
	reader := bufio.NewReader(os.Stdin)
	tty := isStdinTTY()

	env, err := cmdEnv(cmd)
	if err != nil {
		return err
	}
	cwd := env.CWD()
	cfg := env.Config()

	// 1. Name
	name := strings.TrimSpace(prefilledName)
	if name == "" {
		if !tty {
			return fmt.Errorf("a context name is required in non-interactive mode")
		}
		fmt.Fprint(out, "Context name: ")
		raw, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("reading context name: %w", err)
		}
		name = strings.TrimSpace(raw)
		if name == "" {
			return fmt.Errorf("context name cannot be empty")
		}
	}
	if _, exists := cfg.Contexts[name]; exists {
		return fmt.Errorf("context %q already exists", name)
	}

	// 2. Agent
	agentName := opts.agent
	if agentName == "" {
		if detected := singleAgentOnPath(); detected != "" {
			agentName = detected
			fmt.Fprintf(out, "Using agent: %s (auto-detected)\n", agentName)
		} else if tty {
			fmt.Fprint(out, "Agent: ")
			raw, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("reading agent: %w", err)
			}
			agentName = strings.TrimSpace(raw)
		}
	}
	if agentName == "" {
		return fmt.Errorf("no agent provided and none could be auto-detected on PATH (pass --agent)")
	}
	if !launcher.IsKnownAgent(agentName) {
		return fmt.Errorf("unknown agent %q.\nKnown agents: %s",
			agentName, strings.Join(launcher.KnownAgents, ", "))
	}

	// 3. Secret store (optional)
	secretName := strings.TrimSpace(opts.secret)
	if secretName == "" && tty && opts.secret == "" {
		fmt.Fprint(out, "Secret store name (optional, press enter to skip): ")
		raw, _ := reader.ReadString('\n')
		secretName = strings.TrimSpace(raw)
	}

	// 4. Bind cwd?
	bindHere := false
	switch opts.here {
	case tristateYes:
		bindHere = true
	case tristateNo:
		bindHere = false
	case tristateUnset:
		if tty {
			fmt.Fprintf(out, "Bind this folder to %q now? [Y/n]: ", name)
			raw, _ := reader.ReadString('\n')
			ans := strings.ToLower(strings.TrimSpace(raw))
			bindHere = (ans == "" || ans == "y" || ans == "yes")
		}
	}

	// 5. Build and persist.
	if cfg.Agents == nil {
		cfg.Agents = make(map[string]config.AgentDef)
	}
	if cfg.Contexts == nil {
		cfg.Contexts = make(map[string]config.Context)
	}
	if _, ok := cfg.Agents[agentName]; !ok {
		cfg.Agents[agentName] = config.AgentDef{Binary: agentName}
	}

	newCtx := config.Context{Agent: agentName}
	if secretName != "" {
		newCtx.Secret = secretName
	}
	var bindDesc string
	if bindHere {
		rule, desc := autoDetectMatchRule(cwd)
		newCtx.Match = []config.MatchRule{rule}
		bindDesc = desc
	}
	cfg.Contexts[name] = newCtx
	if cfg.DefaultContext == "" {
		cfg.DefaultContext = name
	}

	if err := config.WriteConfig(cfg); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	fmt.Fprintf(out, "Created context %q (agent: %s).\n", name, agentName)
	if bindHere {
		fmt.Fprintf(out, "Bound this folder (matched %s).\n", bindDesc)
	}
	return nil
}

// singleAgentOnPath returns the single supported agent binary present
// on PATH. If zero or multiple are found, returns "".
func singleAgentOnPath() string {
	scan := launcher.ScanAgents(exec.LookPath)
	if len(scan.Found) == 1 {
		for name := range scan.Found {
			return name
		}
	}
	return ""
}
```

Add the import `"os/exec"` to `cmd/aide/context.go` (used by `singleAgentOnPath`).

Register the new command and delete the old:

```go
// In contextCmd(), replace the stub registration:
cmd.AddCommand(contextCreateCmd())  // alongside the contextBindCmd from Task 2
```

Delete `contextAddCmd` from `cmd/aide/context.go`.

Update the stub-deletion task: the file must no longer contain `contextAddCmd` or `contextAddMatchCmd`. Run:

```bash
grep -n "contextAddCmd\|contextAddMatchCmd" cmd/aide/
```

Expected: zero matches.

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./cmd/aide/ -run TestContextCreate -v
```
Expected: all PASS.

- [ ] **Step 5: Run the full suite**

```bash
go build ./... && go test ./...
```
Expected: green. Fix any breakage from other tests that referenced `contextAddCmd` (delete or migrate them).

- [ ] **Step 6: Commit**

```bash
git add cmd/aide/context.go cmd/aide/context_create_test.go
__GIT_COMMIT_PLUGIN__=1 git commit -m "Add aide context create, replacing add"
```

---

## Task 4: Empty-state helper (logic only, no launcher integration yet)

Pure logic so we can unit-test it deterministically with fake reader/writer and a fake `EmptyStateActions` before wiring into `Launch`.

**Files:**
- Create: `internal/launcher/empty_state.go`
- Create: `internal/launcher/empty_state_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/launcher/empty_state_test.go`:

```go
// internal/launcher/empty_state_test.go
package launcher

import (
	"bytes"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/config"
)

// fakeActions records which method was called and returns the
// configured error.
type fakeActions struct {
	bindCalled, createCalled bool
	bindErr, createErr       error
}

func (f *fakeActions) Bind(_ string) error {
	f.bindCalled = true
	return f.bindErr
}

func (f *fakeActions) Create(_ string) error {
	f.createCalled = true
	return f.createErr
}

func TestHandleEmptyState_NonTTY_Errors_WithFourHints(t *testing.T) {
	cfg := &config.Config{Contexts: map[string]config.Context{
		"work": {Agent: "claude"},
	}}
	var out bytes.Buffer
	_, err := handleEmptyState(cfg, "/some/cwd", strings.NewReader(""), &out, false, &fakeActions{})
	if err == nil {
		t.Fatal("expected error in non-TTY mode")
	}
	for _, want := range []string{
		"aide context bind",
		"aide context create",
		"aide use",
		"aide context set-default",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("non-TTY error must mention %q, got: %v", want, err)
		}
	}
}

func TestHandleEmptyState_TTY_Cancel_ReturnsCancelled(t *testing.T) {
	cfg := &config.Config{Contexts: map[string]config.Context{"work": {Agent: "claude"}}}
	var out bytes.Buffer
	_, err := handleEmptyState(cfg, "/cwd", strings.NewReader("c\n"), &out, true, &fakeActions{})
	if err != ErrEmptyStateCancelled {
		t.Errorf("expected ErrEmptyStateCancelled, got: %v", err)
	}
}

func TestHandleEmptyState_TTY_LaunchOnce_ReturnsContextWithoutPersisting(t *testing.T) {
	cfg := &config.Config{Contexts: map[string]config.Context{"work": {Agent: "claude"}}}
	var out bytes.Buffer
	// Choice [3], then pick the only context (default [1]).
	rc, err := handleEmptyState(cfg, "/cwd", strings.NewReader("3\n\n"), &out, true, &fakeActions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rc == nil || rc.Name != "work" {
		t.Errorf("expected work context, got: %+v", rc)
	}
	if rc.MatchReason != "empty-state launch-once" {
		t.Errorf("launch-once should be marked in MatchReason, got: %q", rc.MatchReason)
	}
}

func TestHandleEmptyState_TTY_BindChoice_DispatchesAction(t *testing.T) {
	cfg := &config.Config{Contexts: map[string]config.Context{"work": {Agent: "claude"}}}
	var out bytes.Buffer
	actions := &fakeActions{}
	// Choice [1], then pick the only context.
	_, err := handleEmptyState(cfg, "/cwd", strings.NewReader("1\n\n"), &out, true, actions)
	if err != ErrEmptyStateActionRanReloadNeeded {
		t.Errorf("choice [1] should signal reload-needed, got: %v", err)
	}
	if !actions.bindCalled {
		t.Errorf("Bind action should have been invoked")
	}
}

func TestHandleEmptyState_TTY_CreateChoice_DispatchesAction(t *testing.T) {
	cfg := &config.Config{Contexts: map[string]config.Context{}}
	var out bytes.Buffer
	actions := &fakeActions{}
	// Choice [2], wizard runs through fake.
	_, err := handleEmptyState(cfg, "/cwd", strings.NewReader("2\n"), &out, true, actions)
	if err != ErrEmptyStateActionRanReloadNeeded {
		t.Errorf("choice [2] should signal reload-needed, got: %v", err)
	}
	if !actions.createCalled {
		t.Errorf("Create action should have been invoked")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/launcher/ -run TestHandleEmptyState -v
```
Expected: FAIL — `undefined: handleEmptyState`.

- [ ] **Step 3: Implement empty_state.go**

Create `internal/launcher/empty_state.go`:

```go
// internal/launcher/empty_state.go
package launcher

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/jskswamy/aide/internal/config"
	aidectx "github.com/jskswamy/aide/internal/context"
)

// ErrEmptyStateCancelled is returned when the user picks [c] at the
// empty-state prompt.
var ErrEmptyStateCancelled = errors.New("aide: cancelled at empty-state prompt")

// ErrEmptyStateActionRanReloadNeeded signals that a [1]/[2] action ran
// successfully and the caller should reload config and re-resolve
// context before continuing the launch. The caller (launcher.Launch)
// handles this transparently; users never see this error.
var ErrEmptyStateActionRanReloadNeeded = errors.New("empty-state action completed; caller should reload config")

// EmptyStateActions is the contract the launcher needs from the cmd
// layer to dispatch [1] / [2] choices. The cmd layer implements this
// using the same code paths as the standalone `bind` / `create`
// commands so behavior matches across surfaces.
type EmptyStateActions interface {
	// Bind attaches cwd to an existing context. The provided name is
	// the user's pick from the empty-state picker; an empty name means
	// the action should run its own picker (e.g. when no context list
	// was shown).
	Bind(name string) error
	// Create runs the create wizard. The provided name pre-fills the
	// wizard's first question; an empty string means "ask".
	Create(name string) error
}

// handleEmptyState runs the interactive prompt (in TTY mode) or returns
// a hard error (in non-TTY mode) when the launcher cannot resolve a
// context for the current folder.
//
// Returns:
//   - In non-TTY mode: error with copy-pasteable next-command hints.
//   - Choice [1]/[2]: invokes actions; returns ErrEmptyStateActionRanReloadNeeded
//     so the caller knows to reload config and re-resolve.
//   - Choice [3]: returns a *ResolvedContext WITHOUT persisting anything.
//   - Choice [c]: returns ErrEmptyStateCancelled.
func handleEmptyState(
	cfg *config.Config,
	cwd string,
	in io.Reader,
	out io.Writer,
	tty bool,
	actions EmptyStateActions,
) (*aidectx.ResolvedContext, error) {
	if !tty {
		return nil, fmt.Errorf(
			"aide: no context matches this folder, and no default_context is configured.\n\n" +
				"To proceed, run one of:\n" +
				"  aide context bind <name>            # attach this folder to existing context\n" +
				"  aide context create [name]          # create a new context for this folder\n" +
				"  aide use <name> -- <agent-args>     # launch once without persisting\n" +
				"  aide context set-default <name>     # use a fallback for unmatched folders",
		)
	}

	reader := bufio.NewReader(in)

	fmt.Fprintln(out, "aide: no context matches this folder.")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "What do you want to do?")
	fmt.Fprintln(out, "  [1] Bind this folder to an existing context")
	fmt.Fprintln(out, "  [2] Create a new context for this folder")
	fmt.Fprintln(out, "  [3] Launch once with an existing context (don't save)")
	fmt.Fprintln(out, "  [c] Cancel")
	fmt.Fprintln(out, "")
	fmt.Fprint(out, "Choose [1]: ")

	raw, _ := reader.ReadString('\n')
	choice := strings.ToLower(strings.TrimSpace(raw))
	if choice == "" {
		choice = "1"
	}

	switch choice {
	case "1":
		picked, err := pickContextForLaunchOnce(cfg, reader, out)
		if err != nil {
			return nil, err
		}
		if err := actions.Bind(picked); err != nil {
			return nil, err
		}
		return nil, ErrEmptyStateActionRanReloadNeeded
	case "2":
		if err := actions.Create(""); err != nil {
			return nil, err
		}
		return nil, ErrEmptyStateActionRanReloadNeeded
	case "3":
		picked, err := pickContextForLaunchOnce(cfg, reader, out)
		if err != nil {
			return nil, err
		}
		ctx := cfg.Contexts[picked]
		return &aidectx.ResolvedContext{
			Name:        picked,
			MatchReason: "empty-state launch-once",
			Context:     ctx,
		}, nil
	case "c", "cancel":
		return nil, ErrEmptyStateCancelled
	default:
		return nil, fmt.Errorf("invalid choice: %q", choice)
	}
}

// pickContextForLaunchOnce shows a numbered menu of existing contexts
// and returns the chosen name.
func pickContextForLaunchOnce(cfg *config.Config, reader *bufio.Reader, out io.Writer) (string, error) {
	if len(cfg.Contexts) == 0 {
		return "", fmt.Errorf("no contexts configured. Run: aide context create <name>")
	}
	names := make([]string, 0, len(cfg.Contexts))
	for n := range cfg.Contexts {
		names = append(names, n)
	}
	sort.Strings(names)

	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Existing contexts:")
	for i, n := range names {
		fmt.Fprintf(out, "  [%d] %s\n", i+1, n)
	}
	fmt.Fprint(out, "Choose [1]: ")
	raw, _ := reader.ReadString('\n')
	input := strings.TrimSpace(raw)
	choice := 1
	if input != "" {
		n, err := strconv.Atoi(input)
		if err != nil || n < 1 || n > len(names) {
			return "", fmt.Errorf("invalid selection: %q", input)
		}
		choice = n
	}
	return names[choice-1], nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/launcher/ -run TestHandleEmptyState -v
```
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/launcher/empty_state.go internal/launcher/empty_state_test.go
__GIT_COMMIT_PLUGIN__=1 git commit -m "Add empty-state helper for launcher (launch-once and non-TTY hints)"
```

---

## Task 5: Wire empty-state into `Launch` with cmd-layer actions

Hook the helper into `Launch`, add the `EmptyStateActions` field on `Launcher`, and write the cmd-layer adapter that wires `Bind` and `Create` to the same code paths as the standalone commands.

**Files:**
- Modify: `internal/launcher/launcher.go` — add `EmptyStateActions` field; dispatch in `Launch`; reload + re-resolve.
- Create: `cmd/aide/empty_state_actions.go` — the adapter.
- Modify: `cmd/aide/main.go` — pass the adapter when constructing `Launcher`.

- [ ] **Step 1: Add the field to Launcher and modify Launch**

In `internal/launcher/launcher.go`, add to the `Launcher` struct:

```go
type Launcher struct {
    // ... existing fields ...

    // EmptyStateActions is invoked when context resolution fails AND
    // no default_context is configured. May be nil in tests; nil
    // disables the interactive empty-state prompt and falls back to
    // the legacy hard error.
    EmptyStateActions EmptyStateActions
}
```

Replace the resolve block in `Launch` (around line 115):

```go
rc, err := aidectx.Resolve(cfg, cwd, remoteURL)
if err != nil {
    return fmt.Errorf("resolving context: %w", err)
}
```

with:

```go
rc, err := aidectx.Resolve(cfg, cwd, remoteURL)
if err != nil {
    if cfg.DefaultContext != "" || l.EmptyStateActions == nil {
        return fmt.Errorf("resolving context: %w", err)
    }
    rc, cfg, err = l.handleEmptyStateLaunch(cfg, cwd, remoteURL)
    if err != nil {
        return err
    }
}
```

Add the helper at the bottom of `internal/launcher/launcher.go`:

```go
// handleEmptyStateLaunch invokes the empty-state prompt and returns
// either a one-shot ResolvedContext (choice [3]) or a freshly
// re-resolved one after [1]/[2] mutated config. The returned config
// reflects the post-action state so the caller doesn't need to reload.
func (l *Launcher) handleEmptyStateLaunch(
	cfg *config.Config,
	cwd string,
	remoteURL string,
) (*aidectx.ResolvedContext, *config.Config, error) {
	tty := isStdinTTY()
	rc, err := handleEmptyState(cfg, cwd, os.Stdin, os.Stderr, tty, l.EmptyStateActions)
	if err == nil {
		return rc, cfg, nil
	}
	if errors.Is(err, ErrEmptyStateCancelled) {
		return nil, cfg, err
	}
	if errors.Is(err, ErrEmptyStateActionRanReloadNeeded) {
		// [1] or [2] persisted a new context. Reload config and re-resolve.
		newCfg, lerr := config.Load(l.configDir(), cwd)
		if lerr != nil {
			return nil, cfg, fmt.Errorf("reloading config after empty-state action: %w", lerr)
		}
		newRc, rerr := aidectx.Resolve(newCfg, cwd, remoteURL)
		if rerr != nil {
			return nil, cfg, fmt.Errorf("resolving context after empty-state action: %w", rerr)
		}
		return newRc, newCfg, nil
	}
	return nil, cfg, err
}

func isStdinTTY() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}
```

Add imports `"errors"`, `"os"`, and `"golang.org/x/term"` to launcher.go if not already present.

- [ ] **Step 2: Write the cmd-layer adapter**

Create `cmd/aide/empty_state_actions.go`:

```go
// cmd/aide/empty_state_actions.go
package main

import (
	"github.com/spf13/cobra"

	"github.com/jskswamy/aide/internal/launcher"
)

// emptyStateAdapter implements launcher.EmptyStateActions by routing
// to the same code paths as the standalone `aide context bind` /
// `aide context create` commands, ensuring behavior matches across
// surfaces.
type emptyStateAdapter struct {
	cmd *cobra.Command
}

func newEmptyStateAdapter(cmd *cobra.Command) launcher.EmptyStateActions {
	return &emptyStateAdapter{cmd: cmd}
}

func (a *emptyStateAdapter) Bind(name string) error {
	bind := contextBindCmd()
	bind.SetOut(a.cmd.OutOrStdout())
	bind.SetErr(a.cmd.ErrOrStderr())
	if name != "" {
		bind.SetArgs([]string{name})
	} else {
		bind.SetArgs(nil)
	}
	return bind.Execute()
}

func (a *emptyStateAdapter) Create(name string) error {
	return runCreateWizard(a.cmd, name, createOptions{})
}
```

- [ ] **Step 3: Wire the adapter in main.go**

In `cmd/aide/main.go`, find the `&launcher.Launcher{ ... }` construction and add `EmptyStateActions: newEmptyStateAdapter(rootCmd)` to the struct literal.

- [ ] **Step 4: Run tests**

```bash
go test ./internal/launcher/... ./cmd/aide/...
```
Expected: green. The new empty-state tests from Task 4 already cover the dispatch behavior with a fake actions implementation.

- [ ] **Step 5: Smoke-test manually**

```bash
go build -o /tmp/aide-new ./cmd/aide
mkdir /tmp/aide-empty-smoke && cd /tmp/aide-empty-smoke
/tmp/aide-new </dev/null
```

Expected (non-TTY): the four-line hint error.

```bash
# In a TTY:
/tmp/aide-new
```

Expected: the prompt. Pick `c` to cancel.

- [ ] **Step 6: Commit**

```bash
git add internal/launcher/launcher.go internal/launcher/empty_state.go internal/launcher/empty_state_test.go cmd/aide/empty_state_actions.go cmd/aide/main.go
__GIT_COMMIT_PLUGIN__=1 git commit -m "Wire empty-state prompt into launcher with cmd-layer actions"
```

---

## Task 6: Sweep status.go help and docs

Old verb references in user-facing strings. The `examples_test.go` guard from the env-set work catches these automatically — failures here mean the help text drifted.

**Files:**
- Modify: `cmd/aide/status.go` — drop the `aide use --context myproject # Add CWD match...` example line; drop the same line at status.go:175.
- Modify: `docs/cli-reference.md` — add bind/create rows; remove add/add-match.
- Modify: `docs/environment.md` — update if it references add-match.
- Create: `docs/getting-started.md` — first-run walkthrough.

- [ ] **Step 1: Update status.go**

Find and remove the line in the `useCmd` `Long:` block (status.go:341):

```go
  aide use --context myproject          # Add CWD match to existing context
```

Replace it with the new shape (preferred path is now `aide context bind`):

```go
  aide use claude --secret personal     # Also set secret on a new binding
```

(or simply remove the line if the example is redundant). Also update status.go:175 in the same way (remove the dated phrasing about "Bind a folder to an agent" if it now duplicates `aide context bind`).

Run the examples-as-tests guard: `go test ./cmd/aide/ -run TestHelpExamplesParse -v`. Expected: green.

- [ ] **Step 2: Update docs/cli-reference.md**

Add a new section under `aide context`:

```markdown
### `aide context bind <name>`

Attach the current folder to an existing context.

| Flag | Description |
| --- | --- |
| `--path` | Force exact folder path match. |
| `--remote` | Force git remote match (errors if not a git repo). |

Examples:

    aide context bind work               # auto-detect: git remote if repo, else folder path
    aide context bind work --path        # force exact folder path match
    aide context bind                    # interactive picker over existing contexts

### `aide context create [name]`

Create a new context.

| Flag | Description |
| --- | --- |
| `--agent <name>` | Set the agent without prompting. |
| `--secret-store <name>` | Bind a secret store at create time. |
| `--here` | Bind cwd as a match rule (auto-detect). |
| `--no-here` | Skip cwd binding entirely. |

Examples:

    aide context create work --agent claude --secret-store firmus --here
    aide context create work --agent claude --no-here
```

Remove any rows or prose that reference `aide context add` or `aide context add-match`.

- [ ] **Step 3: Update docs/environment.md**

Search for `add-match` and replace with `aide context bind <name>` in any explanatory prose. If there's no reference, skip.

```bash
grep -n "add-match\|context add" docs/environment.md
```

Update each match to use the new vocabulary.

- [ ] **Step 4: Create docs/getting-started.md**

Write a short walkthrough showing the first-run / empty-state experience:

```markdown
# Getting started with aide

The first time you run `aide` in a new folder, you'll see:

    aide: no context matches this folder.

    What do you want to do?
      [1] Bind this folder to an existing context
      [2] Create a new context for this folder
      [3] Launch once with an existing context (don't save)
      [c] Cancel

Pick `[2]` to create a new context — aide walks you through naming
it, picking an agent (auto-detected if you have only one supported
agent on PATH), optionally binding a secret store, and finally
attaching the current folder.

If you already have a context (say `work`) and want this folder to
also resolve to it, pick `[1]`, or run directly:

    aide context bind work

By default `bind` matches by git remote URL when the folder is a git
repo with an `origin` remote — so the same context resolves
correctly for any worktree or fresh checkout of the same repo.

For non-interactive use (CI, scripts):

    aide context create work --agent claude --secret-store firmus --no-here
    aide context bind work --path
```

- [ ] **Step 5: Verify all docs pass**

```bash
grep -rn "add-match\|context add\b" docs/ README.md 2>/dev/null
```

Expected: zero matches outside `docs/superpowers/specs/` and `docs/superpowers/plans/` (those describe the redesign itself; they're allowed to mention the old verbs).

- [ ] **Step 6: Commit**

```bash
git add cmd/aide/status.go docs/cli-reference.md docs/environment.md docs/getting-started.md
__GIT_COMMIT_PLUGIN__=1 git commit -m "Update docs and status.go for context UX redesign"
```

---

## Task 7: Final verification

- [ ] **Step 1: Full test suite**

```bash
go test ./...
```
Expected: green.

- [ ] **Step 2: Build**

```bash
go build ./...
```
Expected: success.

- [ ] **Step 3: Vet**

```bash
go vet ./...
```
Expected: clean.

- [ ] **Step 4: Manual smoke tests**

```bash
go build -o /tmp/aide-new ./cmd/aide
mkdir /tmp/aide-ux-smoke && cd /tmp/aide-ux-smoke

# bind: missing context, non-TTY (pipe in /dev/null)
/tmp/aide-new context bind ghost </dev/null
# Expected: error pointing at `aide context create ghost`

# create: scripted no-here
/tmp/aide-new context create test --agent claude --no-here
# Expected: success message; config.yaml written

# bind: existing context, with current folder bound
/tmp/aide-new context bind test
# Expected: success, "matched by path /tmp/aide-ux-smoke"

# Empty-state in a fresh folder
mkdir /tmp/aide-fresh && cd /tmp/aide-fresh
/tmp/aide-new </dev/null
# Expected: non-TTY hard error listing the four hints
```

- [ ] **Step 5: No extra commit needed** — Task 6 was the last commit. Push at session end per the SessionStart protocol.
