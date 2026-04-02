# Test Coverage Improvement Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Raise security-critical packages to 80%+ coverage, migrate all hand-written mocks to mockgen, exclude `cmd/aide/` from CI coverage calculation.

**Architecture:** Add `go.uber.org/mock` for mock generation. Extract an `EditorRunner` interface in `internal/secrets/` to make editor-dependent code testable. Migrate existing hand-written mocks in `internal/launcher/` and `pkg/seatbelt/` to mockgen. Add tests for uncovered helper functions in `internal/trust/` and `internal/launcher/`.

**Tech Stack:** Go, go.uber.org/mock (mockgen), go generate, GNU Make

---

### Task 1: Add mockgen tooling

**Files:**
- Modify: `go.mod`
- Modify: `Makefile`
- Modify: `flake.nix` (if mockgen is available as a nix package, otherwise skip)

- [ ] **Step 1: Add go.uber.org/mock dependency**

```bash
go get go.uber.org/mock@latest
```

- [ ] **Step 2: Install mockgen binary**

```bash
go install go.uber.org/mock/mockgen@latest
```

- [ ] **Step 3: Add `generate` target to Makefile**

Add after the `vet` target in `Makefile`:

```makefile
generate:
	go generate ./...
```

- [ ] **Step 4: Verify mockgen is available**

```bash
mockgen --version
```

Expected: version string like `v0.5.x`

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum Makefile
```

Use `/commit` to commit.

---

### Task 2: Generate mock for `Execer` in `internal/launcher/`

**Files:**
- Modify: `internal/launcher/launcher.go` (add go:generate directive)
- Create: `internal/launcher/mocks/mock_execer.go` (generated)

- [ ] **Step 1: Add go:generate directive to launcher.go**

Add directly above the `Execer` interface definition at line 22:

```go
//go:generate mockgen -destination=mocks/mock_execer.go -package=mocks github.com/jskswamy/aide/internal/launcher Execer
```

- [ ] **Step 2: Create mocks directory and generate**

```bash
mkdir -p internal/launcher/mocks
go generate ./internal/launcher/...
```

- [ ] **Step 3: Verify generated file exists**

```bash
ls internal/launcher/mocks/mock_execer.go
```

Expected: file exists

- [ ] **Step 4: Verify it compiles**

```bash
go build ./internal/launcher/...
```

Expected: no errors

- [ ] **Step 5: Commit**

```bash
git add internal/launcher/launcher.go internal/launcher/mocks/
```

Use `/commit` to commit.

---

### Task 3: Migrate `mockExecer` to generated mock in launcher tests

**Files:**
- Modify: `internal/launcher/launcher_test.go`

- [ ] **Step 1: Update imports in launcher_test.go**

Add to the import block:

```go
"go.uber.org/mock/gomock"
"github.com/jskswamy/aide/internal/launcher/mocks"
```

- [ ] **Step 2: Remove hand-written mockExecer**

Delete the `mockExecer` struct and its `Exec` method (lines 15-28 in launcher_test.go):

```go
// DELETE THIS BLOCK:
// mockExecer captures exec arguments instead of actually executing.
type mockExecer struct {
	binary string
	args   []string
	env    []string
	err    error // error to return from Exec
}

func (m *mockExecer) Exec(binary string, args []string, env []string) error {
	m.binary = binary
	m.args = args
	m.env = env
	return m.err
}
```

- [ ] **Step 3: Update each test that creates `mockExecer`**

Find every test that does `mock := &mockExecer{...}` or `execer: &mockExecer{...}`. Replace with the gomock pattern. For example, a test that previously did:

```go
mock := &mockExecer{}
l := &Launcher{Execer: mock}
// ... run test ...
if mock.binary != "/usr/bin/claude" {
    t.Errorf(...)
}
```

Becomes:

```go
ctrl := gomock.NewController(t)
mockExec := mocks.NewMockExecer(ctrl)
mockExec.EXPECT().
    Exec(gomock.Any(), gomock.Any(), gomock.Any()).
    DoAndReturn(func(binary string, args []string, env []string) error {
        if !strings.HasSuffix(binary, "claude") && !strings.HasSuffix(args[len(args)-1], "claude") {
            t.Errorf("expected claude binary, got %s", binary)
        }
        return nil
    })
l := &Launcher{Execer: mockExec}
```

For tests that need the mock to return an error:

```go
mockExec.EXPECT().
    Exec(gomock.Any(), gomock.Any(), gomock.Any()).
    Return(fmt.Errorf("exec failed"))
```

Apply this pattern to every test function that references `mockExecer`. The exact number of tests varies -- search for `mockExecer` in the file and update each occurrence.

- [ ] **Step 4: Run tests to verify migration**

```bash
go test -race ./internal/launcher/...
```

Expected: all tests pass

- [ ] **Step 5: Commit**

```bash
git add internal/launcher/launcher_test.go
```

Use `/commit` to commit.

---

### Task 4: Generate mock for `Sandbox` interface

**Files:**
- Modify: `internal/sandbox/sandbox.go` (add go:generate directive)
- Create: `internal/sandbox/mocks/mock_sandbox.go` (generated)

- [ ] **Step 1: Add go:generate directive to sandbox.go**

Add directly above the `Sandbox` interface definition at line 17:

```go
//go:generate mockgen -destination=mocks/mock_sandbox.go -package=mocks github.com/jskswamy/aide/internal/sandbox Sandbox
```

- [ ] **Step 2: Generate**

```bash
mkdir -p internal/sandbox/mocks
go generate ./internal/sandbox/...
```

- [ ] **Step 3: Verify it compiles**

```bash
go build ./internal/sandbox/...
```

Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add internal/sandbox/sandbox.go internal/sandbox/mocks/
```

Use `/commit` to commit.

---

### Task 5: Generate mocks for `Module` and `Guard` in `pkg/seatbelt/`

**Files:**
- Modify: `pkg/seatbelt/module.go` (add go:generate directive)
- Create: `pkg/seatbelt/mocks/mock_module.go` (generated)

- [ ] **Step 1: Add go:generate directive to module.go**

Add directly above the `Module` interface definition at line 9:

```go
//go:generate mockgen -destination=mocks/mock_module.go -package=mocks github.com/jskswamy/aide/pkg/seatbelt Module,Guard
```

- [ ] **Step 2: Generate**

```bash
mkdir -p pkg/seatbelt/mocks
go generate ./pkg/seatbelt/...
```

- [ ] **Step 3: Verify it compiles**

```bash
go build ./pkg/seatbelt/...
```

Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add pkg/seatbelt/module.go pkg/seatbelt/mocks/
```

Use `/commit` to commit.

---

### Task 6: Migrate `testModule` to generated mock in seatbelt tests

**Files:**
- Modify: `pkg/seatbelt/profile_test.go`

- [ ] **Step 1: Update imports in profile_test.go**

Add to the import block:

```go
"go.uber.org/mock/gomock"
"github.com/jskswamy/aide/pkg/seatbelt/mocks"
```

- [ ] **Step 2: Remove hand-written testModule**

Delete the `testModule` struct and its methods (lines 8-21 in profile_test.go):

```go
// DELETE THIS BLOCK:
type testModule struct {
	name   string
	rules  []Rule
	result GuardResult
}

func (m *testModule) Name() string { return m.name }
func (m *testModule) Rules(_ *Context) GuardResult {
	if len(m.result.Rules) > 0 || len(m.result.Protected) > 0 || len(m.result.Skipped) > 0 {
		return m.result
	}
	return GuardResult{Rules: m.rules}
}
```

- [ ] **Step 3: Update tests that use testModule**

Each test that creates `&testModule{name: "test", rules: []Rule{...}}` becomes:

```go
ctrl := gomock.NewController(t)
mockMod := mocks.NewMockModule(ctrl)
mockMod.EXPECT().Name().Return("test").AnyTimes()
mockMod.EXPECT().Rules(gomock.Any()).Return(seatbelt.GuardResult{
    Rules: []seatbelt.Rule{seatbelt.AllowOp("process-exec")},
}).AnyTimes()
```

Then use `mockMod` where `&testModule{...}` was used. For example:

```go
// Before:
p := New("/home/user").Use(&testModule{
    name:  "test",
    rules: []Rule{AllowOp("process-exec")},
})

// After:
ctrl := gomock.NewController(t)
mockMod := mocks.NewMockModule(ctrl)
mockMod.EXPECT().Name().Return("test").AnyTimes()
mockMod.EXPECT().Rules(gomock.Any()).Return(GuardResult{
    Rules: []Rule{AllowOp("process-exec")},
}).AnyTimes()
p := New("/home/user").Use(mockMod)
```

Apply to every test in `profile_test.go` that uses `testModule`.

- [ ] **Step 4: Run tests**

```bash
go test -race ./pkg/seatbelt/...
```

Expected: all tests pass

- [ ] **Step 5: Commit**

```bash
git add pkg/seatbelt/profile_test.go
```

Use `/commit` to commit.

---

### Task 7: Create `EditorRunner` interface in `internal/secrets/`

**Files:**
- Create: `internal/secrets/editor.go`
- Create: `internal/secrets/mocks/mock_editor.go` (generated)
- Modify: `internal/secrets/manager.go`

- [ ] **Step 1: Write failing test for EditorRunner integration**

Create `internal/secrets/editor_test.go`:

```go
package secrets

import (
	"testing"
)

func TestRealEditorRunner_implements_interface(t *testing.T) {
	var _ EditorRunner = &RealEditorRunner{}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/secrets/... -run TestRealEditorRunner
```

Expected: FAIL -- `EditorRunner` undefined

- [ ] **Step 3: Create editor.go with interface and real implementation**

Create `internal/secrets/editor.go`:

```go
package secrets

import (
	"io"
	"os/exec"
)

//go:generate mockgen -destination=mocks/mock_editor.go -package=mocks github.com/jskswamy/aide/internal/secrets EditorRunner

// EditorRunner abstracts running an external editor for testability.
type EditorRunner interface {
	// Run launches the editor binary with the given args.
	Run(editor string, args []string, stdin io.Reader, stdout, stderr io.Writer) error
}

// RealEditorRunner launches the actual editor process.
type RealEditorRunner struct{}

// Run executes the editor command.
func (r *RealEditorRunner) Run(editor string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	cmd := exec.Command(editor, args...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/secrets/... -run TestRealEditorRunner
```

Expected: PASS

- [ ] **Step 5: Generate mock**

```bash
mkdir -p internal/secrets/mocks
go generate ./internal/secrets/...
```

- [ ] **Step 6: Add EditorRunner field to Manager**

Modify `internal/secrets/manager.go`. Change the `Manager` struct:

```go
// Manager handles the full lifecycle of sops-encrypted secrets files.
type Manager struct {
	secretsDir string
	runtimeDir string
	editor     EditorRunner
}

// NewManager creates a new secrets Manager.
func NewManager(secretsDir, runtimeDir string) *Manager {
	return &Manager{
		secretsDir: secretsDir,
		runtimeDir: runtimeDir,
		editor:     &RealEditorRunner{},
	}
}

// NewManagerWithEditor creates a Manager with a custom EditorRunner (for testing).
func NewManagerWithEditor(secretsDir, runtimeDir string, editor EditorRunner) *Manager {
	return &Manager{
		secretsDir: secretsDir,
		runtimeDir: runtimeDir,
		editor:     editor,
	}
}
```

- [ ] **Step 7: Update Create() to use the EditorRunner**

In `internal/secrets/manager.go`, replace the editor invocation in `Create()` (around lines 72-78):

```go
// Before:
editor := resolveEditor()
cmd := exec.Command(editor, tmpFile)
cmd.Stdin = os.Stdin
cmd.Stdout = os.Stdout
cmd.Stderr = os.Stderr
if err := cmd.Run(); err != nil {
    return fmt.Errorf("editor exited with error: %w", err)
}

// After:
editorBin := resolveEditor()
if err := m.editor.Run(editorBin, []string{tmpFile}, os.Stdin, os.Stdout, os.Stderr); err != nil {
    return fmt.Errorf("editor exited with error: %w", err)
}
```

- [ ] **Step 8: Update Edit() to use the EditorRunner**

In `internal/secrets/manager.go`, replace the editor invocation in `Edit()` (around lines 300-307):

```go
// Before:
editor := resolveEditor()
cmd := exec.Command(editor, tmpFile)
cmd.Stdin = os.Stdin
cmd.Stdout = os.Stdout
cmd.Stderr = os.Stderr
if err := cmd.Run(); err != nil {
    return fmt.Errorf("editor exited with error: %w", err)
}

// After:
editorBin := resolveEditor()
if err := m.editor.Run(editorBin, []string{tmpFile}, os.Stdin, os.Stdout, os.Stderr); err != nil {
    return fmt.Errorf("editor exited with error: %w", err)
}
```

- [ ] **Step 9: Remove the `"os/exec"` import if no longer used directly**

Check if `exec` is still referenced in `manager.go`. If `Create()` and `Edit()` were the only callers, remove `"os/exec"` from the import block.

- [ ] **Step 10: Run all existing tests**

```bash
go test -race ./internal/secrets/...
```

Expected: all existing tests pass (CreateFromContent/EditFromContent don't use the editor)

- [ ] **Step 11: Commit**

```bash
git add internal/secrets/editor.go internal/secrets/editor_test.go internal/secrets/manager.go internal/secrets/mocks/
```

Use `/commit` to commit.

---

### Task 8: Add tests for secrets helper functions

**Files:**
- Modify: `internal/secrets/manager_test.go` (add tests)
- Modify: `internal/secrets/age_test.go` (add tests)

- [ ] **Step 1: Write tests for validateName**

Add to `internal/secrets/manager_test.go`:

```go
func TestValidateName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid simple", "personal", false},
		{"valid with hyphen", "my-secrets", false},
		{"valid with underscore", "my_secrets", false},
		{"valid with numbers", "secret123", false},
		{"empty", "", true},
		{"starts with hyphen", "-invalid", true},
		{"starts with underscore", "_invalid", true},
		{"has spaces", "my secrets", true},
		{"has dots", "my.secrets", true},
		{"has slash", "path/name", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateName(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}
```

- [ ] **Step 2: Write tests for validateContent**

```go
func TestValidateContent(t *testing.T) {
	tests := []struct {
		name    string
		content []byte
		wantErr bool
	}{
		{"valid", []byte("key: value\n"), false},
		{"empty", []byte(""), true},
		{"whitespace only", []byte("   \n  \n"), true},
		{"comments only", []byte("# comment\n# another\n"), true},
		{"comments with value", []byte("# comment\nkey: value\n"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateContent(tt.content)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateContent() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
```

- [ ] **Step 3: Write tests for validateFlatYAML**

```go
func TestValidateFlatYAML(t *testing.T) {
	tests := []struct {
		name    string
		content []byte
		wantErr bool
	}{
		{"flat map", []byte("key: value\nnum: 42\n"), false},
		{"boolean value", []byte("flag: true\n"), false},
		{"nested map", []byte("parent:\n  child: value\n"), true},
		{"list value", []byte("items:\n  - one\n  - two\n"), true},
		{"invalid yaml", []byte("not: valid: yaml: {{{\n"), true},
		{"scalar only", []byte("just a string\n"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateFlatYAML(tt.content)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateFlatYAML() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
```

- [ ] **Step 4: Write tests for resolveEditor**

```go
func TestResolveEditor(t *testing.T) {
	t.Run("EDITOR set", func(t *testing.T) {
		t.Setenv("EDITOR", "/usr/bin/nano")
		t.Setenv("VISUAL", "")
		got := resolveEditor()
		if got != "/usr/bin/nano" {
			t.Errorf("resolveEditor() = %q, want /usr/bin/nano", got)
		}
	})

	t.Run("VISUAL set no EDITOR", func(t *testing.T) {
		t.Setenv("EDITOR", "")
		t.Setenv("VISUAL", "/usr/bin/code")
		got := resolveEditor()
		if got != "/usr/bin/code" {
			t.Errorf("resolveEditor() = %q, want /usr/bin/code", got)
		}
	})

	t.Run("EDITOR takes precedence", func(t *testing.T) {
		t.Setenv("EDITOR", "/usr/bin/nano")
		t.Setenv("VISUAL", "/usr/bin/code")
		got := resolveEditor()
		if got != "/usr/bin/nano" {
			t.Errorf("resolveEditor() = %q, want /usr/bin/nano", got)
		}
	})

	t.Run("fallback to vi", func(t *testing.T) {
		t.Setenv("EDITOR", "")
		t.Setenv("VISUAL", "")
		got := resolveEditor()
		if got != "vi" {
			t.Errorf("resolveEditor() = %q, want vi", got)
		}
	})
}
```

- [ ] **Step 5: Write tests for fileReadable and defaultKeyPath**

Add to `internal/secrets/age_test.go`:

```go
func TestFileReadable(t *testing.T) {
	t.Run("regular file", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "test.txt")
		if err := os.WriteFile(f, []byte("data"), 0o600); err != nil {
			t.Fatal(err)
		}
		if !fileReadable(f) {
			t.Error("fileReadable() = false for existing regular file")
		}
	})

	t.Run("directory", func(t *testing.T) {
		d := t.TempDir()
		if fileReadable(d) {
			t.Error("fileReadable() = true for directory")
		}
	})

	t.Run("missing", func(t *testing.T) {
		if fileReadable("/nonexistent/path/file.txt") {
			t.Error("fileReadable() = true for missing file")
		}
	})
}

func TestDefaultKeyPath(t *testing.T) {
	t.Run("with XDG_CONFIG_HOME", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "/custom/config")
		got := defaultKeyPath()
		want := "/custom/config/sops/age/keys.txt"
		if got != want {
			t.Errorf("defaultKeyPath() = %q, want %q", got, want)
		}
	})

	t.Run("without XDG_CONFIG_HOME", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "")
		got := defaultKeyPath()
		if !strings.HasSuffix(got, ".config/sops/age/keys.txt") {
			t.Errorf("defaultKeyPath() = %q, want suffix .config/sops/age/keys.txt", got)
		}
	})
}
```

- [ ] **Step 6: Run all secrets tests**

```bash
go test -race -v ./internal/secrets/...
```

Expected: all tests pass

- [ ] **Step 7: Check coverage**

```bash
go test -coverprofile=secrets.cov ./internal/secrets/...
go tool cover -func=secrets.cov | grep total
```

Expected: coverage increased from 66.2%

- [ ] **Step 8: Commit**

```bash
git add internal/secrets/manager_test.go internal/secrets/age_test.go
```

Use `/commit` to commit.

---

### Task 9: Add tests for Create/Edit with mock editor

**Files:**
- Modify: `internal/secrets/manager_test.go`

- [ ] **Step 1: Write test for Create with mock editor**

```go
func TestCreate_WithMockEditor(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockEditor := smocks.NewMockEditorRunner(ctrl)

	tmpDir := t.TempDir()
	secretsDir := filepath.Join(tmpDir, "secrets")
	runtimeDir := filepath.Join(tmpDir, "runtime")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}

	// Generate a test age key pair.
	identity, err := agelib.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	pubKey := identity.Recipient().String()

	// Mock editor writes valid YAML to the temp file.
	mockEditor.EXPECT().
		Run(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ string, args []string, _ io.Reader, _, _ io.Writer) error {
			return os.WriteFile(args[0], []byte("api_key: sk-test-123\n"), 0o600)
		})

	t.Setenv("EDITOR", "fake-editor")
	mgr := NewManagerWithEditor(secretsDir, runtimeDir, mockEditor)
	err = mgr.Create("test", secretsDir, pubKey)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Verify encrypted file was created.
	encPath := filepath.Join(secretsDir, "test.enc.yaml")
	if _, err := os.Stat(encPath); os.IsNotExist(err) {
		t.Error("encrypted file not created")
	}
}
```

Add these imports to the test file:

```go
import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"filippo.io/age"
	"go.uber.org/mock/gomock"

	smocks "github.com/jskswamy/aide/internal/secrets/mocks"
)
```

Note: The `age` import alias may need adjustment based on how the project imports it. Check `go.mod` for the exact module path. Use the `filippo.io/age` package directly to generate test key pairs.

- [ ] **Step 2: Write test for Create when editor returns error**

```go
func TestCreate_EditorError(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockEditor := smocks.NewMockEditorRunner(ctrl)

	tmpDir := t.TempDir()
	runtimeDir := filepath.Join(tmpDir, "runtime")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}

	mockEditor.EXPECT().
		Run(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(fmt.Errorf("editor crashed"))

	t.Setenv("EDITOR", "fake-editor")
	mgr := NewManagerWithEditor(tmpDir, runtimeDir, mockEditor)
	err := mgr.Create("test", filepath.Join(tmpDir, "secrets"), "age1test...")
	if err == nil {
		t.Fatal("Create() expected error when editor fails")
	}
	if !strings.Contains(err.Error(), "editor exited with error") {
		t.Errorf("unexpected error message: %v", err)
	}
}
```

- [ ] **Step 3: Write test for Create when editor saves empty content**

```go
func TestCreate_EditorEmptyContent(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockEditor := smocks.NewMockEditorRunner(ctrl)

	tmpDir := t.TempDir()
	runtimeDir := filepath.Join(tmpDir, "runtime")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}

	// Mock editor writes only the template comment (no actual secrets).
	mockEditor.EXPECT().
		Run(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ string, args []string, _ io.Reader, _, _ io.Writer) error {
			return os.WriteFile(args[0], []byte("# just comments\n"), 0o600)
		})

	t.Setenv("EDITOR", "fake-editor")
	mgr := NewManagerWithEditor(tmpDir, runtimeDir, mockEditor)
	err := mgr.Create("test", filepath.Join(tmpDir, "secrets"), "age1test...")
	if err == nil {
		t.Fatal("Create() expected error for empty content")
	}
	if !strings.Contains(err.Error(), "no secrets entered") {
		t.Errorf("unexpected error: %v", err)
	}
}
```

- [ ] **Step 4: Run tests**

```bash
go test -race -v ./internal/secrets/... -run TestCreate
```

Expected: all pass

- [ ] **Step 5: Check coverage**

```bash
go test -coverprofile=secrets.cov ./internal/secrets/...
go tool cover -func=secrets.cov | grep total
```

Expected: approaching 80%

- [ ] **Step 6: Commit**

```bash
git add internal/secrets/manager_test.go
```

Use `/commit` to commit.

---

### Task 10: Add tests for `internal/trust/` uncovered functions

**Files:**
- Modify: `internal/trust/trust_test.go`

- [ ] **Step 1: Write test for Status.String()**

```go
func TestStatusString(t *testing.T) {
	tests := []struct {
		status Status
		want   string
	}{
		{Trusted, "trusted"},
		{Denied, "denied"},
		{Untrusted, "untrusted"},
		{Status(99), "untrusted"}, // unknown defaults to untrusted
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.status.String()
			if got != tt.want {
				t.Errorf("Status(%d).String() = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Write test for DefaultStore()**

```go
func TestDefaultStore(t *testing.T) {
	t.Run("with XDG_DATA_HOME", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", "/custom/data")
		s := DefaultStore()
		if s.baseDir != "/custom/data/aide" {
			t.Errorf("DefaultStore().baseDir = %q, want /custom/data/aide", s.baseDir)
		}
	})

	t.Run("without XDG_DATA_HOME", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", "")
		s := DefaultStore()
		if !strings.HasSuffix(s.baseDir, ".local/share/aide") {
			t.Errorf("DefaultStore().baseDir = %q, want suffix .local/share/aide", s.baseDir)
		}
	})
}
```

- [ ] **Step 3: Write test for fileExists()**

```go
func TestFileExists(t *testing.T) {
	t.Run("exists", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "test")
		if err := os.WriteFile(f, []byte("data"), 0o600); err != nil {
			t.Fatal(err)
		}
		if !fileExists(f) {
			t.Error("fileExists() = false for existing file")
		}
	})

	t.Run("missing", func(t *testing.T) {
		if fileExists("/nonexistent/path") {
			t.Error("fileExists() = true for missing path")
		}
	})

	t.Run("directory", func(t *testing.T) {
		d := t.TempDir()
		// fileExists returns true for directories too (uses os.Stat)
		if !fileExists(d) {
			t.Error("fileExists() = false for directory")
		}
	})
}
```

- [ ] **Step 4: Write test for atomicWrite()**

```go
func TestAtomicWrite(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "testfile")
		err := atomicWrite(path, []byte("hello"))
		if err != nil {
			t.Fatalf("atomicWrite() error = %v", err)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "hello" {
			t.Errorf("file content = %q, want %q", got, "hello")
		}
	})

	t.Run("read-only directory", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "readonly")
		if err := os.MkdirAll(dir, 0o500); err != nil {
			t.Fatal(err)
		}
		err := atomicWrite(filepath.Join(dir, "fail"), []byte("data"))
		if err == nil {
			t.Error("atomicWrite() expected error for read-only directory")
		}
	})
}
```

- [ ] **Step 5: Run tests**

```bash
go test -race -v ./internal/trust/...
```

Expected: all pass

- [ ] **Step 6: Check coverage**

```bash
go test -coverprofile=trust.cov ./internal/trust/...
go tool cover -func=trust.cov | grep total
```

Expected: coverage above 80%

- [ ] **Step 7: Commit**

```bash
git add internal/trust/trust_test.go
```

Use `/commit` to commit.

---

### Task 11: Add tests for `internal/launcher/` helper functions

**Files:**
- Create: `internal/launcher/helpers_test.go`

- [ ] **Step 1: Write tests for filterEssentialEnv**

Create `internal/launcher/helpers_test.go`:

```go
package launcher

import (
	"testing"
)

func TestFilterEssentialEnv(t *testing.T) {
	env := []string{
		"PATH=/usr/bin",
		"HOME=/home/user",
		"ANTHROPIC_API_KEY=sk-ant-123",
		"SHELL=/bin/bash",
		"RANDOM_VAR=value",
		"TERM=xterm",
	}
	got := filterEssentialEnv(env)
	want := map[string]bool{
		"PATH=/usr/bin":    true,
		"HOME=/home/user":  true,
		"SHELL=/bin/bash":  true,
		"TERM=xterm":       true,
	}
	if len(got) != len(want) {
		t.Errorf("filterEssentialEnv() returned %d entries, want %d", len(got), len(want))
	}
	for _, e := range got {
		if !want[e] {
			t.Errorf("unexpected entry: %s", e)
		}
	}
}

func TestFilterEssentialEnv_Empty(t *testing.T) {
	got := filterEssentialEnv(nil)
	if len(got) != 0 {
		t.Errorf("filterEssentialEnv(nil) = %v, want empty", got)
	}
}
```

- [ ] **Step 2: Write tests for mergeEnv**

```go
func TestMergeEnv(t *testing.T) {
	base := []string{"PATH=/usr/bin", "HOME=/home/user", "EXISTING=old"}
	resolved := map[string]string{"EXISTING": "new", "ADDED": "value"}

	got := mergeEnv(base, resolved)

	// Check that EXISTING was overridden
	foundExisting := false
	foundAdded := false
	for _, e := range got {
		if e == "EXISTING=new" {
			foundExisting = true
		}
		if e == "EXISTING=old" {
			t.Error("old EXISTING value should be replaced")
		}
		if e == "ADDED=value" {
			foundAdded = true
		}
	}
	if !foundExisting {
		t.Error("EXISTING=new not found")
	}
	if !foundAdded {
		t.Error("ADDED=value not found")
	}
}

func TestMergeEnv_EmptyInputs(t *testing.T) {
	got := mergeEnv(nil, nil)
	if len(got) != 0 {
		t.Errorf("mergeEnv(nil, nil) = %v, want empty", got)
	}
}
```

- [ ] **Step 3: Write tests for redactValue**

```go
func TestRedactValue(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"short", "short***"},
		{"12345678", "12345678***"},
		{"123456789", "12345678***"},
		{"sk-ant-api03-very-long-key", "sk-ant-a***"},
		{"", "***"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := redactValue(tt.input)
			if got != tt.want {
				t.Errorf("redactValue(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 4: Write tests for stringSetDiff**

```go
func TestStringSetDiff(t *testing.T) {
	tests := []struct {
		name string
		a    []string
		b    []string
		want int
	}{
		{"disjoint", []string{"x", "y"}, []string{"a", "b"}, 2},
		{"overlap", []string{"x", "y", "z"}, []string{"y"}, 2},
		{"subset", []string{"x", "y"}, []string{"x", "y"}, 0},
		{"empty a", nil, []string{"x"}, 0},
		{"empty b", []string{"x"}, nil, 1},
		{"both empty", nil, nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stringSetDiff(tt.a, tt.b)
			if len(got) != tt.want {
				t.Errorf("stringSetDiff() = %v (len %d), want len %d", got, len(got), tt.want)
			}
		})
	}
}
```

- [ ] **Step 5: Write tests for yoloSource**

```go
func TestYoloSource(t *testing.T) {
	boolPtr := func(b bool) *bool { return &b }

	tests := []struct {
		name        string
		cliFlag     bool
		preferences *bool
		context     *bool
		project     *bool
		want        string
	}{
		{"cli flag", true, nil, nil, nil, "--yolo flag"},
		{"project", false, nil, nil, boolPtr(true), ".aide.yaml"},
		{"context", false, nil, boolPtr(true), nil, "context config"},
		{"preferences", false, boolPtr(true), nil, nil, "preferences"},
		{"default", false, nil, nil, nil, "config"},
		{"project wins over context", false, nil, boolPtr(true), boolPtr(true), ".aide.yaml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := yoloSource(tt.cliFlag, tt.preferences, tt.context, tt.project)
			if got != tt.want {
				t.Errorf("yoloSource() = %q, want %q", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 6: Write tests for wrapTemplateError**

```go
func TestWrapTemplateError(t *testing.T) {
	t.Run("missing key with secret", func(t *testing.T) {
		err := fmt.Errorf("map has no entry for key \"missing\"")
		got := wrapTemplateError(err, "work", "work-secrets")
		if !strings.Contains(got.Error(), "secret key not found") {
			t.Errorf("wrapTemplateError() = %q, want 'secret key not found'", got)
		}
	})

	t.Run("missing key without secret", func(t *testing.T) {
		err := fmt.Errorf("map has no entry for key \"missing\"")
		got := wrapTemplateError(err, "work", "")
		if !strings.Contains(got.Error(), "no secret configured") {
			t.Errorf("wrapTemplateError() = %q, want 'no secret configured'", got)
		}
	})

	t.Run("nil pointer", func(t *testing.T) {
		err := fmt.Errorf("nil pointer evaluating")
		got := wrapTemplateError(err, "work", "work-secrets")
		if got == nil {
			t.Error("wrapTemplateError() should return non-nil error")
		}
	})
}
```

Add `"fmt"` and `"strings"` to the imports.

- [ ] **Step 7: Run tests**

```bash
go test -race -v ./internal/launcher/... -run "TestFilter|TestMerge|TestRedact|TestStringSet|TestYolo|TestWrap"
```

Expected: all pass

- [ ] **Step 8: Check coverage**

```bash
go test -coverprofile=launcher.cov ./internal/launcher/...
go tool cover -func=launcher.cov | grep total
```

Expected: coverage above 80%

- [ ] **Step 9: Commit**

```bash
git add internal/launcher/helpers_test.go
```

Use `/commit` to commit.

---

### Task 12: Update CI to exclude cmd/aide/ and verify mocks

**Files:**
- Modify: `.github/workflows/ci.yml`

- [ ] **Step 1: Update coverage threshold step to exclude cmd/aide/**

Replace the "Check coverage threshold" step in `.github/workflows/ci.yml`:

```yaml
      - name: Check coverage threshold
        run: |
          # Exclude cmd/aide/ (Cobra wiring, untestable without major refactoring)
          grep -v "github.com/jskswamy/aide/cmd/" coverage.out > coverage-filtered.out
          TOTAL=$(go tool cover -func=coverage-filtered.out | grep total | awk '{print $3}' | tr -d '%')
          echo "Total coverage (excluding cmd/aide): ${TOTAL}%"
          if [ "$(echo "$TOTAL < 60" | bc -l)" -eq 1 ]; then
            echo "::error::Coverage ${TOTAL}% is below minimum threshold of 60%"
            exit 1
          fi
```

- [ ] **Step 2: Add mock drift check step**

Add a new step after the "Run tests" step:

```yaml
      - name: Check generated mocks are up to date
        run: |
          go install go.uber.org/mock/mockgen@latest
          go generate ./...
          if ! git diff --quiet -- '*/mocks/'; then
            echo "::error::Generated mocks are out of date. Run 'make generate' and commit."
            git diff -- '*/mocks/'
            exit 1
          fi
```

- [ ] **Step 3: Run CI locally to verify**

```bash
go test -race -coverprofile=coverage.out ./...
grep -v "github.com/jskswamy/aide/cmd/" coverage.out > coverage-filtered.out
go tool cover -func=coverage-filtered.out | grep total
```

Expected: total coverage (excluding cmd/aide) above 60%

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/ci.yml
```

Use `/commit` to commit.

---

### Task 13: Final coverage verification

**Files:** None (verification only)

- [ ] **Step 1: Run full test suite with coverage**

```bash
go test -race -coverprofile=coverage.out ./...
```

Expected: all tests pass

- [ ] **Step 2: Check security-critical package thresholds**

```bash
for PKG in pkg/seatbelt internal/sandbox internal/secrets; do
  echo -n "${PKG}: "
  go tool cover -func=coverage.out | grep "github.com/jskswamy/aide/${PKG}" | awk '{sum += $3; n++} END {if (n>0) printf "%.1f%%\n", sum/n; else print "no data"}'
done
```

Expected: all three above 80%

- [ ] **Step 3: Check overall coverage excluding cmd/aide/**

```bash
grep -v "github.com/jskswamy/aide/cmd/" coverage.out > coverage-filtered.out
go tool cover -func=coverage-filtered.out | grep total
```

Expected: above 60%

- [ ] **Step 4: Verify no hand-written mocks remain**

```bash
grep -rn "type mock\|type fake\|type stub\|type testModule" --include="*_test.go" internal/ pkg/
```

Expected: no matches

- [ ] **Step 5: Verify go generate produces no diff**

```bash
go generate ./...
git diff --quiet -- '*/mocks/' && echo "Mocks up to date" || echo "MOCKS OUT OF DATE"
```

Expected: "Mocks up to date"
