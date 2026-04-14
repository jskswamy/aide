# Toolchain Variant MVP Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the foundation of variant-aware sandbox capabilities: a working python capability that auto-detects uv/pyenv/conda/poetry/venv, grants narrow paths after one-time user consent, and exposes the variant model through `aide cap list/show/variants` and a `--variant` CLI flag.

**Architecture:** Extract a shared content-addressed approval-store primitive from `internal/trust/`, add a sibling `internal/consent/` aggregate that records per-`(project, capability, variants, evidence)` grants under `XDG_DATA_HOME/aide/consent/`, extend `capability.Capability` with a `Variants` slice + per-variant `Markers`, plug a detector + consent-gated selector into the launch path, and surface the model through new CLI commands.

**Tech Stack:** Go 1.25, existing aide packages (`internal/trust`, `internal/capability`, `internal/config`, `cmd/aide`), Cobra CLI, standard library only for new packages.

**Spec:** `docs/superpowers/specs/2026-04-14-generic-toolchain-detection-design.md`

**Scope note:** This plan covers rollout steps 1–3 from the spec (foundation + python + discovery + consent prompt). Steps 4–8 (node migration, `aide doctor`/preflight, GPU capability, ruby/java catalogs, Claude plugin reliability) are follow-up plans — each builds on this foundation.

---

## File Structure

### New packages / files

```
internal/approvalstore/               shared kernel: content-addressed set
├── store.go                          Store type, NewStore, DefaultRoot, Has/Add/Remove/List/Read, Record
└── store_test.go

internal/consent/                     detection-consent aggregate
├── evidence.go                       MarkerMatch, Evidence, Evidence.Digest
├── evidence_test.go
├── consent.go                        Grant, Store, DefaultStore, ConsentHash, Check/Grant/Revoke/List
└── consent_test.go

internal/capability/variant.go        Variant, Marker types; marker matching; detector
internal/capability/variant_test.go

internal/capability/select.go         SelectVariants, Provenance, Prompter interface
internal/capability/select_test.go

internal/ui/consentprompt.go          prompt rendering for variant consent
internal/ui/consentprompt_test.go
```

### Modified files

```
internal/trust/trust.go               refactored to delegate to approvalstore (public API unchanged)
internal/trust/trust_test.go          keep existing assertions; add namespace layout test

internal/capability/capability.go     add Variants, DefaultVariants fields to Capability struct
                                      extend ResolvedCapability.Sources to surface selected variants
internal/capability/builtin.go        extend "python" capability with Variants catalog

internal/capability/detect.go         unchanged (DetectProject still returns coarse names)

internal/config/project.go            accept capabilities.<name>.variants YAML key

cmd/aide/commands.go                  add --variant flag; add `aide cap show/variants/consent` subcommands;
                                      extend `aide cap list` with variant-count hint column
cmd/aide/main.go                      wire SelectVariants into launch path with consent prompter
```

---

## Task 1: `internal/approvalstore/` — content-addressed set primitive

**Files:**
- Create: `internal/approvalstore/store.go`
- Create: `internal/approvalstore/store_test.go`

### Why this first

The existing `internal/trust/` already implements this pattern inline. Extract it into a reusable primitive so `consent` can use the same mechanics without code duplication. No behavioral change — same on-disk layout, same permissions.

### Step 1.1: Write failing test for `NewStore` + `Has` + `Add` + `Read` round-trip

- [ ] **Step 1.1a: Add the test**

```go
// internal/approvalstore/store_test.go
package approvalstore

import (
    "testing"
)

func TestStore_AddHasRead_RoundTrip(t *testing.T) {
    dir := t.TempDir()
    s := NewStore(dir)

    key := "deadbeef"
    body := []byte("hello approval store")

    if s.Has(key) {
        t.Fatalf("Has(%q) = true before Add; want false", key)
    }
    if err := s.Add(key, body); err != nil {
        t.Fatalf("Add: %v", err)
    }
    if !s.Has(key) {
        t.Fatalf("Has(%q) = false after Add; want true", key)
    }
    rec, err := s.Read(key)
    if err != nil {
        t.Fatalf("Read: %v", err)
    }
    if rec.Key != key {
        t.Errorf("Read.Key = %q, want %q", rec.Key, key)
    }
    if string(rec.Body) != string(body) {
        t.Errorf("Read.Body = %q, want %q", rec.Body, body)
    }
    if rec.ModTime.IsZero() {
        t.Errorf("Read.ModTime is zero")
    }
}
```

- [ ] **Step 1.1b: Run — expect compile failure**

```
go test ./internal/approvalstore/...
```
Expected: `package internal/approvalstore is not in std` or `no Go files` — proves we haven't written anything yet.

- [ ] **Step 1.1c: Write minimal implementation**

```go
// internal/approvalstore/store.go
// Package approvalstore provides a content-addressed file-backed set used
// by the trust and consent aggregates under the User Approval bounded
// context. It has no domain concepts — callers supply the hex-encoded
// key and an opaque body; the store persists the pair under its base
// directory.
package approvalstore

import (
    "errors"
    "os"
    "path/filepath"
    "sort"
    "time"
)

// Store is a content-addressed set backed by a directory of files.
type Store struct {
    baseDir string
}

// Record is the result of reading a key from the store.
type Record struct {
    Key     string
    Body    []byte
    ModTime time.Time
}

// NewStore creates a Store rooted at baseDir. The directory is created
// on first write.
func NewStore(baseDir string) *Store {
    return &Store{baseDir: baseDir}
}

// DefaultRoot returns XDG_DATA_HOME/aide (or ~/.local/share/aide when
// XDG_DATA_HOME is unset). Aggregates should nest their namespaces
// underneath this root.
func DefaultRoot() string {
    base := os.Getenv("XDG_DATA_HOME")
    if base == "" {
        home, _ := os.UserHomeDir()
        base = filepath.Join(home, ".local", "share")
    }
    return filepath.Join(base, "aide")
}

// Has reports whether a record with the given key exists.
func (s *Store) Has(key string) bool {
    _, err := os.Stat(filepath.Join(s.baseDir, key))
    return err == nil
}

// Add writes body to the record at key, creating the base directory if
// needed. Add is idempotent: re-adding the same key overwrites the body.
func (s *Store) Add(key string, body []byte) error {
    if key == "" {
        return errors.New("approvalstore: empty key")
    }
    if err := os.MkdirAll(s.baseDir, 0o700); err != nil {
        return err
    }
    return atomicWrite(filepath.Join(s.baseDir, key), body)
}

// Remove deletes the record for key. Missing keys are a no-op.
func (s *Store) Remove(key string) error {
    err := os.Remove(filepath.Join(s.baseDir, key))
    if errors.Is(err, os.ErrNotExist) {
        return nil
    }
    return err
}

// Read returns the record for key, or os.ErrNotExist if absent.
func (s *Store) Read(key string) (Record, error) {
    path := filepath.Join(s.baseDir, key)
    body, err := os.ReadFile(path)
    if err != nil {
        return Record{}, err
    }
    info, err := os.Stat(path)
    if err != nil {
        return Record{}, err
    }
    return Record{Key: key, Body: body, ModTime: info.ModTime()}, nil
}

// List returns all records in the store sorted by key. An empty store
// returns an empty (non-nil) slice.
func (s *Store) List() ([]Record, error) {
    entries, err := os.ReadDir(s.baseDir)
    if err != nil {
        if errors.Is(err, os.ErrNotExist) {
            return []Record{}, nil
        }
        return nil, err
    }
    keys := make([]string, 0, len(entries))
    for _, e := range entries {
        if e.IsDir() {
            continue
        }
        keys = append(keys, e.Name())
    }
    sort.Strings(keys)
    out := make([]Record, 0, len(keys))
    for _, k := range keys {
        rec, err := s.Read(k)
        if err != nil {
            continue // skip unreadable records
        }
        out = append(out, rec)
    }
    return out, nil
}

// atomicWrite writes data to a temp file in the same directory then
// renames it over path. File permissions are 0o600.
func atomicWrite(path string, data []byte) error {
    dir := filepath.Dir(path)
    f, err := os.CreateTemp(dir, ".aide-approval-*")
    if err != nil {
        return err
    }
    tmp := f.Name()
    if err := f.Chmod(0o600); err != nil {
        _ = f.Close()
        _ = os.Remove(tmp)
        return err
    }
    if _, err := f.Write(data); err != nil {
        _ = f.Close()
        _ = os.Remove(tmp)
        return err
    }
    if err := f.Close(); err != nil {
        _ = os.Remove(tmp)
        return err
    }
    return os.Rename(tmp, path)
}
```

- [ ] **Step 1.1d: Run — expect pass**

```
go test ./internal/approvalstore/... -run TestStore_AddHasRead_RoundTrip -v
```
Expected: `PASS`.

### Step 1.2: Tests for `Remove`, `List`, empty store, idempotent `Add`, missing key

- [ ] **Step 1.2a: Add tests**

```go
// append to internal/approvalstore/store_test.go

func TestStore_Remove_IdempotentAndMissing(t *testing.T) {
    s := NewStore(t.TempDir())
    if err := s.Remove("does-not-exist"); err != nil {
        t.Fatalf("Remove missing: %v", err)
    }
    _ = s.Add("k", []byte("v"))
    if err := s.Remove("k"); err != nil {
        t.Fatalf("Remove existing: %v", err)
    }
    if s.Has("k") {
        t.Errorf("Has after Remove = true; want false")
    }
    if err := s.Remove("k"); err != nil {
        t.Fatalf("Remove again: %v", err)
    }
}

func TestStore_List_EmptyAndSorted(t *testing.T) {
    s := NewStore(t.TempDir())
    recs, err := s.List()
    if err != nil {
        t.Fatalf("List on empty: %v", err)
    }
    if recs == nil {
        t.Errorf("List returned nil on empty store; want non-nil slice")
    }
    if len(recs) != 0 {
        t.Errorf("len(List) = %d on empty; want 0", len(recs))
    }

    for _, k := range []string{"c", "a", "b"} {
        if err := s.Add(k, []byte(k)); err != nil {
            t.Fatalf("Add %q: %v", k, err)
        }
    }
    recs, err = s.List()
    if err != nil {
        t.Fatalf("List: %v", err)
    }
    got := []string{recs[0].Key, recs[1].Key, recs[2].Key}
    want := []string{"a", "b", "c"}
    for i := range got {
        if got[i] != want[i] {
            t.Errorf("List[%d] = %q, want %q", i, got[i], want[i])
        }
    }
}

func TestStore_Add_Idempotent(t *testing.T) {
    s := NewStore(t.TempDir())
    if err := s.Add("k", []byte("first")); err != nil {
        t.Fatal(err)
    }
    if err := s.Add("k", []byte("second")); err != nil {
        t.Fatal(err)
    }
    rec, err := s.Read("k")
    if err != nil {
        t.Fatal(err)
    }
    if string(rec.Body) != "second" {
        t.Errorf("Body after re-Add = %q, want %q", rec.Body, "second")
    }
}

func TestStore_Add_EmptyKey(t *testing.T) {
    s := NewStore(t.TempDir())
    if err := s.Add("", []byte("x")); err == nil {
        t.Errorf("Add with empty key returned nil error; want error")
    }
}

func TestStore_Read_MissingKey(t *testing.T) {
    s := NewStore(t.TempDir())
    if _, err := s.Read("nope"); err == nil {
        t.Errorf("Read missing returned nil error")
    }
}
```

- [ ] **Step 1.2b: Run — expect pass**

```
go test ./internal/approvalstore/... -v
```
Expected: all pass.

### Step 1.3: Permissions + concurrency tests

- [ ] **Step 1.3a: Add tests**

```go
// append to internal/approvalstore/store_test.go

import (
    "io/fs"
    "sync"
)

func TestStore_Permissions(t *testing.T) {
    dir := t.TempDir()
    s := NewStore(filepath.Join(dir, "nested"))
    if err := s.Add("k", []byte("v")); err != nil {
        t.Fatal(err)
    }
    info, err := os.Stat(filepath.Join(dir, "nested"))
    if err != nil {
        t.Fatal(err)
    }
    if info.Mode().Perm() != fs.FileMode(0o700) {
        t.Errorf("dir perm = %v, want 0700", info.Mode().Perm())
    }
    fi, err := os.Stat(filepath.Join(dir, "nested", "k"))
    if err != nil {
        t.Fatal(err)
    }
    if fi.Mode().Perm() != fs.FileMode(0o600) {
        t.Errorf("file perm = %v, want 0600", fi.Mode().Perm())
    }
}

func TestStore_Concurrent_DifferentKeys(t *testing.T) {
    s := NewStore(t.TempDir())
    var wg sync.WaitGroup
    const n = 50
    for i := 0; i < n; i++ {
        wg.Add(1)
        go func(i int) {
            defer wg.Done()
            key := []byte{byte('a' + i%26), byte('0' + i/26)}
            _ = s.Add(string(key), []byte("x"))
        }(i)
    }
    wg.Wait()
    recs, err := s.List()
    if err != nil {
        t.Fatal(err)
    }
    if len(recs) == 0 {
        t.Errorf("concurrent Add produced no records")
    }
}
```

- [ ] **Step 1.3b: Run**

```
go test ./internal/approvalstore/... -race -v
```
Expected: all pass, no race warnings.

### Step 1.4: Commit

- [ ] **Step 1.4: Commit**

```bash
git add internal/approvalstore/
# Use /commit inside Claude; direct git commit blocked by hook
```

Commit message (classic style):

```
Add approvalstore package for content-addressed approval records

Extract the directory-backed set primitive from the trust package so
the upcoming consent aggregate can reuse the same storage mechanics.
No behavioral change — the trust package refactor in the next commit
delegates to this primitive while keeping its public API intact.

Store supports Has/Add/Remove/List/Read with atomic writes, 0700
directory and 0600 file permissions, and idempotent Add.
```

---

## Task 2: Refactor `internal/trust/` onto `approvalstore` (no behavioral change)

**Files:**
- Modify: `internal/trust/trust.go` (replace direct filesystem calls with approvalstore)
- Modify: `internal/trust/trust_test.go` (add one namespace-layout assertion; existing tests unchanged)

### Why

Public API of trust must not change. Internally it now owns two `approvalstore.Store` instances (one for `trust/`, one for `deny/`). The existing test suite is the regression gate.

### Step 2.1: Capture existing test baseline

- [ ] **Step 2.1: Run existing trust tests — must pass**

```
go test ./internal/trust/... -v
```
Expected: all pass. Record test names for later verification.

### Step 2.2: Add new namespace-layout test (failing)

- [ ] **Step 2.2a: Add test**

```go
// append to internal/trust/trust_test.go

func TestStore_Namespaces_TrustAndDeny(t *testing.T) {
    base := t.TempDir()
    s := NewStore(base)

    _ = s.Trust("/tmp/a", []byte("content-a"))
    _ = s.Deny("/tmp/b")

    trustDir := filepath.Join(base, "trust")
    denyDir := filepath.Join(base, "deny")
    if _, err := os.Stat(trustDir); err != nil {
        t.Errorf("trust/ namespace missing: %v", err)
    }
    if _, err := os.Stat(denyDir); err != nil {
        t.Errorf("deny/ namespace missing: %v", err)
    }

    trustEntries, _ := os.ReadDir(trustDir)
    denyEntries, _ := os.ReadDir(denyDir)
    if len(trustEntries) != 1 {
        t.Errorf("trust/ entry count = %d, want 1", len(trustEntries))
    }
    if len(denyEntries) != 1 {
        t.Errorf("deny/ entry count = %d, want 1", len(denyEntries))
    }
}
```

- [ ] **Step 2.2b: Run — expect pass (existing behavior already creates these subdirs)**

```
go test ./internal/trust/... -run TestStore_Namespaces_TrustAndDeny -v
```
Expected: PASS. (This test documents the namespace contract we're preserving.)

### Step 2.3: Refactor trust to delegate to approvalstore

- [ ] **Step 2.3a: Replace the implementation**

```go
// internal/trust/trust.go — full replacement

package trust

import (
    "crypto/sha256"
    "encoding/hex"
    "path/filepath"

    "github.com/jskswamy/aide/internal/approvalstore"
)

type Status int

const (
    Untrusted Status = iota
    Trusted
    Denied
)

func (s Status) String() string {
    switch s {
    case Trusted:
        return "trusted"
    case Denied:
        return "denied"
    default:
        return "untrusted"
    }
}

type Store struct {
    trust *approvalstore.Store
    deny  *approvalstore.Store
}

func NewStore(baseDir string) *Store {
    return &Store{
        trust: approvalstore.NewStore(filepath.Join(baseDir, "trust")),
        deny:  approvalstore.NewStore(filepath.Join(baseDir, "deny")),
    }
}

func DefaultStore() *Store {
    return NewStore(approvalstore.DefaultRoot())
}

func FileHash(path string, contents []byte) string {
    h := sha256.New()
    h.Write([]byte(path))
    h.Write([]byte("\n"))
    h.Write(contents)
    return hex.EncodeToString(h.Sum(nil))
}

func PathHash(path string) string {
    h := sha256.New()
    h.Write([]byte(path))
    h.Write([]byte("\n"))
    return hex.EncodeToString(h.Sum(nil))
}

func (s *Store) Check(path string, contents []byte) Status {
    if s.deny.Has(PathHash(path)) {
        return Denied
    }
    if s.trust.Has(FileHash(path, contents)) {
        return Trusted
    }
    return Untrusted
}

func (s *Store) Trust(path string, contents []byte) error {
    if err := s.trust.Add(FileHash(path, contents), []byte(path)); err != nil {
        return err
    }
    return s.deny.Remove(PathHash(path))
}

func (s *Store) Deny(path string) error {
    return s.deny.Add(PathHash(path), []byte(path))
}

func (s *Store) Untrust(path string, contents []byte) error {
    return s.trust.Remove(FileHash(path, contents))
}
```

- [ ] **Step 2.3b: Run full existing test suite — expect pass**

```
go test ./internal/trust/... -v
```
Expected: every previously-passing test still passes.

### Step 2.4: Run downstream tests

- [ ] **Step 2.4: Any caller of trust must still pass**

```
go test ./internal/config/... ./cmd/aide/... -v
```
Expected: no regressions.

### Step 2.5: Commit

- [ ] **Step 2.5: Commit**

Commit message:

```
Refactor trust package to delegate storage to approvalstore

The trust aggregate now owns two approvalstore.Store instances, one
for the trust/ namespace and one for deny/. Public API, on-disk
layout, and file permissions are unchanged; the refactor is
infrastructure-only.

Existing trust tests continue to pass without modification. A new
namespace-layout test documents the subdirectory contract the
trust aggregate depends on.
```

---

## Task 3: `internal/consent/` — evidence + grant types

**Files:**
- Create: `internal/consent/evidence.go`
- Create: `internal/consent/evidence_test.go`

### Step 3.1: Test deterministic, order-insensitive `Evidence.Digest`

- [ ] **Step 3.1a: Add test**

```go
// internal/consent/evidence_test.go
package consent

import "testing"

func TestEvidence_Digest_Deterministic(t *testing.T) {
    e1 := Evidence{
        Variants: []string{"uv", "conda"},
        Matches: []MarkerMatch{
            {Kind: "file", Target: "uv.lock", Matched: true},
            {Kind: "file", Target: "environment.yml", Matched: true},
        },
    }
    e2 := Evidence{
        Variants: []string{"conda", "uv"},
        Matches: []MarkerMatch{
            {Kind: "file", Target: "environment.yml", Matched: true},
            {Kind: "file", Target: "uv.lock", Matched: true},
        },
    }
    if e1.Digest() != e2.Digest() {
        t.Errorf("digest mismatch across equivalent orderings:\n%s\n%s",
            e1.Digest(), e2.Digest())
    }
}

func TestEvidence_Digest_ChangesOnMatchFlip(t *testing.T) {
    base := Evidence{
        Variants: []string{"uv"},
        Matches: []MarkerMatch{
            {Kind: "file", Target: "uv.lock", Matched: true},
        },
    }
    flipped := Evidence{
        Variants: []string{"uv"},
        Matches: []MarkerMatch{
            {Kind: "file", Target: "uv.lock", Matched: false},
        },
    }
    if base.Digest() == flipped.Digest() {
        t.Errorf("digest unchanged after match flip")
    }
}

func TestEvidence_Digest_ChangesOnVariantSetChange(t *testing.T) {
    base := Evidence{Variants: []string{"uv"}, Matches: nil}
    extended := Evidence{Variants: []string{"uv", "conda"}, Matches: nil}
    if base.Digest() == extended.Digest() {
        t.Errorf("digest unchanged after variant added")
    }
}
```

- [ ] **Step 3.1b: Run — expect compile failure**

```
go test ./internal/consent/...
```
Expected: `package internal/consent not found` (not yet created).

- [ ] **Step 3.1c: Write implementation**

```go
// internal/consent/evidence.go
// Package consent stores user approvals of auto-detected toolchain
// variant selections. It is a sibling aggregate to the trust package
// within the User Approval bounded context. Storage mechanics are
// reused via internal/approvalstore.
package consent

import (
    "crypto/sha256"
    "encoding/hex"
    "sort"
    "strings"
)

// MarkerMatch records a single detection marker and whether it matched
// in the scanned project root.
type MarkerMatch struct {
    Kind    string // "file" | "contains" | "glob"
    Target  string // e.g. "uv.lock" or "pyproject.toml:[tool.uv]"
    Matched bool
}

// Evidence is the full detection result for one capability: which
// variants were selected and which markers drove the selection.
type Evidence struct {
    Variants []string
    Matches  []MarkerMatch
}

// Digest returns a SHA-256 over a canonicalized representation of the
// evidence. Order of Variants and Matches does not affect the digest;
// match flips and variant set changes do.
func (e Evidence) Digest() string {
    variants := append([]string(nil), e.Variants...)
    sort.Strings(variants)

    matches := append([]MarkerMatch(nil), e.Matches...)
    sort.Slice(matches, func(i, j int) bool {
        if matches[i].Kind != matches[j].Kind {
            return matches[i].Kind < matches[j].Kind
        }
        if matches[i].Target != matches[j].Target {
            return matches[i].Target < matches[j].Target
        }
        return !matches[i].Matched && matches[j].Matched
    })

    h := sha256.New()
    h.Write([]byte("v1\n"))
    h.Write([]byte(strings.Join(variants, ",")))
    h.Write([]byte("\n"))
    for _, m := range matches {
        h.Write([]byte(m.Kind))
        h.Write([]byte{0})
        h.Write([]byte(m.Target))
        h.Write([]byte{0})
        if m.Matched {
            h.Write([]byte("1"))
        } else {
            h.Write([]byte("0"))
        }
        h.Write([]byte{0})
    }
    return hex.EncodeToString(h.Sum(nil))
}
```

- [ ] **Step 3.1d: Run — expect pass**

```
go test ./internal/consent/... -v
```
Expected: all three tests PASS.

### Step 3.2: Commit evidence primitives

- [ ] **Step 3.2: Commit**

```
Add Evidence and MarkerMatch types for consent aggregate

Evidence captures which variants a detector selected for a capability
and which markers fired. Its Digest is deterministic and
order-insensitive, so equivalent detection outcomes hash to the same
value and any meaningful change (variant added/removed, marker flip)
produces a new digest that invalidates stored consent.
```

---

## Task 4: `internal/consent/` — Store with Check/Grant/Revoke/List

**Files:**
- Create: `internal/consent/consent.go`
- Create: `internal/consent/consent_test.go`

### Step 4.1: Test `ConsentHash` order-insensitivity and scope sensitivity

- [ ] **Step 4.1a: Add test**

```go
// internal/consent/consent_test.go
package consent

import (
    "testing"
    "time"
)

func TestConsentHash_OrderInsensitive(t *testing.T) {
    a := ConsentHash("/p", "python", []string{"uv", "conda"}, "digest")
    b := ConsentHash("/p", "python", []string{"conda", "uv"}, "digest")
    if a != b {
        t.Errorf("ConsentHash order-sensitive: %s vs %s", a, b)
    }
}

func TestConsentHash_ChangesOnScope(t *testing.T) {
    base := ConsentHash("/p", "python", []string{"uv"}, "d")
    changes := []string{
        ConsentHash("/q", "python", []string{"uv"}, "d"),
        ConsentHash("/p", "node", []string{"uv"}, "d"),
        ConsentHash("/p", "python", []string{"pyenv"}, "d"),
        ConsentHash("/p", "python", []string{"uv"}, "other"),
    }
    for i, c := range changes {
        if c == base {
            t.Errorf("change %d did not alter ConsentHash", i)
        }
    }
}
```

- [ ] **Step 4.1b: Run — expect compile failure**

Expected: `undefined: ConsentHash`.

- [ ] **Step 4.1c: Implement**

```go
// internal/consent/consent.go
package consent

import (
    "crypto/sha256"
    "encoding/hex"
    "fmt"
    "path/filepath"
    "sort"
    "strings"
    "time"

    "github.com/jskswamy/aide/internal/approvalstore"
)

// Status is the consent state for a (project, capability, variants,
// evidence) tuple.
type Status int

const (
    NotGranted Status = iota
    Granted
)

// Grant records one user approval.
type Grant struct {
    ProjectRoot string
    Capability  string
    Variants    []string
    Evidence    Evidence
    Summary     string
    ConfirmedAt time.Time
}

// Store persists grants under XDG_DATA_HOME/aide/consent/.
type Store struct {
    set *approvalstore.Store
}

// NewStore creates a consent store rooted at baseDir. baseDir should
// be the XDG-shared aide root; the store nests into consent/.
func NewStore(baseDir string) *Store {
    return &Store{set: approvalstore.NewStore(filepath.Join(baseDir, "consent"))}
}

// DefaultStore returns a Store under approvalstore.DefaultRoot().
func DefaultStore() *Store {
    return NewStore(approvalstore.DefaultRoot())
}

// ConsentHash computes the content-addressed key for a grant. The
// variants slice is sorted internally so ordering does not affect the
// hash.
func ConsentHash(projectRoot, capability string, variants []string, evidenceDigest string) string {
    sorted := append([]string(nil), variants...)
    sort.Strings(sorted)
    h := sha256.New()
    h.Write([]byte("consent-v1\n"))
    h.Write([]byte(projectRoot))
    h.Write([]byte{0})
    h.Write([]byte(capability))
    h.Write([]byte{0})
    h.Write([]byte(strings.Join(sorted, ",")))
    h.Write([]byte{0})
    h.Write([]byte(evidenceDigest))
    return hex.EncodeToString(h.Sum(nil))
}

// Check returns Granted when a record exists matching the exact
// (project, capability, evidence.Variants, evidence.Digest()) tuple.
func (s *Store) Check(projectRoot, capability string, evidence Evidence) Status {
    key := ConsentHash(projectRoot, capability, evidence.Variants, evidence.Digest())
    if s.set.Has(key) {
        return Granted
    }
    return NotGranted
}

// Grant records an approval. ConfirmedAt is set to time.Now if zero.
func (s *Store) Grant(g Grant) error {
    if g.ConfirmedAt.IsZero() {
        g.ConfirmedAt = time.Now().UTC()
    }
    key := ConsentHash(g.ProjectRoot, g.Capability, g.Variants, g.Evidence.Digest())
    body := fmt.Sprintf(
        "project: %s\ncapability: %s\nvariants: %s\nevidence_digest: %s\nevidence_summary: %s\nconfirmed_at: %s\n",
        g.ProjectRoot,
        g.Capability,
        strings.Join(g.Variants, ","),
        g.Evidence.Digest(),
        g.Summary,
        g.ConfirmedAt.Format(time.RFC3339),
    )
    return s.set.Add(key, []byte(body))
}

// Revoke removes every record whose stored body matches projectRoot
// and capability, regardless of variants or evidence digest.
func (s *Store) Revoke(projectRoot, capability string) error {
    records, err := s.set.List()
    if err != nil {
        return err
    }
    prefix := fmt.Sprintf("project: %s\ncapability: %s\n", projectRoot, capability)
    for _, r := range records {
        if strings.HasPrefix(string(r.Body), prefix) {
            if err := s.set.Remove(r.Key); err != nil {
                return err
            }
        }
    }
    return nil
}

// List returns all grants for projectRoot sorted by capability then
// confirmed time.
func (s *Store) List(projectRoot string) ([]Grant, error) {
    records, err := s.set.List()
    if err != nil {
        return nil, err
    }
    prefix := fmt.Sprintf("project: %s\n", projectRoot)
    out := make([]Grant, 0)
    for _, r := range records {
        if !strings.HasPrefix(string(r.Body), prefix) {
            continue
        }
        g, ok := parseGrantBody(r.Body)
        if !ok {
            continue
        }
        out = append(out, g)
    }
    sort.Slice(out, func(i, j int) bool {
        if out[i].Capability != out[j].Capability {
            return out[i].Capability < out[j].Capability
        }
        return out[i].ConfirmedAt.Before(out[j].ConfirmedAt)
    })
    return out, nil
}

func parseGrantBody(body []byte) (Grant, bool) {
    g := Grant{}
    for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
        parts := strings.SplitN(line, ": ", 2)
        if len(parts) != 2 {
            continue
        }
        switch parts[0] {
        case "project":
            g.ProjectRoot = parts[1]
        case "capability":
            g.Capability = parts[1]
        case "variants":
            if parts[1] != "" {
                g.Variants = strings.Split(parts[1], ",")
            }
        case "evidence_summary":
            g.Summary = parts[1]
        case "confirmed_at":
            if t, err := time.Parse(time.RFC3339, parts[1]); err == nil {
                g.ConfirmedAt = t
            }
        }
    }
    if g.ProjectRoot == "" || g.Capability == "" {
        return Grant{}, false
    }
    return g, true
}
```

- [ ] **Step 4.1d: Run — expect pass**

```
go test ./internal/consent/... -v
```

### Step 4.2: Full round-trip tests

- [ ] **Step 4.2a: Add tests**

```go
// append to internal/consent/consent_test.go

func TestStore_GrantCheckRoundTrip(t *testing.T) {
    s := NewStore(t.TempDir())
    ev := Evidence{
        Variants: []string{"uv"},
        Matches: []MarkerMatch{
            {Kind: "file", Target: "uv.lock", Matched: true},
        },
    }
    if s.Check("/proj", "python", ev) != NotGranted {
        t.Fatalf("Check before Grant != NotGranted")
    }
    err := s.Grant(Grant{
        ProjectRoot: "/proj",
        Capability:  "python",
        Variants:    []string{"uv"},
        Evidence:    ev,
        Summary:     "uv.lock",
    })
    if err != nil {
        t.Fatal(err)
    }
    if s.Check("/proj", "python", ev) != Granted {
        t.Errorf("Check after Grant != Granted")
    }
}

func TestStore_Check_EvidenceDigestMismatch(t *testing.T) {
    s := NewStore(t.TempDir())
    ev1 := Evidence{Variants: []string{"uv"}, Matches: []MarkerMatch{{"file", "uv.lock", true}}}
    ev2 := Evidence{Variants: []string{"uv"}, Matches: []MarkerMatch{{"file", "pyproject.toml:[tool.uv]", true}}}
    _ = s.Grant(Grant{ProjectRoot: "/p", Capability: "python", Variants: []string{"uv"}, Evidence: ev1})
    if s.Check("/p", "python", ev2) != NotGranted {
        t.Errorf("Check with different evidence digest returned Granted")
    }
}

func TestStore_Revoke_ClearsAllForCapability(t *testing.T) {
    s := NewStore(t.TempDir())
    evA := Evidence{Variants: []string{"uv"}, Matches: []MarkerMatch{{"file", "uv.lock", true}}}
    evB := Evidence{Variants: []string{"conda"}, Matches: []MarkerMatch{{"file", "environment.yml", true}}}
    _ = s.Grant(Grant{ProjectRoot: "/p", Capability: "python", Variants: []string{"uv"}, Evidence: evA})
    _ = s.Grant(Grant{ProjectRoot: "/p", Capability: "python", Variants: []string{"conda"}, Evidence: evB})
    if err := s.Revoke("/p", "python"); err != nil {
        t.Fatal(err)
    }
    if s.Check("/p", "python", evA) != NotGranted {
        t.Errorf("Revoke left uv grant behind")
    }
    if s.Check("/p", "python", evB) != NotGranted {
        t.Errorf("Revoke left conda grant behind")
    }
}

func TestStore_Revoke_IsIdempotent(t *testing.T) {
    s := NewStore(t.TempDir())
    if err := s.Revoke("/p", "python"); err != nil {
        t.Errorf("Revoke on empty store: %v", err)
    }
}

func TestStore_List_FiltersByProject(t *testing.T) {
    s := NewStore(t.TempDir())
    ev := Evidence{Variants: []string{"uv"}, Matches: []MarkerMatch{{"file", "uv.lock", true}}}
    _ = s.Grant(Grant{ProjectRoot: "/a", Capability: "python", Variants: []string{"uv"}, Evidence: ev, Summary: "a"})
    _ = s.Grant(Grant{ProjectRoot: "/b", Capability: "python", Variants: []string{"uv"}, Evidence: ev, Summary: "b"})
    gs, err := s.List("/a")
    if err != nil {
        t.Fatal(err)
    }
    if len(gs) != 1 || gs[0].ProjectRoot != "/a" {
        t.Errorf("List(/a) = %v, want one grant for /a", gs)
    }
}
```

- [ ] **Step 4.2b: Run — expect pass**

```
go test ./internal/consent/... -v
```

### Step 4.3: Commit

- [ ] **Step 4.3: Commit**

```
Add consent store for detection-derived variant grants

The consent aggregate mirrors trust's on-disk pattern: a
content-addressed set under XDG_DATA_HOME/aide/consent/ keyed by
SHA-256 over (project, capability, variants, evidence digest). Check
returns Granted only when the exact evidence digest matches, so any
change in detected markers re-invalidates a pin. Revoke clears every
grant for (project, capability) across evidence histories.
```

---

## Task 5: Extend `capability.Capability` with Variants + Marker matching

**Files:**
- Create: `internal/capability/variant.go`
- Create: `internal/capability/variant_test.go`
- Modify: `internal/capability/capability.go` (add `Variants`, `DefaultVariants` fields)

### Step 5.1: Add `Variant` and `Marker` types

- [ ] **Step 5.1a: Add test (marker matching)**

```go
// internal/capability/variant_test.go
package capability

import (
    "os"
    "path/filepath"
    "testing"
)

func writeFile(t *testing.T, dir, name, body string) {
    t.Helper()
    if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
        t.Fatal(err)
    }
}

func TestMarker_FileMatch(t *testing.T) {
    dir := t.TempDir()
    writeFile(t, dir, "uv.lock", "")
    m := Marker{File: "uv.lock"}
    if !m.Match(dir) {
        t.Errorf("Marker{File: uv.lock}.Match = false; want true")
    }
    m2 := Marker{File: "missing.lock"}
    if m2.Match(dir) {
        t.Errorf("Marker on missing file matched")
    }
}

func TestMarker_ContainsMatch(t *testing.T) {
    dir := t.TempDir()
    writeFile(t, dir, "pyproject.toml", "[tool.poetry]\nname=\"x\"\n")
    m := Marker{Contains: ContainsSpec{File: "pyproject.toml", Pattern: "[tool.poetry]"}}
    if !m.Match(dir) {
        t.Errorf("Contains marker did not match present pattern")
    }
    m2 := Marker{Contains: ContainsSpec{File: "pyproject.toml", Pattern: "[tool.uv]"}}
    if m2.Match(dir) {
        t.Errorf("Contains marker matched absent pattern")
    }
}

func TestMarker_GlobMatch(t *testing.T) {
    dir := t.TempDir()
    writeFile(t, dir, "a.tf", "")
    m := Marker{GlobPath: "*.tf"}
    if !m.Match(dir) {
        t.Errorf("Glob marker did not match *.tf")
    }
}

func TestMarker_Validate_ExactlyOneFieldSet(t *testing.T) {
    cases := []struct {
        name  string
        m     Marker
        valid bool
    }{
        {"file only", Marker{File: "x"}, true},
        {"contains only", Marker{Contains: ContainsSpec{File: "x", Pattern: "y"}}, true},
        {"glob only", Marker{GlobPath: "*.x"}, true},
        {"empty", Marker{}, false},
        {"file+glob", Marker{File: "x", GlobPath: "*.x"}, false},
    }
    for _, tc := range cases {
        err := tc.m.Validate()
        gotValid := err == nil
        if gotValid != tc.valid {
            t.Errorf("%s: Validate err=%v, wantValid=%v", tc.name, err, tc.valid)
        }
    }
}
```

- [ ] **Step 5.1b: Run — expect compile failure**

- [ ] **Step 5.1c: Implement**

```go
// internal/capability/variant.go
package capability

import (
    "errors"
    "os"
    "path/filepath"
    "strings"
)

const markerMaxReadSize = 64 * 1024

// Variant is a refinement of a Capability: a specific toolchain
// implementation (e.g. uv within python) with its own markers and
// path/env contributions.
type Variant struct {
    Name        string
    Description string
    Markers     []Marker
    Readable    []string
    Writable    []string
    EnvAllow    []string
    EnableGuard []string
}

// Marker is a detection rule. Exactly one of File, Contains, or
// GlobPath must be set.
type Marker struct {
    File     string
    Contains ContainsSpec
    GlobPath string
}

// ContainsSpec describes a substring check within a file.
type ContainsSpec struct {
    File    string
    Pattern string
}

// Validate ensures exactly one field is set.
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
    if n != 1 {
        return errors.New("marker: exactly one of File, Contains, or GlobPath must be set")
    }
    return nil
}

// Match reports whether the marker matches within projectRoot.
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
    return false
}

func containsInBoundedFile(path, pattern string) bool {
    f, err := os.Open(path)
    if err != nil {
        return false
    }
    defer func() { _ = f.Close() }()
    buf := make([]byte, markerMaxReadSize)
    n, _ := f.Read(buf)
    return strings.Contains(string(buf[:n]), pattern)
}

// MatchSummary returns a short human-readable label for a marker,
// suitable for consent prompts and log lines.
func (m Marker) MatchSummary() string {
    switch {
    case m.File != "":
        return m.File
    case m.Contains.File != "":
        return m.Contains.File + ":" + m.Contains.Pattern
    case m.GlobPath != "":
        return m.GlobPath
    }
    return "<empty-marker>"
}
```

- [ ] **Step 5.1d: Run — expect pass**

```
go test ./internal/capability/... -run TestMarker -v
```

### Step 5.2: Add `Variants`/`DefaultVariants` fields to `Capability` struct

- [ ] **Step 5.2a: Modify `internal/capability/capability.go`**

Change the `Capability` struct (around line 10) by adding two fields:

```go
type Capability struct {
    Name        string
    Description string
    Extends     string
    Combines    []string
    Unguard     []string
    Readable    []string
    Writable    []string
    Deny        []string
    EnvAllow    []string
    EnableGuard []string
    Allow       []string
    NetworkMode string
    // NEW:
    Variants        []Variant // optional; when set, detection picks a subset to activate
    DefaultVariants []string  // variants activated when no markers match
}
```

- [ ] **Step 5.2b: Verify existing tests still pass**

```
go test ./internal/capability/... -v
```
Expected: existing tests unchanged; no behavioral change yet (nothing reads `Variants`).

### Step 5.3: Commit

- [ ] **Step 5.3: Commit**

```
Add Variant and Marker types to the capability model

Capabilities may now carry a Variants list of per-toolchain refinements
plus a DefaultVariants fallback. Each Variant declares its own
Markers, Readable/Writable paths, and env-var allow-list. Marker
matching covers exact files, substring-in-file (bounded 64KB), and
glob patterns at the project root.

No user-facing capability behavior changes in this commit — the fields
exist on the struct but no builtin capability populates them yet.
```

---

## Task 6: Detector — `DetectEvidence(cap, projectRoot)`

**Files:**
- Create: `internal/capability/detect_variants.go`
- Create: `internal/capability/detect_variants_test.go`

### Why

The generic detector walks a capability's `Variants` and returns `consent.Evidence` (variants whose markers matched + the full match set).

### Step 6.1: Test with synthetic project

- [ ] **Step 6.1a: Add test**

```go
// internal/capability/detect_variants_test.go
package capability

import (
    "os"
    "path/filepath"
    "testing"
)

func TestDetectEvidence_MatchesAllFiringVariants(t *testing.T) {
    dir := t.TempDir()
    _ = os.WriteFile(filepath.Join(dir, "uv.lock"), nil, 0o600)
    _ = os.WriteFile(filepath.Join(dir, "environment.yml"), nil, 0o600)

    cap := Capability{
        Name: "python",
        Variants: []Variant{
            {Name: "uv", Markers: []Marker{{File: "uv.lock"}}},
            {Name: "conda", Markers: []Marker{{File: "environment.yml"}}},
            {Name: "poetry", Markers: []Marker{{File: "poetry.lock"}}},
        },
    }
    ev := DetectEvidence(cap, dir)
    wantVariants := map[string]bool{"uv": true, "conda": true}
    if len(ev.Variants) != len(wantVariants) {
        t.Fatalf("len(Variants) = %d, want %d (%v)", len(ev.Variants), len(wantVariants), ev.Variants)
    }
    for _, v := range ev.Variants {
        if !wantVariants[v] {
            t.Errorf("unexpected variant detected: %s", v)
        }
    }
}

func TestDetectEvidence_NoMatches_EmptyVariants(t *testing.T) {
    dir := t.TempDir()
    cap := Capability{
        Name: "python",
        Variants: []Variant{
            {Name: "uv", Markers: []Marker{{File: "uv.lock"}}},
        },
    }
    ev := DetectEvidence(cap, dir)
    if len(ev.Variants) != 0 {
        t.Errorf("len(Variants) = %d, want 0", len(ev.Variants))
    }
}
```

- [ ] **Step 6.1b: Run — expect compile failure**

- [ ] **Step 6.1c: Implement**

```go
// internal/capability/detect_variants.go
package capability

import (
    "sort"

    "github.com/jskswamy/aide/internal/consent"
)

// DetectEvidence runs every variant's markers against projectRoot and
// returns evidence naming the variants whose markers ALL fired plus
// the full set of match results (sorted, deterministic).
//
// A variant is considered selected when every one of its markers
// matches. Variants with no markers are never selected by detection
// (callers must pin them via config or flag).
func DetectEvidence(cap Capability, projectRoot string) consent.Evidence {
    selected := make([]string, 0, len(cap.Variants))
    matches := make([]consent.MarkerMatch, 0)
    for _, v := range cap.Variants {
        if len(v.Markers) == 0 {
            continue
        }
        allMatch := true
        for _, m := range v.Markers {
            ok := m.Match(projectRoot)
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

func markerKind(m Marker) string {
    switch {
    case m.File != "":
        return "file"
    case m.Contains.File != "":
        return "contains"
    case m.GlobPath != "":
        return "glob"
    }
    return ""
}
```

- [ ] **Step 6.1d: Run — expect pass**

### Step 6.2: Commit

- [ ] **Step 6.2: Commit**

```
Add DetectEvidence to walk capability variants against project markers

DetectEvidence returns consent.Evidence listing which variants have
every one of their markers satisfied in the project root, together
with the full sorted match set used to compute the evidence digest.
Variants with no markers are skipped — they must be pinned via CLI or
config.
```

---

## Task 7: `SelectVariants` — the five-state decision table

**Files:**
- Create: `internal/capability/select.go`
- Create: `internal/capability/select_test.go`

### Step 7.1: Prompter interface + test fixture

- [ ] **Step 7.1a: Add test**

```go
// internal/capability/select_test.go
package capability

import (
    "testing"

    "github.com/jskswamy/aide/internal/consent"
)

type fakePrompter struct {
    returnedVariants []string
    called           int
}

func (f *fakePrompter) PromptVariantConsent(in PromptInput) PromptResult {
    f.called++
    return PromptResult{
        Decision: PromptYes,
        Variants: f.returnedVariants,
    }
}

func newTestCap() Capability {
    return Capability{
        Name: "python",
        Variants: []Variant{
            {Name: "uv", Markers: []Marker{{File: "uv.lock"}}},
            {Name: "pyenv", Markers: []Marker{{File: ".python-version"}}},
            {Name: "venv"},
        },
        DefaultVariants: []string{"venv"},
    }
}

func TestSelect_StateA_FirstTime_PromptsAndGrants(t *testing.T) {
    dir := t.TempDir()
    writeFile(t, dir, "uv.lock", "")
    cap := newTestCap()
    cstore := consent.NewStore(t.TempDir())
    p := &fakePrompter{returnedVariants: []string{"uv"}}

    got, prov, err := SelectVariants(SelectInput{
        Capability:    cap,
        ProjectRoot:   dir,
        Overrides:     nil,
        YAMLPins:      nil,
        Consent:       cstore,
        Prompter:      p,
        Interactive:   true,
    })
    if err != nil {
        t.Fatal(err)
    }
    if len(got) != 1 || got[0].Name != "uv" {
        t.Errorf("selected = %v, want [uv]", got)
    }
    if prov.Reason != "consent:granted" {
        t.Errorf("Provenance.Reason = %q, want consent:granted", prov.Reason)
    }
    if p.called != 1 {
        t.Errorf("prompter call count = %d, want 1", p.called)
    }
    // Subsequent call should be silent
    p.called = 0
    _, prov2, _ := SelectVariants(SelectInput{
        Capability: cap, ProjectRoot: dir, Consent: cstore,
        Prompter: p, Interactive: true,
    })
    if p.called != 0 {
        t.Errorf("second call prompted again; should be silent")
    }
    if prov2.Reason != "consent:stable" {
        t.Errorf("second Provenance.Reason = %q, want consent:stable", prov2.Reason)
    }
}

func TestSelect_StateD_YAMLPinBypassesConsent(t *testing.T) {
    cap := newTestCap()
    cstore := consent.NewStore(t.TempDir())
    p := &fakePrompter{}
    got, prov, _ := SelectVariants(SelectInput{
        Capability: cap, ProjectRoot: t.TempDir(),
        YAMLPins:   []string{"uv"},
        Consent:    cstore, Prompter: p, Interactive: true,
    })
    if len(got) != 1 || got[0].Name != "uv" {
        t.Errorf("yaml pin not honored; got %v", got)
    }
    if p.called != 0 {
        t.Errorf("prompter called despite yaml pin")
    }
    if prov.Reason != "yaml-pin" {
        t.Errorf("Reason = %q, want yaml-pin", prov.Reason)
    }
}

func TestSelect_StateE_CLIOverrideWins(t *testing.T) {
    cap := newTestCap()
    cstore := consent.NewStore(t.TempDir())
    p := &fakePrompter{}
    got, prov, _ := SelectVariants(SelectInput{
        Capability: cap, ProjectRoot: t.TempDir(),
        Overrides:  []string{"pyenv"},
        YAMLPins:   []string{"uv"},
        Consent:    cstore, Prompter: p, Interactive: true,
    })
    if len(got) != 1 || got[0].Name != "pyenv" {
        t.Errorf("override not honored; got %v", got)
    }
    if p.called != 0 {
        t.Errorf("prompter called despite override")
    }
    if prov.Reason != "cli-override" {
        t.Errorf("Reason = %q, want cli-override", prov.Reason)
    }
}

func TestSelect_StateC_EvidenceChanged_Reprompts(t *testing.T) {
    dir := t.TempDir()
    writeFile(t, dir, "uv.lock", "")
    cap := newTestCap()
    cstore := consent.NewStore(t.TempDir())
    p := &fakePrompter{returnedVariants: []string{"uv"}}

    // grant initial uv
    _, _, _ = SelectVariants(SelectInput{
        Capability: cap, ProjectRoot: dir, Consent: cstore,
        Prompter: p, Interactive: true,
    })
    if p.called != 1 {
        t.Fatalf("initial prompt count = %d", p.called)
    }

    // evidence changes: remove uv.lock, add .python-version
    _ = removeFile(filepath.Join(dir, "uv.lock"))
    writeFile(t, dir, ".python-version", "3.11")
    p.returnedVariants = []string{"pyenv"}
    p.called = 0

    got, prov, _ := SelectVariants(SelectInput{
        Capability: cap, ProjectRoot: dir, Consent: cstore,
        Prompter: p, Interactive: true,
    })
    if p.called != 1 {
        t.Errorf("evidence change did not re-prompt; called=%d", p.called)
    }
    if len(got) != 1 || got[0].Name != "pyenv" {
        t.Errorf("selected = %v, want [pyenv]", got)
    }
    if prov.Reason != "consent:granted" {
        t.Errorf("Reason = %q, want consent:granted", prov.Reason)
    }
}

func TestSelect_NonInteractive_FallsThroughToDefault(t *testing.T) {
    dir := t.TempDir()
    writeFile(t, dir, "uv.lock", "")
    cap := newTestCap()
    cstore := consent.NewStore(t.TempDir())
    got, prov, err := SelectVariants(SelectInput{
        Capability:  cap,
        ProjectRoot: dir,
        Consent:     cstore,
        Prompter:    nil, // no TTY
        Interactive: false,
    })
    if err != nil {
        t.Fatal(err)
    }
    if len(got) != 1 || got[0].Name != "venv" {
        t.Errorf("fallback = %v, want [venv]", got)
    }
    if prov.Reason != "default:non-interactive" {
        t.Errorf("Reason = %q, want default:non-interactive", prov.Reason)
    }
}

func TestSelect_PromptNoKeepsDefault(t *testing.T) {
    dir := t.TempDir()
    writeFile(t, dir, "uv.lock", "")
    cap := newTestCap()
    cstore := consent.NewStore(t.TempDir())
    p := &fakePrompterNo{}
    got, prov, _ := SelectVariants(SelectInput{
        Capability: cap, ProjectRoot: dir, Consent: cstore,
        Prompter: p, Interactive: true,
    })
    if len(got) != 1 || got[0].Name != "venv" {
        t.Errorf("got %v, want [venv] (No answer falls to default)", got)
    }
    if prov.Reason != "default:declined" {
        t.Errorf("Reason = %q, want default:declined", prov.Reason)
    }
}

type fakePrompterNo struct{}
func (f *fakePrompterNo) PromptVariantConsent(in PromptInput) PromptResult {
    return PromptResult{Decision: PromptNo}
}

// helper for the filepath package import + os.Remove
func removeFile(p string) error { return os.Remove(p) }
```

Also in that test file, add these imports at top:
```go
import (
    "os"
    "path/filepath"
    "testing"

    "github.com/jskswamy/aide/internal/consent"
)
```

- [ ] **Step 7.1b: Run — expect compile failure**

- [ ] **Step 7.1c: Implement**

```go
// internal/capability/select.go
package capability

import (
    "time"

    "github.com/jskswamy/aide/internal/consent"
)

// PromptDecision is the user's answer to a consent prompt.
type PromptDecision int

const (
    PromptYes PromptDecision = iota
    PromptNo
    PromptSkip
)

// PromptInput carries the data a prompter needs to render a
// consent request.
type PromptInput struct {
    Capability       string
    DetectedVariants []Variant
    PreviousVariants []string // empty on first-time
    Evidence         consent.Evidence
}

// PromptResult is the prompter's answer. When Decision is PromptYes,
// Variants is the subset the user approved (equal to DetectedVariants
// for a plain "yes", a subset for the Customize sub-flow).
type PromptResult struct {
    Decision PromptDecision
    Variants []string
}

// Prompter renders and collects a user's consent decision.
type Prompter interface {
    PromptVariantConsent(in PromptInput) PromptResult
}

// Provenance traces why a particular variant set was chosen.
type Provenance struct {
    Variants []string
    Reason   string // "cli-override" | "yaml-pin" | "consent:granted" |
                   // "consent:stable" | "default:declined" |
                   // "default:non-interactive" | "default:skipped" |
                   // "default:no-evidence"
}

// SelectInput is the composite set of inputs to SelectVariants.
type SelectInput struct {
    Capability  Capability
    ProjectRoot string
    Overrides   []string // from --variant
    YAMLPins    []string // from .aide.yaml capabilities.<cap>.variants
    Consent     *consent.Store
    Prompter    Prompter
    Interactive bool
    AutoYes     bool // --yes: treat PromptYes as the answer without calling prompter
}

// SelectVariants runs the five-state decision table from the design.
func SelectVariants(in SelectInput) ([]Variant, Provenance, error) {
    // State E: CLI override wins.
    if len(in.Overrides) > 0 {
        selected, err := variantsByName(in.Capability, in.Overrides)
        if err != nil {
            return nil, Provenance{}, err
        }
        return selected, Provenance{Variants: names(selected), Reason: "cli-override"}, nil
    }

    // State D: YAML pin is explicit intent.
    if len(in.YAMLPins) > 0 {
        selected, err := variantsByName(in.Capability, in.YAMLPins)
        if err != nil {
            return nil, Provenance{}, err
        }
        return selected, Provenance{Variants: names(selected), Reason: "yaml-pin"}, nil
    }

    evidence := DetectEvidence(in.Capability, in.ProjectRoot)

    // No markers fired → default.
    if len(evidence.Variants) == 0 {
        defaults, err := variantsByName(in.Capability, in.Capability.DefaultVariants)
        if err != nil {
            return nil, Provenance{}, err
        }
        return defaults, Provenance{Variants: names(defaults), Reason: "default:no-evidence"}, nil
    }

    // State B: stable consent.
    if in.Consent != nil && in.Consent.Check(in.ProjectRoot, in.Capability.Name, evidence) == consent.Granted {
        selected, err := variantsByName(in.Capability, evidence.Variants)
        if err != nil {
            return nil, Provenance{}, err
        }
        return selected, Provenance{Variants: names(selected), Reason: "consent:stable"}, nil
    }

    // States A and C: need a decision.
    if !in.Interactive || in.Prompter == nil {
        defaults, err := variantsByName(in.Capability, in.Capability.DefaultVariants)
        if err != nil {
            return nil, Provenance{}, err
        }
        return defaults, Provenance{Variants: names(defaults), Reason: "default:non-interactive"}, nil
    }

    detected, err := variantsByName(in.Capability, evidence.Variants)
    if err != nil {
        return nil, Provenance{}, err
    }

    var result PromptResult
    if in.AutoYes {
        result = PromptResult{Decision: PromptYes, Variants: evidence.Variants}
    } else {
        result = in.Prompter.PromptVariantConsent(PromptInput{
            Capability:       in.Capability.Name,
            DetectedVariants: detected,
            PreviousVariants: previousVariants(in.Consent, in.ProjectRoot, in.Capability.Name),
            Evidence:         evidence,
        })
    }

    switch result.Decision {
    case PromptYes:
        approved, err := variantsByName(in.Capability, result.Variants)
        if err != nil {
            return nil, Provenance{}, err
        }
        if in.Consent != nil {
            _ = in.Consent.Grant(consent.Grant{
                ProjectRoot: in.ProjectRoot,
                Capability:  in.Capability.Name,
                Variants:    result.Variants,
                Evidence:    evidence,
                Summary:     summarizeEvidence(evidence),
                ConfirmedAt: time.Now().UTC(),
            })
        }
        return approved, Provenance{Variants: names(approved), Reason: "consent:granted"}, nil
    case PromptSkip:
        defaults, err := variantsByName(in.Capability, in.Capability.DefaultVariants)
        if err != nil {
            return nil, Provenance{}, err
        }
        return defaults, Provenance{Variants: names(defaults), Reason: "default:skipped"}, nil
    default: // PromptNo
        defaults, err := variantsByName(in.Capability, in.Capability.DefaultVariants)
        if err != nil {
            return nil, Provenance{}, err
        }
        return defaults, Provenance{Variants: names(defaults), Reason: "default:declined"}, nil
    }
}

func variantsByName(cap Capability, names []string) ([]Variant, error) {
    out := make([]Variant, 0, len(names))
    for _, n := range names {
        found := false
        for _, v := range cap.Variants {
            if v.Name == n {
                out = append(out, v)
                found = true
                break
            }
        }
        if !found {
            return nil, &UnknownVariantError{Capability: cap.Name, Variant: n, Available: allVariantNames(cap)}
        }
    }
    return out, nil
}

func names(vs []Variant) []string {
    out := make([]string, len(vs))
    for i, v := range vs {
        out[i] = v.Name
    }
    return out
}

func allVariantNames(cap Capability) []string {
    out := make([]string, len(cap.Variants))
    for i, v := range cap.Variants {
        out[i] = v.Name
    }
    return out
}

// UnknownVariantError is returned when a caller names a variant the
// capability does not declare.
type UnknownVariantError struct {
    Capability string
    Variant    string
    Available  []string
}

func (e *UnknownVariantError) Error() string {
    return "unknown variant '" + e.Variant + "' for capability '" + e.Capability +
        "'. available: " + joinCSV(e.Available)
}

func joinCSV(s []string) string {
    out := ""
    for i, v := range s {
        if i > 0 {
            out += ", "
        }
        out += v
    }
    return out
}

func previousVariants(store *consent.Store, project, cap string) []string {
    if store == nil {
        return nil
    }
    grants, err := store.List(project)
    if err != nil {
        return nil
    }
    for _, g := range grants {
        if g.Capability == cap {
            return g.Variants
        }
    }
    return nil
}

func summarizeEvidence(e consent.Evidence) string {
    out := ""
    for i, m := range e.Matches {
        if !m.Matched {
            continue
        }
        if i > 0 && out != "" {
            out += ", "
        }
        out += m.Target
    }
    return out
}
```

- [ ] **Step 7.1d: Run — expect pass**

```
go test ./internal/capability/... -v
```

### Step 7.2: Commit

- [ ] **Step 7.2: Commit**

```
Add SelectVariants with five-state decision table

SelectVariants picks the active variant set for a capability under
the precedence rules from the design spec: CLI overrides (--variant)
win, YAML pins are next, then consent-gated auto-detection, and
finally DefaultVariants. Non-interactive contexts never auto-grant,
falling through to DefaultVariants with a warning provenance.

A Prompter interface abstracts the consent UI so the selector is
unit-testable without a TTY.
```

---

## Task 8: Populate python variants in `builtin.go`

**Files:**
- Modify: `internal/capability/builtin.go`
- Modify or add: `internal/capability/builtin_test.go`

### Step 8.1: Replace the static `python` capability with the variant catalog

- [ ] **Step 8.1a: Add test first**

```go
// append to internal/capability/builtin_test.go

func TestBuiltins_PythonHasVariantCatalog(t *testing.T) {
    b := Builtins()
    py, ok := b["python"]
    if !ok {
        t.Fatal("builtin 'python' missing")
    }
    wantNames := map[string]bool{"uv": true, "pyenv": true, "conda": true, "poetry": true, "venv": true}
    if len(py.Variants) != len(wantNames) {
        t.Fatalf("variant count = %d, want %d", len(py.Variants), len(wantNames))
    }
    for _, v := range py.Variants {
        if !wantNames[v.Name] {
            t.Errorf("unexpected variant: %s", v.Name)
        }
    }
    wantDefault := []string{"venv"}
    if len(py.DefaultVariants) != 1 || py.DefaultVariants[0] != wantDefault[0] {
        t.Errorf("DefaultVariants = %v, want %v", py.DefaultVariants, wantDefault)
    }
}
```

- [ ] **Step 8.1b: Run — expect fail**

- [ ] **Step 8.1c: Update `builtin.go` `"python"` entry**

Replace the existing `"python"` entry in `builtin.go` with:

```go
"python": {
    Name:        "python",
    Description: "Python toolchain",
    Variants: []Variant{
        {
            Name:        "uv",
            Description: "uv — fast Python package/project manager",
            Markers: []Marker{
                {File: "uv.lock"},
            },
            Readable: nil,
            Writable: []string{
                "~/.local/share/uv",
                "~/.cache/uv",
            },
            EnvAllow: []string{"UV_CACHE_DIR", "UV_PYTHON_INSTALL_DIR", "VIRTUAL_ENV"},
        },
        {
            Name:        "pyenv",
            Description: "pyenv — Simple Python version management",
            Markers: []Marker{
                {File: ".python-version"},
            },
            Writable: []string{"~/.pyenv"},
            EnvAllow: []string{"PYENV_ROOT", "VIRTUAL_ENV"},
        },
        {
            Name:        "conda",
            Description: "Conda / Mamba — scientific Python",
            Markers: []Marker{
                {File: "environment.yml"},
            },
            Writable: []string{"~/.conda", "~/miniconda3", "~/anaconda3"},
            EnvAllow: []string{"CONDA_PREFIX", "CONDA_DEFAULT_ENV"},
        },
        {
            Name:        "poetry",
            Description: "Poetry — dependency management and packaging",
            Markers: []Marker{
                {File: "poetry.lock"},
            },
            Writable: []string{"~/.cache/pypoetry", "~/Library/Caches/pypoetry"},
            EnvAllow: []string{"POETRY_HOME"},
        },
        {
            Name:        "venv",
            Description: "Standard library venv — no managed interpreter",
            // No markers → never auto-selected; used as safe default.
            EnvAllow: []string{"VIRTUAL_ENV"},
        },
    },
    DefaultVariants: []string{"venv"},
},
```

Also extend `ToSandboxOverrides` in `capability.go`:
- Accept the **selected variants** and merge their `Readable`/`Writable`/`EnvAllow`/`EnableGuard` on top of the capability's base contributions.
- Add a helper `MergeSelectedVariants(resolved *ResolvedCapability, selected []Variant)` that returns a new `ResolvedCapability`.

```go
// internal/capability/capability.go — add near mergeChild
func MergeSelectedVariants(rc *ResolvedCapability, selected []Variant) *ResolvedCapability {
    out := *rc
    out.Readable = copyStrings(rc.Readable)
    out.Writable = copyStrings(rc.Writable)
    out.EnvAllow = copyStrings(rc.EnvAllow)
    out.EnableGuard = copyStrings(rc.EnableGuard)
    for _, v := range selected {
        out.Readable = dedup(append(out.Readable, v.Readable...))
        out.Writable = dedup(append(out.Writable, v.Writable...))
        out.EnvAllow = dedup(append(out.EnvAllow, v.EnvAllow...))
        out.EnableGuard = dedup(append(out.EnableGuard, v.EnableGuard...))
    }
    return &out
}
```

- [ ] **Step 8.1d: Run**

```
go test ./internal/capability/... -v
```
Expected: all pass, including the new `TestBuiltins_PythonHasVariantCatalog` and existing capability tests.

### Step 8.2: Commit

- [ ] **Step 8.2: Commit**

```
Populate python variant catalog and add MergeSelectedVariants

The python capability now declares five variants: uv, pyenv, conda,
poetry, and venv. Each lists the markers that trigger detection plus
the path and environment variable contributions the variant needs.
venv is the default fallback when no markers match.

MergeSelectedVariants layers the chosen variants' paths onto a
ResolvedCapability so downstream sandbox overrides receive the union
of base + variant permissions.
```

---

## Task 9: Consent prompt UI (`internal/ui/consentprompt.go`)

**Files:**
- Create: `internal/ui/consentprompt.go`
- Create: `internal/ui/consentprompt_test.go`

### Step 9.1: Test render first-time vs. change prompt

- [ ] **Step 9.1a: Add rendering tests**

```go
// internal/ui/consentprompt_test.go
package ui

import (
    "strings"
    "testing"

    "github.com/jskswamy/aide/internal/capability"
    "github.com/jskswamy/aide/internal/consent"
)

func TestRenderPrompt_FirstTime_OmitsPrevious(t *testing.T) {
    in := capability.PromptInput{
        Capability: "python",
        DetectedVariants: []capability.Variant{
            {Name: "uv", Writable: []string{"~/.local/share/uv"}, EnvAllow: []string{"UV_CACHE_DIR"},
             Markers: []capability.Marker{{File: "uv.lock"}}},
        },
        PreviousVariants: nil,
        Evidence: consent.Evidence{Variants: []string{"uv"}, Matches: []consent.MarkerMatch{
            {Kind: "file", Target: "uv.lock", Matched: true},
        }},
    }
    out := RenderPrompt(in)
    if strings.Contains(out, "Previously:") {
        t.Errorf("first-time prompt should not contain 'Previously:', got:\n%s", out)
    }
    if !strings.Contains(out, "Detected:") {
        t.Errorf("missing 'Detected:' line")
    }
    if !strings.Contains(out, "uv") {
        t.Errorf("missing variant 'uv'")
    }
    if !strings.Contains(out, "~/.local/share/uv") {
        t.Errorf("missing granted path")
    }
}

func TestRenderPrompt_Change_IncludesPrevious(t *testing.T) {
    in := capability.PromptInput{
        Capability: "python",
        DetectedVariants: []capability.Variant{
            {Name: "conda", Writable: []string{"~/.conda"},
             Markers: []capability.Marker{{File: "environment.yml"}}},
        },
        PreviousVariants: []string{"uv"},
        Evidence: consent.Evidence{Variants: []string{"conda"}},
    }
    out := RenderPrompt(in)
    if !strings.Contains(out, "Previously: uv") {
        t.Errorf("missing 'Previously: uv', got:\n%s", out)
    }
}
```

- [ ] **Step 9.1b: Run — expect compile failure**

- [ ] **Step 9.1c: Implement**

```go
// internal/ui/consentprompt.go
package ui

import (
    "fmt"
    "strings"

    "github.com/jskswamy/aide/internal/capability"
)

// RenderPrompt returns the multi-line prompt text for a variant
// consent request. It never reads from stdin; callers pair this with
// a separate input function (see TTYPrompter below).
func RenderPrompt(in capability.PromptInput) string {
    var b strings.Builder
    fmt.Fprintf(&b, "[aide] %s — detection needs your confirmation\n\n", in.Capability)
    if len(in.PreviousVariants) > 0 {
        fmt.Fprintf(&b, "  Previously: %s\n", strings.Join(in.PreviousVariants, " + "))
    }
    names := make([]string, len(in.DetectedVariants))
    markerLines := make([]string, 0)
    paths := make([]string, 0)
    envs := make([]string, 0)
    for i, v := range in.DetectedVariants {
        names[i] = v.Name
        paths = append(paths, v.Readable...)
        paths = append(paths, v.Writable...)
        envs = append(envs, v.EnvAllow...)
        for _, m := range v.Markers {
            markerLines = append(markerLines, m.MatchSummary())
        }
    }
    fmt.Fprintf(&b, "  Detected:   %s\n", strings.Join(names, " + "))
    if len(markerLines) > 0 {
        fmt.Fprintf(&b, "              (markers: %s)\n", strings.Join(markerLines, ", "))
    }
    fmt.Fprintln(&b)
    if len(paths) > 0 {
        fmt.Fprintf(&b, "  Grants:  %s\n", strings.Join(paths, ", "))
    }
    if len(envs) > 0 {
        fmt.Fprintf(&b, "  Env:     %s\n", strings.Join(envs, ", "))
    }
    fmt.Fprintln(&b)
    b.WriteString("  [Y]es, grant all    [N]o, use default    [D]etails    [S]kip this launch    [C]ustomize\n")
    return b.String()
}
```

- [ ] **Step 9.1d: Run — expect pass**

### Step 9.2: Add TTY-backed Prompter with yes/no/skip/details/customize loop

- [ ] **Step 9.2a: Add test**

```go
// append to internal/ui/consentprompt_test.go

import (
    "bytes"
    "io"
)

func TestTTYPrompter_Yes(t *testing.T) {
    in := bytes.NewBufferString("Y\n")
    var out bytes.Buffer
    p := NewTTYPrompter(in, &out)
    res := p.PromptVariantConsent(capability.PromptInput{
        Capability: "python",
        DetectedVariants: []capability.Variant{{Name: "uv"}},
    })
    if res.Decision != capability.PromptYes {
        t.Errorf("decision = %v, want PromptYes", res.Decision)
    }
    if len(res.Variants) != 1 || res.Variants[0] != "uv" {
        t.Errorf("Variants = %v, want [uv]", res.Variants)
    }
}

func TestTTYPrompter_No(t *testing.T) {
    p := NewTTYPrompter(bytes.NewBufferString("N\n"), io.Discard)
    res := p.PromptVariantConsent(capability.PromptInput{
        Capability: "python",
        DetectedVariants: []capability.Variant{{Name: "uv"}},
    })
    if res.Decision != capability.PromptNo {
        t.Errorf("decision = %v, want PromptNo", res.Decision)
    }
}

func TestTTYPrompter_Skip(t *testing.T) {
    p := NewTTYPrompter(bytes.NewBufferString("S\n"), io.Discard)
    res := p.PromptVariantConsent(capability.PromptInput{
        Capability: "python",
        DetectedVariants: []capability.Variant{{Name: "uv"}},
    })
    if res.Decision != capability.PromptSkip {
        t.Errorf("decision = %v, want PromptSkip", res.Decision)
    }
}

func TestTTYPrompter_CustomizeSelectsSubset(t *testing.T) {
    // Detected: uv, corepack (simulating multi-variant node-like case)
    // User says C, then y for uv, n for corepack.
    in := bytes.NewBufferString("C\ny\nn\n")
    p := NewTTYPrompter(in, io.Discard)
    res := p.PromptVariantConsent(capability.PromptInput{
        Capability: "python",
        DetectedVariants: []capability.Variant{{Name: "uv"}, {Name: "corepack"}},
    })
    if res.Decision != capability.PromptYes {
        t.Errorf("decision = %v, want PromptYes (from Customize)", res.Decision)
    }
    if len(res.Variants) != 1 || res.Variants[0] != "uv" {
        t.Errorf("Variants = %v, want [uv]", res.Variants)
    }
}
```

- [ ] **Step 9.2b: Implement TTY prompter**

```go
// append to internal/ui/consentprompt.go

import (
    "bufio"
    "io"
)

// NewTTYPrompter returns a Prompter that reads from `in` and writes
// prompts to `out`.
func NewTTYPrompter(in io.Reader, out io.Writer) capability.Prompter {
    return &ttyPrompter{in: bufio.NewReader(in), out: out}
}

type ttyPrompter struct {
    in  *bufio.Reader
    out io.Writer
}

func (p *ttyPrompter) PromptVariantConsent(in capability.PromptInput) capability.PromptResult {
    _, _ = fmt.Fprint(p.out, RenderPrompt(in))
    for {
        _, _ = fmt.Fprint(p.out, "> ")
        line, err := p.in.ReadString('\n')
        if err != nil && line == "" {
            return capability.PromptResult{Decision: capability.PromptNo}
        }
        switch strings.ToUpper(strings.TrimSpace(line)) {
        case "Y", "YES":
            names := make([]string, len(in.DetectedVariants))
            for i, v := range in.DetectedVariants {
                names[i] = v.Name
            }
            return capability.PromptResult{Decision: capability.PromptYes, Variants: names}
        case "N", "NO":
            return capability.PromptResult{Decision: capability.PromptNo}
        case "S", "SKIP":
            return capability.PromptResult{Decision: capability.PromptSkip}
        case "D", "DETAILS":
            p.renderDetails(in)
            continue
        case "C", "CUSTOMIZE":
            return p.customizeLoop(in)
        default:
            _, _ = fmt.Fprintln(p.out, "please answer Y, N, D, S, or C")
        }
    }
}

func (p *ttyPrompter) renderDetails(in capability.PromptInput) {
    for _, v := range in.DetectedVariants {
        _, _ = fmt.Fprintf(p.out, "\n  %s (%s)\n", v.Name, v.Description)
        for _, m := range v.Markers {
            _, _ = fmt.Fprintf(p.out, "    marker: %s\n", m.MatchSummary())
        }
        for _, r := range v.Readable {
            _, _ = fmt.Fprintf(p.out, "    readable: %s\n", r)
        }
        for _, w := range v.Writable {
            _, _ = fmt.Fprintf(p.out, "    writable: %s\n", w)
        }
        for _, e := range v.EnvAllow {
            _, _ = fmt.Fprintf(p.out, "    env: %s\n", e)
        }
    }
    _, _ = fmt.Fprintln(p.out)
}

func (p *ttyPrompter) customizeLoop(in capability.PromptInput) capability.PromptResult {
    chosen := make([]string, 0, len(in.DetectedVariants))
    for _, v := range in.DetectedVariants {
        _, _ = fmt.Fprintf(p.out, "  grant %s? [y/N] ", v.Name)
        line, _ := p.in.ReadString('\n')
        if strings.EqualFold(strings.TrimSpace(line), "y") ||
            strings.EqualFold(strings.TrimSpace(line), "yes") {
            chosen = append(chosen, v.Name)
        }
    }
    if len(chosen) == 0 {
        return capability.PromptResult{Decision: capability.PromptNo}
    }
    return capability.PromptResult{Decision: capability.PromptYes, Variants: chosen}
}
```

- [ ] **Step 9.2c: Run**

```
go test ./internal/ui/... -v
```

### Step 9.3: Commit

- [ ] **Step 9.3: Commit**

```
Add consent prompt UI for variant auto-detection

RenderPrompt emits a single template for first-time and
evidence-change prompts; the Previously line is omitted when there is
no prior grant. TTYPrompter reads the user's answer from a bufio
reader so the unit tests inject canned input without touching a real
terminal.

Customize opens a per-variant yes/no loop so users can accept a
subset of detected variants rather than the entire consolidated set.
```

---

## Task 10: `.aide.yaml` variant pins + `--variant` CLI flag

**Files:**
- Modify: `internal/config/project.go` (add `Variants []string` to capability config shape)
- Modify: `cmd/aide/commands.go` (add `--variant` flag, parse, validate)
- Modify: `cmd/aide/main.go` (wire SelectVariants into launch path)

### Step 10.1: Config schema — test first

- [ ] **Step 10.1a: Add test for YAML parsing**

Add a test that a `.aide.yaml` with `capabilities.python.variants: [uv]` parses and exposes the list through the existing project-config API. Adapt to the actual struct in `internal/config/project.go` — wherever the per-capability overrides are stored, add a `Variants []string` field.

(Because the existing config code is large and not reproduced here, the engineer must: locate the struct, add the field, add a table test in the adjacent `*_test.go` with input YAML and expected parsed struct.)

- [ ] **Step 10.1b: Implement the field**

- [ ] **Step 10.1c: Run existing config tests**

```
go test ./internal/config/... -v
```
Expected: no regressions.

### Step 10.2: CLI flag — test first

- [ ] **Step 10.2a: Add test**

Add a test in `cmd/aide/commands_test.go` (or the appropriate existing CLI test file) that asserts:
- `--variant python=uv` with `--with python` parses into `map[string][]string{"python": {"uv"}}`.
- `--variant python=uv,python=conda` produces `{"python": {"uv", "conda"}}`.
- `--variant python=uv` without `--with python` returns a parse error containing `requires --with python`.
- `--variant python=nosuch` returns an error listing valid variants.

- [ ] **Step 10.2b: Implement**

Add to the root command in `cmd/aide/commands.go`:

```go
var variantFlag []string
rootCmd.PersistentFlags().StringSliceVar(&variantFlag, "variant", nil,
    "Pin variants for capabilities in --with (format: capability=variant). "+
    "Repeatable. Must match a capability in --with.")

// After capability resolution and before sandbox render:
variantOverrides, err := parseVariantFlag(variantFlag, withCaps)
if err != nil {
    return err
}
```

Implement `parseVariantFlag`:

```go
// parseVariantFlag turns ["python=uv", "node=pnpm"] into a map keyed by
// capability name. Returns an error if a capability is not active, or
// a variant format is invalid.
func parseVariantFlag(raw []string, activeCaps []string) (map[string][]string, error) {
    active := make(map[string]bool, len(activeCaps))
    for _, c := range activeCaps {
        active[c] = true
    }
    out := make(map[string][]string)
    for _, entry := range raw {
        parts := strings.SplitN(entry, "=", 2)
        if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
            return nil, fmt.Errorf("--variant %q: expected capability=variant", entry)
        }
        cap, variant := parts[0], parts[1]
        if !active[cap] {
            return nil, fmt.Errorf("--variant %s=%s requires --with %s", cap, variant, cap)
        }
        out[cap] = append(out[cap], variant)
    }
    return out, nil
}
```

- [ ] **Step 10.2c: Run**

```
go test ./cmd/aide/... -run Variant -v
```

### Step 10.3: Wire SelectVariants into the launch path

- [ ] **Step 10.3a: At the point in `cmd/aide/main.go` (or wherever `SandboxOverrides` is built) where a capability is resolved, call SelectVariants if `cap.Variants` is non-empty:**

```go
if len(resolvedCap.VariantsSource.Variants) > 0 {
    selected, prov, err := capability.SelectVariants(capability.SelectInput{
        Capability:  resolvedCap.VariantsSource,
        ProjectRoot: projectRoot,
        Overrides:   variantOverrides[resolvedCap.Name],
        YAMLPins:    yamlVariantPins[resolvedCap.Name],
        Consent:     consentStore,
        Prompter:    prompter,
        Interactive: isTerminal,
        AutoYes:     yesFlag,
    })
    if err != nil {
        return err
    }
    resolvedCap = *capability.MergeSelectedVariants(&resolvedCap, selected)
    recordProvenance(resolvedCap.Name, prov) // for aide status
}
```

Exact placement depends on the existing launch flow; the engineer should trace from the `status` command or the main `run` path to find the merge point. Keep the change minimal: detect capability has variants → select → merge → record provenance.

- [ ] **Step 10.3b: Add integration test**

Add a test that builds a temporary project dir with `uv.lock`, runs `aide status --with python`, and asserts the output contains `python: uv (detected: uv.lock)` or equivalent provenance line. Use the existing CLI test harness pattern (see the closest existing integration test in `cmd/aide/`).

- [ ] **Step 10.3c: Run**

```
go test ./cmd/aide/... -v
```

### Step 10.4: Commit

- [ ] **Step 10.4: Commit**

```
Wire --variant flag, YAML pins, and SelectVariants into launch path

The launch path now runs SelectVariants for every active capability
that declares Variants, honoring precedence: --variant overrides,
.aide.yaml pins, consent-gated auto-detection, DefaultVariants.
Chosen variants are merged onto the resolved capability before the
sandbox profile is rendered, and the selection's provenance is
recorded for aide status output.
```

---

## Task 11: Discovery commands — `aide cap show/variants` + variant hint in `aide cap list`

**Files:**
- Modify: `cmd/aide/commands.go` (extend `cap` subcommand group)

### Step 11.1: `aide cap show <capability>`

- [ ] **Step 11.1a: Add test**

Table test: `aide cap show python` output must contain each of `uv`, `pyenv`, `conda`, `poetry`, `venv`, plus each variant's marker summary and at least one of its `Writable` paths.

- [ ] **Step 11.1b: Implement**

```go
// in commands.go, inside the "cap" command group
capShow := &cobra.Command{
    Use:   "show <capability>",
    Short: "Show detailed view of a capability including variants",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        cap, ok := capability.Builtins()[args[0]]
        if !ok {
            return fmt.Errorf("unknown capability: %s", args[0])
        }
        fmt.Fprintf(cmd.OutOrStdout(), "%s — %s\n", cap.Name, cap.Description)
        if len(cap.Variants) == 0 {
            return nil
        }
        fmt.Fprintln(cmd.OutOrStdout(), "\nVariants:")
        for _, v := range cap.Variants {
            fmt.Fprintf(cmd.OutOrStdout(), "  %s — %s\n", v.Name, v.Description)
            for _, m := range v.Markers {
                fmt.Fprintf(cmd.OutOrStdout(), "    marker: %s\n", m.MatchSummary())
            }
            for _, w := range v.Writable {
                fmt.Fprintf(cmd.OutOrStdout(), "    writable: %s\n", w)
            }
            for _, e := range v.EnvAllow {
                fmt.Fprintf(cmd.OutOrStdout(), "    env: %s\n", e)
            }
        }
        if len(cap.DefaultVariants) > 0 {
            fmt.Fprintf(cmd.OutOrStdout(), "\nDefault variants: %s\n", strings.Join(cap.DefaultVariants, ", "))
        }
        return nil
    },
}
capCmd.AddCommand(capShow)
```

- [ ] **Step 11.1c: Run**

### Step 11.2: `aide cap variants`

- [ ] **Step 11.2a: Test**

Asserts output contains `python/uv`, `python/pyenv`, `python/conda`, `python/poetry`, `python/venv`.

- [ ] **Step 11.2b: Implement**

```go
capVariants := &cobra.Command{
    Use:   "variants",
    Short: "List every (capability/variant) pair across all capabilities",
    RunE: func(cmd *cobra.Command, args []string) error {
        names := make([]string, 0)
        for _, c := range capability.Builtins() {
            for _, v := range c.Variants {
                names = append(names, c.Name+"/"+v.Name)
            }
        }
        sort.Strings(names)
        for _, n := range names {
            fmt.Fprintln(cmd.OutOrStdout(), n)
        }
        return nil
    },
}
capCmd.AddCommand(capVariants)
```

- [ ] **Step 11.2c: Run**

### Step 11.3: Extend `aide cap list` with variant count hint

- [ ] **Step 11.3a: Test**

Asserts the line for `python` contains `(5 variants: uv, pyenv, conda, poetry, venv)`.

- [ ] **Step 11.3b: Modify existing `cap list` command**

Find the existing `cap list` rendering. After each row, when the capability has variants, append the hint column:

```go
if len(cap.Variants) > 0 {
    names := make([]string, len(cap.Variants))
    for i, v := range cap.Variants {
        names[i] = v.Name
    }
    fmt.Fprintf(w, " (%d variants: %s)", len(cap.Variants), strings.Join(names, ", "))
}
fmt.Fprintln(w)
```

- [ ] **Step 11.3c: Run**

### Step 11.4: Commit

- [ ] **Step 11.4: Commit**

```
Add aide cap show and aide cap variants, extend aide cap list

Users can now discover the variant catalog without reading the
source: cap list shows a per-capability hint column, cap show lists
every variant with its markers, paths, and env vars, and cap
variants produces a flat (capability/variant) list useful for
feeding --variant.
```

---

## Task 12: `aide cap consent list/revoke` commands

**Files:**
- Modify: `cmd/aide/commands.go` (add `cap consent` subcommand group)

### Step 12.1: `aide cap consent list`

- [ ] **Step 12.1a: Test**

Asserts output contains "no consents" on an empty store; after a synthetic Grant, output contains the capability name and summary.

- [ ] **Step 12.1b: Implement**

```go
capConsent := &cobra.Command{Use: "consent", Short: "Manage detection consents"}
capConsentList := &cobra.Command{
    Use:   "list",
    Short: "List granted consents for the current project (or --project)",
    RunE: func(cmd *cobra.Command, args []string) error {
        project, _ := cmd.Flags().GetString("project")
        if project == "" {
            project, _ = os.Getwd()
        }
        store := consent.DefaultStore()
        grants, err := store.List(project)
        if err != nil {
            return err
        }
        if len(grants) == 0 {
            fmt.Fprintln(cmd.OutOrStdout(), "no consents recorded for", project)
            return nil
        }
        for _, g := range grants {
            fmt.Fprintf(cmd.OutOrStdout(), "%s  variants=%s  confirmed_at=%s  markers=%s\n",
                g.Capability, strings.Join(g.Variants, ","), g.ConfirmedAt.Format(time.RFC3339), g.Summary)
        }
        return nil
    },
}
capConsentList.Flags().String("project", "", "Project root (defaults to cwd)")
capConsent.AddCommand(capConsentList)
capCmd.AddCommand(capConsent)
```

- [ ] **Step 12.1c: Run**

### Step 12.2: `aide cap consent revoke <capability>`

- [ ] **Step 12.2a: Test**

Seeds a grant, runs `aide cap consent revoke python`, re-lists, asserts empty.

- [ ] **Step 12.2b: Implement**

```go
capConsentRevoke := &cobra.Command{
    Use:   "revoke <capability>",
    Short: "Revoke all consents for a capability in the current project",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        project, _ := os.Getwd()
        if err := consent.DefaultStore().Revoke(project, args[0]); err != nil {
            return err
        }
        fmt.Fprintf(cmd.OutOrStdout(), "revoked all %s consents for %s\n", args[0], project)
        return nil
    },
}
capConsent.AddCommand(capConsentRevoke)
```

- [ ] **Step 12.2c: Run**

### Step 12.3: Commit

- [ ] **Step 12.3: Commit**

```
Add aide cap consent list and revoke

Users can inspect which variant grants they have approved for the
current project and revoke them without hand-editing the consent
store. Revoke clears every grant for the capability regardless of
evidence digest, so the next launch re-prompts.
```

---

## Task 13: End-to-end integration test

**Files:**
- Create: `cmd/aide/integration_variant_test.go` (or add to existing integration test file)

### Step 13.1: Full flow

- [ ] **Step 13.1a: Test**

```go
//go:build integration

func TestPythonUVEndToEnd(t *testing.T) {
    tmp := t.TempDir()
    xdg := t.TempDir()
    t.Setenv("XDG_DATA_HOME", xdg)
    _ = os.WriteFile(filepath.Join(tmp, "uv.lock"), []byte(""), 0o600)
    _ = os.WriteFile(filepath.Join(tmp, "pyproject.toml"), []byte("[project]\nname=\"x\"\n"), 0o600)

    // Non-interactive launch: should fall through to default (venv).
    stdout, _, err := runAide(t, tmp, "status", "--with", "python")
    if err != nil {
        t.Fatal(err)
    }
    if !strings.Contains(stdout, "python") {
        t.Fatalf("status missing python, got:\n%s", stdout)
    }
    // No uv consent yet: provenance must indicate non-interactive or default.
    if !strings.Contains(stdout, "non-interactive") && !strings.Contains(stdout, "default") {
        t.Errorf("expected non-interactive fallback provenance; got:\n%s", stdout)
    }

    // Interactive launch with --yes: auto-approves uv.
    stdout, _, err = runAide(t, tmp, "status", "--with", "python", "--yes")
    if err != nil {
        t.Fatal(err)
    }
    if !strings.Contains(stdout, "uv") {
        t.Errorf("with --yes, expected uv in provenance; got:\n%s", stdout)
    }

    // Consent now persisted: a second call without --yes is silent.
    stdout, _, err = runAide(t, tmp, "cap", "consent", "list")
    if err != nil {
        t.Fatal(err)
    }
    if !strings.Contains(stdout, "python") || !strings.Contains(stdout, "uv") {
        t.Errorf("consent list missing python/uv:\n%s", stdout)
    }
}
```

`runAide` is a helper that invokes the Cobra root command with the given args and a working directory; wire it through the existing integration-test harness.

- [ ] **Step 13.1b: Run**

```
go test -tags=integration ./cmd/aide/... -run TestPythonUVEndToEnd -v
```

### Step 13.2: Commit

- [ ] **Step 13.2: Commit**

```
Add end-to-end test for python uv variant consent flow

Covers the three states that exercise the full decision table: a
non-interactive launch falls through to the default variant; a
launch with --yes auto-approves and records consent; a subsequent
cap consent list finds the recorded grant. The test uses a
per-test XDG_DATA_HOME so it cannot pollute the developer's real
consent store.
```

---

## Follow-up plans (out of scope for this MVP)

The spec's rollout steps 4–8 are intentionally deferred. Each becomes its own plan after this MVP ships:

1. **Node migration** — delete `pkg/seatbelt/guards/guard_node_toolchain.go`, add `node` capability with variants (`npm`, `pnpm`, `yarn`, `corepack`, `playwright`, `cypress`, `puppeteer`, `prisma`, `turbo`), alias `--with npm` for one release, `AIDE_SKIP_NODE_MIGRATION` escape hatch, release notes.
2. **`aide doctor` + preflight** — standalone `doctor` subcommand and default-on preflight in the launch path; cross-check selected variants vs. rendered profile; flag unclaimed markers; suggest `--with gpu` on GPU-library imports.
3. **GPU capability** — `gpu` capability + `gpuGuard` emitting Mach-lookup, iokit-open, and framework-read rules; opt-in only; status banner.
4. **Ruby / Java variant catalogs** — apply the python pattern to `ruby` (rbenv, asdf, bundler) and `java` (maven, gradle, sdkman).
5. **Claude plugin reliability** — expand `sandbox-doctor` trigger phrases (uv-specific error strings, `EPERM`, `os error 1`, glcontext errors); document plugin install path; add CLAUDE.md reference at the plugin root.

Each follow-up plan should start from brainstorming (confirm no scope change), then writing-plans, then subagent-driven execution.

---

## Self-review notes

- **Spec coverage:** Every rollout step 1–3 item has an explicit task. Threat-model mitigations land where promised: bounded reads in `Marker.Match` (T2), consent prompt surfaces paths before grant (T1), `--with gpu` is not introduced in this plan by design (GPU is a follow-up; T3 unchanged), consent store lives outside the repo under XDG root (T8, T4).
- **Placeholder scan:** No "TBD", "TODO", or "similar to Task N" — each task shows the actual code. Two places defer to the engineer: (1) exact field location in `internal/config/project.go` (Task 10.1), which requires tracing the existing config struct; (2) the integration-test harness (`runAide`) in Task 13, which must match the existing CLI test pattern. Both are unavoidable without duplicating hundreds of lines of existing code.
- **Type consistency:** `PromptDecision`, `PromptInput`, `PromptResult`, `Prompter`, `Provenance`, `SelectInput`, `Variant`, `Marker`, `ContainsSpec`, `Evidence`, `MarkerMatch`, `Grant`, `ConsentHash`, `DetectEvidence`, `SelectVariants`, `MergeSelectedVariants` — checked: names match across tasks.
- **TDD discipline:** Each production task opens with a failing test, then minimal implementation, then a passing run, then commit.
