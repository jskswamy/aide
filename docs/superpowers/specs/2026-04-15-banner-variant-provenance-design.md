# Banner Variant + Provenance Revamp Design

**Date:** 2026-04-15
**Status:** Proposed
**Beads issue:** AIDE-j6m
**Related:** `2026-04-14-generic-toolchain-detection-design.md` (toolchain variants, shipped in v1.6.0)

## Problem

The aide startup banner shows `✓ python` regardless of which variant
actually applied. A user with `uv.lock` and a user with `.python-version`
see identical banners, but the granted paths differ fundamentally
(`~/.local/share/uv` vs `~/.pyenv`). The only way to discover the
active variant today is to run `aide cap consent list` — the banner,
which is supposed to be a trust-check surface, quietly hides a
material detail.

Beyond variants, v1.6.0 introduced several user-facing concepts the
banner does not reflect:

- **Provenance** — why a capability was activated (detected marker?
  pinned in `.aide.yaml`? overridden via `--variant`? default?)
- **Consent freshness** — is this a long-standing grant or one that
  was just interactively approved?
- **Detection evidence** — which file or glob triggered a suggestion
  or grant?

The three banner styles (`compact` / `clean` / `boxed`) today differ
mainly in decoration, not information density. That ambiguity is the
underlying reason the banner "needs revisiting" — adding variant
surfacing without rethinking the style purposes would just bolt
detail onto every mode.

## Goals

1. **Tier-based information map.** Three styles serve three distinct
   audiences: a 1-second trust check (`compact`), a 10-second launch
   review (`clean`), and a 20-second audit (`boxed`). Each tier's
   content is explicit, not an accident of visual choice.
2. **Critical info preserved in every mode.** Safety-critical signals
   (auto-approve, network mode, ad-hoc path grants) appear in every
   tier; they are never hidden by a narrower style.
3. **Variant + provenance visible at the right tier.** The active
   variant (`python[uv]`) appears in compact so users catch
   mis-selections in a one-second scan; the short provenance tag
   (`detected`, `pinned`, `--variant`, `default`) appears in clean so
   users understand *why*; the full evidence and consent timestamp
   appear in boxed for audit.
4. **Fresh consent discoverable.** A capability whose consent was
   written in this launch carries a `🆕` marker so users can catch
   mis-approvals before they silently stick.
5. **Non-TTY output is quiet by default.** Compact mode is forced
   when stdout is not a terminal, unless the user explicitly
   overrides via `--info-style` or `AIDE_INFO_STYLE`.
6. **Backwards compatible.** New fields on `CapabilityDisplay`
   default to zero values; existing tests and external callers that
   construct banner data without setting them produce unchanged
   output.

## Non-goals

- **Restructuring `BannerData` itself.** New data lives on
  `CapabilityDisplay`; the top-level shape is unchanged.
- **New style names.** The three existing styles (compact / clean /
  boxed) keep their names; only their content contracts sharpen.
- **Team-level config attribution** ("who added this capability to
  .aide.yaml"). Out of scope — not a banner concern; belongs in a
  project-doc tool if ever needed.
- **Interactive banner affordances** ("press D for details"). The
  banner remains a one-shot render at launch; inspection happens via
  `aide cap show` and friends.
- **GPU capability signalling.** When GPU ships, the banner will
  include it automatically (it's just another capability); no
  banner-design work is needed for that feature.

## Design

### Tier map

Every mode shows Tier 1. Clean and boxed add Tier 2. Only boxed adds
Tier 3.

**Tier 1 — Always visible (safety-critical):**

- Context name
- Agent (binary name)
- Sandbox network mode (`disabled` / `outbound` / `unrestricted`)
- Auto-approve flag
- Warnings (credential exposure, composition issues)
- Never-allow entries when non-empty
- Capability names with active variant (`python[uv]`,
  `node[pnpm + corepack]` for multi-variant)
- Fresh-grant indicator (`🆕`) on capabilities whose consent was
  written in this launch
- Extra writable / readable / denied paths (ad-hoc grants beyond
  capabilities)

**Tier 2 — Adds everyday detail (clean + boxed):**

- Match reason (why this context applied)
- Source tag when non-default: `← --with`, `← .aide.yaml`
- Short provenance tag per capability: `(detected)`, `(pinned)`,
  `(--variant)`, `(default)`
- Disabled caps (`○ python disabled ← --without`)
- Suggested caps (detected but not enabled, with one-line enable hint)

**Tier 3 — Audit detail (boxed only):**

- Per-capability granted paths (readable / writable lists)
- Per-capability env vars
- Full evidence summary (`uv.lock, [tool.uv] in pyproject.toml`)
- Consent timestamp (`confirmed 2026-04-15 · 14:22`)
- Guard details (overrides, protected paths)

### Rendered examples

`compact` — one concrete fixture:

```
aide · default · claude · sandbox: outbound
  ✓ python[uv] 🆕, github
  ⊕ readable: ~/.dolt/
⚡ AUTO-APPROVE
```

`clean` — same fixture:

```
aide · default
  Agent     claude
  Matched   project override on top of path glob: ~/source/github.com/jskswamy/*
  Sandbox   network outbound only
    ✓ python[uv] 🆕            (detected)
    ✓ github                   (--with)
    ⊕ readable: ~/.dolt/
    ○ git-remote               detected — aide --with git-remote
  ⚡ AUTO-APPROVE
```

`boxed` — same fixture:

```
┌─ aide ───────────────────────────────────────
│ 🎯 Context   default
│ 📁 Matched   project override on top of path glob: ~/source/github.com/jskswamy/*
│ 🤖 Agent     claude → /usr/bin/sandbox-exec
│ 🛡 sandbox:  network outbound only
│    ✓ python[uv] 🆕          (detected)
│       evidence:  uv.lock, [tool.uv] in pyproject.toml
│       confirmed: 2026-04-15 · 14:22
│       writable:  ~/.local/share/uv, ~/.cache/uv
│       env:       UV_CACHE_DIR, UV_PYTHON_INSTALL_DIR, VIRTUAL_ENV
│    ✓ github                 (--with)
│       writable:  ~/.config/gh
│       env:       GITHUB_TOKEN, GH_TOKEN
│    ⊕ readable:  ~/.dolt/
│    ○ git-remote             detected evidence: [remote in .git/config
│                             enable: aide --with git-remote
│ ⚡ AUTO-APPROVE — all agent actions execute without confirmation
└──────────────────────────────────────────────────
```

### Symbol vocabulary

- `✓` = active capability
- `○` = suggested, not enabled
- `⊕` = ad-hoc writable/readable path (beyond capabilities)
- `⊘` = denied path (never-allow or explicit deny)
- `⚡` = auto-approve flag
- `🆕` = fresh consent recorded in this launch
- Variant in square brackets after cap name: `python[uv]`, multi-variant
  joined with ` + ` (space-plus-space): `node[pnpm + corepack]`
- Provenance tag in parentheses, short vocabulary: `detected`,
  `pinned`, `--variant`, `default`

### Data model changes

Extend `internal/ui/types.go:CapabilityDisplay` with five fields; no
new top-level `BannerData` fields.

```go
type CapabilityDisplay struct {
    Name        string
    Paths       []string
    EnvVars     []string
    Source      string
    Disabled    bool
    Suggested   bool
    // NEW:
    Variants        []string  // ["uv"] or ["pnpm","corepack"]; nil for no-variant caps
    ProvenanceTag   string    // "detected" | "pinned" | "--variant" | "default"; "" for no-variant caps
    FreshGrant      bool      // true when consent was written this launch
    EvidenceSummary string    // Tier 3 only: e.g. "uv.lock, [tool.uv] in pyproject.toml"
    ConfirmedAt     time.Time // Tier 3 only; zero when no stored grant exists
    DetectionHint   string    // for Suggested caps: evidence that fired
}
```

New fields default to zero values; callers that build
`CapabilityDisplay` without touching them render unchanged banners.

### Wiring

- `Variants` / `ProvenanceTag` — `capability.SelectVariants` already
  returns `Provenance{Variants, Reason}`. The launcher resolve step
  (around `launcher.go:254`) collects a
  `map[string]capability.Provenance` keyed by capability name.
- `FreshGrant` — set when `Provenance.Reason == "consent:granted"`
  (not `consent:stable`).
- `EvidenceSummary` — comes from `consent.Evidence.Summary` via
  `summarizeEvidence` inside `SelectVariants`; propagate through
  `Provenance`.
- `ConfirmedAt` — look up via `consent.Store.List(projectRoot)`,
  filter by capability, take most recent grant's `ConfirmedAt`.
  Gated by style — only queried when style == "boxed" so non-audit
  modes pay zero I/O cost.
- `DetectionHint` — for suggested-not-enabled caps, run
  `DetectEvidence` on the capability and surface the first matched
  marker's summary.

### Provenance tag mapping

Small helper function living in `internal/ui/funcmap.go` or
`internal/ui/types.go`:

```go
// provenanceTag maps a capability.Provenance.Reason to the short
// human-readable tag shown in Tier 2 (clean + boxed).
func provenanceTag(reason string) string {
    switch reason {
    case "consent:granted", "consent:stable":
        return "detected"
    case "yaml-pin":
        return "pinned"
    case "cli-override":
        return "--variant"
    case "default:no-evidence", "default:declined",
         "default:skipped", "default:non-interactive":
        return "default"
    }
    return ""
}
```

### Non-TTY auto-downgrade

At the callsite that resolves effective info style (likely in
`cmd/aide/main.go` or `cmd/aide/status.go` where `prefs.InfoStyle`
is read):

```go
// Explicit override (--info-style or AIDE_INFO_STYLE) wins.
// Otherwise, non-TTY stdout forces compact regardless of config.
style := prefs.InfoStyle
if !explicitOverride && !isInteractiveTerminal(os.Stdout) {
    style = "compact"
}
```

The existing `isInteractiveTerminal` helper in `cmd/aide/main.go`
(from the cmdEnv work) covers the check; the only addition is
applying it to banner style resolution.

### Template changes

`internal/ui/templates/compact.tmpl` — add variant suffix,
fresh-grant marker to the cap-name list. Keep the single-line
`⊕ readable:` for extras.

`internal/ui/templates/clean.tmpl` — extend the per-cap line with
variant suffix, fresh-grant marker, and provenance tag in
parentheses.

`internal/ui/templates/boxed.tmpl` — extend the per-cap block with
`evidence:` and `confirmed:` lines when those fields are populated
(they're Tier-3-only, so data is only populated when style ==
"boxed").

Two new template helpers in `internal/ui/funcmap.go`:

```go
// variantSuffix returns "[uv]" or "[pnpm + corepack]" for a slice;
// "" for nil or empty. Multi-variant joins with " + ".
func variantSuffix(variants []string) string

// freshMarker returns " 🆕" when fresh is true; "" otherwise.
// Kept as a helper for style consistency and easy localisation later.
func freshMarker(fresh bool) string
```

## Testing

### Template golden tests

Fixture-driven. One `BannerData` per scenario, rendered through all
three templates, matched against golden files:

- Fresh consent grant (`FreshGrant=true`)
- Stable consent (`FreshGrant=false, ProvenanceTag="detected"`)
- YAML pin (`ProvenanceTag="pinned"`)
- CLI override (`ProvenanceTag="--variant"`)
- Default fallback (`ProvenanceTag="default"`, no variant)
- Multi-variant (`Variants=["pnpm","corepack"]`)
- No-variant cap (`Variants=nil`, `ProvenanceTag=""`)
- All-together (multiple caps, a suggested cap, extras, warnings,
  auto-approve — the "kitchen sink" fixture)

### Helper unit tests

- `provenanceTag` — table over every `Reason` value (including
  unknowns, which should return `""`).
- `variantSuffix` — `nil → ""`, `[]string{} → ""`, `["uv"] → "[uv]"`,
  `["pnpm","corepack"] → "[pnpm + corepack]"`.
- `freshMarker` — trivial bool-to-string.

### Non-TTY auto-downgrade test

Stub `isInteractiveTerminal` with controllable input; assert:

- TTY + no explicit override → user's configured style preserved
- Non-TTY + no explicit override → compact
- Non-TTY + explicit `--info-style=boxed` → boxed (explicit wins)
- Non-TTY + explicit `AIDE_INFO_STYLE=clean` → clean (explicit wins)

### Integration test

In `cmd/aide`, construct a temp project with `uv.lock`, run `aide
status --info-style=clean`, assert the rendered output contains:

- `python[uv]`
- `🆕` (fresh grant)
- `(detected)` (provenance tag)

### Regression gate

Existing `internal/ui/banner_test.go` tests must pass unchanged. The
new fields default to zero values; fixtures that don't set them
produce banners identical to today's output (minus the already-expected
format tweaks the templates now support — if any existing test's
golden file encodes `✓ python` and the cap has no variant, the
output still reads `✓ python`).

## Threat Model

No trust-boundary changes. The banner is a read-only display
surface; it writes no files, makes no network calls, grants no
permissions. Threats to consider are purely informational:

- **T1 — Fresh-grant marker misattribution.** A user might over-trust
  a `🆕` or under-trust its absence. Mitigation: the marker is
  documented in the style guide (`docs/` content, not in scope for
  this spec directly). The symbol appears only when
  `Provenance.Reason == "consent:granted"` in this specific
  invocation — it's mechanically accurate.
- **T2 — Consent timestamp staleness.** The `ConfirmedAt` shown in
  boxed mode comes from the consent store; if the store is tampered
  with, the timestamp lies. Mitigation: the consent store already
  lives under `XDG_DATA_HOME/aide/consent/` with the same trust
  boundary as the rest of aide's approval state. No new surface.
- **T3 — Cross-cap evidence leak.** The `EvidenceSummary` shown in
  boxed mode contains marker strings from project files. These are
  already exposed via `aide cap consent list`. No new exposure.

## Migration

**Backwards compatible.** The new fields default to zero values;
existing banner fixtures and templates render unchanged. The
template extensions are additive (e.g., `{{if .Variants}}[{{join .Variants " + "}}]{{end}}`);
when `Variants` is nil the extension emits nothing.

No flag day. Ship as a single feature branch + release.

## Alternatives considered

- **Separate top-level field on `BannerData`** (e.g., `Provenance
  map[string]string`). Rejected — coupling variant and provenance to
  individual capability lines makes the data model match the
  rendering structure. Separate maps would require lookups during
  template iteration.
- **Drop the three-style split down to two (scan + audit).**
  Evaluated during brainstorming; user requested we keep three with
  sharpened purposes. The middle mode (`clean`) has a genuine role
  as the everyday launch view.
- **Inline the provenance tag without a short vocabulary** (e.g.,
  render `Reason` verbatim: "consent:granted"). Rejected — the raw
  reason strings are implementation details, not user-facing
  vocabulary. A short mapped tag is more stable against future
  `Reason` additions.
- **Surface consent-prompt availability** ("press Y to approve
  uv variant" inline in banner). Rejected — the consent prompt
  happens *before* banner render, not alongside it. The banner is a
  post-decision summary.
- **Render marker evidence in clean mode too.** Rejected — marker
  strings can be long (k8s: four glob-contains markers) and clutter
  the 10-second view. Users who want them open boxed.

## Rollout

1. **Phase 1 — Data model + helpers.** Extend `CapabilityDisplay`,
   add `provenanceTag` / `variantSuffix` / `freshMarker` helpers,
   unit-test the helpers.
2. **Phase 2 — Templates.** Update compact / clean / boxed templates
   to consume the new fields. Golden tests for every scenario.
3. **Phase 3 — Wiring.** Launcher collects `Provenance` per
   capability, populates `CapabilityDisplay` fields. Conditional
   `consent.Store.List` only when style == "boxed".
4. **Phase 4 — Non-TTY auto-downgrade.** Add the check at the
   banner-style resolution callsite. Unit-test the four combinations.
5. **Phase 5 — Integration test.** Temp project + `aide status` →
   assert key tokens.
6. **Close AIDE-j6m.** Open follow-ups for:
   - "style guide doc" explaining the symbol vocabulary (if none
     exists today)
   - "consent-prompt UX gap: error message for `--variant bare-name`
     doesn't show the `capability=variant` form" (observed during
     v1.6.0 shake-out)

## Open questions

- **Emoji vs ASCII fallback.** The design uses `✓ ○ ⊕ ⊘ ⚡ 🆕`.
  Existing templates already use some of these; `🆕` is new. If
  `!isInteractiveTerminal` or `NO_COLOR` affect symbol rendering
  today, check and apply the same rule to `🆕` (likely fall back to
  `*` or leave blank). Worth confirming at implementation time but
  not a spec-level concern.
- **`confirmed` timestamp format.** Spec uses `2026-04-15 · 14:22`
  (local time, middle-dot separator). Could also use RFC3339.
  Readability wins here; RFC3339 is verbose for a banner.
- **Multi-cap inline on compact** — current compact mode puts all
  active caps on one `✓ ...` line separated by commas. If a project
  has 8 capabilities, that line gets long. Accept wrapping? Or
  switch to stacked lines once N ≥ some threshold? Defer — report
  after real use.
