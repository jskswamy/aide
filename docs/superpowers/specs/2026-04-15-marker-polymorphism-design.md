# Marker Polymorphism Design

**Date:** 2026-04-15
**Status:** Proposed
**Beads issue:** AIDE-72f
**Related:** `2026-04-15-detect-unification-design.md` (AIDE-c7o, landed)

## Problem

`internal/capability/variant.go` defines `Marker` as a sum-typed struct
with five mutually-exclusive fields:

```go
type Marker struct {
    File         string
    Contains     ContainsSpec
    GlobPath     string
    DirExists    string
    GlobContains GlobContainsSpec
}
```

Four parallel discriminator switches re-inspect "which field is non-empty":

- `Marker.Validate()` — branches across 5 fields and enforces "exactly one set"
- `Marker.Match(fsys fs.FS)` — branches across 5 fields to call different
  `fs.*` operations
- `Marker.MatchSummary()` — branches across 5 fields to produce a kind-specific label
- `markerKind(m)` in `detect_variants.go` — branches across 5 fields to produce
  a discriminator string

Adding a 6th kind requires edits at all four sites. The pattern violates
Open-Closed: the `Marker` type is closed to extension without surgery.

## Goals

1. **Single source of truth per kind.** Each marker kind owns its own
   `Validate` / `Match` / `Summary` / `Kind` methods. Adding a new kind
   means adding one concrete type; zero switch edits anywhere.
2. **Type-system-enforced mutual exclusion.** The "exactly one field set"
   runtime check becomes a compile-time guarantee — you literally cannot
   construct a marker with two kinds set.
3. **Readable catalog.** `builtin.go`'s 15-capability marker literals
   read as sentences: `FileMarker{Path: "uv.lock"}` over `Marker{File: "uv.lock"}`.
4. **Domain-aligned naming.** Concrete type names match ubiquitous-language
   descriptions of what each marker means.
5. **Behavior-preserving.** Every existing test — golden detection, marker
   matching, variant selection, python-uv end-to-end — passes after the
   refactor with at most literal-syntax rewrites.

## Non-goals

- **YAML unmarshaling for user-defined markers.** Deferred; no caller
  exists yet. `CapabilityDef` in `internal/config/schema.go` does not
  expose `Markers`, so user-defined capabilities via `.aide.yaml` cannot
  declare markers today. When they can, the layer will be mechanical
  on top of the clean interface hierarchy landed here.
- **Sealed interface (unexported method to prevent external `Marker`
  implementations).** Rejected: `capability` is an `internal/` package;
  third-party implementers cannot exist. A sealed interface adds
  ceremony without a real threat.
- **Generic / type-parameterized API.** Rejected: the problem is many
  behaviors over one interface, not one behavior over many types. Go
  generics serve the opposite shape.
- **Hidden structs + constructor functions** (`NewFileMarker(path) Marker`).
  Rejected: plain struct literals in `builtin.go` are more readable than
  constructor calls; there is no invariant that literals could violate
  beyond what `Validate()` catches.

## Design

### Data model — one interface, five concrete types

```go
// internal/capability/marker.go

// Marker is a detection rule evaluated against a project filesystem.
// Markers are value objects: no identity beyond their content, no
// lifecycle, no mutation.
type Marker interface {
    // Validate returns a non-nil error when the marker is malformed
    // (e.g. zero-valued required fields). Called at Capability
    // registration / test time, not at every Match call.
    Validate() error

    // Match reports whether the marker matches within fsys. fsys is
    // typically os.DirFS(projectRoot) in production or fstest.MapFS
    // in tests; paths are relative to the fsys root.
    Match(fsys fs.FS) bool

    // Summary returns a short human-readable label for the marker,
    // suitable for consent prompts and log lines.
    Summary() string

    // Kind returns the discriminator string used in consent.MarkerMatch
    // and diagnostic output: "file" | "contains" | "glob" | "dir" |
    // "glob-contains".
    Kind() string
}
```

Concrete types:

```go
// FileMarker matches when a file exists at the given relative path.
type FileMarker struct {
    Path string
}

// ContainsMarker matches when File contains Pattern within the first
// markerMaxReadSize bytes.
type ContainsMarker struct {
    File    string
    Pattern string
}

// GlobMarker matches when at least one filesystem entry matches
// Pattern (e.g. "*.tf", "*/*.yaml").
type GlobMarker struct {
    Pattern string
}

// DirMarker matches when a directory exists at the given relative path.
type DirMarker struct {
    Path string
}

// GlobContainsMarker matches when any file matching Glob contains
// Pattern. Scans at most globContainsMaxFiles files per marker.
type GlobContainsMarker struct {
    Glob    string
    Pattern string
}
```

Naming changes from current struct field names:

| Current field (struct form) | Concrete type (interface form) |
|---|---|
| `File string` | `FileMarker{Path}` |
| `Contains ContainsSpec{File, Pattern}` | `ContainsMarker{File, Pattern}` |
| `GlobPath string` | `GlobMarker{Pattern}` |
| `DirExists string` | `DirMarker{Path}` |
| `GlobContains GlobContainsSpec{Glob, Pattern}` | `GlobContainsMarker{Glob, Pattern}` |

**Deleted types:**
- `type Marker struct { ... }` — replaced by the interface of the same name
- `type ContainsSpec struct { File, Pattern string }` — inlined into `ContainsMarker`
- `type GlobContainsSpec struct { Glob, Pattern string }` — inlined into `GlobContainsMarker`

**Deleted / renamed functions:**
- `func markerKind(m Marker) string` in `detect_variants.go` — deleted; call sites use `m.Kind()` directly
- `func (m Marker) MatchSummary() string` — renamed to `Summary()` on each concrete type

### Method bodies — where each branch lands

`Validate` splits across the 5 types, each checking its own required fields:

```go
func (f FileMarker) Validate() error {
    if f.Path == "" {
        return errors.New("FileMarker: Path is required")
    }
    return nil
}

func (c ContainsMarker) Validate() error {
    if c.File == "" || c.Pattern == "" {
        return errors.New("ContainsMarker: File and Pattern are required")
    }
    return nil
}

func (g GlobMarker) Validate() error {
    if g.Pattern == "" {
        return errors.New("GlobMarker: Pattern is required")
    }
    return nil
}

func (d DirMarker) Validate() error {
    if d.Path == "" {
        return errors.New("DirMarker: Path is required")
    }
    return nil
}

func (gc GlobContainsMarker) Validate() error {
    if gc.Glob == "" || gc.Pattern == "" {
        return errors.New("GlobContainsMarker: Glob and Pattern are required")
    }
    return nil
}
```

The "exactly one field set" invariant from the struct form disappears
entirely — the type system enforces it.

`Match` splits per type:

```go
func (f FileMarker) Match(fsys fs.FS) bool {
    fi, err := fs.Stat(fsys, f.Path)
    return err == nil && !fi.IsDir()
}

func (c ContainsMarker) Match(fsys fs.FS) bool {
    return containsInBoundedFileFS(fsys, c.File, c.Pattern)
}

func (g GlobMarker) Match(fsys fs.FS) bool {
    matches, _ := fs.Glob(fsys, g.Pattern)
    return len(matches) > 0
}

func (d DirMarker) Match(fsys fs.FS) bool {
    fi, err := fs.Stat(fsys, d.Path)
    return err == nil && fi.IsDir()
}

func (gc GlobContainsMarker) Match(fsys fs.FS) bool {
    matches, _ := fs.Glob(fsys, gc.Glob)
    if len(matches) > globContainsMaxFiles {
        matches = matches[:globContainsMaxFiles]
    }
    for _, p := range matches {
        if containsInBoundedFileFS(fsys, p, gc.Pattern) {
            return true
        }
    }
    return false
}
```

`Summary` and `Kind` are trivial per-type accessors:

```go
func (f FileMarker) Summary() string { return f.Path }
func (f FileMarker) Kind() string    { return "file" }

func (c ContainsMarker) Summary() string { return c.File + ":" + c.Pattern }
func (c ContainsMarker) Kind() string    { return "contains" }

func (g GlobMarker) Summary() string { return g.Pattern }
func (g GlobMarker) Kind() string    { return "glob" }

func (d DirMarker) Summary() string { return d.Path + "/" }
func (d DirMarker) Kind() string    { return "dir" }

func (gc GlobContainsMarker) Summary() string { return gc.Glob + ":" + gc.Pattern }
func (gc GlobContainsMarker) Kind() string    { return "glob-contains" }
```

Receiver style: value receivers everywhere. Markers are small value
objects with no mutation; value receivers make interface satisfaction
unambiguous (a concrete struct literal automatically implements the
interface).

### Helper functions unchanged

- `AnyMarkerMatches(fsys fs.FS, ms []Marker) bool` — still loops; interface
  dispatch replaces struct-branch discrimination.
- `AllMarkersMatch(fsys fs.FS, ms []Marker) bool` — same.
- `containsInBoundedFileFS` (package-private) — called by `ContainsMarker.Match`
  and `GlobContainsMarker.Match`. Stays.
- `markerMaxReadSize` (64 KiB) and `globContainsMaxFiles` (50) constants — stay.

### Caller updates

- `detect_variants.go:DetectEvidence` — `markerKind(m)` becomes `m.Kind()`.
  The `markerKind` free function is deleted.
- `builtin.go` — all `Markers: []Marker{{...}}` literals rewrite to
  concrete types across 15 capabilities (~40 marker literals total).
- Test files (`variant_test.go`, `detect_variants_test.go`,
  `detect_golden_test.go`, `detect_integration_test.go`) — construct
  markers via concrete types.

### File layout

```
internal/capability/
├── marker.go         NEW — Marker interface + 5 concrete types + their methods
├── marker_test.go    NEW — TestMarker_* tests moved from variant_test.go
├── variant.go        SLIMMED — keeps Variant, AnyMarkerMatches, AllMarkersMatch,
│                     containsInBoundedFileFS, markerMaxReadSize, globContainsMaxFiles
├── variant_test.go   SLIMMED — Variant and Any/AllMarkersMatch tests stay
├── detect_variants.go  (markerKind helper deleted; m.Kind() at call site)
├── builtin.go        (marker literals rewritten)
├── detect.go         (unchanged — already uses Marker via interface)
└── select.go         (unchanged)
```

Rationale: `variant.go` currently mixes Variant logic, Marker types,
Marker helpers, and bounded-read utilities. Splitting marker concerns
into their own file is a natural byproduct of the refactor.

## Testing

### Regression gate

Every test currently passing continues to pass after the refactor with
at most literal-syntax rewrites. Critical gates:

- `TestDetectProject_Golden` (25 fixtures) — full behavior snapshot of
  top-level detection across built-in capabilities.
- `TestDetectEvidence_*` — variant-selection correctness.
- `TestPythonUV_EndToEnd` — the canonical end-to-end path.
- `TestAnyMarkerMatches_*` and `TestAllMarkersMatch_*` — helper semantics.
- `TestMarker_FileMatch`, `TestMarker_ContainsMatch`, `TestMarker_GlobMatch`,
  `TestMarker_DirExists_*`, `TestMarker_GlobContains_*`, `TestMarker_File_RejectsDirectory`,
  `TestMarker_ContainsMatch_ReadBoundary` — per-kind behavior.

### Test adaptation

One test becomes meaningless and is replaced:

- **Delete:** `TestMarker_Validate_ExactlyOneFieldSet` — its table case
  "file+glob" (two fields set on one struct) is now syntactically
  impossible. The type system enforces the invariant.
- **Add:** per-type validation tests:
  - `TestFileMarker_Validate_RejectsEmptyPath`
  - `TestContainsMarker_Validate_RejectsPartial` (File without Pattern and vice versa)
  - `TestGlobMarker_Validate_RejectsEmptyPattern`
  - `TestDirMarker_Validate_RejectsEmptyPath`
  - `TestGlobContainsMarker_Validate_RejectsPartial`

### New compile-time contract

```go
// marker_test.go
func TestMarkerInterface_AllKindsImplement(t *testing.T) {
    var _ Marker = FileMarker{}
    var _ Marker = ContainsMarker{}
    var _ Marker = GlobMarker{}
    var _ Marker = DirMarker{}
    var _ Marker = GlobContainsMarker{}
}
```

Compile-time guard: if someone removes a required method, the package
fails to build. Runtime assertion avoids the need for reflective
introspection.

### Acceptance sweep

```bash
# The old type machinery is fully removed:
grep -rn 'ContainsSpec\|GlobContainsSpec\|markerKind' internal/capability/
# Expected: zero

# Interface satisfaction compiles:
go build ./internal/capability/...

# All tests green:
go test ./... -race -count=1
go vet ./...
```

## Migration

**Big-bang cutover in a single commit.** The golden test + variant
evidence tests + end-to-end integration test together form the regression
gate; if they pass after the rewrite, behavior is preserved.

Commit content:

1. Create `internal/capability/marker.go` with the interface + 5 concrete
   types + their methods.
2. Delete the old `Marker` struct, `ContainsSpec`, and `GlobContainsSpec`
   from `variant.go`.
3. Delete `markerKind(m)` from `detect_variants.go`; replace call sites
   with `m.Kind()`.
4. Rewrite marker literals in `builtin.go` (~40 sites across 15 capabilities).
5. Move `TestMarker_*` tests to `marker_test.go`; update construction
   syntax. Rewrite `TestMarker_Validate_ExactlyOneFieldSet` as five
   per-type validation tests. Add `TestMarkerInterface_AllKindsImplement`.
6. Update any other test file that constructs markers
   (`detect_variants_test.go`, `detect_golden_test.go`,
   `detect_integration_test.go`).
7. `go build ./...` + `go vet ./...` + `go test ./... -race -count=1`
   — all green.

Incremental per-commit migration was considered and rejected: the
rewrite of `builtin.go`'s 40 literal sites plus `markerKind` call sites
in `detect_variants.go` plus test files is the whole migration. Doing
it in stages forces an ugly intermediate type name (`MarkerI`, `legacyMarker`)
or a throw-away dual-form phase that adds review noise without review benefit.

## Threat Model

No trust-boundary changes. The refactor is purely internal. Specifically:

- **T1: Behavioral drift.** Risk of a per-type method implementing the
  same logic slightly differently than the corresponding struct branch.
  Mitigation: golden test + per-kind behavior tests + end-to-end test
  are the safety net. If any case differs, one of them fails.
- **T2: Unintended interface implementations.** Risk that a random
  `capability`-package type accidentally satisfies `Marker`. Mitigation:
  `Marker`'s method set (`Validate / Match / Summary / Kind`) is
  domain-specific enough that no existing type accidentally matches; the
  new `TestMarkerInterface_AllKindsImplement` compile-time test pins the
  expected implementers.
- **T3: Slice-of-interface allocation surprise.** `[]Marker` holding
  concrete value types incurs one pointer-sized header per element (Go's
  interface representation). For 15 capabilities with ~5 markers each,
  this is ~75 pointers of overhead at program startup — negligible.
  Mitigation: none needed; documented here for future-reader clarity.

## Alternatives considered

- **Sealed interface** (`isMarker()` unexported method). Rejected —
  `internal/capability` prevents external implementation at the package-
  boundary level; sealing adds ceremony without a real threat.
- **Generics.** Rejected — the problem is "many behaviors over one
  interface." Generics solve the opposite shape ("one behavior over many
  types"). Forcing a generic adds syntax for no expressiveness gain.
- **Hidden structs with `NewFileMarker(path) Marker` constructors.**
  Rejected — the struct literal form is more readable in `builtin.go`'s
  catalog and there is no invariant hidden structs would protect.
- **Staged migration (side-by-side form, rename then delete).** Rejected —
  the rewrite of 40 literal sites is the refactor; staging it forces
  an ugly intermediate type name without review benefit.
- **YAML unmarshaling for user-defined markers.** Deferred explicitly to
  a future issue with a concrete caller. User-defined capabilities in
  `.aide.yaml` today cannot declare markers; when they can, a custom
  `UnmarshalYAML` dispatcher layers cleanly on top of this design.

## Rollout

1. Single commit implements the refactor per the Migration section.
2. `go build ./...`, `go vet ./...`, and `go test ./... -race -count=1`
   must all be green.
3. Acceptance sweep (`grep` for `ContainsSpec`/`GlobContainsSpec`/`markerKind`)
   returns zero hits.
4. Close AIDE-72f.

## Open questions

- None at this time. Receiver style (value vs pointer), YAML scope, and
  migration strategy are all resolved in the design.
