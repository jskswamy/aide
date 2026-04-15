# DetectProject / DetectEvidence Unification Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Collapse two file-marker detection engines in `internal/capability` into one — `DetectProject` becomes data-driven like `DetectEvidence`, powered by a shared Marker evaluator over `fs.FS`.

**Architecture:** Add two Marker kinds (`DirExists`, `GlobContains`) so every current DetectProject check maps declaratively. Switch `Marker.Match` and the engine to stdlib `io/fs` (tests use `testing/fstest.MapFS`). Populate `Capability.Markers` on all 15 built-ins in `builtin.go`. Rewrite `DetectProject` to loop the registry and call a shared `AnyMarkerMatches` helper. Delete the 8 ad-hoc helpers. A golden test is committed *before* the refactor to pin current behaviour.

**Tech Stack:** Go 1.25, stdlib `io/fs` + `testing/fstest.MapFS`. No new dependencies.

**Spec:** `docs/superpowers/specs/2026-04-15-detect-unification-design.md`
**Beads issue:** AIDE-c7o

**Branch / worktree:** start with a new worktree from main.

---

## File Structure

### Modified files

```
internal/capability/variant.go          add DirExists, GlobContains kinds;
                                        AnyMarkerMatches, AllMarkersMatch;
                                        Marker.Match switches to fs.FS
internal/capability/variant_test.go     new marker-kind tests; any/all helpers tests

internal/capability/detect.go           DetectProject rewritten as registry loop;
                                        8 helpers deleted
internal/capability/detect_test.go      golden test landed first; existing tests ported

internal/capability/detect_variants.go  DetectEvidence switches to fs.FS
internal/capability/detect_variants_test.go  tests port to fstest.MapFS

internal/capability/builtin.go          populate Markers on all 15 built-ins
internal/capability/builtin_test.go     TestBuiltins_AllCapabilitiesHaveMarkers

internal/capability/capability.go       Capability struct gains Markers []Marker
                                        (already has Variants, DefaultVariants)

internal/capability/select.go           SelectInput.FS fs.FS added; callers updated

internal/launcher/launcher.go           wrap os.DirFS(projectRoot) before DetectProject
internal/sandbox/capabilities.go        thread fs.FS through VariantSelectionOptions

cmd/aide/detect_integration_test.go     NEW — smoke test os.DirFS production path
```

### Commit plan

1. Golden safety net (1 commit)
2. Add new Marker kinds (DirExists, GlobContains) + Any/All helpers (1 commit)
3. Convert `Marker.Match` + `DetectEvidence` signatures to `fs.FS`; update callers (1 commit)
4. Thread `fs.FS` through `SelectInput` and launcher/sandbox call sites (1 commit)
5. Populate `Capability.Markers` on all 15 built-ins (1 commit)
6. Rewrite `DetectProject` as a registry loop; delete the 8 ad-hoc helpers (1 commit)
7. Add integration smoke test (1 commit)

Seven commits total. Each leaves `go test ./... -race` green.

---

## Task 1: Golden safety net for DetectProject

**Files:**
- Create: `internal/capability/detect_golden_test.go`

**Why this task is first:** Nothing else in this plan runs without a regression gate for DetectProject's current behaviour. This test is committed alone, passing against the *old* implementation. Later tasks refactor around it; if the refactor drifts, this test fails.

- [ ] **Step 1.1: Write the failing test skeleton**

```go
// internal/capability/detect_golden_test.go
package capability

import (
    "os"
    "path/filepath"
    "reflect"
    "sort"
    "testing"
)

// writeFixture materialises a map of relative path → file contents
// into a fresh t.TempDir() and returns the dir. Empty contents mean
// "create the file with zero bytes." A trailing "/" in the key means
// "create as a directory, not a file."
func writeFixture(t *testing.T, files map[string]string) string {
    t.Helper()
    dir := t.TempDir()
    for p, body := range files {
        full := filepath.Join(dir, p)
        if p[len(p)-1] == '/' {
            if err := os.MkdirAll(full, 0o700); err != nil {
                t.Fatal(err)
            }
            continue
        }
        if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
            t.Fatal(err)
        }
        if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
            t.Fatal(err)
        }
    }
    return dir
}

func TestDetectProject_Golden(t *testing.T) {
    cases := []struct {
        name  string
        files map[string]string
        want  []string
    }{
        // 15 minimal fixtures
        {"docker only", map[string]string{"Dockerfile": ""}, []string{"docker"}},
        {"terraform only", map[string]string{"main.tf": ""}, []string{"terraform"}},
        {"go only", map[string]string{"go.mod": "module x\n"}, []string{"go"}},
        {"rust only", map[string]string{"Cargo.toml": ""}, []string{"rust"}},
        {"python only", map[string]string{"pyproject.toml": ""}, []string{"python"}},
        {"ruby only", map[string]string{"Gemfile": ""}, []string{"ruby"}},
        {"java only", map[string]string{"pom.xml": ""}, []string{"java"}},
        {"k8s dir only", map[string]string{"k8s/": ""}, []string{"k8s"}},
        {"github only", map[string]string{".github/workflows/": ""}, []string{"github"}},
        {"helm only", map[string]string{"Chart.yaml": ""}, []string{"helm"}},
        {"npm only", map[string]string{"package.json": "{}"}, []string{"npm"}},
        {"vault only", map[string]string{".vault-token": "xxx"}, []string{"vault"}},
        {"aws via go.mod", map[string]string{
            "go.mod": "module x\nrequire github.com/aws/aws-sdk-go v1\n",
        }, []string{"go", "aws"}},
        {"gcp via requirements", map[string]string{
            "requirements.txt": "google-cloud-storage\n",
        }, []string{"python", "gcp"}},
        {"git-remote via config", map[string]string{
            ".git/config": "[remote \"origin\"]\n\turl = git@github.com:x/y.git\n",
        }, []string{"git-remote"}},

        // 5 combo fixtures
        {"go service + docker + ci", map[string]string{
            "go.mod":                       "module x\n",
            "Dockerfile":                   "FROM golang:1.25\n",
            ".github/workflows/ci.yml":     "name: ci\n",
        }, []string{"docker", "go", "github"}},
        {"python data + conda + k8s", map[string]string{
            "pyproject.toml":   "[project]\nname=\"x\"\n",
            "environment.yml":  "name: env\n",
            "k8s/deploy.yaml":  "apiVersion: apps/v1\n",
        }, []string{"python", "k8s"}},
        {"terraform nested", map[string]string{
            "modules/vpc/main.tf": "resource \"x\" \"y\" {}\n",
        }, []string{"terraform"}},
        {"node monorepo with aws sdk", map[string]string{
            "package.json": "{\"dependencies\":{\"aws-sdk\":\"^2\"}}\n",
        }, []string{"aws", "npm"}},
        {"empty project", map[string]string{}, nil},

        // 5 edge / negative fixtures
        {"terraform only depth-1 (not root)", map[string]string{
            "infra/main.tf": "",
        }, []string{"terraform"}},
        {"k8s yaml at depth-1", map[string]string{
            "deploy/svc.yaml": "apiVersion: v1\nkind: Service\n",
        }, []string{"k8s"}},
        {"yaml without apiVersion", map[string]string{
            "config.yaml": "name: foo\n",
        }, nil},
        {"Dockerfile as directory", map[string]string{
            "Dockerfile/": "",
        }, nil},
        {"go.mod only in subdir (no root detection)", map[string]string{
            "submodule/go.mod": "module y\n",
        }, nil},
    }

    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            dir := writeFixture(t, tc.files)
            got := DetectProject(dir)

            // Golden match is exact on content. Order is preserved.
            if !reflect.DeepEqual(sortedCopy(got), sortedCopy(tc.want)) {
                t.Fatalf("DetectProject content mismatch\n  got:  %v\n  want: %v",
                    got, tc.want)
            }
        })
    }
}

func sortedCopy(in []string) []string {
    out := append([]string(nil), in...)
    sort.Strings(out)
    return out
}
```

- [ ] **Step 1.2: Run test to verify it passes against the old code**

```
go test ./internal/capability/... -run TestDetectProject_Golden -v
```

Expected: PASS. All 25 table cases green against the existing DetectProject implementation.

If any case fails, fix the expectation to match actual behaviour — this is a snapshot of current truth, not a wish.

- [ ] **Step 1.3: Commit**

```
Add DetectProject golden safety net before marker unification

Snapshots current DetectProject output against 25 fixtures covering
15 minimal single-capability cases, 5 realistic combinations, and 5
edge/negative cases (terraform nested-only, k8s yaml depth-1, yaml
without apiVersion, Dockerfile as a directory, go.mod only in a
subdirectory).

Passes against the existing hand-rolled DetectProject; the next
commit converts the implementation to Marker-driven detection and
the same test must continue to pass.
```

Use `__GIT_COMMIT_PLUGIN__=1 git commit -m "$(cat <<'EOF' ... EOF)"`.

---

## Task 2: Add DirExists and GlobContains marker kinds + Any/All helpers

**Files:**
- Modify: `internal/capability/variant.go`
- Modify: `internal/capability/variant_test.go`

**Scope:** additive. No caller changes. Old callers continue to use the three existing kinds. The new engine work in later tasks will use the new kinds.

- [ ] **Step 2.1: Write failing tests for DirExists**

Append to `internal/capability/variant_test.go`:

```go
func TestMarker_DirExists_Match(t *testing.T) {
    dir := t.TempDir()
    if err := os.Mkdir(filepath.Join(dir, "k8s"), 0o700); err != nil {
        t.Fatal(err)
    }
    m := Marker{DirExists: "k8s"}
    if !m.Match(dir) {
        t.Errorf("DirExists marker did not match existing directory")
    }
}

func TestMarker_DirExists_RejectsFile(t *testing.T) {
    dir := t.TempDir()
    writeFile(t, dir, "k8s", "")
    m := Marker{DirExists: "k8s"}
    if m.Match(dir) {
        t.Errorf("DirExists matched a file at the same name; want directory only")
    }
}

func TestMarker_DirExists_Missing(t *testing.T) {
    m := Marker{DirExists: "k8s"}
    if m.Match(t.TempDir()) {
        t.Errorf("DirExists matched a missing directory")
    }
}
```

- [ ] **Step 2.2: Run — expect fail (DirExists field undefined)**

```
go test ./internal/capability/... -run TestMarker_DirExists -v
```

- [ ] **Step 2.3: Write failing tests for GlobContains**

Append to the same file:

```go
func TestMarker_GlobContains_Match(t *testing.T) {
    dir := t.TempDir()
    writeFile(t, dir, "deploy.yaml", "apiVersion: apps/v1\nkind: Deployment\n")
    m := Marker{GlobContains: GlobContainsSpec{Glob: "*.yaml", Pattern: "apiVersion:"}}
    if !m.Match(dir) {
        t.Errorf("GlobContains did not match yaml containing pattern")
    }
}

func TestMarker_GlobContains_NoMatch(t *testing.T) {
    dir := t.TempDir()
    writeFile(t, dir, "deploy.yaml", "name: foo\n")
    m := Marker{GlobContains: GlobContainsSpec{Glob: "*.yaml", Pattern: "apiVersion:"}}
    if m.Match(dir) {
        t.Errorf("GlobContains matched yaml without pattern")
    }
}

func TestMarker_GlobContains_FilesCap(t *testing.T) {
    dir := t.TempDir()
    // 60 yaml files, none contain apiVersion. Scan must not exceed the cap.
    // (The test asserts the negative outcome; the cap is a safety, not a
    // functional requirement visible here. A separate allocation test is
    // added if we later want to assert exact file-count behaviour.)
    for i := 0; i < 60; i++ {
        writeFile(t, dir, fmt.Sprintf("f%02d.yaml", i), "name: foo\n")
    }
    m := Marker{GlobContains: GlobContainsSpec{Glob: "*.yaml", Pattern: "apiVersion:"}}
    if m.Match(dir) {
        t.Errorf("GlobContains matched despite no file containing pattern")
    }
}
```

Add `"fmt"` to the test file's import block if not already present.

- [ ] **Step 2.4: Run — expect fail (GlobContains field undefined)**

- [ ] **Step 2.5: Write failing tests for AnyMarkerMatches and AllMarkersMatch**

Append:

```go
func TestAnyMarkerMatches_Empty(t *testing.T) {
    if AnyMarkerMatches(t.TempDir(), nil) {
        t.Errorf("AnyMarkerMatches on empty list returned true")
    }
}

func TestAnyMarkerMatches_FirstMatchWins(t *testing.T) {
    dir := t.TempDir()
    writeFile(t, dir, "Dockerfile", "")
    ms := []Marker{{File: "Dockerfile"}, {File: "never.txt"}}
    if !AnyMarkerMatches(dir, ms) {
        t.Errorf("AnyMarkerMatches did not return true on first-marker match")
    }
}

func TestAnyMarkerMatches_NoneMatch(t *testing.T) {
    dir := t.TempDir()
    ms := []Marker{{File: "a"}, {File: "b"}}
    if AnyMarkerMatches(dir, ms) {
        t.Errorf("AnyMarkerMatches returned true when none matched")
    }
}

func TestAllMarkersMatch_Empty(t *testing.T) {
    if AllMarkersMatch(t.TempDir(), nil) {
        t.Errorf("AllMarkersMatch on empty list returned true; want false")
    }
}

func TestAllMarkersMatch_AllMatch(t *testing.T) {
    dir := t.TempDir()
    writeFile(t, dir, "a", "")
    writeFile(t, dir, "b", "")
    ms := []Marker{{File: "a"}, {File: "b"}}
    if !AllMarkersMatch(dir, ms) {
        t.Errorf("AllMarkersMatch did not return true when all matched")
    }
}

func TestAllMarkersMatch_OneFails(t *testing.T) {
    dir := t.TempDir()
    writeFile(t, dir, "a", "")
    ms := []Marker{{File: "a"}, {File: "b"}}
    if AllMarkersMatch(dir, ms) {
        t.Errorf("AllMarkersMatch returned true with one marker failing")
    }
}
```

- [ ] **Step 2.6: Run — expect fail (helpers undefined)**

- [ ] **Step 2.7: Implement the new kinds and helpers**

Edit `internal/capability/variant.go`. Update the `Marker` struct and add types + helpers:

```go
// Marker is a detection rule. Exactly one of File, Contains,
// GlobPath, DirExists, or GlobContains must be set.
type Marker struct {
    File         string
    Contains     ContainsSpec
    GlobPath     string
    DirExists    string          // NEW — directory at this relative path exists
    GlobContains GlobContainsSpec // NEW — any file matching Glob contains Pattern
}

// GlobContainsSpec describes a combined glob + substring check.
// Useful for "any yaml at depth-0 or depth-1 contains apiVersion:".
type GlobContainsSpec struct {
    Glob    string
    Pattern string
}

// globContainsMaxFiles caps the number of files a single GlobContains
// marker will scan. Prevents DoS via a wildcard that matches a huge
// directory.
const globContainsMaxFiles = 50
```

Extend `Marker.Validate` to cover 5 kinds:

```go
func (m Marker) Validate() error {
    n := 0
    if m.File != "" {
        n++
    }
    if m.Contains.File != "" || m.Contains.Pattern != "" {
        if m.Contains.File == "" || m.Contains.Pattern == "" {
            return errors.New("marker: Contains requires both File and Pattern")
        }
        n++
    }
    if m.GlobPath != "" {
        n++
    }
    if m.DirExists != "" {
        n++
    }
    if m.GlobContains.Glob != "" || m.GlobContains.Pattern != "" {
        if m.GlobContains.Glob == "" || m.GlobContains.Pattern == "" {
            return errors.New("marker: GlobContains requires both Glob and Pattern")
        }
        n++
    }
    if n != 1 {
        return errors.New("marker: exactly one of File, Contains, GlobPath, DirExists, or GlobContains must be set")
    }
    return nil
}
```

Extend `Marker.Match` with the two new kinds:

```go
func (m Marker) Match(projectRoot string) bool {
    if m.File != "" {
        fi, err := os.Stat(filepath.Join(projectRoot, m.File))
        return err == nil && !fi.IsDir()
    }
    if m.Contains.File != "" {
        return containsInBoundedFile(
            filepath.Join(projectRoot, m.Contains.File),
            m.Contains.Pattern,
        )
    }
    if m.GlobPath != "" {
        matches, _ := filepath.Glob(filepath.Join(projectRoot, m.GlobPath))
        return len(matches) > 0
    }
    if m.DirExists != "" {
        fi, err := os.Stat(filepath.Join(projectRoot, m.DirExists))
        return err == nil && fi.IsDir()
    }
    if m.GlobContains.Glob != "" {
        matches, _ := filepath.Glob(filepath.Join(projectRoot, m.GlobContains.Glob))
        if len(matches) > globContainsMaxFiles {
            matches = matches[:globContainsMaxFiles]
        }
        for _, p := range matches {
            if containsInBoundedFile(p, m.GlobContains.Pattern) {
                return true
            }
        }
        return false
    }
    return false
}
```

Extend `MatchSummary`:

```go
func (m Marker) MatchSummary() string {
    switch {
    case m.File != "":
        return m.File
    case m.Contains.File != "":
        return m.Contains.File + ":" + m.Contains.Pattern
    case m.GlobPath != "":
        return m.GlobPath
    case m.DirExists != "":
        return m.DirExists + "/"
    case m.GlobContains.Glob != "":
        return m.GlobContains.Glob + ":" + m.GlobContains.Pattern
    }
    return "<empty-marker>"
}
```

Append helpers at the end of the file:

```go
// AnyMarkerMatches reports whether at least one marker in ms matches
// somewhere under projectRoot. An empty list returns false. Use for
// top-level Capability.Markers (presence-of-evidence semantics).
func AnyMarkerMatches(projectRoot string, ms []Marker) bool {
    for _, m := range ms {
        if m.Match(projectRoot) {
            return true
        }
    }
    return false
}

// AllMarkersMatch reports whether every marker in ms matches under
// projectRoot. An empty list returns false. Use for Variant.Markers
// (specificity-of-evidence semantics).
func AllMarkersMatch(projectRoot string, ms []Marker) bool {
    if len(ms) == 0 {
        return false
    }
    for _, m := range ms {
        if !m.Match(projectRoot) {
            return false
        }
    }
    return true
}
```

Note: `Match` is still on `string` projectRoot. The `fs.FS` conversion happens in Task 3.

- [ ] **Step 2.8: Run — all new tests pass, existing tests still pass**

```
go test ./internal/capability/... -race -v
go vet ./internal/capability/...
go build ./...
```

- [ ] **Step 2.9: Commit**

```
Add DirExists and GlobContains marker kinds plus Any/All helpers

Extends Marker with two kinds needed to cover the current
DetectProject checks declaratively — DirExists matches a directory
at a relative path (for k8s/, .github/workflows/, etc.) and
GlobContains scans files matching a glob for a substring (for k8s
yaml apiVersion detection). GlobContains caps the number of files
scanned at 50 per marker to bound worst-case scan cost.

AnyMarkerMatches and AllMarkersMatch wrap the iteration patterns
used by top-level Capability detection (OR) and per-Variant
selection (AND). No existing caller has changed yet; the engine
migration lands in the next commit.
```

---

## Task 3: Convert Marker.Match and DetectEvidence to fs.FS

**Files:**
- Modify: `internal/capability/variant.go` (Match signature)
- Modify: `internal/capability/variant_test.go` (tests pass fs.FS)
- Modify: `internal/capability/detect_variants.go` (DetectEvidence signature)
- Modify: `internal/capability/detect_variants_test.go` (tests pass fs.FS)
- Modify: `internal/capability/select.go` (DetectEvidence call)

- [ ] **Step 3.1: Change Marker.Match to take fs.FS**

Edit `variant.go`. Replace the `Match` body:

```go
// Match reports whether the marker matches within fsys. fsys is
// typically os.DirFS(projectRoot) in production or fstest.MapFS in
// tests; paths are relative to the fsys root.
func (m Marker) Match(fsys fs.FS) bool {
    if m.File != "" {
        fi, err := fs.Stat(fsys, m.File)
        return err == nil && !fi.IsDir()
    }
    if m.Contains.File != "" {
        return containsInBoundedFileFS(fsys, m.Contains.File, m.Contains.Pattern)
    }
    if m.GlobPath != "" {
        matches, _ := fs.Glob(fsys, m.GlobPath)
        return len(matches) > 0
    }
    if m.DirExists != "" {
        fi, err := fs.Stat(fsys, m.DirExists)
        return err == nil && fi.IsDir()
    }
    if m.GlobContains.Glob != "" {
        matches, _ := fs.Glob(fsys, m.GlobContains.Glob)
        if len(matches) > globContainsMaxFiles {
            matches = matches[:globContainsMaxFiles]
        }
        for _, p := range matches {
            if containsInBoundedFileFS(fsys, p, m.GlobContains.Pattern) {
                return true
            }
        }
        return false
    }
    return false
}
```

Add the fs.FS-flavoured bounded read, and a thin wrapper so the
path-based one used by the soon-to-be-retired containsInBoundedFile
callers still compiles (we'll delete these in Task 6 after DetectProject
is rewritten):

```go
func containsInBoundedFileFS(fsys fs.FS, path, pattern string) bool {
    f, err := fsys.Open(path)
    if err != nil {
        return false
    }
    defer func() { _ = f.Close() }()
    buf := make([]byte, markerMaxReadSize)
    n, _ := f.Read(buf)
    return strings.Contains(string(buf[:n]), pattern)
}
```

Update `AnyMarkerMatches` / `AllMarkersMatch` to take `fs.FS`:

```go
func AnyMarkerMatches(fsys fs.FS, ms []Marker) bool {
    for _, m := range ms {
        if m.Match(fsys) {
            return true
        }
    }
    return false
}

func AllMarkersMatch(fsys fs.FS, ms []Marker) bool {
    if len(ms) == 0 {
        return false
    }
    for _, m := range ms {
        if !m.Match(fsys) {
            return false
        }
    }
    return true
}
```

Remove the old path-based `containsInBoundedFile` helper from `variant.go` — it's no longer called. Add imports: `"io/fs"`.

- [ ] **Step 3.2: Update variant_test.go to pass fs.FS**

Change test call sites. Example:

```go
// Before
m := Marker{File: "uv.lock"}
if !m.Match(dir) { ... }

// After
m := Marker{File: "uv.lock"}
if !m.Match(os.DirFS(dir)) { ... }
```

Apply the same transform to every `m.Match(dir)`, `AnyMarkerMatches(dir, ...)`, and `AllMarkersMatch(dir, ...)` call in `variant_test.go`.

- [ ] **Step 3.3: Change DetectEvidence signature**

Edit `detect_variants.go`:

```go
func DetectEvidence(fsys fs.FS, cap Capability) consent.Evidence {
    selected := make([]string, 0, len(cap.Variants))
    matches := make([]consent.MarkerMatch, 0)
    for _, v := range cap.Variants {
        if len(v.Markers) == 0 {
            continue
        }
        allMatch := true
        for _, m := range v.Markers {
            ok := m.Match(fsys)
            matches = append(matches, consent.MarkerMatch{
                Kind:    markerKind(m),
                Target:  m.MatchSummary(),
                Matched: ok,
            })
            if !ok {
                allMatch = false
            }
        }
        if allMatch {
            selected = append(selected, v.Name)
        }
    }
    sort.Strings(selected)
    return consent.Evidence{Variants: selected, Matches: matches}
}
```

Extend `markerKind` to cover the two new kinds:

```go
func markerKind(m Marker) string {
    switch {
    case m.File != "":
        return "file"
    case m.Contains.File != "":
        return "contains"
    case m.GlobPath != "":
        return "glob"
    case m.DirExists != "":
        return "dir"
    case m.GlobContains.Glob != "":
        return "glob-contains"
    }
    return ""
}
```

Add imports: `"io/fs"`.

- [ ] **Step 3.4: Update SelectVariants' call to DetectEvidence**

Edit `select.go` around line 95:

```go
// Was: evidence := DetectEvidence(in.Capability, in.ProjectRoot)
evidence := DetectEvidence(os.DirFS(in.ProjectRoot), in.Capability)
```

Add import: `"os"`.

`SelectInput.ProjectRoot` stays as `string` — `SelectVariants` is the only place that wraps. This keeps callers (launcher, sandbox, main.go) unchanged in this task.

- [ ] **Step 3.5: Update detect_variants_test.go to pass fs.FS**

Convert every `DetectEvidence(cap, dir)` call to `DetectEvidence(os.DirFS(dir), cap)`.

The fastest rewrite uses MapFS for the table-driven tests, but an equivalent `os.DirFS(dir)` wrap keeps the diff minimal. Prefer the `os.DirFS` wrap for this step — MapFS conversion is a separate win and can come later.

- [ ] **Step 3.6: Run — all tests still pass**

```
go test ./internal/capability/... -race -v
go vet ./...
go build ./...
```

- [ ] **Step 3.7: Commit**

```
Switch Marker.Match and DetectEvidence to io/fs

Marker.Match and the Any/All helpers now take fs.FS instead of a
projectRoot string; DetectEvidence likewise. SelectVariants wraps
its input path with os.DirFS once, so no call site above the
capability package changes in this commit.

The transition enables tests to use testing/fstest.MapFS for
fixture-driven coverage and is a prerequisite for the DetectProject
rewrite, which will loop the registry over the same FS.
```

---

## Task 4: Propagate fs.FS through launcher and sandbox

**Files:**
- Modify: `internal/capability/select.go` (add FS to SelectInput)
- Modify: `internal/capability/select_test.go` (fixture tests pass FS)
- Modify: `internal/sandbox/capabilities.go` (threading)
- Modify: `internal/launcher/launcher.go` (wrap DirFS for DetectProject)

**Scope:** thread `fs.FS` through the upstream callers so the next task can convert DetectProject. Keep the path string alongside — provenance and log lines still need it.

- [ ] **Step 4.1: Widen SelectInput**

Edit `select.go`:

```go
type SelectInput struct {
    Capability  Capability
    ProjectRoot string  // still here for Provenance / diagnostics
    FS          fs.FS   // NEW — when nil, SelectVariants uses os.DirFS(ProjectRoot)
    Overrides   []string
    YAMLPins    []string
    Consent     *consent.Store
    Prompter    Prompter
    Interactive bool
    AutoYes     bool
}
```

Update the DetectEvidence call inside SelectVariants to prefer `in.FS`:

```go
fsys := in.FS
if fsys == nil {
    fsys = os.DirFS(in.ProjectRoot)
}
evidence := DetectEvidence(fsys, in.Capability)
```

Add import: `"io/fs"`.

- [ ] **Step 4.2: Update select_test.go**

No functional change required; existing tests don't set FS, so the nil branch keeps working via os.DirFS. If you want fixture-based MapFS tests for SelectVariants, add them as new tests — not part of this task's required scope.

- [ ] **Step 4.3: Pass FS from VariantSelectionOptions**

Edit `internal/sandbox/capabilities.go`. Extend `VariantSelectionOptions`:

```go
type VariantSelectionOptions struct {
    ProjectRoot  string
    FS           fs.FS             // NEW — when nil, capability.SelectVariants uses os.DirFS
    CLIOverrides map[string][]string
    YAMLPins     map[string][]string
    Consent      *consent.Store
    Prompter     capability.Prompter
    Interactive  bool
    AutoYes      bool
}
```

Pass it through to each `SelectVariants` call:

```go
selected, _, selErr := capability.SelectVariants(capability.SelectInput{
    Capability:  def,
    ProjectRoot: opts.ProjectRoot,
    FS:          opts.FS,       // new
    Overrides:   opts.CLIOverrides[rc.Name],
    // ...
})
```

Add import: `"io/fs"`.

- [ ] **Step 4.4: Launcher wraps DirFS once**

Edit `internal/launcher/launcher.go` around line 244 where `VariantSelectionOptions` is built:

```go
opts := sandbox.VariantSelectionOptions{
    ProjectRoot:  cwd,
    FS:           os.DirFS(cwd),
    CLIOverrides: l.VariantOverrides,
    // ...unchanged...
}
```

The launcher also calls `DetectProject` around line 514. Update that call site to wrap:

```go
// Was: suggestions := capability.DetectProject(projectRoot)
suggestions := capability.DetectProject(os.DirFS(projectRoot))
```

Note: `DetectProject`'s signature is still `(string) []string` at this point — the above change is speculative. **DO NOT APPLY IT YET** — leave the existing `DetectProject(projectRoot)` call until Task 6 changes the signature. Flag it with a TODO-less comment that the Task 6 diff will adjust.

Concretely: in this task, only the `SelectVariants` call via `VariantSelectionOptions` gets the FS. `DetectProject` stays string-based until Task 6.

- [ ] **Step 4.5: Run**

```
go test ./... -race -v
go vet ./...
```

All green.

- [ ] **Step 4.6: Commit**

```
Thread fs.FS through SelectInput and VariantSelectionOptions

Adds an optional FS field to capability.SelectInput and
sandbox.VariantSelectionOptions so callers can inject a filesystem.
When FS is nil, SelectVariants falls back to os.DirFS(ProjectRoot),
preserving the current behaviour and all existing tests.

The launcher populates VariantSelectionOptions.FS from
os.DirFS(cwd) so variant detection runs against the same rooted
filesystem the sandbox profile will eventually grant access to.
DetectProject still takes a path — its migration is the next
commit, coupled with the registry-driven rewrite.
```

---

## Task 5: Populate Capability.Markers on all 15 built-ins

**Files:**
- Modify: `internal/capability/capability.go` (add `Markers []Marker` field)
- Modify: `internal/capability/builtin.go`
- Modify: `internal/capability/builtin_test.go`

**Scope:** additive data. `DetectProject` still runs the old code path. The new markers are in place, ready for Task 6 to switch over.

- [ ] **Step 5.1: Add Markers field to Capability struct**

Edit `capability.go`:

```go
type Capability struct {
    Name             string
    Description      string
    Extends          string
    Combines         []string
    Unguard          []string
    Readable         []string
    Writable         []string
    Deny             []string
    EnvAllow         []string
    EnableGuard      []string
    Allow            []string
    NetworkMode      string
    // NEW — top-level detection rules (OR: any match → capability applies).
    // Used by DetectProject. Distinct from Variant.Markers, which uses AND.
    Markers          []Marker
    Variants         []Variant
    DefaultVariants  []string
}
```

- [ ] **Step 5.2: Write failing test asserting every built-in has Markers**

Append to `internal/capability/builtin_test.go`:

```go
func TestBuiltins_AllCapabilitiesDetectableByDetectProject_HaveMarkers(t *testing.T) {
    // Every capability that DetectProject currently detects must
    // declare Markers, so the Task 6 rewrite can loop the registry.
    detectable := map[string]bool{
        "docker": true, "terraform": true, "go": true, "rust": true,
        "python": true, "ruby": true, "java": true, "k8s": true,
        "github": true, "helm": true, "aws": true, "gcp": true,
        "npm": true, "vault": true, "git-remote": true,
    }
    b := Builtins()
    for name := range detectable {
        c, ok := b[name]
        if !ok {
            t.Errorf("builtin %q missing from registry", name)
            continue
        }
        if len(c.Markers) == 0 {
            t.Errorf("builtin %q has no Markers; DetectProject cannot detect it",
                name)
        }
    }
}
```

Run — expect FAIL for all 15.

- [ ] **Step 5.3: Populate markers in builtin.go**

Edit each of the 15 built-in entries in `internal/capability/builtin.go`. Add a `Markers` field populated as shown:

```go
"docker": {
    Name: "docker", Description: "Docker daemon and registry credentials",
    Markers: []Marker{
        {File: "Dockerfile"},
        {File: "docker-compose.yaml"},
        {File: "docker-compose.yml"},
    },
    // existing fields...
},

"terraform": {
    Name: "terraform", Description: "Terraform state and providers",
    Markers: []Marker{
        {GlobPath: "*.tf"},
        {GlobPath: "*/*.tf"},
    },
    // existing fields...
},

"go": {
    Name: "go", Description: "Go toolchain",
    Markers: []Marker{
        {File: "go.mod"},
        {File: "go.sum"},
    },
    // existing fields...
},

"rust": {
    Name: "rust", Description: "Rust toolchain",
    Markers: []Marker{{File: "Cargo.toml"}},
    // existing fields...
},

"python": {
    Name: "python", Description: "Python toolchain",
    Markers: []Marker{
        {File: "pyproject.toml"},
        {File: "requirements.txt"},
        {File: "Pipfile"},
        {File: "setup.py"},
    },
    Variants: []Variant{
        // existing uv/pyenv/conda/poetry/venv entries, unchanged
    },
    DefaultVariants: []string{"venv"},
},

"ruby": {
    Name: "ruby", Description: "Ruby toolchain",
    Markers: []Marker{
        {File: "Gemfile"},
        {GlobPath: "*.gemspec"},
    },
    // existing fields...
},

"java": {
    Name: "java", Description: "Java/JVM toolchain",
    Markers: []Marker{
        {File: "pom.xml"},
        {File: "build.gradle"},
        {File: "build.gradle.kts"},
    },
    // existing fields...
},

"k8s": {
    Name: "k8s", Description: "Kubernetes cluster access",
    Markers: []Marker{
        {DirExists: "k8s"},
        {DirExists: "kubernetes"},
        {DirExists: "manifests"},
        {GlobContains: GlobContainsSpec{Glob: "*.yaml",   Pattern: "apiVersion:"}},
        {GlobContains: GlobContainsSpec{Glob: "*.yml",    Pattern: "apiVersion:"}},
        {GlobContains: GlobContainsSpec{Glob: "*/*.yaml", Pattern: "apiVersion:"}},
        {GlobContains: GlobContainsSpec{Glob: "*/*.yml",  Pattern: "apiVersion:"}},
    },
    // existing fields...
},

"github": {
    Name: "github", Description: "GitHub CLI and credentials",
    Markers: []Marker{{DirExists: ".github/workflows"}},
    // existing fields...
},

"helm": {
    Name: "helm", Description: "Helm charts and releases",
    Markers: []Marker{
        {File: "Chart.yaml"},
        {File: "helmfile.yaml"},
    },
    // existing fields...
},

"aws": {
    Name: "aws", Description: "AWS CLI and credentials",
    Markers: []Marker{
        {Contains: ContainsSpec{File: "go.mod",           Pattern: "aws-sdk-go"}},
        {Contains: ContainsSpec{File: "requirements.txt", Pattern: "boto3"}},
        {Contains: ContainsSpec{File: "package.json",     Pattern: "aws-sdk"}},
    },
    // existing fields...
},

"gcp": {
    Name: "gcp", Description: "Google Cloud CLI and credentials",
    Markers: []Marker{
        {Contains: ContainsSpec{File: "go.mod",           Pattern: "cloud.google.com"}},
        {Contains: ContainsSpec{File: "requirements.txt", Pattern: "google-cloud"}},
        {Contains: ContainsSpec{File: "package.json",     Pattern: "@google-cloud"}},
    },
    // existing fields...
},

"npm": {
    Name: "npm", Description: "npm and yarn registry credentials",
    Markers: []Marker{{File: "package.json"}},
    // existing fields...
},

"vault": {
    Name: "vault", Description: "HashiCorp Vault access",
    Markers: []Marker{{File: ".vault-token"}},
    // existing fields...
},

"git-remote": {
    Name: "git-remote", Description: "Git remote operations (push, fetch, pull) via SSH and HTTPS",
    Markers: []Marker{
        {Contains: ContainsSpec{File: ".git/config", Pattern: "[remote "}},
    },
    // existing fields...
},
```

For each capability above, keep every existing field (`Writable`, `EnvAllow`, `EnableGuard`, `NetworkMode`, etc.) — the diff adds `Markers` only.

- [ ] **Step 5.4: Run**

```
go test ./internal/capability/... -race -v
```

`TestBuiltins_AllCapabilitiesDetectableByDetectProject_HaveMarkers` now passes. `TestDetectProject_Golden` still passes (it runs the old code path). Everything else unchanged.

- [ ] **Step 5.5: Commit**

```
Populate top-level Markers on all 15 built-in capabilities

Each built-in that DetectProject currently detects now declares its
detection rules as a Markers slice in builtin.go. The set mirrors
the existing hand-rolled logic exactly:

  - simple file presence: docker, go, rust, python, ruby, java,
    helm, npm, vault
  - glob at root and one level deep: terraform, ruby (.gemspec),
    k8s (*.yaml/*.yml top and depth-1)
  - directory presence: k8s (k8s/ kubernetes/ manifests/), github
    (.github/workflows)
  - bounded substring in a known file: aws, gcp, git-remote

DetectProject still runs the hand-rolled code path — the next
commit flips it over to loop the registry and consume these
Markers.
```

---

## Task 6: Rewrite DetectProject and delete the 8 ad-hoc helpers

**Files:**
- Modify: `internal/capability/detect.go`
- Modify: `internal/launcher/launcher.go`
- Delete: 8 helpers (`fileExists`, `dirExists`, `containsInFile`, `containsInFileByPath`, `hasFileWithExtension`, `hasFileWithExtensionOneLevelDeep`, `hasYAMLWithAPIVersion`, `checkYAMLsForAPIVersion`)

**Scope:** the big-bang. Everything previous was prep.

- [ ] **Step 6.1: Rewrite DetectProject in detect.go**

Replace the entire current `DetectProject` plus every helper in `detect.go` with:

```go
// Package capability's project-detection surface.
//
// DetectProject walks the built-in registry and returns the names of
// capabilities whose top-level Markers match under fsys. Intended for
// aide cap suggest and launcher startup hints.
//
// Suggestions are returned in a deterministic order (the registry's
// sorted key order), so callers can rely on stable output for goldens
// and human display.

package capability

import (
    "io/fs"
    "sort"
)

// DetectProject returns built-in capability names whose top-level
// Markers match somewhere under fsys. fsys is typically
// os.DirFS(projectRoot) in production.
func DetectProject(fsys fs.FS) []string {
    b := Builtins()
    names := make([]string, 0, len(b))
    for name := range b {
        names = append(names, name)
    }
    sort.Strings(names)

    var out []string
    for _, name := range names {
        c := b[name]
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

Important ordering detail: the old DetectProject returns suggestions in **insertion order** of the if-ladder (docker, terraform, go, rust, python, ruby, java, k8s, github, helm, aws, gcp, npm, vault, git-remote). The new registry loop sorts by name alphabetically, which produces a DIFFERENT order.

This will break the golden test's order-sensitive assertion. Resolve by choosing one of:

- **Option A (preferred):** Change golden's assertion to set-match via `sortedCopy()`. The golden test above already uses `sortedCopy` on both `got` and `want`, so the comparison is order-insensitive — the test passes regardless of which engine emits which order. **Re-read Task 1's Step 1.1: the assertion is already set-based.** No change needed to the golden test.
- **Option B:** Preserve insertion order by declaring a `detectOrder []string` slice in `detect.go`. Only needed if `aide cap suggest`'s display order matters to users — currently it doesn't (suggestions are informational).

Going with **A**. Update the golden test's doc comment to reflect: "Golden match is by set, not order; DetectProject's output order is not part of its contract."

- [ ] **Step 6.2: Update launcher's DetectProject call**

Edit `internal/launcher/launcher.go` around line 514:

```go
// Before
if suggestions := capability.DetectProject(projectRoot); len(suggestions) > 0 {

// After
if suggestions := capability.DetectProject(os.DirFS(projectRoot)); len(suggestions) > 0 {
```

`os` is likely already imported; verify.

- [ ] **Step 6.3: Update existing DetectProject tests (pre-golden)**

The legacy `detect_test.go` has ~20 case-per-capability tests like `TestDetectProject_Dockerfile`. They call `DetectProject(dir)` with a string path. Update each call site to `DetectProject(os.DirFS(dir))`.

Fast scripted rewrite:
```
sed -i '' 's/DetectProject(dir)/DetectProject(os.DirFS(dir))/g' internal/capability/detect_test.go
```

Add `"os"` to the imports of that test file if not already present. Verify with `goimports` or `go vet`.

- [ ] **Step 6.4: Delete the 8 helpers from detect.go**

The full file `detect.go` should after this step contain ONLY:

- Package comment
- Imports
- `DetectProject(fsys fs.FS) []string`

All of `maxReadSize`, `fileExists`, `dirExists`, `hasFileWithExtension`, `hasFileWithExtensionOneLevelDeep`, `containsInFile`, `containsInFileByPath`, `hasYAMLWithAPIVersion`, `checkYAMLsForAPIVersion` are gone.

- [ ] **Step 6.5: Verify the grep**

```
grep -rn 'fileExists\|dirExists\|containsInFile\|containsInFileByPath\|hasFileWithExtension\|hasYAMLWithAPIVersion\|checkYAMLsForAPIVersion' internal/capability/
```

Expected: zero output.

- [ ] **Step 6.6: Full test run**

```
go test ./... -race -v
go vet ./...
go build ./...
```

All green. Golden test, all prior DetectProject tests, variant tests, DetectEvidence tests, SelectVariants tests, launcher tests, sandbox tests — every one.

- [ ] **Step 6.7: Commit**

```
Rewrite DetectProject as registry loop and delete ad-hoc helpers

DetectProject now iterates the built-in registry in sorted key order
and returns each capability whose top-level Markers have any match
under the injected fs.FS. Behaviour is equivalent to the hand-rolled
if-ladder it replaces — the golden test, landed two commits ago and
unchanged since, continues to pass.

Deleted helpers: fileExists, dirExists, containsInFile,
containsInFileByPath, hasFileWithExtension,
hasFileWithExtensionOneLevelDeep, hasYAMLWithAPIVersion,
checkYAMLsForAPIVersion. All 15 built-ins now carry their detection
rules as data in builtin.go; future language-support plans (node,
ruby variants, java variants) add rules declaratively with no new
engine code.
```

---

## Task 7: Smoke-test the os.DirFS production path

**Files:**
- Create: `cmd/aide/detect_integration_test.go`

**Why:** The refactor is covered by unit tests against `os.DirFS` and `fstest.MapFS` equivalently. One small integration test locks in that the production wrapper works end-to-end — `aide cap suggest` (or the equivalent CLI surface) returns the expected capabilities for a realistic on-disk project.

- [ ] **Step 7.1: Write the test**

```go
// cmd/aide/detect_integration_test.go
package main

import (
    "bytes"
    "os"
    "path/filepath"
    "sort"
    "strings"
    "testing"

    "github.com/jskswamy/aide/internal/capability"
)

// TestDetectProject_OsDirFS_RealFilesystem verifies the wrap used in
// production (os.DirFS) matches the behaviour the unit tests cover
// with fstest.MapFS. Three realistic project layouts.
func TestDetectProject_OsDirFS_RealFilesystem(t *testing.T) {
    cases := []struct {
        name  string
        files map[string]string
        want  []string
    }{
        {"go service", map[string]string{
            "go.mod":                   "module x\n",
            "Dockerfile":               "FROM golang:1.25\n",
            ".github/workflows/ci.yml": "name: ci\n",
        }, []string{"docker", "go", "github"}},
        {"python k8s", map[string]string{
            "pyproject.toml":   "[project]\nname=\"x\"\n",
            "k8s/deploy.yaml":  "apiVersion: apps/v1\n",
        }, []string{"python", "k8s"}},
        {"node + aws", map[string]string{
            "package.json": "{\"dependencies\":{\"aws-sdk\":\"^2\"}}\n",
        }, []string{"aws", "npm"}},
    }

    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            dir := t.TempDir()
            for p, body := range tc.files {
                full := filepath.Join(dir, p)
                if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
                    t.Fatal(err)
                }
                if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
                    t.Fatal(err)
                }
            }
            got := capability.DetectProject(os.DirFS(dir))
            sort.Strings(got)
            sort.Strings(tc.want)
            if strings.Join(got, ",") != strings.Join(tc.want, ",") {
                t.Errorf("DetectProject(os.DirFS(%s)) = %v, want %v",
                    dir, got, tc.want)
            }
            _ = bytes.Buffer{} // keep "bytes" import live for future assertions
        })
    }
}
```

- [ ] **Step 7.2: Run**

```
go test ./cmd/aide/... -run TestDetectProject_OsDirFS -race -v
```

Expected: three cases pass.

- [ ] **Step 7.3: Full regression**

```
go test ./... -race
go vet ./...
go build ./...
```

Clean.

- [ ] **Step 7.4: Commit**

```
Add integration smoke test for DetectProject via os.DirFS

Covers three realistic project layouts (Go+Docker+CI, Python+K8s,
Node+AWS) exercising the production wrapper. Complements the unit
coverage in detect_test.go (which uses t.TempDir directly) and
detect_variants_test.go (which uses fstest.MapFS via SelectVariants).

Locks in the invariant that unit-test fixtures and on-disk fixtures
produce identical DetectProject output for equivalent trees.
```

---

## Post-landing checklist

After Task 7 lands:

- [ ] Close AIDE-c7o with `bd close AIDE-c7o`
- [ ] AIDE-72f (Marker-as-interface) is unblocked — 5 marker kinds now justify the polymorphism refactor. No action required here.
- [ ] The related issue AIDE-2kg (consolidate bounded file-read helpers) is **obsoleted** by this work — `containsInBoundedFileFS` is the single bounded reader, the 64 KB cap is the single constant, and the deleted helpers are gone. Close AIDE-2kg with `bd close AIDE-2kg --reason="Resolved as part of AIDE-c7o: detect.go ad-hoc readers deleted, containsInBoundedFileFS in variant.go is the single bounded-read primitive."`

---

## Self-review

**Spec coverage:**
- Data model (5 marker kinds, `Capability.Markers`) — Task 2 adds kinds, Task 5 adds Markers field and populates.
- Evaluation engine (`AnyMarkerMatches` / `AllMarkersMatch`, `fs.FS` signatures) — Task 2 adds helpers, Task 3 switches to fs.FS.
- `DetectProject` rewrite — Task 6.
- `DetectEvidence` signature change + call sites — Task 3, Task 4.
- Builtin catalog population — Task 5.
- Deletion of 8 helpers — Task 6.
- Threat model's GlobContains 50-file cap — Task 2 (`globContainsMaxFiles`).
- Golden safety net — Task 1, lands first.
- No escape hatch — not implemented; verified by absence across the plan.
- Integration smoke — Task 7.
- `grep -rn` helper-deletion check — Task 6, Step 6.5.

**Placeholder scan:** No "TBD", "implement later", "handle edge cases", "similar to Task N". Every step shows code or an exact shell command.

**Type consistency:** `Marker`, `ContainsSpec`, `GlobContainsSpec`, `AnyMarkerMatches`, `AllMarkersMatch`, `DetectProject(fs.FS)`, `DetectEvidence(fs.FS, Capability)`, `SelectInput.FS`, `VariantSelectionOptions.FS` — names match across all tasks. `markerMaxReadSize` is the one size constant (already existed in Task 5 pre-work); no duplicate introduced. `globContainsMaxFiles` is new and unambiguous.

**Order of commits:** Task 1 golden test committed first against old code path. Tasks 2–5 are additive (no behaviour change at the DetectProject level). Task 6 is the flip point — all previous work was prep so this diff is the minimum viable behaviour change. Task 7 is post-flip verification.
