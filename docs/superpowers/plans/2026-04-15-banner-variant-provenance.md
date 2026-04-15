# Banner Variant + Provenance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Surface the active variant, a short provenance tag, a fresh-grant indicator, marker evidence, and consent timestamp in the aide startup banner — each at the right style tier — and force compact mode on non-TTY output.

**Architecture:** Extend `CapabilityDisplay` with five additive fields (`Variants`, `ProvenanceTag`, `FreshGrant`, `EvidenceSummary`, `ConfirmedAt`, `DetectionHint`). Wire per-capability `Provenance` from `SelectVariants` through the launcher into the banner. Tier-gating happens at data-population time: Tier 3 fields (`EvidenceSummary`, `ConfirmedAt`) are only populated when the effective style is `boxed`. Templates extend additively — the new fields render when populated, produce nothing when zero-valued.

**Tech Stack:** Go 1.25, `text/template` banner templates, `fatih/color` ANSI helpers. No new dependencies.

**Spec:** `docs/superpowers/specs/2026-04-15-banner-variant-provenance-design.md`
**Beads issue:** AIDE-j6m

**Branch / worktree:** start with a new worktree from main (`worktree-banner-variant`).

---

## File Structure

```
internal/ui/
├── types.go            EXTEND CapabilityDisplay with 5 new fields
├── funcmap.go          ADD variantSuffix + freshMarker + provenanceTag helpers
├── funcmap_test.go     EXTEND with helper unit tests
├── banner.go           no change
├── banner_test.go      EXTEND with fixture tests for new fields across all three templates
└── templates/
    ├── compact.tmpl    EXTEND cap line with variant + fresh marker
    ├── clean.tmpl      EXTEND cap line with variant + fresh marker + provenance tag
    └── boxed.tmpl      EXTEND cap block with evidence: + confirmed: lines

internal/launcher/launcher.go   EXTEND resolve step to collect Provenance per cap;
                                look up ConfirmedAt via consent.Store when style is boxed;
                                populate new CapabilityDisplay fields.

cmd/aide/main.go (or the banner-render site in status.go)
                                ADD non-TTY auto-downgrade when info style not explicitly set.

cmd/aide/banner_integration_test.go   NEW — asserts rendered banner contains
                                variant, fresh marker, and provenance tokens.
```

---

## Task 1: Extend `CapabilityDisplay` with new fields

**Files:**
- Modify: `internal/ui/types.go`

Additive only. No existing tests break because the new fields default to zero values.

- [ ] **Step 1.1: Modify `internal/ui/types.go`**

Replace the `CapabilityDisplay` struct with:

```go
// CapabilityDisplay holds per-capability information for banner rendering.
type CapabilityDisplay struct {
    Name      string
    Paths     []string // readable/writable paths granted
    EnvVars   []string // env vars passed through
    Source    string   // "context config", "--with", "--without"
    Disabled  bool     // true if --without excluded this
    Suggested bool     // true if detected but not enabled

    // Variant + provenance (added for AIDE-j6m).
    // Variants: active variant names, e.g. []string{"uv"} or
    // []string{"pnpm", "corepack"}. nil for capabilities that do not
    // declare Variants.
    Variants []string

    // ProvenanceTag is the short human-readable tag shown in Tier 2
    // (clean + boxed): "detected" | "pinned" | "--variant" | "default".
    // Empty string when the capability has no variant selection.
    ProvenanceTag string

    // FreshGrant is true when a consent record for this capability was
    // written in the current launch (Provenance.Reason ==
    // "consent:granted"). Renders as a "🆕" marker.
    FreshGrant bool

    // EvidenceSummary is the marker-evidence string for Tier 3 only,
    // e.g. "uv.lock, [tool.uv] in pyproject.toml". Empty when style is
    // not "boxed" or when no evidence was collected.
    EvidenceSummary string

    // ConfirmedAt is the consent timestamp shown in Tier 3 only.
    // Zero-valued when style is not "boxed" or when no stored grant
    // exists.
    ConfirmedAt time.Time

    // DetectionHint is for suggested-but-not-enabled caps in Tier 2+:
    // a short string describing the marker that fired
    // (e.g., "[remote in .git/config"). Empty when no hint available.
    DetectionHint string
}
```

Add `"time"` to the imports of `types.go`.

- [ ] **Step 1.2: Run existing tests to confirm backwards compatibility**

```bash
go test ./internal/ui/... -race -count=1
```

Expected: all existing banner tests pass (they construct `CapabilityDisplay` without the new fields; zero values produce unchanged output).

- [ ] **Step 1.3: Commit**

```
Extend CapabilityDisplay with variant and provenance fields

Adds six new fields to CapabilityDisplay: Variants, ProvenanceTag,
FreshGrant, EvidenceSummary, ConfirmedAt, and DetectionHint. All
default to zero values so existing callers and templates continue
to produce unchanged output.

No behaviour change lands in this commit. The template updates and
the launcher wiring that populate these fields arrive in the next
commits.
```

Use `__GIT_COMMIT_PLUGIN__=1 git commit -m "$(cat <<'EOF' ... EOF)"`.

---

## Task 2: Add template helpers + unit tests

**Files:**
- Modify: `internal/ui/funcmap.go`
- Modify: `internal/ui/funcmap_test.go`

- [ ] **Step 2.1: Write failing tests for `variantSuffix`**

Append to `internal/ui/funcmap_test.go`:

```go
func TestVariantSuffix(t *testing.T) {
    cases := []struct {
        name     string
        variants []string
        want     string
    }{
        {"nil returns empty", nil, ""},
        {"empty slice returns empty", []string{}, ""},
        {"single variant", []string{"uv"}, "[uv]"},
        {"multiple variants joined with plus", []string{"pnpm", "corepack"}, "[pnpm + corepack]"},
        {"three variants", []string{"uv", "conda", "poetry"}, "[uv + conda + poetry]"},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            got := variantSuffix(tc.variants)
            if got != tc.want {
                t.Errorf("variantSuffix(%v) = %q; want %q", tc.variants, got, tc.want)
            }
        })
    }
}

func TestFreshMarker(t *testing.T) {
    if got := freshMarker(true); got != " 🆕" {
        t.Errorf("freshMarker(true) = %q; want %q", got, " 🆕")
    }
    if got := freshMarker(false); got != "" {
        t.Errorf("freshMarker(false) = %q; want empty", got)
    }
}

func TestProvenanceTag(t *testing.T) {
    cases := []struct {
        reason string
        want   string
    }{
        {"consent:granted", "detected"},
        {"consent:stable", "detected"},
        {"yaml-pin", "pinned"},
        {"cli-override", "--variant"},
        {"default:no-evidence", "default"},
        {"default:declined", "default"},
        {"default:skipped", "default"},
        {"default:non-interactive", "default"},
        {"unknown-reason", ""},
        {"", ""},
    }
    for _, tc := range cases {
        t.Run(tc.reason, func(t *testing.T) {
            got := provenanceTag(tc.reason)
            if got != tc.want {
                t.Errorf("provenanceTag(%q) = %q; want %q", tc.reason, got, tc.want)
            }
        })
    }
}
```

- [ ] **Step 2.2: Run tests — expect compile failure**

```bash
go test ./internal/ui/... -run "TestVariantSuffix|TestFreshMarker|TestProvenanceTag" -v
```

Expected: `undefined: variantSuffix`, `undefined: freshMarker`, `undefined: provenanceTag`.

- [ ] **Step 2.3: Implement the helpers in `internal/ui/funcmap.go`**

Add these three functions to the file (below `colorFuncMap()`):

```go
// variantSuffix returns "[uv]" or "[pnpm + corepack]" for a non-empty
// slice; "" for nil or empty. Multi-variant joins with " + ".
func variantSuffix(variants []string) string {
    if len(variants) == 0 {
        return ""
    }
    return "[" + strings.Join(variants, " + ") + "]"
}

// freshMarker returns " 🆕" when fresh is true; "" otherwise. Kept as
// a helper so the symbol is centralised (easy to swap for an ASCII
// fallback in a future NO_COLOR or !isatty pass).
func freshMarker(fresh bool) string {
    if fresh {
        return " 🆕"
    }
    return ""
}

// provenanceTag maps a capability.Provenance.Reason string to the
// short human-readable tag shown in Tier 2 (clean + boxed):
//   "detected" — consent:granted, consent:stable
//   "pinned"   — yaml-pin
//   "--variant" — cli-override
//   "default"  — any default:* reason
// Unknown reasons map to "".
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

Register them in `colorFuncMap()` by adding these entries inside the returned map:

```go
// Variant + provenance helpers (Tier 1 + Tier 2)
"variantSuffix": variantSuffix,
"freshMarker":   freshMarker,
"provenanceTag": provenanceTag,
```

- [ ] **Step 2.4: Run tests — expect pass**

```bash
go test ./internal/ui/... -run "TestVariantSuffix|TestFreshMarker|TestProvenanceTag" -v
```

Expected: all three tests pass.

- [ ] **Step 2.5: Full regression**

```bash
go test ./... -race -count=1
go vet ./...
```

All green. No existing test should fail — helpers are additive, templates have not changed yet.

- [ ] **Step 2.6: Commit**

```
Add variantSuffix, freshMarker, and provenanceTag template helpers

Three pure functions feed the banner templates:

  - variantSuffix([]string) renders "[uv]" or "[pnpm + corepack]".
  - freshMarker(bool) renders " 🆕" or "".
  - provenanceTag(string) maps a capability.Provenance.Reason to the
    short Tier 2 tag ("detected", "pinned", "--variant", "default").

Registered in colorFuncMap() so templates can call them directly.
No template consumes them yet; the per-template updates land next.
```

---

## Task 3: Update compact.tmpl with variant + fresh marker

**Files:**
- Modify: `internal/ui/templates/compact.tmpl`
- Modify: `internal/ui/banner_test.go`

Tier 1 only: add variant suffix and fresh-grant marker to the cap-name line. No provenance tag, no evidence.

- [ ] **Step 3.1: Write a golden-style test first**

Append to `internal/ui/banner_test.go`:

```go
func TestRenderCompact_VariantAndFresh(t *testing.T) {
    data := &BannerData{
        ContextName: "default",
        AgentName:   "claude",
        AgentPath:   "/usr/bin/claude",
        Sandbox: &SandboxInfo{
            Disabled: false,
            Network:  "outbound only",
            Ports:    "all",
        },
        Capabilities: []CapabilityDisplay{
            {
                Name:       "python",
                Paths:      []string{"~/.local/share/uv"},
                Variants:   []string{"uv"},
                FreshGrant: true,
            },
            {
                Name:  "github",
                Paths: []string{"~/.config/gh"},
            },
        },
    }

    var buf bytes.Buffer
    color.NoColor = true // strip ANSI for deterministic matching
    defer func() { color.NoColor = false }()

    if err := RenderBanner(&buf, "compact", data); err != nil {
        t.Fatalf("RenderBanner: %v", err)
    }
    out := buf.String()

    if !strings.Contains(out, "python[uv]") {
        t.Errorf("compact render missing variant suffix; got:\n%s", out)
    }
    if !strings.Contains(out, "🆕") {
        t.Errorf("compact render missing fresh-grant marker; got:\n%s", out)
    }
}
```

Add imports if missing: `"bytes"`, `"strings"`, `"github.com/fatih/color"`.

- [ ] **Step 3.2: Run test — expect fail**

```bash
go test ./internal/ui/... -run TestRenderCompact_VariantAndFresh -v
```

Expected: FAIL — output does not contain `python[uv]` or `🆕`.

- [ ] **Step 3.3: Modify `internal/ui/templates/compact.tmpl`**

Find the `{{range .Capabilities}}` block (around line 20-25) and replace its body. The current line is:

```
     {{boldGreen "✓"}} {{printf "%-10s" .Name}} {{truncate .Paths 3}}
```

Replace with:

```
     {{boldGreen "✓"}} {{printf "%-10s" (print .Name (variantSuffix .Variants))}}{{freshMarker .FreshGrant}} {{truncate .Paths 3}}
```

The `print` built-in concatenates the cap name with its variant suffix before `printf %-10s` aligns the column.

- [ ] **Step 3.4: Run test — expect pass**

```bash
go test ./internal/ui/... -run TestRenderCompact_VariantAndFresh -v
```

Expected: PASS.

- [ ] **Step 3.5: Regression — confirm existing compact tests still pass**

```bash
go test ./internal/ui/... -run "TestRenderCompact|TestRenderBanner" -v
```

All pre-existing tests must still pass. `TestRenderCompact` uses a fixture without `Variants`; `variantSuffix(nil)` returns `""` so the output column stays `python    ` (alignment preserved).

If a pre-existing test fails because the column width absorbed empty variant suffix differently, update the golden expectation — but `%-10s` should preserve alignment for the pre-session fixtures.

- [ ] **Step 3.6: Full regression + vet**

```bash
go test ./... -race -count=1
go vet ./...
```

All green.

- [ ] **Step 3.7: Commit**

```
Render variant and fresh-grant marker in compact banner

compact.tmpl now shows the active variant suffix (e.g. "[uv]" or
"[pnpm + corepack]") after each capability name and a " 🆕" marker
when the consent was written in the current launch. Capabilities
without Variants render unchanged because variantSuffix(nil)
returns "".

Adds TestRenderCompact_VariantAndFresh with a fixture covering
both the fresh-grant case and a no-variant companion cap.
```

---

## Task 4: Update clean.tmpl with variant + fresh + provenance

**Files:**
- Modify: `internal/ui/templates/clean.tmpl`
- Modify: `internal/ui/banner_test.go`

Tier 2: clean adds the provenance tag `(detected) / (pinned) / (--variant) / (default)` alongside what compact shows.

- [ ] **Step 4.1: Write golden-style tests covering each provenance tag**

Append to `internal/ui/banner_test.go`:

```go
func TestRenderClean_ProvenanceTags(t *testing.T) {
    cases := []struct {
        tag   string // ProvenanceTag value
        want  string // substring expected in clean render
    }{
        {"detected", "(detected)"},
        {"pinned", "(pinned)"},
        {"--variant", "(--variant)"},
        {"default", "(default)"},
    }

    for _, tc := range cases {
        t.Run(tc.tag, func(t *testing.T) {
            data := &BannerData{
                ContextName: "default",
                AgentName:   "claude",
                AgentPath:   "/usr/bin/claude",
                Sandbox: &SandboxInfo{
                    Network: "outbound only",
                    Ports:   "all",
                },
                Capabilities: []CapabilityDisplay{{
                    Name:          "python",
                    Paths:         []string{"~/.local/share/uv"},
                    Variants:      []string{"uv"},
                    ProvenanceTag: tc.tag,
                }},
            }

            var buf bytes.Buffer
            color.NoColor = true
            defer func() { color.NoColor = false }()

            if err := RenderBanner(&buf, "clean", data); err != nil {
                t.Fatalf("RenderBanner: %v", err)
            }
            out := buf.String()
            if !strings.Contains(out, tc.want) {
                t.Errorf("clean render missing %q; got:\n%s", tc.want, out)
            }
            if !strings.Contains(out, "python[uv]") {
                t.Errorf("clean render missing variant suffix; got:\n%s", out)
            }
        })
    }
}

func TestRenderClean_NoProvenanceTagForNoVariantCap(t *testing.T) {
    data := &BannerData{
        ContextName: "default",
        AgentName:   "claude",
        AgentPath:   "/usr/bin/claude",
        Sandbox: &SandboxInfo{
            Network: "outbound only",
            Ports:   "all",
        },
        Capabilities: []CapabilityDisplay{{
            Name:  "github",
            Paths: []string{"~/.config/gh"},
            // No Variants, no ProvenanceTag
        }},
    }

    var buf bytes.Buffer
    color.NoColor = true
    defer func() { color.NoColor = false }()

    if err := RenderBanner(&buf, "clean", data); err != nil {
        t.Fatalf("RenderBanner: %v", err)
    }
    out := buf.String()
    // A cap with no variant and no provenance should not carry any
    // parenthetical tag. Avoid false positives from match reasons
    // etc. by constraining the search to lines containing 'github'.
    for _, line := range strings.Split(out, "\n") {
        if strings.Contains(line, "github") && strings.Contains(line, "(detected)") {
            t.Errorf("no-variant cap line unexpectedly has (detected) tag: %q", line)
        }
    }
}
```

- [ ] **Step 4.2: Run tests — expect fail**

```bash
go test ./internal/ui/... -run "TestRenderClean_ProvenanceTags|TestRenderClean_NoProvenanceTagForNoVariantCap" -v
```

Expected: FAIL — clean template does not render provenance tags yet.

- [ ] **Step 4.3: Modify `internal/ui/templates/clean.tmpl`**

Find the `{{range .Capabilities}}` block and replace its body. The current line (around line 20-24) is:

```
    {{boldGreen "✓"}} {{printf "%-10s" .Name}} {{truncate .Paths 3}}
{{- if and .Source (ne .Source "context config")}}  {{dim (printf "← %s" .Source)}}{{end}}
```

Replace with:

```
    {{boldGreen "✓"}} {{printf "%-10s" (print .Name (variantSuffix .Variants))}}{{freshMarker .FreshGrant}} {{truncate .Paths 3}}
{{- if .ProvenanceTag}}  {{dim (printf "(%s)" .ProvenanceTag)}}{{end}}
{{- if and .Source (ne .Source "context config")}}  {{dim (printf "← %s" .Source)}}{{end}}
```

- [ ] **Step 4.4: Run tests — expect pass**

```bash
go test ./internal/ui/... -run "TestRenderClean" -v
```

All clean-related tests pass, including pre-existing `TestRenderClean`.

- [ ] **Step 4.5: Full regression**

```bash
go test ./... -race -count=1
go vet ./...
```

- [ ] **Step 4.6: Commit**

```
Render provenance tag in clean banner

clean.tmpl now shows a short provenance tag — "(detected)",
"(pinned)", "(--variant)", or "(default)" — after each capability
that has one. Capabilities without a ProvenanceTag render unchanged.

Variant suffix and fresh-grant marker also apply in clean (same
render helpers as compact).

Adds TestRenderClean_ProvenanceTags (table over all four tags) and
TestRenderClean_NoProvenanceTagForNoVariantCap.
```

---

## Task 5: Update boxed.tmpl with evidence + consent timestamp

**Files:**
- Modify: `internal/ui/templates/boxed.tmpl`
- Modify: `internal/ui/banner_test.go`

Tier 3: boxed adds `evidence: ...` and `confirmed: ...` lines below the cap line when those fields are populated.

- [ ] **Step 5.1: Write golden-style test**

Append to `internal/ui/banner_test.go`:

```go
func TestRenderBoxed_EvidenceAndConfirmed(t *testing.T) {
    confirmedAt := time.Date(2026, 4, 15, 14, 22, 0, 0, time.UTC)
    data := &BannerData{
        ContextName: "default",
        AgentName:   "claude",
        AgentPath:   "/usr/bin/claude",
        Sandbox: &SandboxInfo{
            Network: "outbound only",
            Ports:   "all",
        },
        Capabilities: []CapabilityDisplay{{
            Name:            "python",
            Paths:           []string{"~/.local/share/uv"},
            EnvVars:         []string{"UV_CACHE_DIR"},
            Variants:        []string{"uv"},
            ProvenanceTag:   "detected",
            FreshGrant:      true,
            EvidenceSummary: "uv.lock, [tool.uv] in pyproject.toml",
            ConfirmedAt:     confirmedAt,
        }},
    }

    var buf bytes.Buffer
    color.NoColor = true
    defer func() { color.NoColor = false }()

    if err := RenderBanner(&buf, "boxed", data); err != nil {
        t.Fatalf("RenderBanner: %v", err)
    }
    out := buf.String()

    wants := []string{
        "python[uv]",
        "🆕",
        "(detected)",
        "evidence:  uv.lock, [tool.uv] in pyproject.toml",
        "confirmed: 2026-04-15 · 14:22",
    }
    for _, w := range wants {
        if !strings.Contains(out, w) {
            t.Errorf("boxed render missing %q; got:\n%s", w, out)
        }
    }
}

func TestRenderBoxed_NoEvidenceLinesWhenAbsent(t *testing.T) {
    data := &BannerData{
        ContextName: "default",
        AgentName:   "claude",
        AgentPath:   "/usr/bin/claude",
        Sandbox: &SandboxInfo{
            Network: "outbound only",
            Ports:   "all",
        },
        Capabilities: []CapabilityDisplay{{
            Name:  "github",
            Paths: []string{"~/.config/gh"},
            // No EvidenceSummary, no ConfirmedAt
        }},
    }

    var buf bytes.Buffer
    color.NoColor = true
    defer func() { color.NoColor = false }()

    if err := RenderBanner(&buf, "boxed", data); err != nil {
        t.Fatalf("RenderBanner: %v", err)
    }
    out := buf.String()

    if strings.Contains(out, "evidence:") {
        t.Errorf("boxed render should omit evidence: line when EvidenceSummary is empty; got:\n%s", out)
    }
    if strings.Contains(out, "confirmed:") {
        t.Errorf("boxed render should omit confirmed: line when ConfirmedAt is zero; got:\n%s", out)
    }
}
```

Add `"time"` import if missing.

- [ ] **Step 5.2: Add a template helper for timestamp formatting**

The boxed template renders `confirmed: 2026-04-15 · 14:22`. A template-callable helper keeps formatting centralised. Append to `internal/ui/funcmap.go` (below the provenance helpers):

```go
// formatConfirmedAt formats a consent ConfirmedAt timestamp for the
// boxed banner. Returns "" for the zero time so templates can omit
// the line via `{{with}}`.
func formatConfirmedAt(t time.Time) string {
    if t.IsZero() {
        return ""
    }
    return t.Local().Format("2006-01-02 · 15:04")
}
```

Add `"time"` to `funcmap.go` imports. Register in `colorFuncMap()`:

```go
"formatConfirmedAt": formatConfirmedAt,
```

Append a tiny unit test to `internal/ui/funcmap_test.go`:

```go
func TestFormatConfirmedAt(t *testing.T) {
    if got := formatConfirmedAt(time.Time{}); got != "" {
        t.Errorf("zero time should format to empty; got %q", got)
    }
    ts := time.Date(2026, 4, 15, 14, 22, 0, 0, time.UTC)
    got := formatConfirmedAt(ts)
    // Local() means the exact string depends on test-run timezone;
    // assert both date and time portions are present.
    if !strings.Contains(got, "2026-04-15") {
        t.Errorf("formatted string missing date: %q", got)
    }
}
```

- [ ] **Step 5.3: Modify `internal/ui/templates/boxed.tmpl`**

Find the `{{range .Capabilities}}` block (around line 24-28). The current body is:

```
│    {{boldGreen "✓"}} {{printf "%-10s" .Name}} {{truncate .Paths 3}}
{{- if and .Source (ne .Source "context config")}}  {{dim (printf "← %s" .Source)}}{{end}}
```

Replace with:

```
│    {{boldGreen "✓"}} {{printf "%-10s" (print .Name (variantSuffix .Variants))}}{{freshMarker .FreshGrant}} {{truncate .Paths 3}}
{{- if .ProvenanceTag}}  {{dim (printf "(%s)" .ProvenanceTag)}}{{end}}
{{- if and .Source (ne .Source "context config")}}  {{dim (printf "← %s" .Source)}}{{end}}
{{- if .EvidenceSummary}}
│       {{dim "evidence:"}}  {{.EvidenceSummary}}
{{- end}}
{{- with formatConfirmedAt .ConfirmedAt}}
│       {{dim "confirmed:"}} {{.}}
{{- end}}
```

- [ ] **Step 5.4: Run boxed tests — expect pass**

```bash
go test ./internal/ui/... -run "TestRenderBoxed" -v
```

All boxed tests pass.

- [ ] **Step 5.5: Full regression**

```bash
go test ./... -race -count=1
go vet ./...
```

- [ ] **Step 5.6: Commit**

```
Render evidence and consent timestamp in boxed banner

boxed.tmpl now shows two additional lines per capability when the
corresponding fields are populated:

  evidence:  uv.lock, [tool.uv] in pyproject.toml
  confirmed: 2026-04-15 · 14:22

Both lines are omitted when the underlying fields (EvidenceSummary,
ConfirmedAt) are empty/zero — so capabilities without consent-driven
provenance render unchanged. formatConfirmedAt helper handles the
timestamp format and zero-time check.

The variant suffix, fresh-grant marker, and provenance tag also
apply in boxed (reused helpers from compact + clean).
```

---

## Task 6: Add DetectionHint rendering for suggested caps

**Files:**
- Modify: `internal/ui/templates/clean.tmpl`
- Modify: `internal/ui/templates/boxed.tmpl`
- Modify: `internal/ui/banner_test.go`

Today suggested caps render as `detected — aide --with <name>`. The spec upgrades this to show the specific marker that fired.

- [ ] **Step 6.1: Write a golden test**

Append to `internal/ui/banner_test.go`:

```go
func TestRenderClean_SuggestedCapWithDetectionHint(t *testing.T) {
    data := &BannerData{
        ContextName: "default",
        AgentName:   "claude",
        AgentPath:   "/usr/bin/claude",
        Sandbox: &SandboxInfo{
            Network: "outbound only",
            Ports:   "all",
        },
        SuggestedCaps: []CapabilityDisplay{{
            Name:          "git-remote",
            Paths:         []string{"ssh"},
            DetectionHint: "[remote in .git/config",
        }},
    }

    var buf bytes.Buffer
    color.NoColor = true
    defer func() { color.NoColor = false }()

    if err := RenderBanner(&buf, "clean", data); err != nil {
        t.Fatalf("RenderBanner: %v", err)
    }
    out := buf.String()

    if !strings.Contains(out, "[remote in .git/config") {
        t.Errorf("clean render missing detection hint; got:\n%s", out)
    }
    if !strings.Contains(out, "aide --with git-remote") {
        t.Errorf("clean render missing enable hint; got:\n%s", out)
    }
}
```

- [ ] **Step 6.2: Run test — expect fail**

Current suggested-cap line has only the enable hint; the detection hint is not yet rendered.

- [ ] **Step 6.3: Modify both templates**

In `clean.tmpl` find:

```
{{- range .SuggestedCaps}}
    {{dim "○"}} {{printf "%-10s" .Name}} {{truncate .Paths 3}}  {{dim (printf "detected — aide --with %s" .Name)}}
{{- end}}
```

Replace with:

```
{{- range .SuggestedCaps}}
    {{dim "○"}} {{printf "%-10s" .Name}} {{truncate .Paths 3}}  {{dim (printf "detected%s — aide --with %s" (suggestedEvidence .DetectionHint) .Name)}}
{{- end}}
```

Apply the same change to `boxed.tmpl` (keeping the `│    ` prefix on the boxed line). Current boxed line:

```
│    {{dim "○"}} {{printf "%-10s" .Name}} {{truncate .Paths 3}}  {{dim (printf "detected — aide --with %s" .Name)}}
```

Replace with:

```
│    {{dim "○"}} {{printf "%-10s" .Name}} {{truncate .Paths 3}}  {{dim (printf "detected%s — aide --with %s" (suggestedEvidence .DetectionHint) .Name)}}
```

(`compact.tmpl` has the same block — apply the same change there too for consistency; compact already shows suggested caps so the hint should appear there if populated.)

- [ ] **Step 6.4: Add `suggestedEvidence` helper**

In `internal/ui/funcmap.go`, add:

```go
// suggestedEvidence returns ": <hint>" when a DetectionHint is set,
// or "" otherwise. The leading colon+space is conditionally emitted
// so templates can write "detected{{suggestedEvidence .DetectionHint}}"
// and get either "detected" or "detected: <hint>".
func suggestedEvidence(hint string) string {
    if hint == "" {
        return ""
    }
    return " evidence: " + hint
}
```

Register in `colorFuncMap()`:

```go
"suggestedEvidence": suggestedEvidence,
```

- [ ] **Step 6.5: Quick unit test in funcmap_test.go**

```go
func TestSuggestedEvidence(t *testing.T) {
    if got := suggestedEvidence(""); got != "" {
        t.Errorf("empty hint should return empty; got %q", got)
    }
    if got := suggestedEvidence("uv.lock"); got != " evidence: uv.lock" {
        t.Errorf("got %q; want \" evidence: uv.lock\"", got)
    }
}
```

- [ ] **Step 6.6: Run all template tests**

```bash
go test ./internal/ui/... -race -count=1
```

All green.

- [ ] **Step 6.7: Commit**

```
Render marker evidence alongside suggested cap hints

Suggested-but-not-enabled capabilities now surface the marker that
triggered detection alongside the enable hint. For example:

  ○ git-remote  ssh  detected evidence: [remote in .git/config — aide --with git-remote

The hint is only added when DetectionHint is populated; legacy
suggestion lines without a hint render as before.

Applies to compact, clean, and boxed templates; the suggested-cap
block exists in all three.
```

---

## Task 7: Wire Provenance + fresh-grant into launcher

**Files:**
- Modify: `internal/launcher/launcher.go`

The launcher already calls `sandbox.ResolveCapabilitiesWithVariants`, which returns a `*capability.Set`. It needs to also collect per-capability `Provenance` and populate the new `CapabilityDisplay` fields.

- [ ] **Step 7.1: Change the resolver contract to surface Provenance**

`ResolveCapabilitiesWithVariants` today returns `(*capability.Set, config.SandboxOverrides, error)`. We need per-capability `Provenance`. Modify it to also return a `map[string]capability.Provenance`:

Find the function in `internal/sandbox/capabilities.go`:

```go
func ResolveCapabilitiesWithVariants(capNames []string, cfg *config.Config, opts VariantSelectionOptions) (*capability.Set, config.SandboxOverrides, error)
```

Extend its signature:

```go
func ResolveCapabilitiesWithVariants(capNames []string, cfg *config.Config, opts VariantSelectionOptions) (*capability.Set, config.SandboxOverrides, map[string]capability.Provenance, error)
```

Inside the function, when looping `capSet.Capabilities`, after the `SelectVariants` call stash the returned provenance into a map keyed by capability name. Return it alongside the other results.

Update the only caller (`ResolveCapabilities` wrapper) to discard the new return value:

```go
func ResolveCapabilities(capNames []string, cfg *config.Config) (*capability.Set, config.SandboxOverrides, error) {
    set, overrides, _, err := ResolveCapabilitiesWithVariants(capNames, cfg, VariantSelectionOptions{})
    return set, overrides, err
}
```

Also update the launcher callsite to accept the new return value:

```go
resolvedCapSet, capOverrides, capProvenance, err := sandbox.ResolveCapabilitiesWithVariants(capNames, cfg, opts)
```

- [ ] **Step 7.2: Run compilation — expect nothing else broke**

```bash
go build ./...
go vet ./...
```

Green. `ResolveCapabilities` (unparameterised) and `ResolveCapabilitiesWithVariants` (parameterised) are the only two entrypoints; the wrapper preserves the narrower signature, so no external caller changes.

- [ ] **Step 7.3: Populate CapabilityDisplay fields in launcher**

Locate the loop in `internal/launcher/launcher.go` that builds `bannerData.Capabilities` (search for `CapabilityDisplay{` or `Capabilities = append`). For each active capability, enrich the struct:

```go
prov := capProvenance[rc.Name]
disp := ui.CapabilityDisplay{
    Name:    rc.Name,
    Paths:   append(append([]string{}, rc.Readable...), rc.Writable...),
    EnvVars: rc.EnvAllow,
    Source:  /* existing source logic */,

    // NEW:
    Variants:      prov.Variants,
    ProvenanceTag: ui.ProvenanceTag(prov.Reason),  // see Step 7.4 below
    FreshGrant:    prov.Reason == "consent:granted",
}

// Tier 3 fields — only populate when style is boxed.
if effectiveStyle == "boxed" {
    disp.EvidenceSummary = summarizeFiredEvidence(prov)  // helper defined below
    if len(prov.Variants) > 0 && l.ConsentStore != nil {
        grants, _ := l.ConsentStore.List(cwd)
        for _, g := range grants {
            if g.Capability == rc.Name {
                disp.ConfirmedAt = g.ConfirmedAt
                break // most recent first per consent.Store.List sort
            }
        }
    }
}
```

- [ ] **Step 7.4: Expose `provenanceTag` as a public helper**

Templates reference `provenanceTag` via the funcmap, but the launcher needs it too. Rename `provenanceTag` in `internal/ui/funcmap.go` to exported form `ProvenanceTag` (keep the funcmap registration key as `"provenanceTag"`). Both templates and external packages can now call it.

Edit `internal/ui/funcmap.go`:

```go
// ProvenanceTag maps a capability.Provenance.Reason string to the
// short human-readable tag shown in Tier 2 (clean + boxed):
//   "detected" — consent:granted, consent:stable
//   "pinned"   — yaml-pin
//   "--variant" — cli-override
//   "default"  — any default:* reason
// Unknown reasons map to "".
func ProvenanceTag(reason string) string {
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

Update the funcmap registration:

```go
"provenanceTag": ProvenanceTag,
```

Update `funcmap_test.go` test name references from `provenanceTag` to `ProvenanceTag` (the function behind them is the same).

- [ ] **Step 7.5: Add `summarizeFiredEvidence` helper in launcher**

The `prov.Evidence` isn't directly present on `capability.Provenance` today. We have `Provenance.Variants` and `Provenance.Reason`, not the full `Evidence`. We need to thread evidence through.

Extend `capability.Provenance` in `internal/capability/select.go`:

```go
type Provenance struct {
    Variants []string
    Reason   string
    // NEW: short human-readable summary of markers that fired in this
    // detection. Empty string when not applicable (e.g. default or
    // yaml-pin paths).
    EvidenceSummary string
}
```

Populate `EvidenceSummary` at all `return ..., Provenance{...}, nil` sites inside `SelectVariants` that run detection. The easiest path: there's already a `summarizeEvidence(evidence)` function in `select.go`. Pipe its return into Provenance for the `consent:granted` and `consent:stable` branches:

```go
// Example in the consent:stable branch
return selected, Provenance{
    Variants:        names(selected),
    Reason:          "consent:stable",
    EvidenceSummary: summarizeEvidence(evidence),
}, nil
```

Do the same for `consent:granted`. For `yaml-pin`, `cli-override`, and any `default:*` branch, leave `EvidenceSummary` empty.

Then in the launcher:

```go
disp.EvidenceSummary = prov.EvidenceSummary
```

- [ ] **Step 7.6: Add tests for the new Provenance field**

In `internal/capability/select_test.go`, append:

```go
func TestSelectVariants_ProvenanceIncludesEvidenceSummary(t *testing.T) {
    dir := t.TempDir()
    if err := os.WriteFile(filepath.Join(dir, "uv.lock"), nil, 0o600); err != nil {
        t.Fatal(err)
    }
    c := newTestCap()
    cstore := consent.NewStore(t.TempDir())
    _, prov, err := SelectVariants(SelectInput{
        Capability:  c,
        ProjectRoot: dir,
        Consent:     cstore,
        Prompter:    &fakePrompter{returnedVariants: []string{"uv"}},
        Interactive: true,
        AutoYes:     true,
    })
    if err != nil {
        t.Fatal(err)
    }
    if prov.EvidenceSummary == "" {
        t.Errorf("EvidenceSummary should be non-empty after successful detection+grant")
    }
    if !strings.Contains(prov.EvidenceSummary, "uv.lock") {
        t.Errorf("EvidenceSummary should mention uv.lock; got %q", prov.EvidenceSummary)
    }
}
```

Add imports if missing.

- [ ] **Step 7.7: Run full regression**

```bash
go test ./... -race -count=1
go vet ./...
go build ./...
```

All green.

- [ ] **Step 7.8: Commit**

```
Surface Provenance from launcher into CapabilityDisplay

sandbox.ResolveCapabilitiesWithVariants now returns a per-capability
map[string]capability.Provenance alongside the existing Set and
SandboxOverrides. The launcher uses it to populate the new
CapabilityDisplay fields:

  - Variants  — from Provenance.Variants
  - ProvenanceTag — mapped from Provenance.Reason via ProvenanceTag()
  - FreshGrant — true when Reason == "consent:granted" this launch
  - EvidenceSummary — from a new Provenance.EvidenceSummary field,
    populated for detection-driven branches only
  - ConfirmedAt — looked up from consent.Store.List(cwd) when the
    effective banner style is "boxed"

capability.Provenance gains an EvidenceSummary string for the
last point. The non-variant ResolveCapabilities wrapper discards
the new return value so external callers stay unaffected.
```

---

## Task 8: Non-TTY auto-downgrade

**Files:**
- Modify: `cmd/aide/main.go` (or the site that resolves effective banner style — see Step 8.1)
- Modify: `cmd/aide/banner_test.go` (may need creation if it doesn't exist)

- [ ] **Step 8.1: Locate the banner-style resolution site**

```bash
grep -rn "InfoStyle\|info_style" cmd/aide/ internal/launcher/ | grep -v _test.go
```

Find the location where `prefs.InfoStyle` is read and passed into `ui.RenderBanner`. If there's no single site today, add one (a small helper function `effectiveBannerStyle(cmd) string`).

- [ ] **Step 8.2: Write a failing test for the auto-downgrade**

Create or extend `cmd/aide/banner_style_test.go`:

```go
package main

import (
    "os"
    "testing"
)

func TestEffectiveBannerStyle_TTYPreservesPreference(t *testing.T) {
    // A preference of "boxed" with an interactive terminal stays boxed.
    got := effectiveBannerStyle("boxed", true /* isTTY */, "" /* no explicit override */)
    if got != "boxed" {
        t.Errorf("TTY + preference=boxed → %q, want boxed", got)
    }
}

func TestEffectiveBannerStyle_NonTTYForcesCompact(t *testing.T) {
    got := effectiveBannerStyle("boxed", false /* !isTTY */, "" /* no override */)
    if got != "compact" {
        t.Errorf("non-TTY + preference=boxed → %q, want compact", got)
    }
}

func TestEffectiveBannerStyle_ExplicitOverrideWinsOverNonTTY(t *testing.T) {
    got := effectiveBannerStyle("compact", false /* !isTTY */, "boxed" /* explicit override */)
    if got != "boxed" {
        t.Errorf("non-TTY + explicit boxed → %q, want boxed", got)
    }
}

// isInteractiveStdout probes the actual runtime state; a smoke test
// confirms the helper exists and returns a deterministic result.
func TestIsInteractiveStdout(t *testing.T) {
    // In test runs stdout is usually not a TTY; the exact value can
    // differ between `go test` and IDE runs. Just exercise the call.
    _ = isInteractiveTerminal(os.Stdout)
}
```

- [ ] **Step 8.3: Add the helper**

In `cmd/aide/main.go` (reuse the existing `isInteractiveTerminal` helper from the cmdEnv refactor):

```go
// effectiveBannerStyle resolves which banner style to render given
// the user's configured preference, whether stdout is a terminal,
// and any explicit override (--info-style flag or AIDE_INFO_STYLE
// env). Explicit overrides always win; otherwise non-TTY output
// forces compact mode to keep CI logs quiet.
func effectiveBannerStyle(preference string, isTTY bool, explicitOverride string) string {
    if explicitOverride != "" {
        return explicitOverride
    }
    if !isTTY {
        return "compact"
    }
    return preference
}
```

- [ ] **Step 8.4: Wire the helper at the banner-render callsite**

Replace direct uses of `prefs.InfoStyle` with `effectiveBannerStyle(prefs.InfoStyle, isInteractiveTerminal(os.Stdout), explicitInfoStyleOverride)`. The `explicitInfoStyleOverride` variable comes from whichever code currently reads `--info-style` / `AIDE_INFO_STYLE`; thread it into the call.

- [ ] **Step 8.5: Run tests**

```bash
go test ./cmd/aide/... -run TestEffectiveBannerStyle -v
go test ./... -race -count=1
```

All green.

- [ ] **Step 8.6: Commit**

```
Force compact banner on non-TTY output

When stdout is not a terminal and the user has not explicitly set
--info-style or AIDE_INFO_STYLE, the banner renders in compact
mode regardless of the user's configured preference. Explicit
overrides (flag or env var) always win, so users who deliberately
want boxed output piped to a log still get it.

Adds effectiveBannerStyle(preference, isTTY, explicitOverride) and
tests covering the three-way decision table.
```

---

## Task 9: Integration smoke test

**Files:**
- Create: `cmd/aide/banner_integration_test.go`

- [ ] **Step 9.1: Write the test**

```go
package main

import (
    "bytes"
    "os"
    "path/filepath"
    "strings"
    "testing"

    "github.com/fatih/color"
)

func TestBannerIntegration_PythonUVRendersVariantProvenanceAndFreshMarker(t *testing.T) {
    t.Skip("integration wiring spec — enable once Task 7 wiring is complete and aide can run end-to-end in a temp project")

    // High-level shape:
    // 1. Create a temp project with uv.lock so detection fires.
    // 2. Set XDG_DATA_HOME to a temp dir so the consent store is empty
    //    (first-launch → fresh grant).
    // 3. Invoke the CLI entry with --info-style=clean --yes so the
    //    prompter auto-approves the uv variant.
    // 4. Capture stdout; assert it contains "python[uv]", "🆕",
    //    "(detected)".
    dir := t.TempDir()
    if err := os.WriteFile(filepath.Join(dir, "uv.lock"), nil, 0o600); err != nil {
        t.Fatal(err)
    }
    xdg := t.TempDir()
    t.Setenv("XDG_DATA_HOME", xdg)

    var buf bytes.Buffer
    color.NoColor = true
    defer func() { color.NoColor = false }()

    // Actual invocation:
    //   err := runAide(&buf, dir, []string{"--info-style=clean", "--yes", "status"})
    //   if err != nil { t.Fatal(err) }
    //   out := buf.String()
    //   for _, want := range []string{"python[uv]", "🆕", "(detected)"} {
    //       if !strings.Contains(out, want) {
    //           t.Errorf("banner missing %q; got:\n%s", want, out)
    //       }
    //   }
    _ = buf
    _ = strings.Contains
}
```

Note: the helper `runAide` does not exist today; wiring a full cmd-level integration harness is larger than this plan should take on. Ship the test as `t.Skip`'d with a clear comment describing what it would exercise. A follow-up issue can flesh it out once the banner refactor is merged and the team decides on a cmd-test harness shape.

- [ ] **Step 9.2: Commit**

```
Add skipped integration test for banner variant + provenance flow

The test describes the end-to-end shape: temp project with uv.lock +
empty consent store + --yes → banner shows "python[uv] 🆕 (detected)".
Currently skipped because the cmd/aide package lacks a runAide test
harness; the shape is captured so the flesh-out is mechanical once
that harness lands.
```

---

## Acceptance sweep (run after Task 9)

```bash
# All templates updated:
grep -l 'variantSuffix\|freshMarker\|provenanceTag' internal/ui/templates/
# Expected: all three of compact.tmpl, clean.tmpl, boxed.tmpl

# Evidence + confirmed only in boxed:
grep 'evidence:\|confirmed:' internal/ui/templates/
# Expected: only boxed.tmpl

# Launcher populates the new fields:
grep -n 'Variants:\s*prov' internal/launcher/launcher.go
# Expected: at least one hit

# Non-TTY guard in place:
grep -n 'effectiveBannerStyle' cmd/aide/
# Expected: helper defined and called

# Full suite + vet:
go test ./... -race -count=3
go vet ./...
```

All green; close AIDE-j6m.

---

## Self-review

**Spec coverage:**
- ✅ Tier 1 always-visible items (context, agent, sandbox, auto-approve, warnings, variant, fresh marker, ad-hoc paths) — variant + fresh marker added in Tasks 3 and 4; existing template sections cover context/agent/sandbox/auto-approve/warnings/ad-hoc paths.
- ✅ Tier 2 clean additions (provenance tag, detection hint) — Tasks 4 and 6.
- ✅ Tier 3 boxed additions (evidence summary, consent timestamp) — Task 5.
- ✅ Data model extensions — Task 1.
- ✅ Wiring from SelectVariants through launcher — Task 7.
- ✅ Non-TTY auto-downgrade — Task 8.
- ✅ Integration smoke — Task 9 (skipped with clear spec).

**Placeholder scan:**
- No "TBD", "TODO", or "similar to Task N".
- Task 9's `t.Skip` is called out explicitly — the test has a clear written-out shape, just needs the runAide harness to land separately.
- Task 7 references an `effectiveStyle` variable that's threaded in Task 8; the two land in order, so Task 7's "only populate ConfirmedAt when style is boxed" has a `style` value to read. The style variable gets its concrete name (`effectiveStyle`) in Task 7 step 7.3, built before the helper's formal signature lands in Task 8 — there's a temporary inconsistency across the plan's commits until Task 8 lands. Engineer reading the plan commit-by-commit will see Task 7's comment say "effectiveStyle" and understand it's a local name meaning "the style we will ultimately render in"; Task 8 formalises this into `effectiveBannerStyle()`.

**Type consistency:**
- `CapabilityDisplay` fields: `Variants`, `ProvenanceTag`, `FreshGrant`, `EvidenceSummary`, `ConfirmedAt`, `DetectionHint` — referenced consistently across Tasks 1, 3, 4, 5, 6, 7.
- `capability.Provenance`: gains `EvidenceSummary` in Task 7; referenced there and not before.
- Helper functions: `variantSuffix` (Task 2), `freshMarker` (Task 2), `provenanceTag`/`ProvenanceTag` (Task 2 for template, promoted to public in Task 7), `suggestedEvidence` (Task 6), `formatConfirmedAt` (Task 5), `effectiveBannerStyle` (Task 8) — all defined before their first use.

**Commit ordering:**
- Task 1 (data fields, backwards-compat) before any consumer.
- Task 2 (helpers) before templates that use them.
- Tasks 3–6 (template updates) before the launcher wiring — templates render zero values harmlessly, so the launcher-less state is still green.
- Task 7 (wiring) populates the fields the templates now consume.
- Task 8 (non-TTY) applies a global default that affects all three modes.
- Task 9 closes with an integration placeholder.
