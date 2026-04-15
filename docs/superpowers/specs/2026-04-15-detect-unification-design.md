# DetectProject / DetectEvidence Unification Design

**Date:** 2026-04-15
**Status:** Proposed
**Beads issue:** AIDE-c7o
**Related:** AIDE-72f (Marker-as-interface, blocked by this), AIDE-2kg (consolidate bounded file-read helpers), `docs/superpowers/specs/2026-04-14-generic-toolchain-detection-design.md`

## Problem

Two independent file-marker detection engines live side-by-side in
`internal/capability`:

- **`DetectProject`** (`detect.go`) — a hand-rolled if-ladder over 15
  capabilities, supported by 8 ad-hoc helpers (`fileExists`,
  `dirExists`, `containsInFile`, `containsInFileByPath`,
  `hasFileWithExtension`, `hasFileWithExtensionOneLevelDeep`,
  `hasYAMLWithAPIVersion`, `checkYAMLsForAPIVersion`). Answers "what
  capabilities does this project need?" — drives `aide cap suggest`.
- **`DetectEvidence`** (`detect_variants.go`) — evaluates
  `Variant.Markers` uniformly via one small engine. Answers "which
  variants of a capability apply here?" — drives the consent-gated
  sandbox selection.

The two engines overlap in intent but diverge in implementation,
vocabulary, and test surface. The node / ruby / java follow-up plans
will each need both flavours of detection; at the current rate they
will recreate the duplication three more times.

## Goals

1. **Single detection engine.** One evaluator, one vocabulary
   (Marker), one set of tests.
2. **Declarative built-ins.** Every built-in capability's detection
   rules live as data in `builtin.go`, not as Go code.
3. **No new privileged extension points.** Detection surface stays
   fully declarative — no "escape hatch" function pointers.
4. **Behavior-preserving.** `DetectProject` output is identical on
   every fixture the current implementation covers, verified by a
   golden test introduced *before* the refactor lands.
5. **Testable without disk I/O.** Detection runs against any
   `fs.FS` so unit tests use `testing/fstest.MapFS`.

## Non-goals

- Rewriting `Marker` as an interface — tracked as AIDE-72f; blocks on
  this spec to land the fifth marker kind and prove the shape.
- Touching filesystem abstractions outside `internal/capability` —
  launcher, config, trust, consent, approvalstore stay on `os.*`.
- Adding write-capable filesystem abstraction (afero) — stdlib
  `io/fs` covers read-only detection needs without a new dependency.
- Changing the user-facing CLI (`aide cap suggest`, `aide status`
  provenance, `--variant` flag, consent prompt) — all behavior is
  unchanged.

## Design

### Data model

Extend `Marker` in `internal/capability/variant.go` with two new
kinds (total: 5). Exactly one field is set per marker — enforced by
`Marker.Validate`.

```go
type Marker struct {
    File         string           // existing — file exists at this relative path
    Contains     ContainsSpec     // existing — file contains substring
    GlobPath     string           // existing — any file matches this glob
    DirExists    string           // NEW    — directory exists at this path
    GlobContains GlobContainsSpec // NEW    — any glob-matched file contains substring
}

type GlobContainsSpec struct {
    Glob    string  // e.g. "*.yaml" or "*/*.yml"
    Pattern string  // e.g. "apiVersion:"
}
```

Add a `Markers` field to the top-level `Capability`:

```go
type Capability struct {
    Name             string
    Description      string
    // ...existing fields...
    Markers          []Marker   // NEW — top-level detection (OR semantics)
    Variants         []Variant  // unchanged — per-variant detection (AND per variant)
    DefaultVariants  []string
}
```

`Variant.Markers` keeps its existing semantic: every marker on a
variant must match for that variant to be selected (AND). The new
`Capability.Markers` uses OR: any marker match means the capability
applies. The two semantics are hardcoded in the evaluator, not
configurable — the split maps directly to the real-world distinction
between presence of evidence ("is this a python project?") and
specificity of evidence ("does this python project use uv?").

### Evaluation engine

`Marker.Match` switches from `string` projectRoot to `fs.FS`:

```go
func (m Marker) Match(fsys fs.FS) bool
```

Per-kind implementation uses stdlib only:

- **File** — `fs.Stat(fsys, m.File)`, reject dirs.
- **Contains** — `fs.ReadFile` up to `markerMaxReadSize` bytes,
  `strings.Contains`.
- **GlobPath** — `fs.Glob(fsys, m.GlobPath)`; non-empty match list.
- **DirExists** — `fs.Stat`, accept only when `IsDir()`.
- **GlobContains** — `fs.Glob`; for each match (capped at
  `globContainsMaxFiles`, default 50) read-and-scan up to
  `markerMaxReadSize` bytes; return true on first hit.

Two helpers replace direct iteration at call sites:

```go
// AnyMarkerMatches returns true if at least one marker matches.
// Use for top-level Capability detection.
func AnyMarkerMatches(fsys fs.FS, ms []Marker) bool

// AllMarkersMatch returns true if every marker matches.
// Returns false when len(ms) == 0.
// Use for Variant selection.
func AllMarkersMatch(fsys fs.FS, ms []Marker) bool
```

Rewritten `DetectProject`:

```go
func DetectProject(fsys fs.FS) []string {
    var out []string
    for _, name := range orderedCapabilityNames() {
        c := Builtins()[name]
        if len(c.Markers) == 0 {
            continue
        }
        if AnyMarkerMatches(fsys, c.Markers) {
            out = append(out, name)
        }
    }
    return out
}
```

`DetectEvidence` keeps its shape but its `projectRoot string` becomes
`fsys fs.FS`:

```go
func DetectEvidence(fsys fs.FS, cap Capability) consent.Evidence {
    // unchanged logic — loops Variants, all-markers-match → select
}
```

### Call-site changes

Production callers wrap with `os.DirFS`:

- `cmd/aide/commands.go:capSuggestForPathCmd` (and any other caller
  of `DetectProject`) — `os.DirFS(path)` before calling.
- `internal/sandbox/capabilities.go:ResolveCapabilitiesWithVariants`
  — builds `fsys := os.DirFS(opts.ProjectRoot)` once and passes it
  to `SelectVariants`.
- `internal/capability/select.go:SelectInput` replaces
  `ProjectRoot string` with `FS fs.FS` (with a secondary
  `ProjectRoot string` retained only for provenance/reporting — the
  path still surfaces in `aide status` output).

Tests that previously used `t.TempDir()` remain valid — they wrap
the tempdir with `os.DirFS` and exercise the real filesystem, which
is sometimes the right coverage (e.g., the e2e consent test). New
tests prefer `fstest.MapFS` for speed and visibility.

### Builtin catalog

All 15 built-in capabilities declare their detection rules as
`Markers` in `internal/capability/builtin.go`. Representative
samples:

```go
"terraform": {
    Markers: []Marker{
        {GlobPath: "*.tf"},
        {GlobPath: "*/*.tf"},
    },
    // ...
},

"k8s": {
    Markers: []Marker{
        {DirExists: "k8s"},
        {DirExists: "kubernetes"},
        {DirExists: "manifests"},
        {GlobContains: GlobContainsSpec{Glob: "*.yaml",   Pattern: "apiVersion:"}},
        {GlobContains: GlobContainsSpec{Glob: "*.yml",    Pattern: "apiVersion:"}},
        {GlobContains: GlobContainsSpec{Glob: "*/*.yaml", Pattern: "apiVersion:"}},
        {GlobContains: GlobContainsSpec{Glob: "*/*.yml",  Pattern: "apiVersion:"}},
    },
    // ...
},

"aws": {
    Markers: []Marker{
        {Contains: ContainsSpec{File: "go.mod",           Pattern: "aws-sdk-go"}},
        {Contains: ContainsSpec{File: "requirements.txt", Pattern: "boto3"}},
        {Contains: ContainsSpec{File: "package.json",     Pattern: "aws-sdk"}},
    },
    // ...
},

"git-remote": {
    Markers: []Marker{
        {Contains: ContainsSpec{File: ".git/config", Pattern: "[remote "}},
    },
    // ...
},
```

Simpler capabilities (docker, go, rust, python, ruby, java, helm,
npm, vault, github, gcp) use `File` / `GlobPath` / `DirExists` as
appropriate. Every current DetectProject check maps directly to a
marker — no capability requires special-casing.

### Deletion

The refactor removes all 8 ad-hoc helpers:
`fileExists`, `dirExists`, `containsInFile`, `containsInFileByPath`,
`hasFileWithExtension`, `hasFileWithExtensionOneLevelDeep`,
`hasYAMLWithAPIVersion`, `checkYAMLsForAPIVersion`. Their semantics
are now expressed by the 5 marker kinds. Post-refactor verification:

```
grep -rn 'fileExists\|dirExists\|containsInFile\|containsInFileByPath\
\|hasFileWithExtension\|hasYAMLWithAPIVersion\|checkYAMLsForAPIVersion' \
internal/capability/
```

must return zero hits.

## Threat Model

The detection layer runs in aide's parent process before `sandbox-exec`
is applied, and it reads untrusted project files. Threat delta relative
to the pre-refactor implementation:

**T1 — Malicious project file triggers auto-grant.**
Unchanged. Detection surfaces *suggestions* via `aide cap suggest`
(advisory) and drives the consent-gated variant prompt. Neither
auto-grants anything. The `aide trust` gate and the consent store
remain the authorization mechanisms.

**T2 — Unbounded content read.**
Reduced. Pre-refactor, `hasYAMLWithAPIVersion` called
`containsInFileByPath` (64 KB cap) for every `.yaml`/`.yml` at root
and one level deep — no explicit file-count cap. Post-refactor,
`GlobContains` imposes `globContainsMaxFiles` (50) as a ceiling on
files scanned per marker. 64 KB per-file cap preserved.

**T3 — DoS via wide filesystem scan.**
Reduced. `fs.Glob` patterns in markers are bounded (no `**/` wildcards
available in stdlib glob); combined with `globContainsMaxFiles` cap
the worst-case scan per marker is 50 × 64 KB = 3.2 MB. Pre-refactor
had no such bound on `hasYAMLWithAPIVersion`.

**T4 — Escape-hatch capability authors.**
Eliminated as an option. The design explicitly rejects an "escape
hatch" function-pointer field on `Capability`. Built-in authors
(TCB) must express detection via markers or add a new marker kind
through deliberate review.

**T5 — `fs.FS` injection.**
New surface. `Marker.Match` takes an `fs.FS`. In production callers
wrap `os.DirFS(projectRoot)`; the resulting FS is rooted at the
project and cannot traverse upward (stdlib `os.DirFS` enforces this).
Tests inject `fstest.MapFS`. No caller paths currently accept an
externally-supplied `fs.FS` from untrusted input.

**Residual risk:** GlobContains's 50-file cap may be too tight for
large k8s manifest directories; if the fixture hits the cap the
marker may not fire and k8s detection silently downgrades to
DirExists-only. Mitigation: the existing `k8s` DirExists markers
(`k8s/`, `kubernetes/`, `manifests/`) are the primary signal;
GlobContains is a secondary signal for projects that place manifests
at the root. The cap is a tunable constant; revisit if
user-reported false-negatives surface.

## Testing

### Fixture design

The safety net is a golden detection test with ~25 fixtures in
`internal/capability/detect_test.go`. All fixtures use
`fstest.MapFS`:

- **15 minimal fixtures** — one per built-in capability, with just
  the files/dirs that should trigger detection.
- **5 multi-capability fixtures**:
  - Go service with Dockerfile + `.github/workflows/ci.yml`
  - Python data project with `pyproject.toml`, `environment.yml`, `k8s/`
  - Terraform repo with `modules/*.tf` (nested, not root)
  - Node monorepo with `package.json` containing `aws-sdk`
  - Empty project (no markers — returns empty slice)
- **5 edge/negative fixtures**:
  - Terraform depth-0 only; depth-1 only; neither; `.tf` at depth-2 (should NOT match)
  - K8s via dir alone; via YAML-apiVersion-only; via YAML at depth-1; YAML without `apiVersion:` (NOT k8s)
  - `Dockerfile` as a directory (should NOT match)
  - `go.mod` inside a subdirectory only (should NOT match go)
  - File with `aws-sdk-go` in a comment line beyond 64 KB (should NOT match aws)

**Assertion style:** exact output match. `DetectProject(fsys)`
returns `[]string{...}` in exactly the registry order. Any ordering
regression fails the test.

**Commit ordering:**

1. Golden test lands *first*, as an isolated commit. At that point
   it passes against the *old* `DetectProject`. This is the
   behavior snapshot.
2. The refactor commit changes `detect.go`, `variant.go`, `builtin.go`
   together. The golden test must pass unchanged — that's the
   refactor's acceptance signal.

### Unit coverage

- `variant_test.go` additions:
  - `TestMarker_DirExists_*` (matches dir, rejects file at same name, rejects missing, rejects symlink-to-file if relevant on darwin)
  - `TestMarker_GlobContains_*` (matches when a glob file contains pattern, doesn't match when absent, respects `globContainsMaxFiles`, respects per-file 64 KB cap)
  - `TestMarker_Validate_FiveKinds` — expand table to cover the 5-kind constraint
  - `TestAnyMarkerMatches_*` (empty list → false; single match → true; no match → false)
  - `TestAllMarkersMatch_*` (empty list → false; all match → true; any fail → false)

- `detect_variants_test.go` — existing five tests (`_MatchesAllFiringVariants`, `_NoMatches_EmptyVariants`, `_AllMarkersRequiredPerVariant`, `_SkipsVariantsWithoutMarkers`, `_SortedVariants`) port to `fstest.MapFS`. Behavior unchanged; no new test cases needed.

- `builtin_test.go` — small addition: `TestBuiltins_AllCapabilitiesHaveMarkers` asserts every built-in that used to be detected by `DetectProject` now has non-empty `Markers`.

### Integration coverage

- `cmd/aide/variant_flag_test.go`, `cap_consent_test.go`, `cap_discovery_test.go` — unchanged.
- `internal/sandbox/variants_e2e_test.go` — unchanged (it already uses `t.TempDir()`; production path wraps with `os.DirFS`).
- `cmd/aide/detect_integration_test.go` (new) — smoke test that `aide cap suggest <path>` on a real tempdir produces the same output it did before the refactor. Uses a small set of the golden fixtures written to disk to confirm the `os.DirFS` production path matches the `fstest.MapFS` test path.

### Regression verification

After the rewrite:
```
go test ./internal/capability/... -race -count=3
go test ./... -race
go vet ./...
grep -rn 'fileExists\|dirExists\|containsInFile\|containsInFileByPath\
\|hasFileWithExtension\|hasYAMLWithAPIVersion\|checkYAMLsForAPIVersion' \
internal/capability/
```

Final grep must return zero hits. All tests green.

## Migration strategy

Big-bang cutover in two commits:

1. **Commit 1 — golden safety net.**
   - Add `TestDetectProject_Golden` with ~25 fixtures.
   - No production code changes. Test passes against the old
     implementation. Establishes the behavior contract.

2. **Commit 2 — refactor.**
   - Extend `Marker` with `DirExists` + `GlobContains` kinds.
   - Add `AnyMarkerMatches` / `AllMarkersMatch` helpers.
   - Convert `Marker.Match` and `DetectEvidence` to `fs.FS`.
   - Populate `Capability.Markers` in `builtin.go` for all 15 built-ins.
   - Rewrite `DetectProject` to loop over registry and call
     `AnyMarkerMatches`.
   - Delete the 8 ad-hoc helpers.
   - Update `SelectVariants` and sandbox call sites to pass
     `fs.FS`.
   - Run `go test ./... -race` — must be green.
   - Run the grep above — must return zero hits.

An earlier proposal to ship in 15 incremental commits (one per
capability) was rejected: the intermediate states would have a
registry half-hardcoded and half-declarative, polluting git blame
and making the golden test do the wrong thing at half the commits.

## Alternatives considered

**Escape-hatch function pointer on Capability.** Rejected —
creates a normalized pattern where future capability authors reach
for arbitrary Go code instead of declarative markers, eroding the
"detection is declarative and reviewable" invariant. AIDE-c7o's core
value is exactly this invariant.

**General file-pattern query grammar.** Rejected as premature —
the five-kind enumeration covers every current need and is simple
to audit. A grammar-based approach would be more flexible but also
more surface, and flexibility we don't need today.

**User-configurable `Match.Mode: AnyOf | AllOf` on Markers.**
Rejected — exposes an implementation detail as user configuration.
The top-level OR / variant AND split maps to real-world detection
semantics and doesn't benefit from being knob-ified.

**afero.Fs for filesystem abstraction.** Rejected — stdlib
`io/fs` + `testing/fstest.MapFS` cover the read-only detection
needs without a new dependency to the security-sensitive
`internal/capability` package. afero's write support and non-OS
backends are not needed here.

**Incremental / parallel-run migration.** Rejected — the output is
a pure function of the filesystem, fully testable offline. The
golden test gives the same signal a parallel run would and avoids
dragging a dead code path through review.

## Rollout

1. Commit the golden safety net (~25 fixtures; passes against old code).
2. Commit the refactor (5 marker kinds, declarative builtins, deleted helpers).
3. Verify `aide cap suggest` against representative real-world projects
   locally — one Go, one Python, one k8s, one node+aws.
4. Close AIDE-c7o.
5. AIDE-72f (Marker-as-interface) is unblocked — 5 marker kinds
   make the polymorphism refactor obviously valuable.

## Open questions

- `globContainsMaxFiles = 50` — pulled out of the air. Worth
  revisiting once real-world use surfaces an undercount.
- Should the `SelectInput.ProjectRoot` path-string stay for
  provenance, or should `Provenance` carry the `fs.FS` reference
  directly? Provenance strings currently embed the project path;
  keeping the string seems lower-churn. Recommend: keep it, defer
  any cleanup.
