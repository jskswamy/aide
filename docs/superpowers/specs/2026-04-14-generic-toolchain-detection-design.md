# Generic Toolchain Variant Detection & GPU Capability Design

**Date:** 2026-04-14
**Status:** Proposed
**Related:** `SANDBOX-ISSUES.md`, `2026-03-25-capabilities-design.md`, `2026-03-22-sandbox-guards-design.md`, `2026-03-26-narrow-scoped-reads-trust-gate-design.md`

## Problem

When running AI agents under aide's sandbox, real-world Python workloads fail
because the `python` capability only grants pyenv paths. A project using `uv`
(which stores managed interpreters in `~/.local/share/uv/python`) hits
`Operation not permitted` mid-task. The same latent gap exists for every
language with multiple package managers or runtime variants.

Additionally, GPU-accelerated workloads (ManimGL, PyTorch with Metal, etc.)
fail because seatbelt blocks Mach services and IOKit access with no capability
to grant it.

Users discover these failures only after the agent runs, often minutes into a
task. The failure mode is opaque — a generic OS error with no indication that
the sandbox is the cause or how to fix it.

## Goals

1. **Narrow-surface toolchain permissions** — grant only the paths that the
   project's actual toolchain variant uses (not a union of all variants).
2. **Generic across languages** — the same mechanism handles Python (uv,
   pyenv, conda, poetry, venv), Node (npm, pnpm, yarn), Ruby (rbenv, asdf),
   Rust (rustup vs system), JVM (sdkman, maven, gradle), etc.
3. **Opt-in GPU capability** — `--with gpu` grants the Mach/IOKit surface
   needed for OpenGL/Metal workloads; never auto-activated.
4. **Proactive diagnostics** — catch toolchain/sandbox mismatches *before* the
   agent starts, both in the default launch flow and via an explicit `aide
   doctor` subcommand.
5. **Plugin reliability** — make the Claude Code `sandbox-doctor` skill
   trigger reliably as a secondary safety net when issues slip past preflight.
6. **Informed consent** — every auto-detected permission grant requires
   one-time explicit user approval, persisted like aide's existing file-trust
   ledger, re-prompted only when detection evidence changes.

## Non-goals

- Supporting every exotic toolchain (e.g., Nix's language-specific wrappers).
  Start with the top 3–5 variants per language.
- Fontconfig / matplotlib config dirs (per `SANDBOX-ISSUES.md` issue #4).
  Users handle these themselves via custom capabilities.
- Preserving the legacy always-on node behavior verbatim. The existing
  `node-toolchain` guard is replaced by the variant-aware `node` capability
  in this spec; projects that previously got all variants for free will
  either auto-detect the right one and prompt for consent, or fall through
  to `DefaultVariants`.

## Design

### Architecture overview

```
project files ──►  Detector  ──► Evidence ──►  Consent Store
                    │                │              │
                    ▼                ▼              ▼
              markers match     digest       granted? (Y/N/prompt)
                                                     │
                                                     ▼
                                              selected variants
                                                     │
                                                     ▼
                                             Capability overrides
                                                     │
                                                     ▼
                                              Seatbelt profile
```

The design extends the existing capability layer (`internal/capability/`)
rather than introducing a parallel mechanism. Capabilities remain the
user-facing primitive; variants are an internal refinement; consent is
persisted outside the repo in a content-addressed ledger that mirrors the
existing `aide trust` pattern.

### Domain model (DDD)

The design introduces one new bounded-context aggregate (**Consent**) that
sits alongside the existing **Trust** aggregate, both within the
**User Approval** context. They are distinct aggregates — different identity,
different lifecycle, different business rules — but they share infrastructure
mechanics (content-addressed file-backed sets under `XDG_DATA_HOME/aide/`).

| Aspect | Trust (existing) | Consent (new) |
|---|---|---|
| Question answered | "Is this config file authentic?" | "Did the user approve this detection-derived grant?" |
| Aggregate root | `(absolute_path, content_hash)` | `(project_root, capability, variant, evidence_digest)` |
| Invalidation | File content changes | Detection evidence changes |
| Auto-refresh | Yes (aide auto-re-trusts after on-behalf edits) | No (always re-prompt on evidence change) |
| Negative form | Explicit `Deny` record | No deny — absence of grant is the negative |
| Scope | Per-file | Per-project × capability × variant |
| CLI | `aide trust/untrust/deny` | `aide cap consent list/revoke` |

Because storage mechanics are identical, a new **shared-kernel** package
factors the content-addressed set primitive out of the current `trust`
package and both aggregates use it.

### Package structure

```
internal/
├── approvalstore/           NEW — shared kernel (infrastructure primitive)
│   └── store.go             content-addressed file-backed set; no domain terms
├── trust/                   EXISTING — file authenticity aggregate; public API unchanged
│   └── trust.go             refactored to delegate storage to approvalstore
└── consent/                 NEW — detection consent aggregate
    └── consent.go           uses approvalstore; Evidence/Grant domain types
```

**`internal/approvalstore/`** — generic content-addressed set:

```go
package approvalstore

type Store struct { baseDir string }

func NewStore(baseDir string) *Store
func DefaultRoot() string              // XDG_DATA_HOME/aide (shared by all aggregates)

func (s *Store) Has(key string) bool
func (s *Store) Add(key string, body []byte) error
func (s *Store) Remove(key string) error
func (s *Store) List() ([]Record, error)
func (s *Store) Read(key string) (Record, error)

type Record struct { Key string; Body []byte; ModTime time.Time }
```

No domain terminology. No `FileHash` or `ConsentHash` here. Just a primitive
for "does a record with this key exist, and what metadata did we store with
it?" Storage layout: `<baseDir>/<namespace>/<hex-key>`.

**`internal/trust/`** — unchanged public API, internals now delegate:

```go
// Existing public surface stays identical:
type Status int  // Untrusted | Trusted | Denied
type Store struct { /* now wraps two approvalstore.Store instances (trust, deny) */ }

func DefaultStore() *Store
func FileHash(path string, contents []byte) string
func PathHash(path string) string
func (s *Store) Check(path string, contents []byte) Status
func (s *Store) Trust(path string, contents []byte) error
func (s *Store) Deny(path string) error
func (s *Store) Untrust(path string, contents []byte) error
```

Zero migration for existing callers.

**`internal/consent/`** — new aggregate:

```go
package consent

type Status int  // Granted | NotGranted

type MarkerMatch struct {
    Kind     string // "file" | "contains" | "glob"
    Target   string // "uv.lock" or "pyproject.toml:[tool.uv]"
    Matched  bool
}

type Evidence struct {
    Variants []string       // all variants selected by detection, sorted
    Matches  []MarkerMatch  // union across selected variants, sorted, deterministic
}

func (e Evidence) Digest() string  // SHA-256 over canonicalized tuple

type Grant struct {
    ProjectRoot string
    Capability  string
    Variants    []string   // the combined set the user approved (sorted)
    Evidence    Evidence
    Summary     string     // human-readable marker list
    ConfirmedAt time.Time
}

type Store struct { set *approvalstore.Store }

func DefaultStore() *Store
func ConsentHash(projectRoot, cap string, variants []string, evidenceDigest string) string
    // variants are sorted before hashing so order-insensitive

func (s *Store) Check(projectRoot, cap string, evidence Evidence) Status
    // evidence.Variants is the set to check — returns Granted only if
    // a record exists matching the exact set + digest
func (s *Store) Grant(g Grant) error
func (s *Store) Revoke(projectRoot, cap string) error
    // removes all records matching (projectRoot, cap) regardless of
    // variants or evidence digest — clears all consents for this capability
func (s *Store) List(projectRoot string) ([]Grant, error)
```

Storage namespace: `XDG_DATA_HOME/aide/consent/`. Each record at
`consent/<consent-hash>` contains a human-readable body:

```
project: /Users/subramk/projects/foo
capability: python
variant: uv
evidence_digest: sha256:a1b2c3...
evidence_summary: uv.lock, [tool.uv] in pyproject.toml
confirmed_at: 2026-04-14T10:30:00Z
```

The file's existence at the computed hash path is the authoritative check.

### Capability model extension

Capabilities gain an optional list of **variants**. Each variant declares
detection markers and path/env contributions.

```go
// internal/capability/capability.go (extended)
type Variant struct {
    Name        string   // "uv", "pyenv", "conda"
    Description string
    Markers     []Marker
    Readable    []string
    Writable    []string
    EnvAllow    []string
    EnableGuard []string
}

type Marker struct {
    // Exactly one of File, Contains, GlobPath must be set.
    File     string
    Contains struct { File, Pattern string }
    GlobPath string
}

type Capability struct {
    Name        string
    Description string
    // existing fields
    Variants        []Variant // new
    DefaultVariants []string  // variants to activate when no markers match
}
```

When a capability has `Variants`, its top-level `Writable`/`Readable`/etc.
fields are the **base** (common to all variants); selected variants layer on
top.

### Variant selection & consent flow

At launch (and at `aide doctor`), for each active capability with variants:

```go
func SelectVariants(
    cap Capability,
    projectRoot string,
    overrides []string,              // from --variant flag
    yamlPins []string,               // from .aide.yaml
    consentStore *consent.Store,
    prompter Prompter,               // nil in non-interactive contexts
) (variants []Variant, provenance Provenance, err error)
```

**Decision table** (single, consistent algorithm across all capabilities):

| State | Evidence | `.aide.yaml` pin | `--variant` override | Consent store | Action |
|---|---|---|---|---|---|
| A. First time | uv | — | — | (no grant) | **Prompt** → on yes, record grant |
| B. Stable | uv | — or `[uv]` | — | Granted for digest | Silent, use uv |
| C. Evidence changed | conda | `[uv]` | — | NotGranted (digest mismatch) | **Prompt** → on yes, new grant |
| D. Explicit user pin | anything | `[uv]` | — | (consent irrelevant) | Silent — `.aide.yaml` is explicit intent |
| E. CLI override | anything | anything | `python=uv` | (consent irrelevant) | Silent — explicit flag wins |

`.aide.yaml` variant pins are treated as the user's explicit intent (state
D/E) — they bypass the consent store because the act of writing them to the
config is itself the approval (gated by existing `aide trust`).

**Precedence (highest first):** `--variant` flag → `.aide.yaml` pin →
auto-detection (consent-gated) → `DefaultVariants`.

### Consent prompt (single template for all states)

Per-capability prompt, consolidating all auto-selected variants for that
capability into one approval. Multi-variant capabilities (commonly `node`)
produce a single prompt with the combined grant, not one prompt per variant.

```
[aide] node — detection needs your confirmation

  Previously: npm                              (marker: package-lock.json)   ← omitted first-time
  Detected:   pnpm + corepack + playwright    (markers: pnpm-lock.yaml,
                                                        "packageManager" in package.json,
                                                        @playwright/test in package.json)

  Grants:  ~/.nvm, ~/.fnm,
           ~/.config/pnpm, ~/.pnpm-state, ~/.pnpm-store,
           ~/.local/share/pnpm, ~/Library/pnpm,
           ~/.cache/node/corepack, ~/Library/Caches/node/corepack,
           ~/Library/Caches/ms-playwright
  Env:     (none)

  [Y]es, grant all    [N]o, use default    [D]etails    [S]kip this launch    [C]ustomize
```

- First-time prompts omit the "Previously" line; otherwise identical layout.
- `D`etails expands to full path list, env vars, and every matched marker
  broken down per variant.
- `S`kip forgoes the grant for this launch only; no ledger write.
- `N`o records no grant; `DefaultVariants` applies. User can always rerun with
  `--variant cap=X` to force.
- `C`ustomize opens a variant-by-variant yes/no sub-flow for users who want
  to accept some but not all detected variants. Saves the reduced selection
  as the consent record.

The consent record stores the **combined set of variants** the user approved
together, keyed on the combined evidence digest. Evidence digest is computed
over the sorted union of all detected variants' `MarkerMatch` tuples — so any
change in the detected set (variant added, variant no longer detected,
marker changed) produces a new digest and re-prompts.

### Non-interactive handling

When no TTY is available (CI, scripts):

- States B, D, E: silent, proceed.
- States A, C: no auto-grant. Print advisory warning, fall through to
  `DefaultVariants`. Suggest explicit `--variant` or running interactively.
- New `--yes` flag: auto-approve all prompts (for scripted setup flows
  where the user has pre-reviewed the project).

### CLI surface

**`--with` (unchanged)** — coarse capabilities only.

**`--variant` (new)** — pins variants for capabilities in `--with`.
- Format: `<capability>=<variant>`, comma-separated.
- Validation: must reference a capability in `--with`; error at parse time
  otherwise.
- Unknown variants: error with the list of valid variants + pointer to
  `aide cap show <capability>`.

**`.aide.yaml`** — optional variant pins; no machine-managed blocks:

```yaml
capabilities:
  python:
    variants: [uv]    # optional; detection runs if absent
```

**Discovery commands:**
- `aide cap list` — coarse capabilities + variant count hint column.
- `aide cap show <capability>` — detailed per-variant view.
- `aide cap variants` — flat list (`python/uv`, `node/pnpm`, ...).
- Shell completion suggests variants based on active `--with`.

**Consent commands:**
- `aide cap consent list [--project <path>]` — show granted consents.
- `aide cap consent revoke <capability>` — clear all consents for this
  capability in the current project; next launch re-prompts.

### Initial variant catalog

**python** (default: `venv`):
- `uv` — markers: `uv.lock`, `[tool.uv]` in `pyproject.toml`. Paths:
  `~/.local/share/uv`, `~/.cache/uv`. Env: `UV_CACHE_DIR`,
  `UV_PYTHON_INSTALL_DIR`.
- `pyenv` — markers: `.python-version`. Paths: `~/.pyenv`. Env: `PYENV_ROOT`.
- `conda` — markers: `environment.yml`, `conda-lock.yml`. Paths: `~/.conda`,
  `~/miniconda3`, `~/anaconda3`. Env: `CONDA_PREFIX`, `CONDA_DEFAULT_ENV`.
- `poetry` — markers: `poetry.lock`, `[tool.poetry]` in `pyproject.toml`.
  Paths: `~/.cache/pypoetry`, `~/Library/Caches/pypoetry`. Env: `POETRY_HOME`.
- `venv` — default fallback. Paths: none beyond project root. Env:
  `VIRTUAL_ENV`.

**node** (default: `npm`) — replaces the legacy always-on `node-toolchain`
guard. The guard is deleted; its contents are split into the variants below,
plus a shared `node` base (the version managers `~/.nvm` and `~/.fnm`, which
every variant needs).

Base (always applied when `node` capability is active): `~/.nvm`, `~/.fnm`.

- `npm` — markers: `package-lock.json`, `.npmrc`. Paths: `~/.npm`,
  `~/.config/npm`, `~/.cache/npm`, `~/.cache/node`, `~/Library/Caches/npm`,
  `~/.npmrc`, `~/.config/configstore`, `~/.node-gyp`, `~/.cache/node-gyp`.
  Env: `NPM_TOKEN`, `NODE_AUTH_TOKEN`.
- `pnpm` — markers: `pnpm-lock.yaml`, `pnpm-workspace.yaml`. Paths:
  `~/.config/pnpm`, `~/.pnpm-state`, `~/.pnpm-store`, `~/.local/share/pnpm`,
  `~/.local/state/pnpm`, `~/Library/pnpm`, `~/Library/Caches/pnpm`,
  `~/Library/Preferences/pnpm`.
- `yarn` — markers: `yarn.lock`, `.yarnrc`, `.yarnrc.yml`. Paths: `~/.yarn`,
  `~/.yarnrc`, `~/.yarnrc.yml`, `~/.config/yarn`, `~/.cache/yarn`,
  `~/Library/Caches/Yarn`.
- `corepack` — markers: `"packageManager"` key in `package.json`. Paths:
  `~/.cache/node/corepack`, `~/Library/Caches/node/corepack`. (Typically
  activates alongside another variant.)
- `playwright` — markers: `@playwright/test` in `package.json`. Paths:
  `~/Library/Caches/ms-playwright`, `~/.cache/ms-playwright`.
- `cypress` — markers: `cypress` in `package.json`. Paths:
  `~/Library/Caches/Cypress`.
- `puppeteer` — markers: `puppeteer` in `package.json`. Paths:
  `~/.cache/puppeteer`.
- `prisma` — markers: `prisma` in `package.json`, `schema.prisma` file.
  Paths: `~/.cache/prisma`, `~/Library/Caches/prisma-nodejs`,
  `~/Library/Caches/checkpoint-nodejs`, `~/Library/Caches/claude-cli-nodejs`.
- `turbo` — markers: `turbo` in `package.json`, `turbo.json`. Paths:
  `~/.cache/turbo`, `~/Library/Caches/turbo`,
  `~/Library/Application Support/turborepo`.

Auto-detection typically produces a multi-variant selection (e.g.,
`pnpm + corepack + playwright`) which is surfaced as a single consolidated
consent prompt per capability rather than one prompt per variant, to avoid
fatigue. The prompt shows the combined path list and marker list for all
auto-selected variants together.

**ruby** (default: `rbenv`):
- `rbenv` — `.ruby-version`. Paths: `~/.rbenv`.
- `asdf` — `.tool-versions`. Paths: `~/.asdf`.
- `bundler` — `Gemfile.lock`. Paths: `~/.bundle`.

**java** — initial pass keeps current bundled behavior (maven + gradle +
sdkman together). Variant split is a follow-up.

### GPU capability

New `gpu` capability — **opt-in only, never auto-detected, never
consent-prompted**.

```go
"gpu": {
    Name:        "gpu",
    Description: "GPU/display access for OpenGL, Metal, ML training",
    EnableGuard: []string{"gpu"},
}
```

A new `gpuGuard` in `pkg/seatbelt/guards/guard_gpu.go` emits SBPL for:
- `(allow mach-lookup)` for `com.apple.windowserver.active`,
  `com.apple.iosurface`, `com.apple.cglcache`, `com.apple.CoreGraphics.*`,
  `com.apple.metal.*`.
- `(allow iokit-open)` for `IOAccelerator`, `IOGPU`, `IOSurfaceRoot`.
- Read access to `/System/Library/Frameworks/{OpenGL,Metal,CoreGraphics,IOKit}.framework`.

Guard type: `opt-in`. Activated only by explicit `--with gpu`.
`aide status` shows a warning banner when active.

Preflight may **suggest** `--with gpu` if it detects GPU-using imports, but
never prompts to activate.

### Preflight diagnostics

**Default integration.** The default `aide` launch path runs preflight before
handing off to the agent. A `--no-preflight` flag skips it.

**Standalone.** `aide doctor` runs the same checks without launching.

**Checks:**
1. For each active capability with variants: run detectors, consult consent
   store, report the selected variant + provenance.
2. Cross-check: selected variants' paths not shadowed by `Deny` rules.
3. Unclaimed markers: evidence matches for capabilities not in the active
   set → warn.
4. GPU heuristic: `moderngl`/`torch`/`jax`/`tensorflow` imports detected but
   no `--with gpu` → suggest.

**Output** (human-readable default, `--format=json` for scripts):

```
✓ python  variant=uv  (detected: uv.lock; consented 2026-04-14)  paths ok
⚠ python  uv.lock found but ~/.local/share/uv/python not in profile
  fix:   aide sandbox show  (confirm profile renders uv paths)
⚠ moderngl detected but --with gpu not active
  fix:   rerun with --with gpu  (note: grants Mach/IOKit surface)
```

Exit codes: `0` clean; `1` warnings (advisory); `2` fatal (unknown variant
etc.). Warnings never block launch. Future `--strict-preflight` possible but
not in scope.

### Claude Code plugin reliability

1. **Broaden trigger phrases** in `sandbox-doctor` SKILL: add `os error 1`,
   `errno 1`, `EPERM`, `PermissionError: [Errno 1]`, `read-only file system`,
   uv-specific `Failed to discover managed Python installations`, and
   moderngl/glcontext GPU error patterns.
2. **Install docs** in the plugin README: verified install path from local
   marketplace; `/plugin list` verification step.
3. **Reference in CLAUDE.md** at the plugin root pointing the agent at the
   skill index so unfamiliar sandbox errors trigger consultation.

## Threat Model

### Trust boundaries

- **aide process (TCB)** — outside sandbox: config, detection, profile render,
  agent spawn.
- **Agent process (untrusted)** — inside sandbox.
- **Project files (untrusted input)** — detectors read; may be attacker-crafted.
- **`.aide.yaml`** — gated by existing `aide trust` mechanism.
- **Consent store (`XDG_DATA_HOME/aide/consent/`)** — writable only by the user
  account; same trust boundary as `XDG_DATA_HOME/aide/trust/`.

### Threats and mitigations

**T1 — Malicious project plants marker files to widen permissions.**
*Mitigation:* Auto-detected grants require one-time interactive consent.
Attacker dropping `uv.lock` surfaces a prompt showing exact paths before any
grant. Variant path sets exclude credential-adjacent directories (`~/.pypirc`,
etc.) by construction. Residual risk: user approves without reading the
prompt — same class as any OS permission dialog.

**T2 — Detector reads leak file content via logs.**
*Mitigation:* Bounded reads (64KB ceiling, matching existing detection code).
Substring-only matching. File contents never echoed to logs or included in
consent store bodies — only the marker-match boolean and a short human
summary cross the boundary.

**T3 — GPU capability escalation.**
*Mitigation:* Strict opt-in via `--with gpu`; never auto-detected; never
consent-prompted. `aide status` warning when active. Documented as trust
escalation. Future `gpu-readonly` variant noted as follow-up.

**T4 — Malicious `.aide.yaml` variant pins.**
*Mitigation:* `.aide.yaml` is gated by existing `aide trust`. Variant pins
are just another config key under that gate — no new attack surface. Consent
store lives outside the repo, so a committed `.aide.yaml` cannot pre-populate
consents.

**T5 — Preflight false negatives.**
*Mitigation:* Output labels itself "best-effort — runtime denials may still
occur." Points to `aide sandbox show` for full policy. The `sandbox-doctor`
plugin remains the runtime safety net. Confirmed-consent grants give
preflight authoritative ground truth, raising signal quality.

**T6 — Preflight noise drives users to `--no-preflight`.**
*Mitigation:* Advisory-only exits. Concrete fix commands in every warning.
Dominant noise source — "should we grant this?" — is a one-time consent
prompt, not a repeated preflight warning. Once consented, subsequent
preflights are clean.

**T7 — Consent fatigue.**
*Mitigation:* Scope-limit: one prompt per variant per project, never
re-asked unless evidence digest changes. Prompts show concrete paths, not
abstract names. `Details` option for full disclosure. Non-interactive
contexts never auto-activate — require explicit `--with`/`--variant`/config.

**T8 — Consent-store tampering.**
*Mitigation:* Store lives under the user's XDG data home; write access
equivalent to write access on the trust store. No new attack surface.
Consent records are content-addressed: a tampered record at an incorrect
hash path is simply ignored (no match). An attacker with user-level write
access has bigger problems than this store.

**Net residual risk:**
- T3 (GPU) is the only meaningful residual by design — GPU access deliberately
  bypasses the consent flow; must stay a conscious flag.
- T7 (consent fatigue) is a known UX risk with standard mitigations.
- All other threats are reduced to background level by the consent ledger,
  bounded reads, existing trust gates, or both.

## Alternatives considered

**A1: Flat variant capabilities (`python-uv`, `python-pyenv`).** Rejected —
user feedback was explicit that `aide cap list` should stay coarse.

**A2: Detector interface in the guard layer.** Rejected — duplicates the
capability-as-primitive model; capabilities remain the single user-facing
concept.

**A3: Always-broad guards (status quo for node-toolchain).** Rejected —
violates least-permission. The legacy `node-toolchain` guard is deleted in
this spec and replaced with a variant-aware `node` capability. Users see
one consent prompt per node project at the transition point; resulting
grant is narrower than the old blanket grant.

**A4: Store consent inside `.aide.yaml`.** Rejected — conflates
user-editable declarative config with machine-managed approval state;
would embed machine state in project repos; violates the parallel with the
existing direnv-style trust store.

**A5: Single `trust` package covering both file-trust and
detection-consent.** Rejected — different aggregates, different lifecycle
rules, different public APIs. Sharing only infrastructure (via
`approvalstore`) is the correct DDD seam.

## Migration

- Existing `python` capability: extend with `Variants`; current `Writable:
  [~/.pyenv]` becomes the `pyenv` variant; add `DefaultVariants: [venv]`.
  Users with `--with python` on a uv project get the consent prompt on
  first run.
- Existing `npm` capability: folded into the new `node` capability as a
  variant. `--with npm` aliased to `--with node --variant node=npm` for
  backward compat (one release); deprecation warning printed; removed
  the following release.
- Existing `node-toolchain` guard (`pkg/seatbelt/guards/guard_node_toolchain.go`):
  **deleted**. Its contents are migrated into the `node` capability's base
  + variants as catalogued above. Removed from
  `pkg/seatbelt/guards/registry.go` always-guards list.
- Existing `internal/trust/`: refactored internally to use
  `internal/approvalstore/`; **public API unchanged**.
- Existing `.aide.yaml` files without `variants:`: continue to work.
  Defaults + consent flow kick in on first launch.
- Existing `DetectProject` output: the `"npm"` suggestion becomes `"node"`;
  the detector also walks `package.json` to identify which variants apply.
  Callers already treat output as opaque names, so no caller changes needed.

### Behavioral change for node projects (one-time transition)

Before: every node project got all variants (npm + pnpm + yarn + playwright
+ prisma + turbo + etc.) unconditionally via the always-on guard. No prompt.

After: each node project gets a consent prompt on first launch post-upgrade,
listing the auto-detected variants. User approves once per project. The
approved set is narrower than the old blanket grant (only what's used).
Users with heterogeneous projects (e.g., a test suite that uses Cypress
alongside Playwright) see both detected if both markers are present.

This is an intentional, visible migration — one prompt per project — rather
than silent narrowing or silent broadening. Release notes must call this
out. An environment variable `AIDE_SKIP_NODE_MIGRATION=1` can be set for
one release to preserve legacy unconditional behavior for users who need
time to adapt (removed the release after).

## Rollout

1. `internal/approvalstore/` extraction + `internal/trust/` refactor (no
   behavioral change).
2. `internal/consent/` package + consent store.
3. Capability variant model + `python` variants + `--variant` flag +
   discovery commands + consent prompt integration (including multi-variant
   consolidated prompt + `C`ustomize flow).
4. `node` capability + variant catalog + deletion of legacy
   `node-toolchain` guard + `npm` capability deprecation alias +
   `AIDE_SKIP_NODE_MIGRATION` transition flag + release notes.
5. `aide doctor` subcommand + preflight-by-default on launch.
6. `gpu` capability and guard.
7. `ruby` / `java` variant catalogs.
8. Claude plugin trigger-phrase expansion + install documentation.

## Testing

Every new package ships with unit tests and, where it crosses a process
boundary (CLI, filesystem, seatbelt profile), integration tests. TDD per
project convention: tests precede implementation.

### `internal/approvalstore/` (new)

- `Add`/`Has`/`Remove`/`List`/`Read` round-trip on `t.TempDir()`.
- `Add` is idempotent (re-adding same key overwrites body, no error).
- `Has` returns false for unknown key, true immediately after `Add`.
- `List` returns empty slice (not nil) on empty store.
- `List` is deterministic order (sorted by key).
- Body bytes preserved exactly (no normalization, no encoding loss).
- Permissions: store dir created with `0700`; records written `0600`.
- Missing base dir auto-created on first `Add`.
- Concurrent-safe: parallel `Add` of different keys does not corrupt state
  (goroutine test with `sync.WaitGroup` and `t.Parallel()`).
- Malformed record file (unreadable) handled gracefully by `Read`/`List`.

### `internal/trust/` (refactored)

The public API is unchanged, so all **existing trust tests must continue to
pass with zero modification**. This is the primary regression gate for the
refactor.

New tests added for the refactor:
- Trust store and deny store use separate `approvalstore.Store` namespaces
  under the same base dir (`trust/` and `deny/` subdirs).
- `Trust(path, contents)` removes any existing `Deny(path)` record
  (preserves current behavior through the new abstraction).
- `Deny(path)` removes any existing `Trust(path, contents)` record.
- `DefaultStore()` resolves to `XDG_DATA_HOME/aide/` (or
  `~/.local/share/aide/`) and namespaces into `trust/` + `deny/`.

### `internal/consent/` (new)

Unit tests:
- `Evidence.Digest()` is deterministic: same `Variants` + `Matches` in any
  order produce the same digest (canonical sort inside).
- `Evidence.Digest()` changes when any marker match flips.
- `Evidence.Digest()` changes when a variant is added or removed from the
  set.
- `ConsentHash` is order-insensitive over `variants`: `[npm, pnpm]` and
  `[pnpm, npm]` produce identical hash.
- `ConsentHash` changes with `projectRoot`, `capability`, `variants` set,
  or `evidenceDigest`.
- `Check` / `Grant` / `Revoke` round-trip on `t.TempDir()`.
- `Check` returns `NotGranted` when evidence digest differs from a stored
  grant, even if `(project, capability, variants)` matches.
- `Revoke(project, cap)` removes **all** records for that pair regardless
  of variant set or evidence digest.
- `Revoke` is idempotent (no error when nothing to remove).
- `List(project)` returns only records for that project (filters across all
  records in the store).
- Record body is human-readable text (asserts against format).

Integration tests (crossing filesystem + `approvalstore`):
- Full lifecycle across two processes: process A grants; process B in the
  same XDG dir sees `Granted`.
- Corrupted record file: `Check` returns `NotGranted`, no panic.
- `Grant` then manual filesystem change (record removed) → `Check` returns
  `NotGranted`.

### `internal/capability/` (extended)

- `Variant` struct merge: base `Writable`/`EnvAllow` plus selected variant's
  `Writable`/`EnvAllow` union correctly; no duplicates.
- `Marker` validation: exactly one of `File`/`Contains`/`GlobPath` must be
  set; struct with multiple set is a build-time or test-time error.
- `SelectVariants` decision table — one test per state (A–E):
  - A. First time, no pin, no override → calls `prompter`, returns
    approved variants, writes consent.
  - B. Stable, consent digest matches → silent, returns pinned variants,
    `prompter` not called.
  - C. Evidence changed, consent digest mismatch → calls `prompter`, new
    consent record written.
  - D. `.aide.yaml` pin present → silent, returns pinned variants,
    consent store not consulted.
  - E. `--variant` override → silent, returns override, config pin and
    consent store both ignored.
- Precedence test: `--variant` wins over `.aide.yaml` pin wins over
  detection wins over `DefaultVariants`.
- Non-interactive (`prompter == nil`): states A and C fall through to
  `DefaultVariants` with a warning; never auto-grant.
- `--yes` flag: states A and C auto-grant without prompting.
- `Customize` sub-flow: user approves subset of detected variants → only
  that subset is granted and persisted.
- `--variant cap=X` where `cap` not in `--with`: parse error.
- `--variant cap=unknown-variant`: error lists valid variants.
- `ToSandboxOverrides` with selected variants: the resulting
  `ReadableExtra`/`WritableExtra`/`EnvAllow` matches the union of base +
  variants; `Deny` never includes variant paths.

### Detector tests (per capability)

Table-driven tests with synthetic project roots in `t.TempDir()`:
- **python**: `uv.lock` alone → `[uv]`; `uv.lock` + `[tool.uv]` in
  `pyproject.toml` → `[uv]` (same variant, both markers matter for
  digest); `poetry.lock` alone → `[poetry]`; `environment.yml` alone →
  `[conda]`; `.python-version` alone → `[pyenv]`; no markers →
  `DefaultVariants` (`[venv]`); conda + uv both → `[conda, uv]`.
- **node**: `package-lock.json` → `[npm]`; `pnpm-lock.yaml` → `[pnpm]`;
  `yarn.lock` → `[yarn]`; `"packageManager"` key in `package.json` →
  adds `corepack`; `@playwright/test` dependency → adds `playwright`;
  `turbo.json` → adds `turbo`; realistic monorepo fixture (pnpm +
  corepack + playwright) → all three.
- **ruby**: `.ruby-version` → `[rbenv]`; `.tool-versions` → `[asdf]`;
  `Gemfile.lock` → adds `bundler`.
- Malformed marker files: truncated/binary `pyproject.toml`, unreadable
  `package.json` → detector returns no false positives and does not panic.
- Content-boundary: `pyproject.toml` with `[tool.uv]` past the 64KB read
  limit is not detected (matches existing behavior).
- Permission-denied files: detector skips them silently.

### Prompt rendering tests

- First-time prompt omits "Previously" line; subsequent-change prompt
  includes it with the prior variant set.
- Multi-variant consolidated prompt lists all approved variants and all
  markers in deterministic order.
- `[D]etails` expansion includes every path and env var grouped by variant.
- `[C]ustomize` flow loops through each detected variant, records per-
  variant yes/no, persists only the approved subset.
- `[S]kip` returns the default variants for this launch without writing to
  the consent store.
- Non-TTY input: prompt never blocks; returns "use default" outcome.

### `aide doctor` / preflight tests

- Clean project (all consents granted, no missing markers): exit 0.
- Unclaimed markers (e.g., `uv.lock` but `--with python` not active):
  warning with exit 1.
- Stale consent (digest mismatch simulated by mutating evidence): warning
  with exit 1.
- Unknown variant in `--variant`: exit 2.
- GPU suggestion: `moderngl` in a `.py` file triggers the suggestion;
  absence does not.
- `--format=json` output validates against schema.
- `aide doctor` runs without launching the agent (assert no agent process
  spawned in tests using a mock launcher).
- `--no-preflight` on launch path skips all checks.

### `gpu` capability & guard

- `--with gpu` alone activates `gpuGuard` and emits expected SBPL
  fragments (Mach services, IOKit operations, framework reads).
- Without `--with gpu`, `gpuGuard` is not in the rendered profile.
- `aide status` shows the GPU warning banner iff active.
- GPU is **never** auto-detected: a project with `moderngl` in its
  `pyproject.toml` does not auto-activate `gpu` even when preflight
  suggests it. (Regression guard against future drift.)
- Rendered profile compiles under `sandbox-exec -p` validation
  (integration test on darwin).

### Node migration tests

- Legacy `node-toolchain` guard is removed from `registry.go` — compile-
  time assertion via test that `GuardByName("node-toolchain")` returns
  `false`.
- `--with npm` emits deprecation warning and resolves to `--with node
  --variant node=npm` (captured via stderr assertion).
- `AIDE_SKIP_NODE_MIGRATION=1`: legacy behavior restored (all variants
  granted unconditionally, no prompt). Feature-flag test that also
  asserts the warning banner recommending removal.
- Detector output: `DetectProject` on a project with `package.json` now
  suggests `"node"` (not `"npm"`).
- Snapshot tests for the rendered seatbelt profile against a set of
  representative node projects (npm-only, pnpm monorepo, yarn + prisma,
  pnpm + playwright + turbo) to lock in the exact path set produced by
  each variant combination.

### CLI surface tests

- `aide cap list` includes variant count hint for capabilities that have
  variants.
- `aide cap show python` lists all five variants with markers and paths.
- `aide cap variants` produces flat list format.
- `aide cap consent list` filters by `--project`.
- `aide cap consent revoke python` removes all python consents for the
  current project.
- Shell-completion output for `--variant` contains only variants from
  capabilities present in `--with`.

### Existing-test regression gates

- Every test under `internal/trust/` passes without modification.
- Every test in `pkg/seatbelt/guards/` passes; the deleted
  `node-toolchain` guard's tests are replaced by the node variant tests
  (no net coverage loss).
- `internal/capability/detect_test.go`: existing python/go/rust/docker/etc.
  detection paths still return their canonical capability names
  (`"python"`, `"go"`, `"rust"`, `"docker"`); only `"npm"` → `"node"`
  changed, and that change has a dedicated test.
- Full seatbelt-profile snapshot tests for each built-in agent module
  (claude, aider, copilot, etc.) updated to reflect the new node variant
  rendering; diffs reviewed as part of the implementation PR.

### Coverage target

Maintain or exceed the repository's existing coverage threshold. New
packages (`approvalstore`, `consent`) target >90% line coverage given
their small, primitive nature.

## Open questions

- Preflight-by-default latency: measure at implementation time before
  deciding whether to flag-gate initial rollout.
- User-defined capabilities (`aide cap add`) declaring `Variants`: no
  special-casing planned; treat identically to built-ins.
- Whether `aide trust` should also trigger a consent-list review for the
  project (useful UX but adds coupling across aggregates). Defer.
