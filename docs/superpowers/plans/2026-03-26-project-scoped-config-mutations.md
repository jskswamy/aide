# Project-Scoped Config Mutations Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Default CLI mutation commands to project-level `.aide.yaml` instead of user-level `~/.config/aide/config.yaml`, with `--global` to opt into user-level.

**Architecture:** Add `DisabledCapabilities` field to `ProjectOverride`, change `applyProjectOverride()` to use additive merge for capabilities and field-level merge for sandbox, add `WriteProjectOverride()` and `resolveProjectOverrideForMutation()`, then update all 12 affected commands to use `--global` flag routing.

**Tech Stack:** Go, cobra, gopkg.in/yaml.v3

**Spec:** `docs/superpowers/specs/2026-03-26-project-scoped-config-mutations-design.md`

---

### Task 1: Add DisabledCapabilities to ProjectOverride schema

**Files:**
- Modify: `internal/config/schema.go:264-273`
- Test: `internal/config/schema_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestProjectOverride_DisabledCapabilities_RoundTrip(t *testing.T) {
	input := `
capabilities:
  - k8s
  - terraform
disabled_capabilities:
  - docker
`
	var po ProjectOverride
	if err := yaml.Unmarshal([]byte(input), &po); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(po.Capabilities) != 2 || po.Capabilities[0] != "k8s" {
		t.Errorf("expected capabilities [k8s terraform], got %v", po.Capabilities)
	}
	if len(po.DisabledCapabilities) != 1 || po.DisabledCapabilities[0] != "docker" {
		t.Errorf("expected disabled_capabilities [docker], got %v", po.DisabledCapabilities)
	}

	data, err := yaml.Marshal(&po)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var po2 ProjectOverride
	if err := yaml.Unmarshal(data, &po2); err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}
	if len(po2.DisabledCapabilities) != 1 || po2.DisabledCapabilities[0] != "docker" {
		t.Errorf("round-trip failed: got %v", po2.DisabledCapabilities)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestProjectOverride_DisabledCapabilities -v`
Expected: FAIL — `DisabledCapabilities` field does not exist

- [ ] **Step 3: Add the field to ProjectOverride**

In `internal/config/schema.go`, add to the `ProjectOverride` struct (after `Capabilities` field on line 272):

```go
DisabledCapabilities []string `yaml:"disabled_capabilities,omitempty"`
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -run TestProjectOverride_DisabledCapabilities -v`
Expected: PASS

- [ ] **Step 5: Commit**

```
git add internal/config/schema.go internal/config/schema_test.go
/commit Add DisabledCapabilities field to ProjectOverride
```

---

### Task 2: Change capabilities merge to additive with subtraction

**Files:**
- Modify: `internal/context/resolver.go:114-153`
- Test: `internal/context/resolver_test.go`

- [ ] **Step 1: Write failing tests**

Add three tests to `internal/context/resolver_test.go`:

```go
func TestResolve_ProjectOverrideCapabilities_Additive(t *testing.T) {
	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"work": {
				Agent:        "claude",
				Capabilities: []string{"docker"},
			},
		},
		DefaultContext: "work",
		ProjectOverride: &config.ProjectOverride{
			Capabilities: []string{"k8s", "aws"},
		},
	}
	rc, err := Resolve(cfg, "/tmp/somedir", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Additive: docker + k8s + aws
	caps := rc.Context.Capabilities
	if len(caps) != 3 {
		t.Fatalf("expected 3 capabilities, got %d: %v", len(caps), caps)
	}
	want := map[string]bool{"docker": true, "k8s": true, "aws": true}
	for _, c := range caps {
		if !want[c] {
			t.Errorf("unexpected capability %q in %v", c, caps)
		}
	}
}

func TestResolve_ProjectOverrideCapabilities_Dedup(t *testing.T) {
	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"work": {
				Agent:        "claude",
				Capabilities: []string{"docker", "k8s"},
			},
		},
		DefaultContext: "work",
		ProjectOverride: &config.ProjectOverride{
			Capabilities: []string{"k8s", "aws"},
		},
	}
	rc, err := Resolve(cfg, "/tmp/somedir", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// docker + k8s + aws (k8s deduped)
	caps := rc.Context.Capabilities
	if len(caps) != 3 {
		t.Fatalf("expected 3 capabilities (deduped), got %d: %v", len(caps), caps)
	}
}

func TestResolve_ProjectOverrideDisabledCapabilities(t *testing.T) {
	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"work": {
				Agent:        "claude",
				Capabilities: []string{"docker", "k8s"},
			},
		},
		DefaultContext: "work",
		ProjectOverride: &config.ProjectOverride{
			Capabilities:         []string{"aws"},
			DisabledCapabilities: []string{"docker"},
		},
	}
	rc, err := Resolve(cfg, "/tmp/somedir", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// (docker + k8s + aws) - docker = k8s + aws
	caps := rc.Context.Capabilities
	if len(caps) != 2 {
		t.Fatalf("expected 2 capabilities after disable, got %d: %v", len(caps), caps)
	}
	for _, c := range caps {
		if c == "docker" {
			t.Errorf("docker should be disabled, but found in %v", caps)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/context/ -run "TestResolve_ProjectOverrideCapabilities_Additive|TestResolve_ProjectOverrideCapabilities_Dedup|TestResolve_ProjectOverrideDisabledCapabilities" -v`
Expected: First two FAIL (replace semantics give wrong count), third FAIL (DisabledCapabilities ignored)

- [ ] **Step 3: Update the existing test**

The existing `TestResolve_ProjectOverrideCapabilities` (line 802) tests replace semantics. Update it to test additive semantics:

```go
func TestResolve_ProjectOverrideCapabilities(t *testing.T) {
	cwd := t.TempDir()
	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"work": {
				Agent:        "claude",
				Capabilities: []string{"docker"},
				Match:        []config.MatchRule{{Path: cwd}},
			},
		},
		DefaultContext: "work",
		ProjectOverride: &config.ProjectOverride{
			Capabilities: []string{"k8s", "aws"},
		},
	}
	rc, err := Resolve(cfg, cwd, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Additive merge: docker + k8s + aws
	if len(rc.Context.Capabilities) != 3 {
		t.Errorf("expected 3 capabilities from additive merge, got %d: %v",
			len(rc.Context.Capabilities), rc.Context.Capabilities)
	}
}
```

- [ ] **Step 4: Implement additive merge with subtraction**

In `internal/context/resolver.go`, add helper functions and update `applyProjectOverride`:

```go
// dedupStrings returns a new slice with duplicate strings removed, preserving order.
func dedupStrings(s []string) []string {
	seen := make(map[string]bool, len(s))
	var result []string
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	return result
}

// subtractStrings returns elements in a that are not in b.
func subtractStrings(a, b []string) []string {
	remove := make(map[string]bool, len(b))
	for _, v := range b {
		remove[v] = true
	}
	var result []string
	for _, v := range a {
		if !remove[v] {
			result = append(result, v)
		}
	}
	return result
}
```

Then replace the capabilities block in `applyProjectOverride` (line 135-137):

```go
// Capabilities: additive merge, then subtract disabled
if len(po.Capabilities) > 0 || len(po.DisabledCapabilities) > 0 {
	merged := dedupStrings(append(rc.Context.Capabilities, po.Capabilities...))
	rc.Context.Capabilities = subtractStrings(merged, po.DisabledCapabilities)
}
```

- [ ] **Step 5: Run all resolver tests**

Run: `go test ./internal/context/ -v`
Expected: ALL PASS

- [ ] **Step 6: Commit**

```
git add internal/context/resolver.go internal/context/resolver_test.go
/commit Change capabilities merge to additive with subtraction
```

---

### Task 3: Change sandbox merge to field-level

**Files:**
- Modify: `internal/context/resolver.go:129-131`
- Test: `internal/context/resolver_test.go`

- [ ] **Step 1: Write failing tests**

```go
func TestResolve_ProjectOverrideSandbox_AdditiveExtra(t *testing.T) {
	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"work": {
				Agent: "claude",
				Sandbox: &config.SandboxRef{Inline: &config.SandboxPolicy{
					DeniedExtra:   []string{"/etc/passwd"},
					ReadableExtra: []string{"/opt/tools"},
				}},
			},
		},
		DefaultContext: "work",
		ProjectOverride: &config.ProjectOverride{
			Sandbox: &config.SandboxPolicy{
				DeniedExtra:   []string{"/etc/shadow"},
				ReadableExtra: []string{"/opt/data"},
			},
		},
	}
	rc, err := Resolve(cfg, "/tmp/somedir", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	inline := rc.Context.Sandbox.Inline
	if len(inline.DeniedExtra) != 2 {
		t.Errorf("expected 2 denied_extra (additive), got %v", inline.DeniedExtra)
	}
	if len(inline.ReadableExtra) != 2 {
		t.Errorf("expected 2 readable_extra (additive), got %v", inline.ReadableExtra)
	}
}

func TestResolve_ProjectOverrideSandbox_ReplaceBase(t *testing.T) {
	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"work": {
				Agent: "claude",
				Sandbox: &config.SandboxRef{Inline: &config.SandboxPolicy{
					Writable: []string{"/tmp"},
					Network:  &config.NetworkPolicy{Mode: "none"},
				}},
			},
		},
		DefaultContext: "work",
		ProjectOverride: &config.ProjectOverride{
			Sandbox: &config.SandboxPolicy{
				Writable: []string{"/var/data"},
				Network:  &config.NetworkPolicy{Mode: "outbound"},
			},
		},
	}
	rc, err := Resolve(cfg, "/tmp/somedir", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	inline := rc.Context.Sandbox.Inline
	if len(inline.Writable) != 1 || inline.Writable[0] != "/var/data" {
		t.Errorf("expected writable replaced to [/var/data], got %v", inline.Writable)
	}
	if inline.Network.Mode != "outbound" {
		t.Errorf("expected network mode 'outbound', got %q", inline.Network.Mode)
	}
}

func TestResolve_ProjectOverrideSandbox_ProfileExpanded(t *testing.T) {
	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"work": {
				Agent:   "claude",
				Sandbox: &config.SandboxRef{ProfileName: "strict"},
			},
		},
		Sandboxes: map[string]config.SandboxPolicy{
			"strict": {
				Writable: []string{"/tmp"},
				Readable: []string{"/usr"},
			},
		},
		DefaultContext: "work",
		ProjectOverride: &config.ProjectOverride{
			Sandbox: &config.SandboxPolicy{
				ReadableExtra: []string{"/opt/data"},
			},
		},
	}
	rc, err := Resolve(cfg, "/tmp/somedir", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	inline := rc.Context.Sandbox.Inline
	if inline == nil {
		t.Fatal("expected inline sandbox after profile expansion")
	}
	if len(inline.Readable) != 1 || inline.Readable[0] != "/usr" {
		t.Errorf("expected base readable from profile, got %v", inline.Readable)
	}
	if len(inline.ReadableExtra) != 1 || inline.ReadableExtra[0] != "/opt/data" {
		t.Errorf("expected readable_extra merged, got %v", inline.ReadableExtra)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/context/ -run "TestResolve_ProjectOverrideSandbox" -v`
Expected: FAIL — wholesale replace loses additive fields

- [ ] **Step 3: Implement field-level sandbox merge**

Replace the sandbox block in `applyProjectOverride` (line 129-131 of resolver.go). The function needs access to `Config.Sandboxes` for profile expansion, so change its signature:

```go
func applyProjectOverride(rc *ResolvedContext, po *config.ProjectOverride, sandboxes map[string]config.SandboxPolicy)
```

Update both call sites in `Resolve` (lines 62 and 110) to pass `cfg.Sandboxes`.

Then implement:

```go
if po.Sandbox != nil {
	// Ensure we have an inline policy to merge into.
	// If context uses a profile reference, expand it first.
	if rc.Context.Sandbox == nil {
		rc.Context.Sandbox = &config.SandboxRef{Inline: &config.SandboxPolicy{}}
	}
	if rc.Context.Sandbox.ProfileName != "" && sandboxes != nil {
		if profile, ok := sandboxes[rc.Context.Sandbox.ProfileName]; ok {
			profileCopy := profile
			rc.Context.Sandbox = &config.SandboxRef{Inline: &profileCopy}
		}
	}
	if rc.Context.Sandbox.Inline == nil {
		rc.Context.Sandbox.Inline = &config.SandboxPolicy{}
	}
	inline := rc.Context.Sandbox.Inline

	// Additive fields (append + dedup)
	inline.DeniedExtra = dedupStrings(append(inline.DeniedExtra, po.Sandbox.DeniedExtra...))
	inline.ReadableExtra = dedupStrings(append(inline.ReadableExtra, po.Sandbox.ReadableExtra...))
	inline.WritableExtra = dedupStrings(append(inline.WritableExtra, po.Sandbox.WritableExtra...))
	inline.GuardsExtra = dedupStrings(append(inline.GuardsExtra, po.Sandbox.GuardsExtra...))
	inline.Unguard = dedupStrings(append(inline.Unguard, po.Sandbox.Unguard...))

	// Replace-if-set fields
	if len(po.Sandbox.Writable) > 0 {
		inline.Writable = po.Sandbox.Writable
	}
	if len(po.Sandbox.Readable) > 0 {
		inline.Readable = po.Sandbox.Readable
	}
	if len(po.Sandbox.Denied) > 0 {
		inline.Denied = po.Sandbox.Denied
	}
	if len(po.Sandbox.Guards) > 0 {
		inline.Guards = po.Sandbox.Guards
	}
	if po.Sandbox.Network != nil {
		inline.Network = po.Sandbox.Network
	}
	if po.Sandbox.AllowSubprocess != nil {
		inline.AllowSubprocess = po.Sandbox.AllowSubprocess
	}
	if po.Sandbox.CleanEnv != nil {
		inline.CleanEnv = po.Sandbox.CleanEnv
	}
}
```

- [ ] **Step 4: Run all resolver tests**

Run: `go test ./internal/context/ -v`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```
git add internal/context/resolver.go internal/context/resolver_test.go
/commit Change sandbox merge to field-level additive and replace
```

---

### Task 4: Add WriteProjectOverride and findProjectConfigForWrite

**Files:**
- Create: `internal/config/project_writer.go`
- Test: `internal/config/project_writer_test.go`

- [ ] **Step 1: Write failing tests**

```go
package config

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestWriteProjectOverride_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ProjectConfigFileName)

	po := &ProjectOverride{
		Capabilities: []string{"k8s"},
		Env:          map[string]string{"FOO": "bar"},
	}
	if err := WriteProjectOverride(path, po); err != nil {
		t.Fatalf("WriteProjectOverride() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var loaded ProjectOverride
	if err := yaml.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if len(loaded.Capabilities) != 1 || loaded.Capabilities[0] != "k8s" {
		t.Errorf("expected capabilities [k8s], got %v", loaded.Capabilities)
	}
	if loaded.Env["FOO"] != "bar" {
		t.Errorf("expected env FOO=bar, got %v", loaded.Env)
	}
}

func TestWriteProjectOverride_Atomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ProjectConfigFileName)

	// Write initial
	po1 := &ProjectOverride{Capabilities: []string{"docker"}}
	if err := WriteProjectOverride(path, po1); err != nil {
		t.Fatalf("initial write error = %v", err)
	}

	// Write updated
	po2 := &ProjectOverride{Capabilities: []string{"k8s", "aws"}}
	if err := WriteProjectOverride(path, po2); err != nil {
		t.Fatalf("update write error = %v", err)
	}

	// Verify updated content
	loaded, err := loadProjectOverride(path)
	if err != nil {
		t.Fatalf("loadProjectOverride() error = %v", err)
	}
	if len(loaded.Capabilities) != 2 {
		t.Errorf("expected 2 capabilities after update, got %v", loaded.Capabilities)
	}
}

func TestFindProjectConfigForWrite_ExistingFile(t *testing.T) {
	dir := t.TempDir()
	aidePath := filepath.Join(dir, ProjectConfigFileName)
	if err := os.WriteFile(aidePath, []byte("agent: claude\n"), 0644); err != nil {
		t.Fatal(err)
	}
	subdir := filepath.Join(dir, "sub")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatal(err)
	}

	got := FindProjectConfigForWrite(subdir)
	if got != aidePath {
		t.Errorf("expected %q, got %q", aidePath, got)
	}
}

func TestFindProjectConfigForWrite_GitRoot(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatal(err)
	}
	subdir := filepath.Join(dir, "src", "pkg")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatal(err)
	}

	got := FindProjectConfigForWrite(subdir)
	expected := filepath.Join(dir, ProjectConfigFileName)
	if got != expected {
		t.Errorf("expected %q (git root), got %q", expected, got)
	}
}

func TestFindProjectConfigForWrite_FallbackToCwd(t *testing.T) {
	dir := t.TempDir()
	// No .git, no .aide.yaml
	got := FindProjectConfigForWrite(dir)
	expected := filepath.Join(dir, ProjectConfigFileName)
	if got != expected {
		t.Errorf("expected %q (cwd fallback), got %q", expected, got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -run "TestWriteProjectOverride|TestFindProjectConfigForWrite" -v`
Expected: FAIL — functions don't exist

- [ ] **Step 3: Implement WriteProjectOverride and FindProjectConfigForWrite**

Create `internal/config/project_writer.go`:

```go
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// WriteProjectOverride writes the given ProjectOverride to the given path atomically.
func WriteProjectOverride(path string, po *ProjectOverride) error {
	data, err := yaml.Marshal(po)
	if err != nil {
		return fmt.Errorf("marshaling project override: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("writing temp file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("renaming temp file: %w", err)
	}
	return nil
}

// FindProjectConfigForWrite walks up from startDir to find where to write .aide.yaml.
// 1. If .aide.yaml exists in any ancestor (up to git root), return its path.
// 2. If a .git directory is found (no .aide.yaml), return .aide.yaml in that dir.
// 3. If neither found, return .aide.yaml in startDir.
func FindProjectConfigForWrite(startDir string) string {
	dir := startDir
	for {
		candidate := filepath.Join(dir, ProjectConfigFileName)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}

		gitDir := filepath.Join(dir, ".git")
		if _, err := os.Stat(gitDir); err == nil {
			return filepath.Join(dir, ProjectConfigFileName)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return filepath.Join(startDir, ProjectConfigFileName)
		}
		dir = parent
	}
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/config/ -run "TestWriteProjectOverride|TestFindProjectConfigForWrite" -v`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```
git add internal/config/project_writer.go internal/config/project_writer_test.go
/commit Add WriteProjectOverride and FindProjectConfigForWrite
```

---

### Task 5: Add resolveProjectOverrideForMutation helper

**Files:**
- Modify: `cmd/aide/commands.go:3280`

- [ ] **Step 1: Implement the function**

Add after `resolveContextForMutation` (line 3302 of `cmd/aide/commands.go`):

```go
// resolveProjectOverrideForMutation loads the global config and project override
// for mutation. Returns the global config (for validation), the project override
// (empty if .aide.yaml doesn't exist), and the path to write .aide.yaml to.
func resolveProjectOverrideForMutation() (*config.Config, *config.ProjectOverride, string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, nil, "", fmt.Errorf("getting working directory: %w", err)
	}
	cfg, err := config.Load(config.Dir(), cwd)
	if err != nil {
		return nil, nil, "", fmt.Errorf("loading config: %w", err)
	}
	poPath := config.FindProjectConfigForWrite(cwd)
	po := cfg.ProjectOverride
	if po == nil {
		po = &config.ProjectOverride{}
	}
	return cfg, po, poPath, nil
}
```

- [ ] **Step 2: Build to verify compilation**

Run: `go build ./cmd/aide/`
Expected: Success

- [ ] **Step 3: Commit**

```
git add cmd/aide/commands.go
/commit Add resolveProjectOverrideForMutation helper
```

---

### Task 6: Update cap enable/disable to use --global flag routing

**Files:**
- Modify: `cmd/aide/commands.go` (capEnableCmd ~line 4100, capDisableCmd ~line 4155)

- [ ] **Step 1: Update capEnableCmd**

Replace the current `capEnableCmd` function. Key changes:
- Add `--global` flag
- Add `--context` flag validation (error if used without `--global`)
- Route to project or global path based on flag

```go
func capEnableCmd() *cobra.Command {
	var contextName string
	var global bool
	cmd := &cobra.Command{
		Use:               "enable <capability>[,capability...]",
		Short:             "Enable capabilities (project-level by default)",
		Args:              cobra.ExactArgs(1),
		SilenceUsage:      true,
		ValidArgsFunction: capabilityCompletionFunc,
		RunE: func(cmd *cobra.Command, args []string) error {
			capNames := splitCommaList(args[0])

			if !global && contextName != "" {
				return fmt.Errorf("the --context flag requires --global")
			}

			if global {
				// Existing path: write to global config
				cfg, ctxName, ctx, err := resolveContextForMutation(contextName)
				if err != nil {
					return err
				}
				userCaps := capability.FromConfigDefs(cfg.Capabilities)
				registry := capability.MergedRegistry(userCaps)
				for _, capName := range capNames {
					if _, ok := registry[capName]; !ok {
						return fmt.Errorf("unknown capability: %q", capName)
					}
				}
				for _, capName := range capNames {
					already := false
					for _, c := range ctx.Capabilities {
						if c == capName {
							already = true
							break
						}
					}
					if already {
						fmt.Fprintf(cmd.OutOrStdout(), "Capability %q is already enabled for context %q\n", capName, ctxName)
						continue
					}
					ctx.Capabilities = append(ctx.Capabilities, capName)
					fmt.Fprintf(cmd.OutOrStdout(), "Capability %q enabled for context %q (global)\n", capName, ctxName)
				}
				cfg.Contexts[ctxName] = ctx
				return config.WriteConfig(cfg)
			}

			// Project path: write to .aide.yaml
			cfg, po, poPath, err := resolveProjectOverrideForMutation()
			if err != nil {
				return err
			}
			userCaps := capability.FromConfigDefs(cfg.Capabilities)
			registry := capability.MergedRegistry(userCaps)
			for _, capName := range capNames {
				if _, ok := registry[capName]; !ok {
					return fmt.Errorf("unknown capability: %q", capName)
				}
			}
			for _, capName := range capNames {
				// Remove from disabled if present
				po.DisabledCapabilities = removeFromSlice(po.DisabledCapabilities, capName)
				// Add if not already present
				already := false
				for _, c := range po.Capabilities {
					if c == capName {
						already = true
						break
					}
				}
				if already {
					fmt.Fprintf(cmd.OutOrStdout(), "Capability %q is already enabled in project\n", capName)
					continue
				}
				po.Capabilities = append(po.Capabilities, capName)
				fmt.Fprintf(cmd.OutOrStdout(), "Capability %q enabled in project (%s)\n", capName, poPath)
			}
			return config.WriteProjectOverride(poPath, po)
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "Apply to user-level config instead of project")
	cmd.Flags().StringVar(&contextName, "context", "", "Target context name (requires --global)")
	return cmd
}
```

- [ ] **Step 2: Update capDisableCmd**

Same pattern. Key difference for project path: if capability is not in `po.Capabilities`, add to `po.DisabledCapabilities` (to negate global context caps):

```go
func capDisableCmd() *cobra.Command {
	var contextName string
	var global bool
	cmd := &cobra.Command{
		Use:               "disable <capability>[,capability...]",
		Short:             "Disable capabilities (project-level by default)",
		Args:              cobra.ExactArgs(1),
		SilenceUsage:      true,
		ValidArgsFunction: capabilityCompletionFunc,
		RunE: func(cmd *cobra.Command, args []string) error {
			capNames := splitCommaList(args[0])

			if !global && contextName != "" {
				return fmt.Errorf("the --context flag requires --global")
			}

			if global {
				cfg, ctxName, ctx, err := resolveContextForMutation(contextName)
				if err != nil {
					return err
				}
				for _, capName := range capNames {
					found := false
					for _, c := range ctx.Capabilities {
						if c == capName {
							found = true
							break
						}
					}
					if !found {
						fmt.Fprintf(cmd.ErrOrStderr(), "warning: capability %q is not enabled for context %q\n", capName, ctxName)
						continue
					}
					ctx.Capabilities = removeFromSlice(ctx.Capabilities, capName)
					fmt.Fprintf(cmd.OutOrStdout(), "Capability %q disabled for context %q (global)\n", capName, ctxName)
				}
				cfg.Contexts[ctxName] = ctx
				return config.WriteConfig(cfg)
			}

			// Project path
			cfg, po, poPath, err := resolveProjectOverrideForMutation()
			if err != nil {
				return err
			}
			_ = cfg // loaded for context, not needed for disable validation
			for _, capName := range capNames {
				removed := false
				for _, c := range po.Capabilities {
					if c == capName {
						removed = true
						break
					}
				}
				if removed {
					po.Capabilities = removeFromSlice(po.Capabilities, capName)
					fmt.Fprintf(cmd.OutOrStdout(), "Capability %q removed from project (%s)\n", capName, poPath)
				} else {
					// Not in project caps — add to disabled to negate global
					already := false
					for _, c := range po.DisabledCapabilities {
						if c == capName {
							already = true
							break
						}
					}
					if already {
						fmt.Fprintf(cmd.OutOrStdout(), "Capability %q is already disabled in project\n", capName)
						continue
					}
					po.DisabledCapabilities = append(po.DisabledCapabilities, capName)
					fmt.Fprintf(cmd.OutOrStdout(), "Capability %q disabled in project (%s)\n", capName, poPath)
				}
			}
			return config.WriteProjectOverride(poPath, po)
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "Apply to user-level config instead of project")
	cmd.Flags().StringVar(&contextName, "context", "", "Target context name (requires --global)")
	return cmd
}
```

- [ ] **Step 3: Update capCheckCmd**

`cap check` is read-only — no `--global` flag needed. Already updated to use `splitCommaList` in previous commit.

- [ ] **Step 4: Build and verify**

Run: `go build ./cmd/aide/`
Expected: Success

- [ ] **Step 5: Commit**

```
git add cmd/aide/commands.go
/commit Route cap enable/disable to project-level by default
```

---

### Task 7: Update sandbox mutation commands with --global flag

**Files:**
- Modify: `cmd/aide/commands.go:3319` (sandboxDenyCmd), `cmd/aide/commands.go:3346` (sandboxAllowCmd), `cmd/aide/commands.go:3381` (sandboxResetCmd), `cmd/aide/commands.go:2729` (sandboxNetworkCmd), `cmd/aide/commands.go:3405` (sandboxPortsCmd), `cmd/aide/commands.go:3504` (sandboxGuardCmd), `cmd/aide/commands.go:3544` (sandboxUnguardCmd)

All 7 commands follow the same pattern:
- Add `var global bool` alongside existing `var contextName string`
- Add `--context` requires `--global` validation
- Wrap existing logic in `if global { ... }` block (preserving `ensureInlineSandbox` for global path)
- Add `else { ... }` block using `resolveProjectOverrideForMutation()` + `ensureProjectSandbox()` + `WriteProjectOverride()`
- Add both flags at the end

- [ ] **Step 1: Create helper for project sandbox mutation**

Add after `ensureInlineSandbox` (line 3317 of `cmd/aide/commands.go`):

```go
// ensureProjectSandbox ensures the project override has a non-nil SandboxPolicy.
func ensureProjectSandbox(po *config.ProjectOverride) *config.SandboxPolicy {
	if po.Sandbox == nil {
		po.Sandbox = &config.SandboxPolicy{}
	}
	return po.Sandbox
}
```

- [ ] **Step 2: Update sandboxDenyCmd (line 3319)**

Replace the function:

```go
func sandboxDenyCmd() *cobra.Command {
	var contextName string
	var global bool
	cmd := &cobra.Command{
		Use:          "deny <path>",
		Short:        "Add a path to the denied_extra list (project-level by default)",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			if !global && contextName != "" {
				return fmt.Errorf("the --context flag requires --global")
			}
			if global {
				cfg, ctxName, ctx, err := resolveContextForMutation(contextName)
				if err != nil {
					return err
				}
				sp := ensureInlineSandbox(&ctx)
				sp.DeniedExtra = append(sp.DeniedExtra, path)
				cfg.Contexts[ctxName] = ctx
				if err := config.WriteConfig(cfg); err != nil {
					return fmt.Errorf("writing config: %w", err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Added %s to denied_extra for context %q (global)\n", path, ctxName)
				return nil
			}
			_, po, poPath, err := resolveProjectOverrideForMutation()
			if err != nil {
				return err
			}
			sp := ensureProjectSandbox(po)
			sp.DeniedExtra = append(sp.DeniedExtra, path)
			if err := config.WriteProjectOverride(poPath, po); err != nil {
				return fmt.Errorf("writing project config: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Added %s to denied_extra in project (%s)\n", path, poPath)
			return nil
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "Apply to user-level config instead of project")
	cmd.Flags().StringVar(&contextName, "context", "", "Target context name (requires --global)")
	return cmd
}
```

- [ ] **Step 3: Update sandboxAllowCmd (line 3346)**

Replace the function:

```go
func sandboxAllowCmd() *cobra.Command {
	var contextName string
	var write bool
	var global bool
	cmd := &cobra.Command{
		Use:          "allow <path>",
		Short:        "Add a path to readable_extra or writable_extra (project-level by default)",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			if !global && contextName != "" {
				return fmt.Errorf("the --context flag requires --global")
			}
			listName := "readable_extra"
			if write {
				listName = "writable_extra"
			}
			if global {
				cfg, ctxName, ctx, err := resolveContextForMutation(contextName)
				if err != nil {
					return err
				}
				sp := ensureInlineSandbox(&ctx)
				if write {
					sp.WritableExtra = append(sp.WritableExtra, path)
				} else {
					sp.ReadableExtra = append(sp.ReadableExtra, path)
				}
				cfg.Contexts[ctxName] = ctx
				if err := config.WriteConfig(cfg); err != nil {
					return fmt.Errorf("writing config: %w", err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Added %s to %s for context %q (global)\n", path, listName, ctxName)
				return nil
			}
			_, po, poPath, err := resolveProjectOverrideForMutation()
			if err != nil {
				return err
			}
			sp := ensureProjectSandbox(po)
			if write {
				sp.WritableExtra = append(sp.WritableExtra, path)
			} else {
				sp.ReadableExtra = append(sp.ReadableExtra, path)
			}
			if err := config.WriteProjectOverride(poPath, po); err != nil {
				return fmt.Errorf("writing project config: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Added %s to %s in project (%s)\n", path, listName, poPath)
			return nil
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "Apply to user-level config instead of project")
	cmd.Flags().StringVar(&contextName, "context", "", "Target context name (requires --global)")
	cmd.Flags().BoolVar(&write, "write", false, "add to writable_extra instead of readable_extra")
	return cmd
}
```

- [ ] **Step 4: Update sandboxResetCmd (line 3381)**

Replace the function:

```go
func sandboxResetCmd() *cobra.Command {
	var contextName string
	var global bool
	cmd := &cobra.Command{
		Use:          "reset",
		Short:        "Reset sandbox to defaults (project-level by default)",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !global && contextName != "" {
				return fmt.Errorf("the --context flag requires --global")
			}
			if global {
				cfg, ctxName, ctx, err := resolveContextForMutation(contextName)
				if err != nil {
					return err
				}
				ctx.Sandbox = nil
				cfg.Contexts[ctxName] = ctx
				if err := config.WriteConfig(cfg); err != nil {
					return fmt.Errorf("writing config: %w", err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Reset sandbox to defaults for context %q (global)\n", ctxName)
				return nil
			}
			_, po, poPath, err := resolveProjectOverrideForMutation()
			if err != nil {
				return err
			}
			po.Sandbox = nil
			if err := config.WriteProjectOverride(poPath, po); err != nil {
				return fmt.Errorf("writing project config: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Reset sandbox overrides in project (%s)\n", poPath)
			return nil
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "Apply to user-level config instead of project")
	cmd.Flags().StringVar(&contextName, "context", "", "Target context name (requires --global)")
	return cmd
}
```

- [ ] **Step 5: Update sandboxNetworkCmd (line 2729)**

Replace the function:

```go
func sandboxNetworkCmd() *cobra.Command {
	var contextName string
	var global bool
	cmd := &cobra.Command{
		Use:          "network <mode>",
		Short:        "Set network mode for sandbox (project-level by default)",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := args[0]
			validModes := map[string]bool{"outbound": true, "none": true, "unrestricted": true}
			if !validModes[mode] {
				return fmt.Errorf("invalid network mode %q (must be outbound, none, or unrestricted)", mode)
			}
			if !global && contextName != "" {
				return fmt.Errorf("the --context flag requires --global")
			}
			if global {
				cfg, ctxName, ctx, err := resolveContextForMutation(contextName)
				if err != nil {
					return err
				}
				sp := ensureInlineSandbox(&ctx)
				sp.Network = &config.NetworkPolicy{Mode: mode}
				cfg.Contexts[ctxName] = ctx
				if err := config.WriteConfig(cfg); err != nil {
					return fmt.Errorf("writing config: %w", err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Set network mode to %q for context %q (global)\n", mode, ctxName)
				return nil
			}
			_, po, poPath, err := resolveProjectOverrideForMutation()
			if err != nil {
				return err
			}
			sp := ensureProjectSandbox(po)
			sp.Network = &config.NetworkPolicy{Mode: mode}
			if err := config.WriteProjectOverride(poPath, po); err != nil {
				return fmt.Errorf("writing project config: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Set network mode to %q in project (%s)\n", mode, poPath)
			return nil
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "Apply to user-level config instead of project")
	cmd.Flags().StringVar(&contextName, "context", "", "Target context name (requires --global)")
	return cmd
}
```

- [ ] **Step 6: Update sandboxPortsCmd (line 3405)**

Replace the function. Note: project path preserves `Network.Mode` by initializing `Network` if nil:

```go
func sandboxPortsCmd() *cobra.Command {
	var contextName string
	var global bool
	cmd := &cobra.Command{
		Use:          "ports <port1> [port2] ...",
		Short:        "Set allowed network ports for sandbox (project-level by default)",
		Args:         cobra.MinimumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			var ports []int
			for _, arg := range args {
				p, err := strconv.Atoi(arg)
				if err != nil {
					return fmt.Errorf("invalid port %q: %w", arg, err)
				}
				if p < 1 || p > 65535 {
					return fmt.Errorf("port %d out of range (must be 1-65535)", p)
				}
				ports = append(ports, p)
			}
			if !global && contextName != "" {
				return fmt.Errorf("the --context flag requires --global")
			}
			if global {
				cfg, ctxName, ctx, err := resolveContextForMutation(contextName)
				if err != nil {
					return err
				}
				sp := ensureInlineSandbox(&ctx)
				sp.Network = &config.NetworkPolicy{Mode: "outbound", AllowPorts: ports}
				cfg.Contexts[ctxName] = ctx
				if err := config.WriteConfig(cfg); err != nil {
					return fmt.Errorf("writing config: %w", err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Set allowed ports to %v for context %q (global)\n", ports, ctxName)
				return nil
			}
			_, po, poPath, err := resolveProjectOverrideForMutation()
			if err != nil {
				return err
			}
			sp := ensureProjectSandbox(po)
			if sp.Network == nil {
				sp.Network = &config.NetworkPolicy{Mode: "outbound"}
			}
			sp.Network.AllowPorts = ports
			if err := config.WriteProjectOverride(poPath, po); err != nil {
				return fmt.Errorf("writing project config: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Set allowed ports to %v in project (%s)\n", ports, poPath)
			return nil
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "Apply to user-level config instead of project")
	cmd.Flags().StringVar(&contextName, "context", "", "Target context name (requires --global)")
	return cmd
}
```

- [ ] **Step 7: Update sandboxGuardCmd (line 3504)**

Replace the function. Note: for project path, we don't have the named-profile check (project override is always an inline SandboxPolicy) and we use `sandbox.EnableGuard` with the project sandbox:

```go
func sandboxGuardCmd() *cobra.Command {
	var contextName string
	var global bool
	cmd := &cobra.Command{
		Use:          "guard <name>",
		Short:        "Enable an additional guard (project-level by default)",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if !global && contextName != "" {
				return fmt.Errorf("the --context flag requires --global")
			}
			if global {
				cfg, ctxName, ctx, err := resolveContextForMutation(contextName)
				if err != nil {
					return err
				}
				if ctx.Sandbox != nil && !ctx.Sandbox.Disabled && ctx.Sandbox.Inline == nil && ctx.Sandbox.ProfileName != "" {
					return fmt.Errorf("context %q uses a named sandbox profile %q; modify the profile directly", ctxName, ctx.Sandbox.ProfileName)
				}
				sp := ensureInlineSandbox(&ctx)
				r := sandbox.EnableGuard(sp, name)
				for _, w := range r.Warnings {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", w)
				}
				if !r.OK() {
					return fmt.Errorf("%s", r.Errors[0])
				}
				cfg.Contexts[ctxName] = ctx
				if err := config.WriteConfig(cfg); err != nil {
					return fmt.Errorf("writing config: %w", err)
				}
				if len(r.Warnings) == 0 {
					fmt.Fprintf(cmd.OutOrStdout(), "Guard %q enabled for context %q (global)\n", name, ctxName)
				}
				return nil
			}
			_, po, poPath, err := resolveProjectOverrideForMutation()
			if err != nil {
				return err
			}
			sp := ensureProjectSandbox(po)
			r := sandbox.EnableGuard(sp, name)
			for _, w := range r.Warnings {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", w)
			}
			if !r.OK() {
				return fmt.Errorf("%s", r.Errors[0])
			}
			if err := config.WriteProjectOverride(poPath, po); err != nil {
				return fmt.Errorf("writing project config: %w", err)
			}
			if len(r.Warnings) == 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "Guard %q enabled in project (%s)\n", name, poPath)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "Apply to user-level config instead of project")
	cmd.Flags().StringVar(&contextName, "context", "", "Target context name (requires --global)")
	return cmd
}
```

- [ ] **Step 8: Update sandboxUnguardCmd (line 3544)**

Replace the function. Same pattern as guard:

```go
func sandboxUnguardCmd() *cobra.Command {
	var contextName string
	var global bool
	cmd := &cobra.Command{
		Use:          "unguard <name>",
		Short:        "Disable a guard (project-level by default)",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if !global && contextName != "" {
				return fmt.Errorf("the --context flag requires --global")
			}
			if global {
				cfg, ctxName, ctx, err := resolveContextForMutation(contextName)
				if err != nil {
					return err
				}
				if ctx.Sandbox != nil && !ctx.Sandbox.Disabled && ctx.Sandbox.Inline == nil && ctx.Sandbox.ProfileName != "" {
					return fmt.Errorf("context %q uses a named sandbox profile %q; modify the profile directly", ctxName, ctx.Sandbox.ProfileName)
				}
				sp := ensureInlineSandbox(&ctx)
				r := sandbox.DisableGuard(sp, name)
				for _, w := range r.Warnings {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", w)
				}
				if !r.OK() {
					return fmt.Errorf("%s", r.Errors[0])
				}
				cfg.Contexts[ctxName] = ctx
				if err := config.WriteConfig(cfg); err != nil {
					return fmt.Errorf("writing config: %w", err)
				}
				if len(r.Warnings) == 0 {
					fmt.Fprintf(cmd.OutOrStdout(), "Guard %q disabled for context %q (global)\n", name, ctxName)
				}
				return nil
			}
			_, po, poPath, err := resolveProjectOverrideForMutation()
			if err != nil {
				return err
			}
			sp := ensureProjectSandbox(po)
			r := sandbox.DisableGuard(sp, name)
			for _, w := range r.Warnings {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", w)
			}
			if !r.OK() {
				return fmt.Errorf("%s", r.Errors[0])
			}
			if err := config.WriteProjectOverride(poPath, po); err != nil {
				return fmt.Errorf("writing project config: %w", err)
			}
			if len(r.Warnings) == 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "Guard %q disabled in project (%s)\n", name, poPath)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "Apply to user-level config instead of project")
	cmd.Flags().StringVar(&contextName, "context", "", "Target context name (requires --global)")
	return cmd
}
```

- [ ] **Step 9: Build and verify**

Run: `go build ./cmd/aide/`
Expected: Success

- [ ] **Step 10: Commit**

```
git add cmd/aide/commands.go
/commit Route sandbox mutation commands to project-level by default
```

---

### Task 8: Update env set/remove with --global flag

**Files:**
- Modify: `cmd/aide/commands.go:1694` (envSetCmd), `cmd/aide/commands.go:2046` (envRemoveCmd)

Note: `envSetCmd` has complex `--from-secret` logic. The project path only applies to the simple `KEY VALUE` form; `--from-secret` is inherently context-scoped (secrets reference contexts) and should require `--global`.

- [ ] **Step 1: Update envSetCmd (line 1694)**

Add `var global bool` flag. Wrap the existing context-resolution + write logic in `if global { ... }`. Add project path for the simple `KEY VALUE` case:

```go
func envSetCmd() *cobra.Command {
	var fromSecret string
	var contextName string
	var global bool

	cmd := &cobra.Command{
		Use:   "set KEY [VALUE]",
		Short: "Set an environment variable (project-level by default)",
		Long: `Set an environment variable on the project or context.

Examples:
  aide env set ANTHROPIC_API_KEY sk-ant-xxx              # project-level
  aide env set ANTHROPIC_API_KEY --global                # context-level
  aide env set ANTHROPIC_API_KEY --from-secret api_key   # requires --global
  aide env set ANTHROPIC_API_KEY --from-secret --global  # interactive picker`,
		Args:         cobra.RangeArgs(1, 2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			hasValueArg := len(args) == 2
			isFromSecret := cmd.Flags().Changed("from-secret")

			if !global && contextName != "" {
				return fmt.Errorf("the --context flag requires --global")
			}
			if isFromSecret && !global {
				return fmt.Errorf("--from-secret requires --global (secrets are context-scoped)")
			}

			if hasValueArg && isFromSecret {
				return fmt.Errorf("cannot specify both a value argument and --from-secret")
			}
			if !hasValueArg && !isFromSecret {
				return fmt.Errorf("must specify either a value argument or --from-secret")
			}

			if global {
				// Existing context-level logic (preserving full --from-secret flow)
				out := cmd.OutOrStdout()
				reader := bufio.NewReader(os.Stdin)
				isInteractive := isFromSecret && strings.TrimSpace(fromSecret) == ""

				cwd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("getting working directory: %w", err)
				}
				cfg, err := config.Load(config.Dir(), cwd)
				if err != nil {
					return fmt.Errorf("loading config: %w", err)
				}

				var targetName string
				if contextName != "" {
					targetName = contextName
					if _, ok := cfg.Contexts[targetName]; !ok {
						return fmt.Errorf("context %q not found", targetName)
					}
				} else {
					remoteURL := aidectx.DetectRemote(cwd, "origin")
					resolved, err := aidectx.Resolve(cfg, cwd, remoteURL)
					if err != nil {
						return err
					}
					targetName = resolved.Name
				}

				ctx := cfg.Contexts[targetName]

				var value string
				if isFromSecret {
					// Preserve existing --from-secret logic
					_ = isInteractive
					_ = reader
					_ = out
					// ... (existing logic preserved verbatim from current envSetCmd)
					return fmt.Errorf("--from-secret flow: copy existing logic from current envSetCmd lines 1752-2038")
				}
				value = args[1]

				if ctx.Env == nil {
					ctx.Env = make(map[string]string)
				}
				ctx.Env[key] = value
				cfg.Contexts[targetName] = ctx
				if err := config.WriteConfig(cfg); err != nil {
					return fmt.Errorf("writing config: %w", err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Set %s on context %q (global)\n", key, targetName)
				return nil
			}

			// Project path: simple KEY VALUE
			value := args[1]
			_, po, poPath, err := resolveProjectOverrideForMutation()
			if err != nil {
				return err
			}
			if po.Env == nil {
				po.Env = make(map[string]string)
			}
			po.Env[key] = value
			if err := config.WriteProjectOverride(poPath, po); err != nil {
				return fmt.Errorf("writing project config: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Set %s in project (%s)\n", key, poPath)
			return nil
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "Apply to user-level config instead of project")
	cmd.Flags().StringVar(&contextName, "context", "", "Target context (requires --global)")
	cmd.Flags().StringVar(&fromSecret, "from-secret", "", "Wire value from secret (requires --global)")
	return cmd
}
```

**IMPORTANT:** The `--from-secret` flow (lines 1752-2038 in the current code) is substantial and must be preserved verbatim in the `if global { ... }` branch. The placeholder `return fmt.Errorf(...)` above must be replaced with the actual existing logic. Copy it wholesale — do not modify it.

- [ ] **Step 2: Update envRemoveCmd (line 2046)**

Replace the function:

```go
func envRemoveCmd() *cobra.Command {
	var contextName string
	var global bool

	cmd := &cobra.Command{
		Use:          "remove KEY",
		Short:        "Remove an environment variable (project-level by default)",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			if !global && contextName != "" {
				return fmt.Errorf("the --context flag requires --global")
			}
			if global {
				cfg, ctxName, ctx, err := resolveContextForMutation(contextName)
				if err != nil {
					return err
				}
				if ctx.Env == nil || ctx.Env[key] == "" {
					return fmt.Errorf("env var %q not found on context %q", key, ctxName)
				}
				delete(ctx.Env, key)
				cfg.Contexts[ctxName] = ctx
				if err := config.WriteConfig(cfg); err != nil {
					return fmt.Errorf("writing config: %w", err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Removed %s from context %q (global)\n", key, ctxName)
				return nil
			}
			_, po, poPath, err := resolveProjectOverrideForMutation()
			if err != nil {
				return err
			}
			if po.Env == nil || po.Env[key] == "" {
				return fmt.Errorf("env var %q not found in project config", key)
			}
			delete(po.Env, key)
			if err := config.WriteProjectOverride(poPath, po); err != nil {
				return fmt.Errorf("writing project config: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed %s from project (%s)\n", key, poPath)
			return nil
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "Apply to user-level config instead of project")
	cmd.Flags().StringVar(&contextName, "context", "", "Target context (requires --global)")
	return cmd
}
```

- [ ] **Step 3: Build and verify**

Run: `go build ./cmd/aide/`
Expected: Success

- [ ] **Step 4: Commit**

```
git add cmd/aide/commands.go
/commit Route env set/remove to project-level by default
```

---

### Task 9: Full integration verification

**Files:** None (verification only)

- [ ] **Step 1: Run all tests**

Run: `go test ./...`
Expected: ALL PASS

- [ ] **Step 2: Build binary**

Run: `go build -o /tmp/aide-test ./cmd/aide/`
Expected: Success

- [ ] **Step 3: Manual smoke test**

In a temp directory with a git repo:
```bash
cd /tmp && mkdir test-project && cd test-project && git init
/tmp/aide-test cap enable k8s,terraform
cat .aide.yaml  # should show capabilities: [k8s, terraform]
/tmp/aide-test cap disable k8s
cat .aide.yaml  # should show capabilities: [terraform]
/tmp/aide-test sandbox deny /etc/secrets
cat .aide.yaml  # should show sandbox.denied_extra: [/etc/secrets]
/tmp/aide-test env set FOO=bar
cat .aide.yaml  # should show env: {FOO: bar}
```

- [ ] **Step 4: Verify --global still works**

```bash
/tmp/aide-test cap enable --global docker --context work
# Should write to ~/.config/aide/config.yaml
```

- [ ] **Step 5: Final commit if any fixups needed**

```
/commit Fix integration issues from smoke testing
```
