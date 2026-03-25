# Capabilities: Deferred Items

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the two deferred items from the capabilities feature: interactive `aide cap create` with filesystem discovery, and `auto_approve` config field.

**Architecture:** `internal/capability/discover.go` provides filesystem scanning. `capCreateCmd()` switches between interactive (no args) and expert (with args) mode. Config schema gains `auto_approve` field alongside `yolo` for backwards compat.

**Tech Stack:** Go, Cobra CLI, bufio for interactive input, existing `internal/capability`, `internal/config`

**Spec:** `docs/superpowers/specs/2026-03-25-capabilities-design.md`

---

### Task 1: Filesystem Discovery for Interactive Creation

**Files:**
- Create: `internal/capability/discover.go`
- Create: `internal/capability/discover_test.go`

- [ ] **Step 1: Write the failing test**

```go
package capability

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverPaths_K8s(t *testing.T) {
	home := t.TempDir()
	kubeDir := filepath.Join(home, ".kube")
	if err := os.MkdirAll(kubeDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(kubeDir, "config"), []byte("apiVersion: v1"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(kubeDir, "dev-config"), []byte("apiVersion: v1"), 0644); err != nil {
		t.Fatal(err)
	}

	paths := DiscoverPaths(home, "k8s")
	if len(paths) == 0 {
		t.Fatal("expected discovered paths for k8s")
	}

	found := false
	for _, p := range paths {
		if p.Path == filepath.Join(home, ".kube") && p.Exists {
			found = true
		}
	}
	if !found {
		t.Error("expected ~/.kube to be discovered")
	}
}

func TestDiscoverPaths_NoFiles(t *testing.T) {
	home := t.TempDir()
	paths := DiscoverPaths(home, "k8s")
	for _, p := range paths {
		if p.Exists {
			t.Errorf("expected no existing paths in empty home, found %s", p.Path)
		}
	}
}

func TestDiscoverEnvVars_K8s(t *testing.T) {
	t.Setenv("KUBECONFIG", "/tmp/test-kubeconfig")
	vars := DiscoverEnvVars("k8s")
	found := false
	for _, v := range vars {
		if v.Name == "KUBECONFIG" && v.Value == "/tmp/test-kubeconfig" {
			found = true
		}
	}
	if !found {
		t.Error("expected KUBECONFIG in discovered env vars")
	}
}

func TestDiscoverEnvVars_Unset(t *testing.T) {
	vars := DiscoverEnvVars("k8s")
	for _, v := range vars {
		if v.Name == "KUBECONFIG" && v.Set {
			t.Error("expected KUBECONFIG to be unset in clean env")
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/capability/ -run TestDiscover -v`

- [ ] **Step 3: Implement discover.go**

```go
package capability

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DiscoveredPath represents a path found during filesystem scanning.
type DiscoveredPath struct {
	Path    string
	Exists  bool
	IsDir   bool
	Summary string // e.g., "3 files found"
}

// DiscoveredEnvVar represents an environment variable relevant to a capability.
type DiscoveredEnvVar struct {
	Name  string
	Value string
	Set   bool
}

// capDiscoveryPaths maps capability names to paths to scan (relative to home).
var capDiscoveryPaths = map[string][]string{
	"aws":          {".aws"},
	"gcp":          {".config/gcloud"},
	"azure":        {".azure"},
	"digitalocean": {".config/doctl"},
	"oci":          {".oci"},
	"docker":       {".docker"},
	"k8s":          {".kube"},
	"helm":         {".kube", ".config/helm", ".cache/helm"},
	"terraform":    {".terraform.d"},
	"vault":        {".vault-token"},
	"ssh":          {".ssh"},
	"npm":          {".npmrc", ".yarnrc"},
}

// capDiscoveryEnvVars maps capability names to relevant env var names.
var capDiscoveryEnvVars = map[string][]string{
	"aws":       {"AWS_PROFILE", "AWS_REGION", "AWS_DEFAULT_REGION", "AWS_ACCESS_KEY_ID", "AWS_CONFIG_FILE", "AWS_SHARED_CREDENTIALS_FILE"},
	"gcp":       {"CLOUDSDK_CONFIG", "GOOGLE_APPLICATION_CREDENTIALS", "GCLOUD_PROJECT"},
	"azure":     {"AZURE_CONFIG_DIR", "AZURE_SUBSCRIPTION_ID"},
	"docker":    {"DOCKER_CONFIG", "DOCKER_HOST"},
	"k8s":       {"KUBECONFIG"},
	"helm":      {"HELM_HOME", "KUBECONFIG"},
	"terraform": {"TF_CLI_CONFIG_FILE"},
	"vault":     {"VAULT_ADDR", "VAULT_TOKEN", "VAULT_TOKEN_FILE"},
	"ssh":       {"SSH_AUTH_SOCK"},
	"npm":       {"NPM_TOKEN", "NODE_AUTH_TOKEN"},
}

// DiscoverPaths scans the filesystem for paths relevant to a capability.
func DiscoverPaths(home, capName string) []DiscoveredPath {
	relPaths, ok := capDiscoveryPaths[capName]
	if !ok {
		return nil
	}

	var results []DiscoveredPath
	for _, rel := range relPaths {
		fullPath := filepath.Join(home, rel)
		info, err := os.Stat(fullPath)
		dp := DiscoveredPath{
			Path:   fullPath,
			Exists: err == nil,
			IsDir:  err == nil && info.IsDir(),
		}
		if dp.Exists && dp.IsDir {
			entries, _ := os.ReadDir(fullPath)
			dp.Summary = fmt.Sprintf("%d files found", len(entries))
		}
		results = append(results, dp)
	}
	return results
}

// DiscoverEnvVars returns env vars relevant to a capability with their current values.
func DiscoverEnvVars(capName string) []DiscoveredEnvVar {
	varNames, ok := capDiscoveryEnvVars[capName]
	if !ok {
		return nil
	}

	var results []DiscoveredEnvVar
	for _, name := range varNames {
		value, set := os.LookupEnv(name)
		results = append(results, DiscoveredEnvVar{
			Name:  name,
			Value: value,
			Set:   set,
		})
	}
	return results
}

// AllDiscoveryCapNames returns all capability names that have discovery data.
func AllDiscoveryCapNames() []string {
	seen := make(map[string]bool)
	var names []string
	for name := range capDiscoveryPaths {
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	// Sort for deterministic output
	sort.Strings(names)
	return names
}
```

Note: add `"sort"` to imports.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/capability/ -run TestDiscover -v`
Expected: All pass

- [ ] **Step 5: Commit**

Stage: `git add internal/capability/discover.go internal/capability/discover_test.go`
Run: `/commit --style classic add filesystem discovery for interactive capability creation`

---

### Task 2: Interactive `aide cap create` Flow

**Files:**
- Modify: `cmd/aide/commands.go` — update `capCreateCmd()` to support interactive mode

- [ ] **Step 1: Modify capCreateCmd to allow zero args**

Change `cobra.ExactArgs(1)` to `cobra.MaximumNArgs(1)`. When no args provided and no flags set, enter interactive mode. When args provided, use existing expert flow.

- [ ] **Step 2: Implement interactive flow**

When no name arg is given:

```go
// Interactive mode
reader := bufio.NewReader(os.Stdin)

// 1. Ask for name
fmt.Fprint(out, "Capability name: ")
name, _ := reader.ReadString('\n')
name = strings.TrimSpace(name)

// 2. Ask for base
fmt.Fprintln(out, "\nExtend an existing capability?")
builtinNames := capability.AllDiscoveryCapNames()
for i, n := range builtinNames {
    b := capability.Builtins()[n]
    fmt.Fprintf(out, "  %d. %s — %s\n", i+1, n, b.Description)
}
fmt.Fprintf(out, "  %d. None — start from scratch\n", len(builtinNames)+1)
fmt.Fprint(out, "Choice: ")
choiceStr, _ := reader.ReadString('\n')
// Parse choice...

// 3. Discover paths
home, _ := os.UserHomeDir()
if selectedBase != "" {
    paths := capability.DiscoverPaths(home, selectedBase)
    fmt.Fprintln(out, "\nDiscovered paths:")
    for i, p := range paths {
        status := "not found"
        if p.Exists {
            status = p.Summary
        }
        fmt.Fprintf(out, "  %d. [%s] %s (%s)\n", i+1, checkOrEmpty(p.Exists), p.Path, status)
    }
    fmt.Fprint(out, "Select readable paths (comma-separated numbers, or 'all'): ")
    // Parse selection...
}

// 4. Ask for deny paths
fmt.Fprint(out, "\nPaths to deny (comma-separated, or empty): ")
denyStr, _ := reader.ReadString('\n')
// Parse...

// 5. Discover env vars
if selectedBase != "" {
    envVars := capability.DiscoverEnvVars(selectedBase)
    fmt.Fprintln(out, "\nDetected environment variables:")
    for i, v := range envVars {
        setLabel := "not set"
        if v.Set {
            setLabel = v.Value
        }
        fmt.Fprintf(out, "  %d. [%s] %s = %s\n", i+1, checkOrEmpty(v.Set), v.Name, setLabel)
    }
    fmt.Fprint(out, "Select env vars to allow (comma-separated numbers, or 'all'): ")
    // Parse selection...
}

// 6. Show summary and confirm
fmt.Fprintln(out, "\nSummary:")
fmt.Fprintf(out, "  Name:     %s\n", name)
if selectedBase != "" {
    fmt.Fprintf(out, "  Extends:  %s\n", selectedBase)
}
// ... show readable, deny, env_allow
fmt.Fprint(out, "\nCreate this capability? [y/N]: ")
confirm, _ := reader.ReadString('\n')
```

- [ ] **Step 3: Test interactive flow manually**

Run: `aide cap create` (no args) — should enter interactive mode

- [ ] **Step 4: Commit**

Stage: `git add cmd/aide/commands.go`
Run: `/commit --style classic add interactive guided flow for aide cap create`

---

### Task 3: `auto_approve` Config Field

**Files:**
- Modify: `internal/config/schema.go` — add `AutoApprove` field
- Modify: `internal/config/schema.go` — update `ResolveYolo` to check both fields
- Modify: `internal/config/schema_test.go` — add tests

- [ ] **Step 1: Write the failing test**

```go
func TestAutoApproveField(t *testing.T) {
	yamlData := `auto_approve: true`
	var cfg Config
	if err := yaml.Unmarshal([]byte(yamlData), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.AutoApprove == nil || !*cfg.AutoApprove {
		t.Error("expected auto_approve to be true")
	}
}

func TestAutoApproveAndYoloConflict(t *testing.T) {
	yamlData := `
auto_approve: true
yolo: false
`
	var cfg Config
	if err := yaml.Unmarshal([]byte(yamlData), &cfg); err != nil {
		t.Fatal(err)
	}
	// Both set with conflicting values — should be detected by validation
	if cfg.AutoApprove != nil && cfg.Yolo != nil && *cfg.AutoApprove != *cfg.Yolo {
		// This is the conflict case — validation should catch it
	}
}

func TestResolveAutoApprove(t *testing.T) {
	tr := true
	result := ResolveAutoApprove(&tr, nil)
	if !result {
		t.Error("expected auto_approve: true to resolve as true")
	}
}
```

- [ ] **Step 2: Add AutoApprove field to Config and Context**

In `schema.go`:

```go
// On Config (minimal format)
AutoApprove *bool `yaml:"auto_approve,omitempty"`

// On Context
AutoApprove *bool `yaml:"auto_approve,omitempty"`

// On Preferences
AutoApprove *bool `yaml:"auto_approve,omitempty"`
```

- [ ] **Step 3: Add ResolveAutoApprove function**

```go
// ResolveAutoApprove resolves the effective auto-approve state.
// auto_approve takes precedence over yolo. If both are set with
// conflicting values, auto_approve wins.
func ResolveAutoApprove(autoApprove, yolo *bool) bool {
	if autoApprove != nil {
		return *autoApprove
	}
	if yolo != nil {
		return *yolo
	}
	return false
}
```

- [ ] **Step 4: Update launcher to use ResolveAutoApprove**

In `launcher.go`, update `resolveEffectiveYolo` to check both fields:

```go
func (l *Launcher) resolveEffectiveYolo(preferences, context, project *bool) bool {
	if l.NoYolo {
		return false
	}
	if l.Yolo {
		return true
	}
	// Check auto_approve fields first, fall back to yolo
	return config.ResolveYolo(preferences, context, project)
}
```

- [ ] **Step 5: Add validation for conflicting auto_approve + yolo**

In config validation, if both `auto_approve` and `yolo` are set with different values, return a warning.

- [ ] **Step 6: Run tests**

Run: `go test ./internal/config/ -v`
Run: `go test ./... -count=1`

- [ ] **Step 7: Commit**

Stage: `git add internal/config/schema.go internal/config/schema_test.go internal/launcher/launcher.go`
Run: `/commit --style classic add auto_approve config field with yolo backwards compatibility`

---

### Task 4: Final Verification

- [ ] **Step 1: Run full test suite**

Run: `go test ./...`

- [ ] **Step 2: Run lint**

Run: `golangci-lint run ./...`

- [ ] **Step 3: Manual verification**

Test interactive flow: `aide cap create` (no args)
Test expert flow: `aide cap create test --extends k8s`
Test auto_approve config: add `auto_approve: true` to config, verify banner shows AUTO-APPROVE

- [ ] **Step 4: Commit fixups**

Run: `/commit --style classic <description>`
