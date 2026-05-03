# Release Notes Workflow

**Status:** Draft
**Date:** 2026-05-03

## Problem

Today the GitHub release for each tag is the flat commit list goreleaser
emits by default — useful as an audit trail, useless as a "what changed
for me" summary. Two examples from this cycle:

- v1.7.0's release notes are 11 commit subjects, none of which name the
  user-facing impact (banner variant + provenance behavior).
- v1.8.0 will ship two CLI breaking changes (`--from-secret` removed,
  `aide context add`/`add-match` removed) plus an empty-state launcher
  redesign. Without prose, the breaking changes are invisible until a
  user's script breaks.

The pattern of "remember to write release notes at tag time" failed
this cycle — the breaking changes nearly shipped without callouts.

## Goals

- Each release ships with hand-written prose explaining breaking
  changes, new features, and notable internal changes.
- Notes are written when the user-facing impact is freshest in mind:
  at spec/plan commit time.
- The mechanism is reusable — set up once, runs the same way on every
  release.
- Out of scope for now: a long-form `CHANGELOG.md`, automated
  changelog generation from conventional commits, CI guards that
  block tags when notes are stale.

## Design

### File and config

- **`RELEASE_NOTES.md`** at repo root, hand-edited.
- **`.goreleaser.yml`** gets one block:

  ```yaml
  release:
    github:
      owner: jskswamy
      name: aide
    header:
      from_file:
        path: RELEASE_NOTES.md
  ```

- **Auto-generated commit list stays.** Goreleaser appends its flat
  commit list below the header. The release body looks like:

  ```
  <RELEASE_NOTES.md contents>

  ## Changelog
  * abc1234 Subject 1
  * def5678 Subject 2
  ```

  This keeps the audit trail without requiring commit-message
  conventions.

### Workflow rule

When a spec introduces a user-facing change, the **same commit that
adds the spec** also updates `RELEASE_NOTES.md`. The brainstorming
skill's "write design doc" step grows one substep:

> If this spec describes a user-facing change, append a matching
> entry to `RELEASE_NOTES.md` in this commit.

What counts as "user-facing":

- Any change to a CLI surface a user types: removed flags, renamed
  commands, new commands, changed defaults.
- Any new behavior a user observes: prompts, output format, error
  messages they would see.
- New features, environment-variable contracts, file-format changes.

What does not count:

- Internal refactors (seam extractions, helper functions, file
  splits).
- Test scaffolding.
- Doc changes that don't reflect a behavior change.
- Performance improvements without observable user-facing change.

When in doubt, add the note. A line of prose is cheap; a missed
breaking change is expensive.

### Per-release lifecycle

Steady state: `RELEASE_NOTES.md` accumulates entries across multiple
specs as features are designed. By the time you decide to tag, the
file is already populated with the next release's notes.

Tag flow:

1. Confirm `RELEASE_NOTES.md` reflects everything that shipped since
   the last tag (`git log <last-tag>..HEAD` is the cross-check).
2. Replace the `## Unreleased` header with `## vX.Y.Z` (the version
   you're about to tag). Drop any empty section headings.
3. `git add RELEASE_NOTES.md && git commit -m "Stamp v1.8.0 release notes"`
4. Tag and push: `git tag -a v1.8.0 -m "Release v1.8.0" && git push origin v1.8.0`.
5. CI runs goreleaser; the GitHub release publishes with the file's
   contents as its header.
6. **Reset for the next release.** Replace `RELEASE_NOTES.md` contents
   with the template (see below) and commit on `main`. The reset
   commit can ride along with the next spec or stand alone.

### Template (the file's reset state)

```markdown
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
```

### Initial v1.8.0 content

For the first use of this workflow, `RELEASE_NOTES.md` ships
pre-populated with the v1.8.0 entries:

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

## Implementation outline

Single-PR change:

1. Add the `release.header` block to `.goreleaser.yml`.
2. Create `RELEASE_NOTES.md` with the v1.8.0 content (above).
3. Update the brainstorming skill's documentation (or this repo's
   `CLAUDE.md`) with a one-line note about the workflow rule:
   *"specs that describe user-facing changes also update
   RELEASE_NOTES.md in the same commit."*
4. Commit, push, tag `v1.8.0`, push tag.

After the v1.8.0 release goes out:

5. Reset `RELEASE_NOTES.md` to the template; commit on `main`.

## Future work (explicit non-goals for this spec)

- Migrate to `CHANGELOG.md` (Keep-a-Changelog) if external users
  start needing a single in-repo timeline.
- Pre-tag CI guard that fails if `RELEASE_NOTES.md` doesn't have a
  version header for the tag being pushed.
- Conventional-commit-driven `changelog.groups` if commit-message
  discipline becomes a project convention.
