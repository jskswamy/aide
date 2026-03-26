# Narrow Scoped Reads + Trust Gate Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Shrink filesystem guard baseline to minimal reads (git + caches), move everything else to capability opt-in, remove redundant guards, add direnv-style trust gate for `.aide.yaml`.

**Architecture:** Three phases — (1) narrow the filesystem guard, (2) add new capabilities + remove redundant guards + expand detection, (3) add trust gate for `.aide.yaml`. Each phase produces working, testable software.

**Tech Stack:** Go, macOS Seatbelt (sandbox-exec), SHA-256, XDG Base Directory spec

---

## Phase 1: Narrow Filesystem Guard

### Task 1: Narrow scoped home reads in filesystem guard

**Files:**
- Modify: `pkg/seatbelt/guards/guard_filesystem.go:57-115`
- Modify: `pkg/seatbelt/guards/filesystem_test.go`

- [ ] **Step 1: Write test for narrowed baseline reads**

The test should verify the filesystem guard only emits allow rules for
`~/.gitconfig`, `~/.config/git/`, `~/.cache/`, `~/Library/Caches/`,
`~/.local/share/aide/`, `~/.config/aide/`, and NOT for the broad paths
like `~/.config/*`, `~/.ssh/*`, `~/.cargo/*`, etc.

```go
func TestFilesystemGuard_NarrowBaseline(t *testing.T) {
    ctx := &seatbelt.Context{
        HomeDir:     "/Users/test",
        ProjectRoot: "/Users/test/project",
        RuntimeDir:  "/tmp/aide-123",
        TempDir:     "/tmp",
    }
    result := guards.FilesystemGuard().Rules(ctx)
    rendered := renderRules(result.Rules)

    // Must be present
    mustContain(t, rendered, `(literal "/Users/test/.gitconfig")`)
    mustContain(t, rendered, `(subpath "/Users/test/.config/git")`)
    mustContain(t, rendered, `(subpath "/Users/test/.cache")`)
    mustContain(t, rendered, `(subpath "/Users/test/Library/Caches")`)
    mustContain(t, rendered, `(subpath "/Users/test/.local/share/aide")`)
    mustContain(t, rendered, `(subpath "/Users/test/.config/aide")`)

    // Must NOT be present (moved to capabilities)
    mustNotContain(t, rendered, `(subpath "/Users/test/.config")`)  // broad .config
    mustNotContain(t, rendered, `(subpath "/Users/test/.ssh")`)
    mustNotContain(t, rendered, `(subpath "/Users/test/.cargo")`)
    mustNotContain(t, rendered, `(subpath "/Users/test/.rustup")`)
    mustNotContain(t, rendered, `(subpath "/Users/test/go")`)
    mustNotContain(t, rendered, `(subpath "/Users/test/.pyenv")`)
    mustNotContain(t, rendered, `(subpath "/Users/test/.gnupg")`)
    mustNotContain(t, rendered, `(subpath "/Users/test/.gradle")`)
    mustNotContain(t, rendered, `(subpath "/Users/test/.m2")`)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/seatbelt/guards/ -run TestFilesystemGuard_NarrowBaseline -v`
Expected: FAIL — current guard still emits broad reads

- [ ] **Step 3: Replace broad home reads with narrow baseline**

In `guard_filesystem.go`, replace the scoped home reads block
(lines 57-106) with:

```go
// Git configuration (read-only)
rules = append(rules,
    seatbelt.SectionAllow("Git configuration"),
    seatbelt.AllowRule(fmt.Sprintf(`(allow file-read*
    %s
    %s)`,
        seatbelt.HomeLiteral(home, ".gitconfig"),
        seatbelt.HomeSubpath(home, ".config/git"))),
)

// aide's own paths
rules = append(rules,
    seatbelt.SectionAllow("aide paths"),
    seatbelt.AllowRule(fmt.Sprintf(`(allow file-read*
    %s)`,
        seatbelt.HomeSubpath(home, ".config/aide"))),
    seatbelt.AllowRule(fmt.Sprintf(`(allow file-read* file-write*
    %s)`,
        seatbelt.HomeSubpath(home, ".local/share/aide"))),
)

// Build caches (read-write)
rules = append(rules,
    seatbelt.SectionAllow("Build caches (read-write)"),
    seatbelt.AllowRule(fmt.Sprintf(`(allow file-read* file-write*
    %s
    %s)`,
        seatbelt.HomeSubpath(home, ".cache"),
        seatbelt.HomeSubpath(home, "Library/Caches"))),
)

// Home directory listing and metadata traversal
rules = append(rules,
    seatbelt.SectionAllow("Home directory traversal"),
    seatbelt.AllowRule(fmt.Sprintf(`(allow file-read-data
    %s)`, seatbelt.HomeLiteral(home, ""))),
    seatbelt.AllowRule(fmt.Sprintf(`(allow file-read-metadata
    %s)`, seatbelt.HomeSubpath(home, ""))),
)
```

Also remove the dotfile regex block and the broad symlink resolution
(`resolveHomeDotfileSymlinks`). Keep the `resolveHomeDotfileSymlinks`
function but only call it for `~/.gitconfig` if it's a symlink.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/seatbelt/guards/ -run TestFilesystemGuard_NarrowBaseline -v`
Expected: PASS

- [ ] **Step 5: Update existing filesystem tests that expect broad reads**

Some existing tests may assert the presence of `~/.ssh`, `~/.config`,
etc. Update them to match the new narrow baseline.

Run: `go test ./pkg/seatbelt/guards/ -v`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add pkg/seatbelt/guards/guard_filesystem.go pkg/seatbelt/guards/filesystem_test.go
git commit -m "Narrow filesystem guard to minimal baseline reads"
```

---

## Phase 2: New Capabilities + Guard Cleanup + Detection

### Task 2: Add language runtime capabilities

**Files:**
- Modify: `internal/capability/builtin.go`
- Modify: `internal/capability/builtin_test.go`

- [ ] **Step 1: Write test for new capabilities**

```go
func TestBuiltins_LanguageRuntimes(t *testing.T) {
    bs := Builtins()
    cases := []struct {
        name     string
        writable []string
    }{
        {"go", []string{"~/go"}},
        {"rust", []string{"~/.cargo", "~/.rustup"}},
        {"python", []string{"~/.pyenv"}},
        {"ruby", []string{"~/.rbenv"}},
        {"java", []string{"~/.sdkman", "~/.gradle", "~/.m2"}},
        {"github", []string{"~/.config/gh"}},
        {"gpg", []string{"~/.gnupg"}},
    }
    for _, tc := range cases {
        cap, ok := bs[tc.name]
        if !ok {
            t.Errorf("missing capability %q", tc.name)
            continue
        }
        if !reflect.DeepEqual(cap.Writable, tc.writable) {
            t.Errorf("%s writable: got %v, want %v",
                tc.name, cap.Writable, tc.writable)
        }
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/capability/ -run TestBuiltins_LanguageRuntimes -v`
Expected: FAIL — capabilities don't exist yet

- [ ] **Step 3: Add language runtime capabilities to builtin.go**

Add after the existing `npm` capability:

```go
// Language runtimes
"go": {
    Name:        "go",
    Description: "Go toolchain",
    Writable:    []string{"~/go"},
    EnvAllow:    []string{"GOPATH", "GOROOT", "GOBIN"},
},
"rust": {
    Name:        "rust",
    Description: "Rust toolchain",
    Writable:    []string{"~/.cargo", "~/.rustup"},
    EnvAllow:    []string{"CARGO_HOME", "RUSTUP_HOME"},
},
"python": {
    Name:        "python",
    Description: "Python toolchain",
    Writable:    []string{"~/.pyenv"},
    EnvAllow:    []string{"PYENV_ROOT", "VIRTUAL_ENV"},
},
"ruby": {
    Name:        "ruby",
    Description: "Ruby toolchain",
    Writable:    []string{"~/.rbenv"},
    EnvAllow:    []string{"RBENV_ROOT", "GEM_HOME"},
},
"java": {
    Name:        "java",
    Description: "Java/JVM toolchain",
    Writable:    []string{"~/.sdkman", "~/.gradle", "~/.m2"},
    EnvAllow:    []string{"JAVA_HOME", "SDKMAN_DIR"},
},

// Dev tools
"github": {
    Name:        "github",
    Description: "GitHub CLI and credentials",
    Writable:    []string{"~/.config/gh"},
    EnvAllow:    []string{"GITHUB_TOKEN", "GH_TOKEN"},
},
"gpg": {
    Name:        "gpg",
    Description: "GPG keys and signing",
    Writable:    []string{"~/.gnupg"},
    EnvAllow:    []string{"GNUPGHOME"},
},
```

Update `TestBuiltins_Count` to reflect new count (12 + 7 = 19).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/capability/ -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/capability/builtin.go internal/capability/builtin_test.go
git commit -m "Add language runtime and dev tool capabilities"
```

### Task 3: Clear Unguard fields from existing capabilities

**Files:**
- Modify: `internal/capability/builtin.go`
- Modify: `internal/capability/builtin_test.go`

- [ ] **Step 1: Write test verifying no Unguard fields on capabilities with removed guards**

```go
func TestBuiltins_NoStaleUnguardRefs(t *testing.T) {
    removedGuards := map[string]bool{
        "cloud-aws": true, "cloud-gcp": true,
        "cloud-azure": true, "cloud-digitalocean": true,
        "cloud-oci": true, "kubernetes": true,
        "docker": true, "terraform": true, "vault": true,
        "ssh-keys": true, "npm": true, "netrc": true,
        "github-cli": true, "password-managers": true,
    }
    for name, cap := range Builtins() {
        for _, ug := range cap.Unguard {
            if removedGuards[ug] {
                t.Errorf("capability %q has stale Unguard ref %q "+
                    "(guard removed)", name, ug)
            }
        }
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/capability/ -run TestBuiltins_NoStaleUnguardRefs -v`
Expected: FAIL — aws, gcp, docker, k8s, etc. still have Unguard fields

- [ ] **Step 3: Remove Unguard fields from all capabilities whose guards are removed**

In `builtin.go`, remove the `Unguard` field from: `aws`, `gcp`,
`azure`, `digitalocean`, `oci`, `docker`, `k8s`, `helm`, `terraform`,
`vault`, `ssh`, `npm`. These guards no longer exist.

Also update `TestBuiltin_K8s_HasCorrectGuard` and
`TestBuiltin_Helm_ExtendsK8sGuard` in `builtin_test.go` — these
assert Unguard values that no longer exist.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/capability/ -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/capability/builtin.go internal/capability/builtin_test.go
git commit -m "Clear stale Unguard fields from capabilities"
```

### Task 4: Remove redundant guards

**Files:**
- Modify: `pkg/seatbelt/guards/registry.go:25-51`
- Delete: `pkg/seatbelt/guards/guard_kubernetes.go`
- Delete: `pkg/seatbelt/guards/guard_sensitive.go` (docker, github-cli, npm, netrc, vercel)
- Delete: `pkg/seatbelt/guards/guard_cloud.go`
- Delete: `pkg/seatbelt/guards/guard_ssh_keys.go`
- Delete: `pkg/seatbelt/guards/guard_browsers.go`
- Delete: various guard test files
- Modify: `internal/sandbox/sandbox.go` — if DefaultPolicy references removed guards

Note: Identify exact filenames by checking which guard structs live in
which files. Some may be in `guard_sensitive.go` or separate files.

- [ ] **Step 1: Identify all guard files to remove**

Run `grep -rn 'func.*Guard().*Guard' pkg/seatbelt/guards/` to map
guard constructors to files. List every file containing ONLY guards
being removed.

- [ ] **Step 2: Remove guards from registry**

In `registry.go`, remove from the `builtinGuards` init block:
- All `cloud-*` guards (aws, gcp, azure, digitalocean, oci)
- `kubernetes`, `docker`, `terraform`, `vault`
- `ssh-keys`, `password-managers`, `browsers`, `mounted-volumes`
- `shell-history`
- `github-cli`, `npm`, `netrc`, `vercel`

Keep only:
- Always: `base`, `system-runtime`, `network`, `filesystem`,
  `keychain`, `node-toolchain`, `nix-toolchain`
- Default: `project-secrets`, `dev-credentials`, `aide-secrets`

Note: `aide-secrets` protects `~/.config/aide/secrets` which is
within the baseline `~/.config/aide/` path — still needed.
Also clean up `ExpandGuardName("cloud")` and `CloudGuardNames()`
helper since all cloud guards are removed.

- [ ] **Step 3: Delete guard source files for removed guards**

Delete all `.go` files that only contain removed guards. For files
like `guard_sensitive.go` that contain multiple guards, check if any
are kept — if not, delete the whole file.

- [ ] **Step 4: Delete corresponding test files**

Delete test files for removed guards.

- [ ] **Step 5: Update DefaultGuardNames test expectations**

If there are tests asserting the count or names of default guards,
update them.

- [ ] **Step 6: Run all tests**

Run: `go test ./pkg/seatbelt/... ./internal/sandbox/... ./internal/capability/... -v`
Expected: All PASS. Fix any compilation errors from removed guards.

- [ ] **Step 7: Build and verify**

Run: `go build ./cmd/aide/`
Then: `./aide sandbox guards` — should show only the kept guards.

- [ ] **Step 8: Commit**

```bash
git add pkg/seatbelt/guards/ internal/sandbox/
git commit -m "Remove redundant guards after baseline narrowing"
```

### Task 5: Expand auto-detection with new markers

**Files:**
- Modify: `internal/capability/detect.go`
- Modify: `internal/capability/detect_test.go`

- [ ] **Step 1: Write tests for new detection markers**

```go
func TestDetect_GoProject(t *testing.T) {
    dir := t.TempDir()
    mustWriteFile(t, filepath.Join(dir, "go.mod"), []byte("module example"))
    suggestions := DetectProject(dir)
    assertContains(t, suggestions, "go", "expected go for go.mod")
}

func TestDetect_RustProject(t *testing.T) {
    dir := t.TempDir()
    mustWriteFile(t, filepath.Join(dir, "Cargo.toml"),
        []byte("[package]\nname = \"test\""))
    suggestions := DetectProject(dir)
    assertContains(t, suggestions, "rust", "expected rust for Cargo.toml")
}

func TestDetect_PythonProject(t *testing.T) {
    dir := t.TempDir()
    mustWriteFile(t, filepath.Join(dir, "pyproject.toml"),
        []byte("[project]\nname = \"test\""))
    suggestions := DetectProject(dir)
    assertContains(t, suggestions, "python",
        "expected python for pyproject.toml")
}

func TestDetect_RubyProject(t *testing.T) {
    dir := t.TempDir()
    mustWriteFile(t, filepath.Join(dir, "Gemfile"),
        []byte("source 'https://rubygems.org'"))
    suggestions := DetectProject(dir)
    assertContains(t, suggestions, "ruby", "expected ruby for Gemfile")
}

func TestDetect_JavaMavenProject(t *testing.T) {
    dir := t.TempDir()
    mustWriteFile(t, filepath.Join(dir, "pom.xml"),
        []byte("<project></project>"))
    suggestions := DetectProject(dir)
    assertContains(t, suggestions, "java", "expected java for pom.xml")
}

func TestDetect_JavaGradleProject(t *testing.T) {
    dir := t.TempDir()
    mustWriteFile(t, filepath.Join(dir, "build.gradle"),
        []byte("apply plugin: 'java'"))
    suggestions := DetectProject(dir)
    assertContains(t, suggestions, "java",
        "expected java for build.gradle")
}

func TestDetect_GitHubProject(t *testing.T) {
    dir := t.TempDir()
    mustMkdirAll(t, filepath.Join(dir, ".github", "workflows"))
    suggestions := DetectProject(dir)
    assertContains(t, suggestions, "github",
        "expected github for .github/workflows/")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/capability/ -run "TestDetect_(Go|Rust|Python|Ruby|Java|GitHub)" -v`
Expected: FAIL

- [ ] **Step 3: Add detection markers to detect.go**

In `DetectProject()`, add checks for each new marker:

```go
// Go
if fileExists(filepath.Join(projectRoot, "go.mod")) ||
    fileExists(filepath.Join(projectRoot, "go.sum")) {
    suggestions = append(suggestions, "go")
}

// Rust
if fileExists(filepath.Join(projectRoot, "Cargo.toml")) {
    suggestions = append(suggestions, "rust")
}

// Python
if fileExists(filepath.Join(projectRoot, "pyproject.toml")) ||
    fileExists(filepath.Join(projectRoot, "requirements.txt")) ||
    fileExists(filepath.Join(projectRoot, "Pipfile")) ||
    fileExists(filepath.Join(projectRoot, "setup.py")) {
    suggestions = append(suggestions, "python")
}

// Ruby
if fileExists(filepath.Join(projectRoot, "Gemfile")) ||
    hasFileWithExtension(projectRoot, ".gemspec") {
    suggestions = append(suggestions, "ruby")
}

// Java/JVM
if fileExists(filepath.Join(projectRoot, "pom.xml")) ||
    fileExists(filepath.Join(projectRoot, "build.gradle")) ||
    fileExists(filepath.Join(projectRoot, "build.gradle.kts")) {
    suggestions = append(suggestions, "java")
}

// GitHub
if dirExists(filepath.Join(projectRoot, ".github", "workflows")) {
    suggestions = append(suggestions, "github")
}

// Helm
if fileExists(filepath.Join(projectRoot, "Chart.yaml")) ||
    fileExists(filepath.Join(projectRoot, "helmfile.yaml")) {
    suggestions = append(suggestions, "helm")
}
```

Also add `kubernetes/` directory check alongside existing `k8s/` and
`manifests/` checks for k8s detection.

- [ ] **Step 4: Run all detection tests**

Run: `go test ./internal/capability/ -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/capability/detect.go internal/capability/detect_test.go
git commit -m "Expand auto-detection for language runtimes"
```

---

## Phase 3: Trust Gate for .aide.yaml

### Task 6: Implement trust store (core library)

**Files:**
- Create: `internal/trust/trust.go`
- Create: `internal/trust/trust_test.go`

- [ ] **Step 1: Write tests for trust store operations**

```go
func TestFileHash(t *testing.T) {
    h := FileHash("/path/to/.aide.yaml", []byte("capabilities:\n  - go\n"))
    if len(h) != 64 { // hex-encoded SHA-256
        t.Errorf("expected 64-char hex hash, got %d chars", len(h))
    }
}

func TestPathHash(t *testing.T) {
    h := PathHash("/path/to/.aide.yaml")
    if len(h) != 64 {
        t.Errorf("expected 64-char hex hash, got %d chars", len(h))
    }
    // path hash != file hash for same path
    fh := FileHash("/path/to/.aide.yaml", []byte("content"))
    if h == fh {
        t.Error("path hash should differ from file hash")
    }
}

func TestTrustAndCheck(t *testing.T) {
    dir := t.TempDir()
    store := NewStore(dir)
    path := "/project/.aide.yaml"
    content := []byte("capabilities:\n  - go\n")

    // Initially untrusted
    status := store.Check(path, content)
    if status != Untrusted {
        t.Errorf("expected Untrusted, got %v", status)
    }

    // Trust it
    store.Trust(path, content)
    status = store.Check(path, content)
    if status != Trusted {
        t.Errorf("expected Trusted, got %v", status)
    }

    // Change content → untrusted again
    status = store.Check(path, []byte("capabilities:\n  - aws\n"))
    if status != Untrusted {
        t.Errorf("expected Untrusted after content change, got %v", status)
    }
}

func TestDenyAndCheck(t *testing.T) {
    dir := t.TempDir()
    store := NewStore(dir)
    path := "/project/.aide.yaml"
    content := []byte("capabilities:\n  - go\n")

    store.Deny(path)
    status := store.Check(path, content)
    if status != Denied {
        t.Errorf("expected Denied, got %v", status)
    }

    // Deny persists even with different content
    status = store.Check(path, []byte("different"))
    if status != Denied {
        t.Errorf("expected Denied with different content, got %v", status)
    }
}

func TestUntrust(t *testing.T) {
    dir := t.TempDir()
    store := NewStore(dir)
    path := "/project/.aide.yaml"
    content := []byte("caps")

    store.Trust(path, content)
    store.Untrust(path, content)
    status := store.Check(path, content)
    if status != Untrusted {
        t.Errorf("expected Untrusted after untrust, got %v", status)
    }
}

func TestTrustRemovesDeny(t *testing.T) {
    dir := t.TempDir()
    store := NewStore(dir)
    path := "/project/.aide.yaml"
    content := []byte("caps")

    store.Deny(path)
    store.Trust(path, content)
    status := store.Check(path, content)
    if status != Trusted {
        t.Errorf("expected Trusted after trust-over-deny, got %v", status)
    }
}

func TestDenyRemovesTrust(t *testing.T) {
    dir := t.TempDir()
    store := NewStore(dir)
    path := "/project/.aide.yaml"
    content := []byte("caps")

    store.Trust(path, content)
    store.Deny(path)
    status := store.Check(path, content)
    if status != Denied {
        t.Errorf("expected Denied after deny-over-trust, got %v", status)
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/trust/ -v`
Expected: FAIL — package doesn't exist

- [ ] **Step 3: Implement trust store**

```go
package trust

import (
    "crypto/sha256"
    "encoding/hex"
    "os"
    "path/filepath"
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

// Store manages trust/deny state for .aide.yaml files.
type Store struct {
    baseDir string // e.g. ~/.local/share/aide
}

// NewStore creates a trust store at the given base directory.
func NewStore(baseDir string) *Store {
    return &Store{baseDir: baseDir}
}

// DefaultStore returns a Store using XDG_DATA_HOME/aide.
func DefaultStore() *Store {
    base := os.Getenv("XDG_DATA_HOME")
    if base == "" {
        home, _ := os.UserHomeDir()
        base = filepath.Join(home, ".local", "share")
    }
    return NewStore(filepath.Join(base, "aide"))
}

// FileHash computes SHA-256(path + "\n" + contents).
func FileHash(path string, contents []byte) string {
    h := sha256.New()
    h.Write([]byte(path))
    h.Write([]byte("\n"))
    h.Write(contents)
    return hex.EncodeToString(h.Sum(nil))
}

// PathHash computes SHA-256(path + "\n").
func PathHash(path string) string {
    h := sha256.New()
    h.Write([]byte(path))
    h.Write([]byte("\n"))
    return hex.EncodeToString(h.Sum(nil))
}

// Check returns the trust status for a file with given content.
func (s *Store) Check(path string, contents []byte) Status {
    ph := PathHash(path)
    if fileExists(filepath.Join(s.baseDir, "deny", ph)) {
        return Denied
    }
    fh := FileHash(path, contents)
    if fileExists(filepath.Join(s.baseDir, "trust", fh)) {
        return Trusted
    }
    return Untrusted
}

// Trust marks a file+content as trusted, removing any deny.
func (s *Store) Trust(path string, contents []byte) error {
    fh := FileHash(path, contents)
    trustDir := filepath.Join(s.baseDir, "trust")
    if err := os.MkdirAll(trustDir, 0o700); err != nil {
        return err
    }
    if err := atomicWrite(filepath.Join(trustDir, fh),
        []byte(path)); err != nil {
        return err
    }
    // Remove deny if exists
    ph := PathHash(path)
    os.Remove(filepath.Join(s.baseDir, "deny", ph))
    return nil
}

// Deny marks a path as denied, removing any trust.
func (s *Store) Deny(path string) error {
    ph := PathHash(path)
    denyDir := filepath.Join(s.baseDir, "deny")
    if err := os.MkdirAll(denyDir, 0o700); err != nil {
        return err
    }
    if err := atomicWrite(filepath.Join(denyDir, ph),
        []byte(path)); err != nil {
        return err
    }
    // Cannot remove trust by path alone — trust is content-addressed.
    // Old trust files become orphaned, which is fine.
    return nil
}

// Untrust removes trust without creating a deny.
func (s *Store) Untrust(path string, contents []byte) error {
    fh := FileHash(path, contents)
    return os.Remove(filepath.Join(s.baseDir, "trust", fh))
}

// atomicWrite writes data to a temp file then renames.
func atomicWrite(path string, data []byte) error {
    dir := filepath.Dir(path)
    f, err := os.CreateTemp(dir, ".aide-trust-*")
    if err != nil {
        return err
    }
    tmp := f.Name()
    if _, err := f.Write(data); err != nil {
        f.Close()
        os.Remove(tmp)
        return err
    }
    f.Close()
    return os.Rename(tmp, path)
}

func fileExists(path string) bool {
    _, err := os.Stat(path)
    return err == nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/trust/ -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/trust/
git commit -m "Add content-addressed trust store for .aide.yaml"
```

### Task 7: Integrate trust gate into launcher

**Files:**
- Modify: `internal/launcher/launcher.go`
- Modify: `cmd/aide/main.go` (or root command file for --ignore-project-config flag)

- [ ] **Step 1: Add trust check before project override application**

In `launcher.go`, after config load and context resolution but before
capabilities are merged, add the trust gate. The trust check should
happen early — before `applyProjectOverride` in `resolver.go` is
called, or by gating the ProjectOverride before passing it to Resolve.

Find where `cfg.ProjectOverride` is set (likely in config loading or
context resolution). Add:

```go
import "github.com/jskswamy/aide/internal/trust"

// In Launch(), after loading config:
if cfg.ProjectOverride != nil {
    aideYamlPath := filepath.Join(projectRoot, ".aide.yaml")
    absPath, _ := filepath.Abs(aideYamlPath)
    contents, err := os.ReadFile(absPath)
    if err == nil {
        store := trust.DefaultStore()
        status := store.Check(absPath, contents)
        switch status {
        case trust.Denied:
            // Silently skip project override
            cfg.ProjectOverride = nil
        case trust.Untrusted:
            // Print warning and skip
            printUntrustedWarning(absPath, cfg.ProjectOverride)
            cfg.ProjectOverride = nil
        case trust.Trusted:
            // Proceed normally
        }
    }
}
```

- [ ] **Step 2: Implement printUntrustedWarning helper**

```go
func printUntrustedWarning(path string, po *config.ProjectOverride) {
    fmt.Fprintf(os.Stderr, "! .aide.yaml is not trusted\n\n")
    if po.Agent != "" {
        fmt.Fprintf(os.Stderr, "  Agent:        %s\n", po.Agent)
    }
    if len(po.Capabilities) > 0 {
        fmt.Fprintf(os.Stderr, "  Capabilities: %s\n",
            strings.Join(po.Capabilities, ", "))
    }
    // ... render other security-relevant fields
    fmt.Fprintf(os.Stderr, "\n")
    fmt.Fprintf(os.Stderr,
        "  Run `aide trust` to approve this configuration.\n")
    fmt.Fprintf(os.Stderr,
        "  Run `aide deny` to permanently block it.\n")
    fmt.Fprintf(os.Stderr,
        "  Run `aide --ignore-project-config` to launch without it.\n")
}
```

- [ ] **Step 3: Add --ignore-project-config flag**

In `cmd/aide/main.go` (or wherever root flags are defined), add:

```go
rootCmd.Flags().Bool("ignore-project-config", false,
    "Launch without applying .aide.yaml")
```

Wire it into launcher to skip ProjectOverride when set.

- [ ] **Step 4: Test manually**

```bash
go build ./cmd/aide/
# In a project with .aide.yaml:
./aide  # Should show untrusted warning
./aide --ignore-project-config  # Should launch without it
```

- [ ] **Step 5: Commit**

```bash
git add internal/launcher/ internal/context/ cmd/aide/
git commit -m "Integrate trust gate into launch flow"
```

### Task 8: Add trust/deny/untrust CLI commands

**Files:**
- Create: `cmd/aide/cmd_trust.go`
- Create: `cmd/aide/cmd_trust_test.go` (or integration test)

- [ ] **Step 1: Implement aide trust command**

```go
var trustCmd = &cobra.Command{
    Use:   "trust",
    Short: "Trust the .aide.yaml in the current directory",
    RunE: func(cmd *cobra.Command, args []string) error {
        aideYamlPath := filepath.Join(".", ".aide.yaml")
        absPath, err := filepath.Abs(aideYamlPath)
        if err != nil {
            return err
        }
        contents, err := os.ReadFile(absPath)
        if err != nil {
            return fmt.Errorf(".aide.yaml not found in current directory")
        }
        store := trust.DefaultStore()
        if err := store.Trust(absPath, contents); err != nil {
            return err
        }
        fmt.Printf("Trusted: %s\n", absPath)
        return nil
    },
}
```

- [ ] **Step 2: Implement aide deny command**

```go
var denyCmd = &cobra.Command{
    Use:   "deny",
    Short: "Deny the .aide.yaml in the current directory",
    RunE: func(cmd *cobra.Command, args []string) error {
        absPath, err := filepath.Abs(".aide.yaml")
        if err != nil {
            return err
        }
        store := trust.DefaultStore()
        if err := store.Deny(absPath); err != nil {
            return err
        }
        fmt.Printf("Denied: %s\n", absPath)
        return nil
    },
}
```

- [ ] **Step 3: Implement aide untrust command**

```go
var untrustCmd = &cobra.Command{
    Use:   "untrust",
    Short: "Remove trust for .aide.yaml without denying",
    RunE: func(cmd *cobra.Command, args []string) error {
        absPath, err := filepath.Abs(".aide.yaml")
        if err != nil {
            return err
        }
        contents, err := os.ReadFile(absPath)
        if err != nil {
            return fmt.Errorf(".aide.yaml not found")
        }
        store := trust.DefaultStore()
        if err := store.Untrust(absPath, contents); err != nil {
            return err
        }
        fmt.Printf("Untrusted: %s\n", absPath)
        return nil
    },
}
```

- [ ] **Step 4: Register commands**

Add to root command init:
```go
rootCmd.AddCommand(trustCmd, denyCmd, untrustCmd)
```

- [ ] **Step 5: Build and test manually**

```bash
go build ./cmd/aide/
./aide trust      # in a project with .aide.yaml
./aide untrust
./aide deny
```

- [ ] **Step 6: Commit**

```bash
git add cmd/aide/cmd_trust.go
git commit -m "Add trust, deny, and untrust CLI commands"
```

### Task 8b: Add `aide trust --path` prefix-based trust

**Files:**
- Modify: `cmd/aide/cmd_trust.go`
- Modify: `internal/trust/trust.go`

- [ ] **Step 1: Write test for prefix-based trust check**

```go
func TestPrefixTrust(t *testing.T) {
    dir := t.TempDir()
    store := NewStore(dir)
    store.AddPrefix("/Users/test/source")

    path := "/Users/test/source/myproject/.aide.yaml"
    content := []byte("capabilities: [go]")

    // First encounter under prefix → auto-trusted
    status := store.Check(path, content)
    if status != Untrusted {
        t.Error("should be untrusted before first CheckAndAutoTrust")
    }

    // CheckAndAutoTrust auto-trusts under prefix
    store.CheckAndAutoTrust(path, content)
    status = store.Check(path, content)
    if status != Trusted {
        t.Error("should be trusted under prefix")
    }

    // Content change → untrusted, then auto-re-trusted
    newContent := []byte("capabilities: [go, aws]")
    status = store.Check(path, newContent)
    if status != Untrusted {
        t.Error("should be untrusted after content change")
    }
    store.CheckAndAutoTrust(path, newContent)
    status = store.Check(path, newContent)
    if status != Trusted {
        t.Error("should be auto-re-trusted under prefix")
    }
}
```

- [ ] **Step 2: Add prefix storage to Store**

```go
// In trust.go:
func (s *Store) AddPrefix(prefix string) error {
    // Store prefixes in baseDir/prefixes/ as files
    dir := filepath.Join(s.baseDir, "prefixes")
    os.MkdirAll(dir, 0o700)
    hash := PathHash(prefix)
    return atomicWrite(filepath.Join(dir, hash), []byte(prefix))
}

func (s *Store) RemovePrefix(prefix string) error {
    hash := PathHash(prefix)
    return os.Remove(filepath.Join(s.baseDir, "prefixes", hash))
}

func (s *Store) ListPrefixes() []string {
    dir := filepath.Join(s.baseDir, "prefixes")
    entries, err := os.ReadDir(dir)
    if err != nil {
        return nil
    }
    var prefixes []string
    for _, e := range entries {
        data, err := os.ReadFile(filepath.Join(dir, e.Name()))
        if err == nil {
            prefixes = append(prefixes, string(data))
        }
    }
    return prefixes
}

func (s *Store) IsUnderPrefix(path string) bool {
    for _, prefix := range s.ListPrefixes() {
        if strings.HasPrefix(path, prefix+"/") {
            return true
        }
    }
    return false
}

func (s *Store) CheckAndAutoTrust(path string, contents []byte) Status {
    status := s.Check(path, contents)
    if status == Untrusted && s.IsUnderPrefix(path) {
        s.Trust(path, contents)
        return Trusted
    }
    return status
}
```

- [ ] **Step 3: Add --path, --list, --remove flags to trust command**

```go
trustCmd.Flags().String("path", "",
    "Auto-trust all .aide.yaml files under this prefix")
trustCmd.Flags().Bool("list", false,
    "List trusted path prefixes")
trustCmd.Flags().String("remove", "",
    "Remove a trusted path prefix")
```

Wire into the RunE function to handle each flag.

- [ ] **Step 4: Update launcher to use CheckAndAutoTrust**

In launcher.go, replace `store.Check(...)` with
`store.CheckAndAutoTrust(...)` so prefix-based auto-trust works
at launch time.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/trust/ -v`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/trust/ cmd/aide/cmd_trust.go
git commit -m "Add prefix-based trust for aide trust --path"
```

### Task 9: Auto-re-trust on aide-initiated modifications

**Files:**
- Modify: `internal/config/project_writer.go` (contains WriteProjectOverride)
- Modify: `internal/trust/trust.go`

- [ ] **Step 1: Find where aide writes .aide.yaml**

Search for code that writes to `.aide.yaml` — likely in
`WriteProjectOverride` or similar functions used by `aide cap enable`,
`aide sandbox allow`, etc.

- [ ] **Step 2: Add auto-re-trust after aide writes .aide.yaml**

Before modifying `.aide.yaml`, record the pre-modification hash.
After writing, verify the pre-modification hash matches the stored
trust. If so, auto-trust the new content.

```go
// In WriteProjectOverride or equivalent:
func WriteProjectOverrideWithTrust(path string, po *config.ProjectOverride) error {
    absPath, _ := filepath.Abs(path)
    store := trust.DefaultStore()

    // Record pre-modification state
    oldContents, err := os.ReadFile(absPath)
    var wasTrusted bool
    if err == nil {
        wasTrusted = store.Check(absPath, oldContents) == trust.Trusted
    }

    // Write the new content
    if err := writeProjectOverride(path, po); err != nil {
        return err
    }

    // Auto-re-trust if previously trusted
    if wasTrusted {
        newContents, _ := os.ReadFile(absPath)
        store.Trust(absPath, newContents)
    }

    return nil
}
```

- [ ] **Step 3: Run all tests**

Run: `go test ./... 2>&1 | tail -20`
Expected: All PASS

- [ ] **Step 4: Commit**

```bash
git add internal/trust/ internal/context/
git commit -m "Auto-re-trust .aide.yaml after aide-initiated changes"
```

### Task 10: End-to-end verification

- [ ] **Step 1: Build final binary**

```bash
go build ./cmd/aide/
```

- [ ] **Step 2: Verify narrow baseline**

```bash
./aide sandbox test 2>&1 | grep -c "subpath.*\.ssh"
# Expected: 0 (no .ssh in baseline)

./aide sandbox test 2>&1 | grep -c "subpath.*\.config\b"
# Expected: 0 (no broad .config)

./aide sandbox test 2>&1 | grep "\.gitconfig"
# Expected: 1 match (literal .gitconfig)
```

- [ ] **Step 3: Verify capabilities expand reads**

```bash
./aide sandbox test --with go,rust,docker 2>&1 | grep -E "go\b|cargo|rustup|docker"
# Expected: all paths present as writable
```

- [ ] **Step 4: Verify guard list**

```bash
./aide sandbox guards
# Expected: only always guards + project-secrets + dev-credentials
# Should NOT show: cloud-aws, docker, kubernetes, ssh-keys, etc.
```

- [ ] **Step 5: Verify trust gate**

```bash
cd /tmp && mkdir test-project && cd test-project
echo "capabilities: [aws]" > .aide.yaml
aide  # Should show untrusted warning
aide trust
aide  # Should launch normally
echo "capabilities: [aws, ssh]" > .aide.yaml
aide  # Should show untrusted warning again (content changed)
```

- [ ] **Step 6: Run full test suite**

```bash
go test ./... -count=1
```

- [ ] **Step 7: Commit any fixes**

```bash
git add <changed-files> && git commit -m "Fix issues from e2e verification"
```

---

## Deferred

These items are part of the spec but deferred to a follow-up:

- **Migration banner:** On first run after upgrade, detect project
  capabilities and print a prominent suggestion banner. Implement via
  `aide doctor` breakage detection.
- **Per-capability symlink resolution:** Each capability resolves
  symlinks for its own paths (e.g., `~/.cargo` symlinked by stow).
  Currently only baseline resolves `~/.gitconfig`.
- **MCP server sandboxing:** Separate design session (non-goal of this
  spec).
