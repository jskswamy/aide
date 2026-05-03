# Release Notes Workflow Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire `RELEASE_NOTES.md` into goreleaser so each tagged release publishes hand-written prose alongside the auto-generated commit list, and document the workflow rule that user-facing specs update the file at spec-commit time.

**Architecture:** Three small, independent artifacts: a new `RELEASE_NOTES.md` populated with v1.8.0's notes, a one-block addition to `.goreleaser.yml` (`release.header.from_file`), and a workflow note appended to `AGENTS.md`. Verification is a local `goreleaser --snapshot` dry-run that renders the would-be release notes. The actual v1.8.0 release happens as a separate post-implementation step (push, wait for CI, tag, push tag).

**Tech Stack:** goreleaser v2 config (`.goreleaser.yml`), markdown.

---

## Spec

See `docs/superpowers/specs/2026-05-03-release-notes-workflow.md`.

## File Map

- **Create:** `RELEASE_NOTES.md` at repo root — pre-populated with v1.8.0 content (it is the file goreleaser will read at tag time).
- **Modify:** `.goreleaser.yml` — add the `release.header.from_file` block.
- **Modify:** `AGENTS.md` — append the one-line workflow rule under a new "Release Notes" section.
- **No source-code changes.** No new tests. Verification is goreleaser dry-run.

---

## Task 1: Create `RELEASE_NOTES.md` with v1.8.0 content

The file ships ready-stamped for v1.8.0 since this same change cycle is the first release that will use the workflow.

**Files:**
- Create: `RELEASE_NOTES.md`

- [ ] **Step 1: Create the file**

Write `/Users/subramk/source/github.com/jskswamy/aide/RELEASE_NOTES.md` with this exact content:

```markdown
## v1.8.0

Two CLI surfaces redesigned for clearer mental model and a guided
empty-state experience when no context matches the current folder.

### 💥 Breaking changes

- `aide env set --from-secret KEY` is **removed**. Use one of:
  - `aide env set FOO --secret-key KEY --global`
  - `aide env set FOO --secret-store NAME --secret-key KEY --global`
  - `aide env set FOO --pick --global`
  - `--global` is now required for any secret reference.
- `aide context add` and `aide context add-match` are **removed**.
  Use `aide context create [name]` and `aide context bind <name>`.

### ✨ New

- `aide` now offers an interactive prompt when no context matches
  the current folder. Non-TTY mode prints concrete remediation
  commands.
- `aide context bind <name>` auto-detects the match rule (git
  remote when available, else folder path). `--path` and `--remote`
  override.
- `aide context create [name]` auto-detects the agent when exactly
  one supported agent is on PATH; prompts to bind cwd in TTY mode.

### 🔧 Internal

- New `examples_test.go` guard ensures every `aide ...` line in
  command help text actually parses against the real flag set.
- Cobra-level parsing tests for `env set` and `context bind/create`
  flag matrices.
```

- [ ] **Step 2: Verify file exists**

Run: `ls -l RELEASE_NOTES.md && head -5 RELEASE_NOTES.md`
Expected: file exists, first line is `## v1.8.0`.

- [ ] **Step 3: Commit (deferred)** — do not commit yet. All three files (RELEASE_NOTES.md, .goreleaser.yml, AGENTS.md) ship in one atomic commit at the end of Task 3, since they collectively introduce the workflow.

---

## Task 2: Wire `RELEASE_NOTES.md` into `.goreleaser.yml`

Add the header block. This is the load-bearing config change.

**Files:**
- Modify: `.goreleaser.yml` (the `release:` section near the bottom)

- [ ] **Step 1: Read the current release section**

Run: `grep -n -A 4 "^release:" .goreleaser.yml`
Expected: shows the existing block:

```
release:
  github:
    owner: jskswamy
    name: aide
```

- [ ] **Step 2: Replace the release section**

In `.goreleaser.yml`, replace:

```yaml
release:
  github:
    owner: jskswamy
    name: aide
```

with:

```yaml
release:
  github:
    owner: jskswamy
    name: aide
  header:
    from_file:
      path: RELEASE_NOTES.md
```

- [ ] **Step 3: Verify the YAML still parses**

Run: `goreleaser check`
Expected: `1 configuration file(s) validated` (or equivalent success line). If goreleaser flags an error about the new block, the path or indentation is wrong — fix it before continuing.

- [ ] **Step 4: Commit (deferred)** — defer to Task 3.

---

## Task 3: Document the workflow rule in `AGENTS.md`

Add a short section so future agents (and humans skimming the doc) know that user-facing specs update `RELEASE_NOTES.md` in the same commit.

**Files:**
- Modify: `AGENTS.md` (append a new section near the bottom, after the existing content but before the beads integration block at line 39)

- [ ] **Step 1: Read AGENTS.md to find the insertion point**

Run: `grep -n "BEGIN BEADS INTEGRATION" AGENTS.md`
Expected: matches the line `<!-- BEGIN BEADS INTEGRATION ... -->` somewhere in the file.

- [ ] **Step 2: Insert the new section before the beads integration marker**

Add this section immediately above the `<!-- BEGIN BEADS INTEGRATION ... -->` line:

```markdown
## Release Notes

User-facing changes are tracked in `RELEASE_NOTES.md` at the repo
root. Goreleaser reads it as the GitHub release header at tag time.

**Rule:** when a spec describes a user-facing change, the SAME
commit that adds the spec also appends a matching entry to
`RELEASE_NOTES.md` under the appropriate heading (`### 💥 Breaking
changes`, `### ✨ New`, or `### 🔧 Internal`).

What counts as user-facing: removed flags, renamed commands, new
commands, changed defaults, new prompts, new error messages users
see, environment-variable contracts, file-format changes.

What does not count: internal refactors, test scaffolding, doc
fixes, performance improvements without observable change.

When in doubt, add the note.

When tagging a release: replace the `## Unreleased` header with the
version (e.g. `## v1.8.0`), commit, then tag. After the release
ships, reset `RELEASE_NOTES.md` to its template.

Full design: `docs/superpowers/specs/2026-05-03-release-notes-workflow.md`.

```

(Note the trailing blank line — preserves spacing before the beads block.)

- [ ] **Step 3: Verify the file still has the beads integration block intact**

Run: `grep -c "BEGIN BEADS INTEGRATION\|END BEADS INTEGRATION" AGENTS.md`
Expected: `2` (both markers present).

- [ ] **Step 4: Commit all three changes atomically**

```bash
git add RELEASE_NOTES.md .goreleaser.yml AGENTS.md
__GIT_COMMIT_PLUGIN__=1 git commit -m "Wire RELEASE_NOTES.md into goreleaser release header"
```

DO NOT add Claude/Anthropic Co-Authored-By.

---

## Task 4: Verify via goreleaser snapshot dry-run

Run goreleaser in snapshot mode (no publish, no tags) and inspect the rendered release notes to confirm the header file is being read.

**Files:**
- None modified. This task is purely verification.

- [ ] **Step 1: Run goreleaser snapshot**

```bash
goreleaser release --snapshot --skip=publish --clean
```

Expected: succeeds without errors. Builds binaries into `dist/`. Goreleaser prints `release notes` somewhere in the output.

- [ ] **Step 2: Inspect the rendered release notes**

The snapshot mode writes the artifacts and metadata into `dist/`. The release notes themselves are passed to GitHub at publish time, but you can render them by running goreleaser with explicit output:

```bash
cat dist/CHANGELOG.md 2>/dev/null || ls dist/
```

If `dist/CHANGELOG.md` exists, it should contain a flat commit list (the auto-changelog footer). The header doesn't render to a file in snapshot mode by default — instead, verify the configuration is being read by:

```bash
goreleaser check && grep -A 4 "^release:" .goreleaser.yml
```

Expected: `goreleaser check` passes; the grep shows the `header:` block is correctly configured.

- [ ] **Step 3: Optional sanity check — render the full release body**

If you want to see exactly what would publish, run:

```bash
goreleaser release --snapshot --skip=publish --clean --release-notes <(echo "")
```

This forces an empty auto-changelog footer; the `dist/` output should reflect the header file's contents at the top. Skip this step if it doesn't behave as expected — the real validation is the actual v1.8.0 tag push.

- [ ] **Step 4: Clean up `dist/`**

Run: `rm -rf dist/`
Expected: directory removed (it's gitignored, but tidy is good).

- [ ] **Step 5: No commit needed.** Verification only.

---

## Post-implementation: tagging v1.8.0

This is the user-facing action that exercises the new workflow. Not a TDD task — these are the operational steps once the implementation tasks above are done.

- [ ] **Step 1: Push the implementation commits**

```bash
git push
```

Wait for CI green on `main`.

- [ ] **Step 2: Tag and push**

```bash
git tag -a v1.8.0 -m "Release v1.8.0"
git push origin v1.8.0
```

This triggers `.github/workflows/release.yml` which runs goreleaser.

- [ ] **Step 3: Verify the release**

```bash
gh release view v1.8.0 --json body --jq .body | head -50
```

Expected: the body starts with `## v1.8.0` (the contents of `RELEASE_NOTES.md`), followed by `## Changelog` and the auto-generated commit list.

If the body still looks like a flat commit list with no prose, the `from_file` path is wrong or the file wasn't included in the tagged commit. Check `git show v1.8.0:RELEASE_NOTES.md` — if missing, retag is needed.

- [ ] **Step 4: Reset `RELEASE_NOTES.md` to the template for the next release**

```bash
cat > RELEASE_NOTES.md <<'EOF'
## Unreleased

<!--
Add release notes here as specs ship. Group under the headings
below. When tagging, replace `## Unreleased` with `## vX.Y.Z`,
commit, then tag.

Headings (use only what applies; drop empty sections):
  ### 💥 Breaking changes
  ### ✨ New
  ### 🔧 Internal

After tagging, reset this file to this template.
-->
EOF

git add RELEASE_NOTES.md
__GIT_COMMIT_PLUGIN__=1 git commit -m "Reset RELEASE_NOTES.md for next release"
git push
```
