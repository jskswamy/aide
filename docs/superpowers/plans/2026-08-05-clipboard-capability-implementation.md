# Clipboard Capability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the ad hoc, Claude-only, read-broken clipboard `mach-lookup` rule with a correct, opt-in, agent-agnostic `clipboard` capability.

**Architecture:** Add a `clipboard` entry to the built-in capability registry (`internal/capability/builtin.go`) whose `Allow` field carries three `mach-lookup` operation expressions (pasteboard + two Launch Services lookups). Delete the old ad hoc rule and its test from the Claude agent module. The existing `Allow` → `Policy.ExtraAllow` → `seatbelt.Context.ExtraAllow` → filesystem-guard rendering pipeline requires no changes — it already turns each `Allow` string into an independent `(allow <op>)` rule.

**Tech Stack:** Go, table-driven `testing` package, macOS Seatbelt (SBPL) via `sandbox-exec`.

## Global Constraints

- Capability is **macOS only** — do not add any Linux-specific handling; `Allow` is already inert on Linux (never read by `internal/sandbox/linux.go`).
- Capability is **opt-in** — no `Markers` field, must not be enabled by default.
- **Single capability, both directions** — no read/write split, no `Direction` field.
- `Allow` entries are **operation expressions**, not full rule text — each string is substituted into `(allow %s)` by `pkg/seatbelt/guards/guard_filesystem.go`. Never write a pre-wrapped `(allow ...)` string into `Allow`.
- This is a breaking change for Claude image-paste users — must be called out in `RELEASE_NOTES.md` under `## Unreleased` (per-feature `####` heading + description + bullets, per existing entries in that file).

---

## File Structure

| File | Responsibility |
|---|---|
| `internal/capability/builtin.go` | Add the `clipboard` capability definition. |
| `internal/capability/builtin_test.go` | New test asserting all three `mach-lookup`/`global-name` entries are present in `Allow`; bump the total-count assertion. |
| `pkg/seatbelt/modules/claude.go` | Delete the ad hoc "osascript clipboard access" rule block (lines 59-68). |
| `pkg/seatbelt/modules/claude_test.go` | Delete `TestClaudeAgent_ClipboardMachService` (now-obsolete: clipboard no longer lives in the Claude module). |
| `docs/capabilities.md` | Add a `clipboard` row to the capability table (Developer Tools section) so `docs/capabilities.md` stays in sync with the registry. |
| `RELEASE_NOTES.md` | Add an `## Unreleased` entry describing the breaking change and bug fix. |

---

### Task 1: Add the `clipboard` built-in capability

**Files:**
- Modify: `internal/capability/builtin.go:253-259` (insert new entry between `"gpg"` and the `// Network` comment block)
- Test: `internal/capability/builtin_test.go`

**Interfaces:**
- Consumes: existing `Capability` struct (`internal/capability/capability.go:15-44`) — no struct changes.
- Produces: `Builtins()["clipboard"]` with `Name`, `Description`, `Allow` populated; consumed later by `internal/sandbox/policy.go:128-130` (`cfg.Allow` → `policy.ExtraAllow`) with no code change needed there.

- [ ] **Step 1: Write the failing test**

Add to `internal/capability/builtin_test.go` (append at end of file):

```go
func TestBuiltin_Clipboard_Exists(t *testing.T) {
	clip, ok := Builtins()["clipboard"]
	if !ok {
		t.Fatal("missing built-in capability 'clipboard'")
	}
	if clip.Description == "" {
		t.Error("expected non-empty description")
	}
}

func TestBuiltin_Clipboard_AllowsAllThreeGlobalNames(t *testing.T) {
	clip := Builtins()["clipboard"]
	want := []string{
		`mach-lookup (global-name "com.apple.pasteboard.1")`,
		`mach-lookup (global-name "com.apple.lsd.mapdb")`,
		`mach-lookup (global-name "com.apple.lsd.modifydb")`,
	}
	for _, w := range want {
		found := false
		for _, got := range clip.Allow {
			if got == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected Allow to contain %q, got %v", w, clip.Allow)
		}
	}
}

func TestBuiltin_Clipboard_NoMarkers(t *testing.T) {
	clip := Builtins()["clipboard"]
	if len(clip.Markers) != 0 {
		t.Errorf("clipboard must be opt-in only (no markers), got %v", clip.Markers)
	}
}
```

Also update the existing count assertion in the same file (it currently expects 21 — adding `clipboard` makes 22):

```go
func TestBuiltins_Count(t *testing.T) {
	if len(Builtins()) != 22 {
		t.Errorf("expected 22 built-in capabilities, got %d", len(Builtins()))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/capability/... -run 'TestBuiltin_Clipboard|TestBuiltins_Count' -v`
Expected: `TestBuiltin_Clipboard_Exists` and `TestBuiltin_Clipboard_AllowsAllThreeGlobalNames` FAIL with a nil-map/missing-key panic or empty-Allow mismatch; `TestBuiltins_Count` FAILs with "expected 22, got 21".

- [ ] **Step 3: Add the capability**

In `internal/capability/builtin.go`, insert after the `"gpg"` entry (currently ending at line 252) and before the `// Network` comment (currently line 254):

```go
		"gpg": {
			Name:        "gpg",
			Description: "GPG keys and signing",
			Writable:    []string{"~/.gnupg"},
			EnvAllow:    []string{"GNUPGHOME"},
		},

		// Clipboard (macOS only — Allow rules are inert on Linux)
		"clipboard": {
			Name:        "clipboard",
			Description: "Read/write access to the system clipboard (pasteboard)",
			Allow: []string{
				`mach-lookup (global-name "com.apple.pasteboard.1")`,
				`mach-lookup (global-name "com.apple.lsd.mapdb")`,
				`mach-lookup (global-name "com.apple.lsd.modifydb")`,
			},
		},

		// Network
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/capability/... -v`
Expected: PASS, including all other existing tests in the package (no regressions).

- [ ] **Step 5: Commit**

```bash
git add internal/capability/builtin.go internal/capability/builtin_test.go
git commit -m "feat: add clipboard capability"
```

---

### Task 2: Remove the ad hoc clipboard rule from the Claude module

**Files:**
- Modify: `pkg/seatbelt/modules/claude.go:59-68`
- Modify: `pkg/seatbelt/modules/claude_test.go:97-104` (delete `TestClaudeAgent_ClipboardMachService`)

**Interfaces:**
- Consumes: nothing new.
- Produces: `claudeAgentModule.Rules()` output with the clipboard block removed; no other module behavior changes.

- [ ] **Step 1: Remove the obsolete test**

Delete this test from `pkg/seatbelt/modules/claude_test.go` (lines 97-104):

```go
func TestClaudeAgent_ClipboardMachService(t *testing.T) {
	ctx := &seatbelt.Context{HomeDir: "/home/user"}
	result := ClaudeAgent().Rules(ctx)
	got := rulesToString(result.Rules)
	if !strings.Contains(got, "com.apple.pasteboard.1") {
		t.Error("expected com.apple.pasteboard.1 in Claude module rules for clipboard image paste")
	}
}
```

- [ ] **Step 2: Run the Claude module tests to confirm the file still compiles and other tests pass**

Run: `go test ./pkg/seatbelt/modules/... -v`
Expected: PASS (the deleted test no longer runs; `TestClaudeAgent_Name`, `TestClaudeAgent_EnvOverride`, `TestClaudeAgent_DefaultConfigDirs`, `TestClaudeAgent_NonConfigPathsAlwaysPresent` all still pass).

- [ ] **Step 3: Remove the ad hoc rule from the module**

In `pkg/seatbelt/modules/claude.go`, delete lines 59-68:

```go
	// osascript reads clipboard images via the per-user pasteboard server.
	// com.apple.pasteboard.1 is the mach service for slot 1 (the standard
	// single-user slot). Without it the sandbox blocks the clipboard read
	// and Claude Code reports "clipboard empty" on Ctrl+V image paste.
	rules = append(rules,
		seatbelt.SectionAllow("osascript clipboard access"),
		seatbelt.AllowRule(`(allow mach-lookup
    (global-name "com.apple.pasteboard.1")
)`),
	)

```

so that the function body goes directly from the closing `)` of the `rules = append(rules, ...)` block for "Claude managed configuration" straight to:

```go
	result := seatbelt.GuardResult{Rules: rules}
	augmentLinuxPaths(ctx, &result)
	return result
}
```

- [ ] **Step 4: Run the Claude module tests to verify they still pass**

Run: `go test ./pkg/seatbelt/modules/... -v`
Expected: PASS. Also grep to confirm the string is gone: `grep -rn "com.apple.pasteboard" pkg/seatbelt/modules/claude.go` should return no matches.

- [ ] **Step 5: Commit**

```bash
git add pkg/seatbelt/modules/claude.go pkg/seatbelt/modules/claude_test.go
git commit -m "fix: remove ad hoc clipboard rule from Claude module

Clipboard access is now the opt-in 'clipboard' capability, not an
unconditional Claude-only rule."
```

---

### Task 3: Manual verification against the real generated profile

This task has no automated test — it exists because the class of bug this
capability fixes (false-success `pbcopy` exit code) does not surface in
unit tests that only check rule text for substring presence. Do this
manually on a macOS machine after Tasks 1 and 2 are merged.

**Files:** none (verification only).

- [ ] **Step 1: Build aide**

```bash
go build -o /tmp/aide-clipboard-check ./cmd/aide
```

- [ ] **Step 2: Generate the profile with clipboard disabled (baseline) and confirm the old rule is gone**

```bash
/tmp/aide-clipboard-check sandbox test | grep -c "com.apple.pasteboard"
```

Expected: `0` — no clipboard rule present when the capability is not requested (image paste under a plain Claude session no longer works until the user opts in; this is the intended breaking change).

- [ ] **Step 3: Generate the profile with `--with clipboard` and confirm all three global-names are present**

```bash
/tmp/aide-clipboard-check sandbox test --with clipboard | grep "global-name"
```

Expected output includes all three lines:
```
(allow mach-lookup (global-name "com.apple.pasteboard.1"))
(allow mach-lookup (global-name "com.apple.lsd.mapdb"))
(allow mach-lookup (global-name "com.apple.lsd.modifydb"))
```

- [ ] **Step 4: Round-trip test the real host pasteboard under the generated profile**

Extract the profile written by `sandbox test --with clipboard` (or use the sandbox's own launch path if `aide sandbox test` doesn't already write a `.sb` file — check `aide sandbox test --help` for a `--write` / output-path flag; if none exists, redirect stdout to a file) and run:

```bash
/tmp/aide-clipboard-check sandbox test --with clipboard > /tmp/clipboard-test.sb
echo "clipboard-roundtrip-$(date +%s)" | sandbox-exec -f /tmp/clipboard-test.sb pbcopy
sandbox-exec -f /tmp/clipboard-test.sb pbpaste
```

Expected: the `pbpaste` output matches what was piped into `pbcopy` — confirming the write actually reached the real host pasteboard (not a silent no-op) and the read works. If `sandbox-exec` reports "Operation not permitted" on invocation itself (a known nested-sandbox artifact when run from inside an already-sandboxed shell), run the same two commands from a plain (non-sandboxed) Terminal window instead.

- [ ] **Step 5: Record the result**

No commit for this task — it's a verification gate. If Step 4 fails, stop and re-open Task 1 (the `Allow` entries) rather than proceeding to Task 4.

---

### Task 4: Update capability docs table

**Files:**
- Modify: `docs/capabilities.md:71-76` (Developer Tools table)

**Interfaces:** none — documentation only.

- [ ] **Step 1: Add the `clipboard` row**

In `docs/capabilities.md`, change:

```markdown
### Developer Tools

| Capability | What it unlocks | Paths | Key env vars |
|------------|----------------|-------|-------------|
| `github` | GitHub CLI and credentials | `~/.config/gh/` | `GITHUB_TOKEN`, `GH_TOKEN` |
| `gpg` | GPG keys and signing | `~/.gnupg/` | `GNUPGHOME` |
```

to:

```markdown
### Developer Tools

| Capability | What it unlocks | Paths | Key env vars |
|------------|----------------|-------|-------------|
| `github` | GitHub CLI and credentials | `~/.config/gh/` | `GITHUB_TOKEN`, `GH_TOKEN` |
| `gpg` | GPG keys and signing | `~/.gnupg/` | `GNUPGHOME` |
| `clipboard` | Read/write access to the system clipboard (macOS only) | — | — |
```

- [ ] **Step 2: Verify rendering**

Run: `grep -n "clipboard" docs/capabilities.md`
Expected: the new row appears exactly once.

- [ ] **Step 3: Commit**

```bash
git add docs/capabilities.md
git commit -m "docs: add clipboard capability to capabilities table"
```

---

### Task 5: Release notes

**Files:**
- Modify: `RELEASE_NOTES.md` (top of file, inside the existing `## Unreleased` section)

**Interfaces:** none — documentation only.

- [ ] **Step 1: Replace the existing clipboard bug-fix entry with the corrected one**

In `RELEASE_NOTES.md`, the current entry (lines 120-129) reads:

```markdown
#### Claude Code image paste (Ctrl+V) now works inside aide sandbox

The macOS sandbox blocked `com.apple.pasteboard.1`, the per-user mach
service that `osascript` uses to read clipboard data. Claude Code uses
osascript to detect and read PNG image data on Ctrl+V, so pasting
screenshots silently failed with "clipboard empty" when running via `aide`.

The fix adds a mach-lookup allow for `com.apple.pasteboard.1` to the
Claude module rules. The entry is scoped to the Claude agent only; no
other sandboxed processes gain clipboard access.
```

Replace it with:

```markdown
#### Clipboard access is now its own opt-in capability (fixes copy-out regression)

The mach-lookup allow added to fix Claude Code's Ctrl+V image paste only
granted `com.apple.pasteboard.1`, one of three Mach services `pbcopy`
needs. Without the other two (`com.apple.lsd.mapdb`, `com.apple.lsd.modifydb`
— Launch Services type lookups used when writing a pasteboard flavor),
`pbcopy` exited 0 but silently failed to write to the real host pasteboard,
breaking any clipboard fallback logic that trusted that exit code. Copying
text out of the sandbox stopped working while paste (a pure read) kept
working.

Clipboard access is now the `clipboard` capability
(`internal/capability/builtin.go`), grants all three required Mach
services, and is available to every agent, not just Claude.

**Breaking change:** clipboard access is opt-in. If you relied on Claude
Code image paste working automatically, add `clipboard` to your context's
`capabilities:` list or pass `--with clipboard`.
```

- [ ] **Step 2: Verify the old heading is gone and the new one is present**

Run: `grep -n "^#### " RELEASE_NOTES.md | head -5`
Expected: the "Clipboard access is now its own opt-in capability" heading appears in place of the old "Claude Code image paste" heading; no duplicate clipboard entries.

- [ ] **Step 3: Commit**

```bash
git add RELEASE_NOTES.md
git commit -m "docs: update release notes for clipboard capability breaking change"
```

---

## Self-Review Notes

- **Spec coverage:** Problem (root cause) → Task 1 (correct `Allow` rules) + Task 3 (manual proof). Design Overview (single capability, opt-in, macOS-only) → Task 1's `Capability` literal (no `Markers`, no `Direction`/split, no Linux-specific code). Changes 1-4 from the spec → Tasks 1, 2, 1 (test), 4 (doc table added as an extra since the spec's "Changes" list didn't call it out, but `docs/capabilities.md` already documents every other built-in and would drift otherwise). Migration/Behavior Change → Task 5. Testing → Task 1 (unit) + Task 2 (obsolete test removed) + Task 3 (manual round-trip). Out of Scope items are not touched by any task.
- **Correction carried from spec review:** the spec's `Allow` code block pre-wraps the rule text in `(allow mach-lookup ...)`, which would double-wrap under the actual rendering path (`pkg/seatbelt/guards/guard_filesystem.go:119-125` does `fmt.Sprintf("(allow %s)", op)`, confirmed by `TestFilesystem_ExtraAllow`). This plan uses three bare `mach-lookup (global-name "...")` operation-expression strings instead — functionally identical to upstream's grouped rule, but in the form the actual rendering pipeline expects. Task 1's code is authoritative for implementation regardless of what the spec's illustrative snippet shows.
- **Type consistency:** `Capability.Allow` is `[]string` throughout (`internal/capability/capability.go:26`); Task 1's literal and Task 1's test both use plain string equality against that same slice — no signature drift.

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-08-05-clipboard-capability-implementation.md`. Two execution options:

1. **Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration
2. **Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

Which approach?
