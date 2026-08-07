# Cap List Status Column Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `STATUS` column to `aide cap list` showing whether each capability is enabled, disabled, or suggested for the current project, and fix `aide cap audit`'s pre-existing bug where it ignores project-level capability overrides.

**Architecture:** A new pure helper, `resolveEffectiveCapabilities(cfg, cwd, contextName)`, becomes the single source of truth for "what capabilities are active here" — used by both `capListCmd` (new `STATUS` column) and `capAuditCmd` (bug fix). Per-row state for `cap list` combines that helper's output with `capability.DetectProject` (already used by the real launcher for marker-based suggestions).

**Tech Stack:** Go, `cobra` CLI framework, table-driven `testing` package.

## Global Constraints

- `--context <name>` skips project-override merging entirely (matches the existing `sandboxTestCmd --context` precedent) — only the no-`--context` (default) path merges `.aide.yaml` project overrides.
- `capability.DetectProject` only scans built-in capabilities' markers (existing behavior, unchanged) — user-defined capabilities never show `suggested`.
- `cap list` must keep working with zero config (no crash, no error) — this is existing behavior (`capCheckCmd`/`capSuggestForPathCmd` already document this contract) that must not regress.
- Column order is `NAME | STATUS | SOURCE | DESCRIPTION` — `STATUS` immediately after `NAME`, before `SOURCE`.
- Status values are exactly: `enabled`, `disabled`, `suggested`, `-` (dash for none-apply). Precedence: `enabled` > `disabled` > `suggested`.

---

## File Structure

| File | Responsibility |
|---|---|
| `cmd/aide/commands.go` | Add `resolveEffectiveCapabilities` helper, alongside the existing `resolveContextForMutation`/`resolveProjectOverrideForMutation`. |
| `cmd/aide/commands_test.go` | New file. Unit tests for `resolveEffectiveCapabilities`. |
| `cmd/aide/cap.go` | Modify `capAuditCmd` to use the new helper (bug fix); modify `capListCmd` to add `--context` flag, `STATUS` column, and a small `capListStatus` helper function. |
| `cmd/aide/cap_test.go` | Add tests for `capAuditCmd`'s override-merge fix and `capListCmd`'s new `STATUS` column/`--context` flag. |

---

### Task 1: `resolveEffectiveCapabilities` helper

**Files:**
- Modify: `cmd/aide/commands.go` (insert after `resolveContextForMutation`, which ends at line 151, before the comment block for `resolveProjectOverrideForMutation` at line 153)
- Create: `cmd/aide/commands_test.go`

**Interfaces:**
- Produces: `func resolveEffectiveCapabilities(cfg *config.Config, cwd, contextName string) (name string, caps []string, err error)` — later tasks call this exact signature.

- [ ] **Step 1: Write the failing tests**

Create `cmd/aide/commands_test.go`:

```go
// cmd/aide/commands_test.go
package main

import (
	"testing"

	"github.com/jskswamy/aide/internal/config"
)

func TestResolveEffectiveCapabilities_DefaultContextNoOverride(t *testing.T) {
	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"work": {
				Capabilities: []string{"go", "docker"},
			},
		},
		DefaultContext: "work",
	}

	name, caps, err := resolveEffectiveCapabilities(cfg, t.TempDir(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "work" {
		t.Errorf("expected context name %q, got %q", "work", name)
	}
	if len(caps) != 2 || caps[0] != "go" || caps[1] != "docker" {
		t.Errorf("expected [go docker], got %v", caps)
	}
}

func TestResolveEffectiveCapabilities_MergesProjectOverride(t *testing.T) {
	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"work": {
				Capabilities: []string{"go", "docker"},
			},
		},
		DefaultContext: "work",
		ProjectOverride: &config.ProjectOverride{
			Capabilities:         []string{"clipboard"},
			DisabledCapabilities: []string{"docker"},
		},
	}

	name, caps, err := resolveEffectiveCapabilities(cfg, t.TempDir(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "work" {
		t.Errorf("expected context name %q, got %q", "work", name)
	}
	want := map[string]bool{"go": true, "clipboard": true}
	if len(caps) != len(want) {
		t.Fatalf("expected 2 capabilities, got %v", caps)
	}
	for _, c := range caps {
		if !want[c] {
			t.Errorf("unexpected capability %q in %v", c, caps)
		}
	}
}

func TestResolveEffectiveCapabilities_ExplicitContextSkipsOverride(t *testing.T) {
	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"other": {
				Capabilities: []string{"go"},
			},
		},
		DefaultContext: "other",
		ProjectOverride: &config.ProjectOverride{
			Capabilities: []string{"clipboard"},
		},
	}

	name, caps, err := resolveEffectiveCapabilities(cfg, t.TempDir(), "other")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "other" {
		t.Errorf("expected context name %q, got %q", "other", name)
	}
	if len(caps) != 1 || caps[0] != "go" {
		t.Errorf("expected [go] (no project override merge for explicit --context), got %v", caps)
	}
}

func TestResolveEffectiveCapabilities_UnknownContextErrors(t *testing.T) {
	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"work": {},
		},
	}

	_, _, err := resolveEffectiveCapabilities(cfg, t.TempDir(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown context, got nil")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/aide/... -run TestResolveEffectiveCapabilities -v`
Expected: FAIL — `resolveEffectiveCapabilities` undefined.

- [ ] **Step 3: Implement the helper**

In `cmd/aide/commands.go`, insert after `resolveContextForMutation` (after its closing `}` at line 151):

```go
// resolveEffectiveCapabilities returns the resolved context name and its
// effective (fully resolved) capabilities list for cwd.
//
// When contextName is "", it resolves the applicable context via
// aidectx.Resolve, which merges any .aide.yaml project override on top
// of the matched context — this answers "what's actually active here."
//
// When contextName is non-empty, it looks up that context by name
// directly, with no project-override merge, matching the existing
// precedent in sandboxTestCmd's --context handling: project overrides
// are tied to cwd, not to an arbitrary named context being inspected.
func resolveEffectiveCapabilities(cfg *config.Config, cwd, contextName string) (name string, caps []string, err error) {
	if contextName == "" {
		remoteURL := aidectx.DetectRemote(cwd, "origin")
		rc, err := aidectx.Resolve(cfg, cwd, remoteURL)
		if err != nil {
			return "", nil, fmt.Errorf("resolving context: %w", err)
		}
		return rc.Name, rc.Context.Capabilities, nil
	}
	ctx, ok := cfg.Contexts[contextName]
	if !ok {
		return "", nil, fmt.Errorf("context %q not found", contextName)
	}
	return contextName, ctx.Capabilities, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/aide/... -run TestResolveEffectiveCapabilities -v`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add cmd/aide/commands.go cmd/aide/commands_test.go
git commit -m "feat: add resolveEffectiveCapabilities helper"
```

---

### Task 2: Fix `cap audit`'s project-override merge bug

**Files:**
- Modify: `cmd/aide/cap.go:776-808` (`capAuditCmd`)
- Modify: `cmd/aide/cap_test.go` (add tests; no existing `capAuditCmd` tests exist today)

**Interfaces:**
- Consumes: `resolveEffectiveCapabilities(cfg *config.Config, cwd, contextName string) (string, []string, error)` from Task 1.

- [ ] **Step 1: Write the failing test**

Add to `cmd/aide/cap_test.go`:

```go
func TestCapAudit_ReflectsProjectOverrideDisabledCapability(t *testing.T) {
	dir := isolatedConfigDir(t)

	configYAML := `default_context: work
contexts:
  work:
    agent: claude
    capabilities: [ssh]
`
	if err := os.WriteFile(filepath.Join(dir, "xdg", "aide", "config.yaml"), []byte(configYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".aide.yaml"), []byte("disabled_capabilities: [ssh]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := runCapCmdInPlace(t, "audit")

	if strings.Contains(out, "ssh") {
		t.Errorf("expected ssh to be excluded by project override disabled_capabilities, got:\n%s", out)
	}
	if !strings.Contains(out, `Context "work" has no capabilities enabled.`) {
		t.Errorf("expected 'no capabilities enabled' message once ssh is disabled, got:\n%s", out)
	}
}

// runCapCmdInPlace builds a fresh `cap` cobra command and runs it in the
// CURRENT working directory (unlike runCapCmd, which chdirs to a fresh
// tempdir). Callers must have already set up an isolated cwd/config
// (e.g. via isolatedConfigDir) before calling this.
func runCapCmdInPlace(t *testing.T, args ...string) string {
	t.Helper()
	var buf bytes.Buffer
	cmd := capCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("cap %v: %v\nout: %s", args, err, buf.String())
	}
	return buf.String()
}
```

Note: `isolatedConfigDir` is already defined in `cmd/aide/context_bind_test.go` (same package) — no need to redefine it. `bytes`, `os`, `path/filepath`, `strings` are already imported in `cap_test.go`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/aide/... -run TestCapAudit_ReflectsProjectOverrideDisabledCapability -v`
Expected: FAIL — the current `capAuditCmd` still sees `ssh` in `ctx.Capabilities` (unmerged), so the output contains `ssh` and does NOT contain the "no capabilities enabled" message.

- [ ] **Step 3: Fix `capAuditCmd`**

In `cmd/aide/cap.go`, replace the `capAuditCmd` function body:

```go
func capAuditCmd() *cobra.Command {
	var contextName string
	cmd := &cobra.Command{
		Use:          "audit",
		Short:        "Show resolved capabilities for the current context",
		Long:         `Reads the active context's capabilities and displays the merged sandbox overrides and any warnings.`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()

			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}
			cfg, err := config.Load(config.Dir(), cwd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			ctxName, caps, err := resolveEffectiveCapabilities(cfg, cwd, contextName)
			if err != nil {
				return err
			}

			if len(caps) == 0 {
				fmt.Fprintf(out, "Context %q has no capabilities enabled.\n", ctxName)
				return nil
			}

			userCaps := capability.FromConfigDefs(cfg.Capabilities)
			registry := capability.MergedRegistry(userCaps)

			set, err := capability.ResolveAll(caps, registry, cfg.NeverAllow, cfg.NeverAllowEnv)
			if err != nil {
				return err
			}

			fmt.Fprintf(out, "Context: %s\n\n", ctxName)
			printCapabilityReport(out, set)
			return nil
		},
	}
	cmd.Flags().StringVar(&contextName, "context", "", "target context name")
	return cmd
}
```

This removes the `resolveContextForMutation` call (which returned the unmerged context) in favor of loading `cfg`/`cwd` directly and calling `resolveEffectiveCapabilities`. `os` and `config` are already imported in `cap.go`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/aide/... -run TestCapAudit -v`
Expected: PASS.

- [ ] **Step 5: Run the full cap.go test suite to check for regressions**

Run: `go test ./cmd/aide/... -v 2>&1 | grep -E "^(--- FAIL|FAIL|ok)"`
Expected: `ok` for `github.com/jskswamy/aide/cmd/aide`, no `FAIL` lines.

- [ ] **Step 6: Commit**

```bash
git add cmd/aide/cap.go cmd/aide/cap_test.go
git commit -m "fix: cap audit now reflects project-level capability overrides"
```

---

### Task 3: `STATUS` column and `--context` flag for `cap list`

**Files:**
- Modify: `cmd/aide/cap.go:117-171` (`capListCmd`)
- Modify: `cmd/aide/cap_test.go` (add tests)

**Interfaces:**
- Consumes: `resolveEffectiveCapabilities` (Task 1); `capability.DetectProject(fsys fs.FS) []string` (existing, in `internal/capability/detect.go`).
- Produces: `func capListStatus(name string, enabled, disabled, suggested map[string]bool) string` — not consumed elsewhere, but named here so its behavior is unambiguous to whoever reads this task in isolation.

- [ ] **Step 1: Write the failing tests**

Add to `cmd/aide/cap_test.go`:

```go
func TestCapListStatus_Precedence(t *testing.T) {
	enabled := map[string]bool{"clipboard": true}
	disabled := map[string]bool{"ssh": true}
	suggested := map[string]bool{"go": true}

	cases := []struct {
		name string
		want string
	}{
		{"clipboard", "enabled"},
		{"ssh", "disabled"},
		{"go", "suggested"},
		{"docker", "-"},
	}
	for _, c := range cases {
		got := capListStatus(c.name, enabled, disabled, suggested)
		if got != c.want {
			t.Errorf("capListStatus(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestCapList_ShowsStatusColumn(t *testing.T) {
	dir := isolatedConfigDir(t)

	configYAML := `default_context: work
contexts:
  work:
    agent: claude
    capabilities: [clipboard]
`
	if err := os.WriteFile(filepath.Join(dir, "xdg", "aide", "config.yaml"), []byte(configYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".aide.yaml"), []byte("disabled_capabilities: [docker]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := runCapCmdInPlace(t, "list")

	lines := strings.Split(out, "\n")
	if !strings.HasPrefix(lines[0], "NAME") {
		t.Fatalf("expected header line starting with NAME, got %q", lines[0])
	}
	header := lines[0]
	nameIdx := strings.Index(header, "NAME")
	statusIdx := strings.Index(header, "STATUS")
	sourceIdx := strings.Index(header, "SOURCE")
	if !(nameIdx < statusIdx && statusIdx < sourceIdx) {
		t.Fatalf("expected column order NAME < STATUS < SOURCE, got header: %q", header)
	}

	findRow := func(capName string) string {
		for _, l := range lines {
			fields := strings.Fields(l)
			if len(fields) > 0 && fields[0] == capName {
				return l
			}
		}
		t.Fatalf("no row found for capability %q in output:\n%s", capName, out)
		return ""
	}

	clipboardRow := findRow("clipboard")
	if !strings.Contains(clipboardRow, "enabled") {
		t.Errorf("expected clipboard row to show 'enabled', got: %q", clipboardRow)
	}
	dockerRow := findRow("docker")
	if !strings.Contains(dockerRow, "disabled") {
		t.Errorf("expected docker row to show 'disabled', got: %q", dockerRow)
	}
	goRow := findRow("go")
	if !strings.Contains(goRow, "suggested") {
		t.Errorf("expected go row to show 'suggested' (go.mod present), got: %q", goRow)
	}
	awsRow := findRow("aws")
	fields := strings.Fields(awsRow)
	if len(fields) < 2 || fields[1] != "-" {
		t.Errorf("expected aws row status to be '-', got: %q", awsRow)
	}
}

func TestCapList_ContextFlagSkipsProjectOverride(t *testing.T) {
	dir := isolatedConfigDir(t)

	configYAML := `default_context: other
contexts:
  other:
    agent: claude
    capabilities: []
  work:
    agent: claude
    capabilities: [clipboard]
`
	if err := os.WriteFile(filepath.Join(dir, "xdg", "aide", "config.yaml"), []byte(configYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".aide.yaml"), []byte("disabled_capabilities: [clipboard]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := runCapCmdInPlace(t, "list", "--context", "work")

	lines := strings.Split(out, "\n")
	for _, l := range lines {
		f := strings.Fields(l)
		if len(f) > 0 && f[0] == "clipboard" {
			if len(f) < 2 || f[1] != "enabled" {
				t.Errorf("expected clipboard status 'enabled' for --context work (project override should not apply), got: %q", l)
			}
			return
		}
	}
	t.Fatalf("no clipboard row found in output:\n%s", out)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/aide/... -run 'TestCapListStatus_Precedence|TestCapList_ShowsStatusColumn|TestCapList_ContextFlagSkipsProjectOverride' -v`
Expected: FAIL — `capListStatus` undefined, and/or output has no `STATUS` column yet.

- [ ] **Step 3: Implement the `STATUS` column and `--context` flag**

Replace `capListCmd` in `cmd/aide/cap.go` (lines 117-171):

```go
func capListCmd() *cobra.Command {
	var contextName string
	cmd := &cobra.Command{
		Use:          "list",
		Short:        "List all capabilities (built-in and user-defined)",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			env, err := cmdEnv(cmd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			registry := env.Registry()
			userCaps := capability.FromConfigDefs(env.Config().Capabilities)
			builtins := capability.Builtins()

			_, enabled, err := resolveEffectiveCapabilities(env.Config(), env.CWD(), contextName)
			if err != nil {
				return err
			}
			enabledSet := make(map[string]bool, len(enabled))
			for _, name := range enabled {
				enabledSet[name] = true
			}

			var disabledSet map[string]bool
			if contextName == "" && env.Config().ProjectOverride != nil {
				disabled := env.Config().ProjectOverride.DisabledCapabilities
				disabledSet = make(map[string]bool, len(disabled))
				for _, name := range disabled {
					disabledSet[name] = true
				}
			}

			suggested := capability.DetectProject(os.DirFS(env.CWD()))
			suggestedSet := make(map[string]bool, len(suggested))
			for _, name := range suggested {
				suggestedSet[name] = true
			}

			// Collect and sort names
			names := make([]string, 0, len(registry))
			for name := range registry {
				names = append(names, name)
			}
			sort.Strings(names)

			fmt.Fprintf(out, "%-20s %-12s %-12s %s\n", "NAME", "STATUS", "SOURCE", "DESCRIPTION")
			for _, name := range names {
				entry := registry[name]
				source := "built-in"
				if _, isBuiltin := builtins[name]; !isBuiltin {
					switch {
					case entry.Extends != "":
						source = "extends"
					case len(entry.Combines) > 0:
						source = "combines"
					default:
						source = "custom"
					}
				} else if _, isUser := userCaps[name]; isUser {
					// User override of a built-in
					source = "custom"
				}
				desc := entry.Description
				if len(entry.Variants) > 0 {
					names := make([]string, len(entry.Variants))
					for i, v := range entry.Variants {
						names[i] = v.Name
					}
					desc = fmt.Sprintf("%s (%d variants: %s)", desc, len(entry.Variants), strings.Join(names, ", "))
				}
				status := capListStatus(name, enabledSet, disabledSet, suggestedSet)
				fmt.Fprintf(out, "%-20s %-12s %-12s %s\n", name, status, source, desc)
			}

			return nil
		},
	}
	cmd.Flags().StringVar(&contextName, "context", "", "target context name")
	return cmd
}

// capListStatus reports a capability's activation state for the current
// project: "enabled" if active, "disabled" if explicitly negated by a
// project override, "suggested" if its markers match this project but
// it isn't enabled, or "-" if none apply. Precedence: enabled > disabled
// > suggested (in practice these never overlap, since suggested is only
// computed for names that aren't enabled, and a name can't be both
// enabled and disabled given how the merge in resolveEffectiveCapabilities
// works).
func capListStatus(name string, enabled, disabled, suggested map[string]bool) string {
	switch {
	case enabled[name]:
		return "enabled"
	case disabled[name]:
		return "disabled"
	case suggested[name]:
		return "suggested"
	default:
		return "-"
	}
}
```

`capability`, `os`, `sort`, `strings`, `fmt` are all already imported in `cap.go`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/aide/... -run 'TestCapListStatus_Precedence|TestCapList_ShowsStatusColumn|TestCapList_ContextFlagSkipsProjectOverride|TestCapList_ShowsVariantHintForPython' -v`
Expected: PASS — including the pre-existing `TestCapList_ShowsVariantHintForPython`, unaffected by the column addition.

- [ ] **Step 5: Run the full test suite**

Run: `go build ./... && go test ./... -count=1`
Expected: PASS, all packages green, no regressions.

- [ ] **Step 6: Manual sanity check**

Run: `go run ./cmd/aide cap list` from within the `aide` repo itself (which has a `go.mod`, so `go` should show `suggested` unless already enabled for your context).
Expected: A `STATUS` column appears between `NAME` and `SOURCE`; `go` shows `suggested` or `enabled`; most rows show `-`.

- [ ] **Step 7: Commit**

```bash
git add cmd/aide/cap.go cmd/aide/cap_test.go
git commit -m "feat: add STATUS column and --context flag to cap list"
```

---

## Self-Review Notes

- **Spec coverage:** Shared resolution helper → Task 1. `cap audit` bug fix → Task 2. `STATUS` column, four states, column order, `--context` flag → Task 3. Error handling (no resolvable context, unknown `--context` name, `DetectProject` filesystem-only) → covered by Task 1's `TestResolveEffectiveCapabilities_UnknownContextErrors` (error case) and Task 3's implementation (no-context-resolves case falls through to `resolveEffectiveCapabilities`'s own error return, which `capListCmd` propagates — matching the design's "keep working with zero config" only where `Resolve` itself doesn't hard-error, i.e. when a `default_context` exists; a config with truly no matchable context and no default already errors today via `cmdEnv`/`Resolve`, unchanged by this plan). Out-of-scope items (user-defined capability detection, `cap show` changes, launcher consent flow, JSON output) are untouched by all three tasks.
- **Placeholder scan:** No TBD/TODO; every step has literal, runnable code.
- **Type consistency:** `resolveEffectiveCapabilities(cfg *config.Config, cwd, contextName string) (name string, caps []string, err error)` is defined in Task 1 and consumed with the identical signature in Task 2 (`capAuditCmd`) and Task 3 (`capListCmd`). `capListStatus(name string, enabled, disabled, suggested map[string]bool) string` is defined and tested in Task 3 only, matching its declared "not consumed elsewhere" interface note.

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-08-07-cap-list-status-implementation.md`. Two execution options:

1. **Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration
2. **Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

Which approach?
