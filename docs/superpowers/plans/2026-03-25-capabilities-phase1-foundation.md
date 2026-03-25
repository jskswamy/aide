# Capabilities Phase 1: Foundation

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enable `aide --with k8s docker` to activate task-oriented permission bundles, resolving them to sandbox policy fields that the existing pipeline already handles.

**Architecture:** New `internal/capability/` package defines Capability types and resolution logic. Built-in capabilities are registered in a registry (following the guard registry pattern). The launcher merges resolved capabilities into `config.SandboxPolicy` before passing to `sandbox.PolicyFromConfig()` — the existing sandbox pipeline is unchanged.

**Tech Stack:** Go, YAML config, existing `internal/config`, `internal/sandbox`, `internal/launcher`, `pkg/seatbelt/guards`

**Spec:** `docs/superpowers/specs/2026-03-25-capabilities-design.md`

---

### Task 1: Capability Types and Resolution Engine

Define the core types and inheritance/combine resolution logic.

**Files:**
- Create: `internal/capability/capability.go`
- Create: `internal/capability/capability_test.go`

- [ ] **Step 1: Write the failing test for basic capability resolution**

In `internal/capability/capability_test.go`:

```go
package capability

import (
	"testing"
)

func TestResolveOne_Builtin(t *testing.T) {
	registry := map[string]Capability{
		"k8s": {
			Name:        "k8s",
			Description: "Kubernetes cluster access",
			Unguard:     []string{"kubernetes"},
			Readable:    []string{"~/.kube"},
			EnvAllow:    []string{"KUBECONFIG"},
		},
	}

	resolved, err := ResolveOne("k8s", registry)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved.Name != "k8s" {
		t.Errorf("expected name k8s, got %s", resolved.Name)
	}
	if len(resolved.Unguard) != 1 || resolved.Unguard[0] != "kubernetes" {
		t.Errorf("expected unguard [kubernetes], got %v", resolved.Unguard)
	}
	if len(resolved.Readable) != 1 || resolved.Readable[0] != "~/.kube" {
		t.Errorf("expected readable [~/.kube], got %v", resolved.Readable)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/capability/ -run TestResolveOne_Builtin -v`
Expected: FAIL — package doesn't exist

- [ ] **Step 3: Implement Capability types and ResolveOne**

Create `internal/capability/capability.go`:

```go
package capability

import "fmt"

// Capability defines a task-oriented permission bundle.
type Capability struct {
	Name        string
	Description string
	Extends     string   // single parent inheritance
	Combines    []string // merge multiple capabilities
	Unguard     []string
	Readable    []string
	Writable    []string
	Deny        []string
	EnvAllow    []string
}

// ResolvedCapability is the flattened result after inheritance resolution.
type ResolvedCapability struct {
	Name     string
	Sources  []string // trace: ["k8s-dev", "k8s"]
	Unguard  []string
	Readable []string
	Writable []string
	Deny     []string
	EnvAllow []string
}

// CapabilitySet is the merged result of multiple activated capabilities.
type CapabilitySet struct {
	Capabilities []ResolvedCapability
	NeverAllow   []string
	NeverAllowEnv []string
}

const maxDepth = 10

// ResolveOne resolves a single capability by name, walking extends/combines chains.
func ResolveOne(name string, registry map[string]Capability) (*ResolvedCapability, error) {
	return resolveOne(name, registry, make(map[string]bool), 0)
}

func resolveOne(name string, registry map[string]Capability, visited map[string]bool, depth int) (*ResolvedCapability, error) {
	if depth > maxDepth {
		return nil, fmt.Errorf("capability inheritance depth exceeds %d for %q", maxDepth, name)
	}
	if visited[name] {
		return nil, fmt.Errorf("circular capability reference: %q", name)
	}
	visited[name] = true

	cap, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown capability: %q", name)
	}

	if cap.Extends != "" && len(cap.Combines) > 0 {
		return nil, fmt.Errorf("capability %q: extends and combines are mutually exclusive", name)
	}

	if cap.Extends != "" {
		parent, err := resolveOne(cap.Extends, registry, visited, depth+1)
		if err != nil {
			return nil, fmt.Errorf("resolving parent of %q: %w", name, err)
		}
		return mergeChild(parent, &cap), nil
	}

	if len(cap.Combines) > 0 {
		result := &ResolvedCapability{Name: name, Sources: []string{name}}
		for _, combineName := range cap.Combines {
			resolved, err := resolveOne(combineName, registry, copyVisited(visited), depth+1)
			if err != nil {
				return nil, fmt.Errorf("resolving combined %q in %q: %w", combineName, name, err)
			}
			result = mergeAdditive(result, resolved)
		}
		// Apply local overrides on top
		result = mergeChild(result, &cap)
		result.Name = name
		return result, nil
	}

	// Base case — no extends, no combines
	return flatten(&cap), nil
}

func flatten(cap *Capability) *ResolvedCapability {
	return &ResolvedCapability{
		Name:     cap.Name,
		Sources:  []string{cap.Name},
		Unguard:  copyStrings(cap.Unguard),
		Readable: copyStrings(cap.Readable),
		Writable: copyStrings(cap.Writable),
		Deny:     copyStrings(cap.Deny),
		EnvAllow: copyStrings(cap.EnvAllow),
	}
}

func mergeChild(parent *ResolvedCapability, child *Capability) *ResolvedCapability {
	return &ResolvedCapability{
		Name:     child.Name,
		Sources:  append([]string{child.Name}, parent.Sources...),
		Unguard:  dedup(append(parent.Unguard, child.Unguard...)),
		Readable: dedup(append(parent.Readable, child.Readable...)),
		Writable: dedup(append(parent.Writable, child.Writable...)),
		Deny:     dedup(append(parent.Deny, child.Deny...)),
		EnvAllow: dedup(append(parent.EnvAllow, child.EnvAllow...)),
	}
}

func mergeAdditive(a, b *ResolvedCapability) *ResolvedCapability {
	return &ResolvedCapability{
		Name:     a.Name,
		Sources:  append(a.Sources, b.Sources...),
		Unguard:  dedup(append(a.Unguard, b.Unguard...)),
		Readable: dedup(append(a.Readable, b.Readable...)),
		Writable: dedup(append(a.Writable, b.Writable...)),
		Deny:     dedup(append(a.Deny, b.Deny...)),
		EnvAllow: dedup(append(a.EnvAllow, b.EnvAllow...)),
	}
}

func copyStrings(s []string) []string {
	if s == nil {
		return nil
	}
	out := make([]string, len(s))
	copy(out, s)
	return out
}

func copyVisited(m map[string]bool) map[string]bool {
	out := make(map[string]bool, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func dedup(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(s))
	var out []string
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
```

- [ ] **Step 4: Run test**

Run: `go test ./internal/capability/ -run TestResolveOne_Builtin -v`
Expected: PASS

- [ ] **Step 5: Add tests for extends, combines, circular, depth, mutual exclusion**

Add to `capability_test.go`:

```go
func TestResolveOne_Extends(t *testing.T) {
	registry := map[string]Capability{
		"k8s": {
			Name:    "k8s",
			Unguard: []string{"kubernetes"},
			Readable: []string{"~/.kube"},
			EnvAllow: []string{"KUBECONFIG"},
		},
		"k8s-dev": {
			Name:     "k8s-dev",
			Extends:  "k8s",
			Readable: []string{"~/.kube/dev-config"},
			Deny:     []string{"~/.kube/prod-config"},
		},
	}

	resolved, err := ResolveOne("k8s-dev", registry)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved.Name != "k8s-dev" {
		t.Errorf("expected name k8s-dev, got %s", resolved.Name)
	}
	// Should inherit parent's unguard
	if len(resolved.Unguard) != 1 || resolved.Unguard[0] != "kubernetes" {
		t.Errorf("expected inherited unguard [kubernetes], got %v", resolved.Unguard)
	}
	// Should merge readable (parent + child)
	if len(resolved.Readable) != 2 {
		t.Errorf("expected 2 readable paths, got %d: %v", len(resolved.Readable), resolved.Readable)
	}
	// Should have child's deny
	if len(resolved.Deny) != 1 || resolved.Deny[0] != "~/.kube/prod-config" {
		t.Errorf("expected deny [~/.kube/prod-config], got %v", resolved.Deny)
	}
	// Should have parent's env_allow
	if len(resolved.EnvAllow) != 1 || resolved.EnvAllow[0] != "KUBECONFIG" {
		t.Errorf("expected env_allow [KUBECONFIG], got %v", resolved.EnvAllow)
	}
}

func TestResolveOne_Combines(t *testing.T) {
	registry := map[string]Capability{
		"aws":    {Name: "aws", Unguard: []string{"cloud-aws"}, EnvAllow: []string{"AWS_PROFILE"}},
		"k8s":    {Name: "k8s", Unguard: []string{"kubernetes"}, EnvAllow: []string{"KUBECONFIG"}},
		"docker": {Name: "docker", Unguard: []string{"docker"}},
		"my-deploy": {
			Name:     "my-deploy",
			Combines: []string{"aws", "k8s", "docker"},
			Deny:     []string{"~/.kube/prod-config"},
		},
	}

	resolved, err := ResolveOne("my-deploy", registry)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolved.Unguard) != 3 {
		t.Errorf("expected 3 unguard entries, got %d: %v", len(resolved.Unguard), resolved.Unguard)
	}
	if len(resolved.EnvAllow) != 2 {
		t.Errorf("expected 2 env_allow entries, got %d: %v", len(resolved.EnvAllow), resolved.EnvAllow)
	}
	if len(resolved.Deny) != 1 {
		t.Errorf("expected 1 deny entry, got %d: %v", len(resolved.Deny), resolved.Deny)
	}
}

func TestResolveOne_CircularReference(t *testing.T) {
	registry := map[string]Capability{
		"a": {Name: "a", Extends: "b"},
		"b": {Name: "b", Extends: "a"},
	}
	_, err := ResolveOne("a", registry)
	if err == nil {
		t.Fatal("expected circular reference error")
	}
}

func TestResolveOne_MutualExclusion(t *testing.T) {
	registry := map[string]Capability{
		"bad": {Name: "bad", Extends: "k8s", Combines: []string{"aws"}},
		"k8s": {Name: "k8s"},
		"aws": {Name: "aws"},
	}
	_, err := ResolveOne("bad", registry)
	if err == nil {
		t.Fatal("expected mutual exclusion error")
	}
}

func TestResolveOne_UnknownCapability(t *testing.T) {
	registry := map[string]Capability{}
	_, err := ResolveOne("nonexistent", registry)
	if err == nil {
		t.Fatal("expected unknown capability error")
	}
}
```

- [ ] **Step 6: Run all tests**

Run: `go test ./internal/capability/ -v`
Expected: All pass

- [ ] **Step 7: Commit**

Stage: `git add internal/capability/capability.go internal/capability/capability_test.go`
Run: `/commit --style classic add capability types and resolution engine with extends and combines support`

---

### Task 2: ResolveAll and CapabilitySet

Add the function that resolves multiple capabilities into a merged set ready for sandbox integration.

**Files:**
- Modify: `internal/capability/capability.go`
- Modify: `internal/capability/capability_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestResolveAll(t *testing.T) {
	registry := map[string]Capability{
		"aws":    {Name: "aws", Unguard: []string{"cloud-aws"}, Readable: []string{"~/.aws"}, EnvAllow: []string{"AWS_PROFILE"}},
		"k8s":    {Name: "k8s", Unguard: []string{"kubernetes"}, Readable: []string{"~/.kube"}, EnvAllow: []string{"KUBECONFIG"}},
		"docker": {Name: "docker", Unguard: []string{"docker"}, Readable: []string{"~/.docker"}},
	}

	set, err := ResolveAll([]string{"aws", "k8s", "docker"}, registry, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(set.Capabilities) != 3 {
		t.Errorf("expected 3 capabilities, got %d", len(set.Capabilities))
	}

	overrides := set.ToSandboxOverrides()
	if len(overrides.Unguard) != 3 {
		t.Errorf("expected 3 unguard, got %d: %v", len(overrides.Unguard), overrides.Unguard)
	}
	if len(overrides.ReadableExtra) != 3 {
		t.Errorf("expected 3 readable, got %d: %v", len(overrides.ReadableExtra), overrides.ReadableExtra)
	}
	if len(overrides.EnvAllow) != 2 {
		t.Errorf("expected 2 env_allow, got %d: %v", len(overrides.EnvAllow), overrides.EnvAllow)
	}
}

func TestResolveAll_NeverAllow(t *testing.T) {
	registry := map[string]Capability{
		"k8s": {Name: "k8s", Readable: []string{"~/.kube"}},
	}
	neverAllow := []string{"~/.kube/prod-config"}

	set, err := ResolveAll([]string{"k8s"}, registry, neverAllow, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	overrides := set.ToSandboxOverrides()
	if len(overrides.DeniedExtra) != 1 || overrides.DeniedExtra[0] != "~/.kube/prod-config" {
		t.Errorf("expected never_allow in denied, got %v", overrides.DeniedExtra)
	}
}

func TestResolveAll_NeverAllowEnv(t *testing.T) {
	registry := map[string]Capability{
		"aws": {Name: "aws", EnvAllow: []string{"AWS_PROFILE", "AWS_SECRET_ACCESS_KEY"}},
	}
	neverAllowEnv := []string{"AWS_SECRET_ACCESS_KEY"}

	set, err := ResolveAll([]string{"aws"}, registry, nil, neverAllowEnv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	overrides := set.ToSandboxOverrides()
	if len(overrides.EnvAllow) != 1 || overrides.EnvAllow[0] != "AWS_PROFILE" {
		t.Errorf("expected AWS_SECRET_ACCESS_KEY stripped, got %v", overrides.EnvAllow)
	}
}

func TestResolveAll_DuplicateNames(t *testing.T) {
	registry := map[string]Capability{
		"k8s": {Name: "k8s", Unguard: []string{"kubernetes"}},
	}
	set, err := ResolveAll([]string{"k8s", "k8s"}, registry, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(set.Capabilities) != 1 {
		t.Errorf("expected dedup to 1 capability, got %d", len(set.Capabilities))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/capability/ -run TestResolveAll -v`

- [ ] **Step 3: Implement ResolveAll and ToSandboxOverrides**

Add to `capability.go`:

```go
// SandboxOverrides contains the merged sandbox policy fields from resolved capabilities.
type SandboxOverrides struct {
	Unguard       []string
	ReadableExtra []string
	WritableExtra []string
	DeniedExtra   []string
	EnvAllow      []string
}

// ResolveAll resolves multiple capability names and returns a merged CapabilitySet.
func ResolveAll(names []string, registry map[string]Capability, neverAllow, neverAllowEnv []string) (*CapabilitySet, error) {
	set := &CapabilitySet{
		NeverAllow:    neverAllow,
		NeverAllowEnv: neverAllowEnv,
	}

	seen := make(map[string]bool)
	for _, name := range names {
		if seen[name] {
			continue
		}
		seen[name] = true

		resolved, err := ResolveOne(name, registry)
		if err != nil {
			return nil, err
		}
		set.Capabilities = append(set.Capabilities, *resolved)
	}

	return set, nil
}

// ToSandboxOverrides merges all capabilities into sandbox policy fields.
func (cs *CapabilitySet) ToSandboxOverrides() SandboxOverrides {
	var o SandboxOverrides

	for _, cap := range cs.Capabilities {
		o.Unguard = append(o.Unguard, cap.Unguard...)
		o.ReadableExtra = append(o.ReadableExtra, cap.Readable...)
		o.WritableExtra = append(o.WritableExtra, cap.Writable...)
		o.DeniedExtra = append(o.DeniedExtra, cap.Deny...)
		o.EnvAllow = append(o.EnvAllow, cap.EnvAllow...)
	}

	// Append never_allow to denied
	o.DeniedExtra = append(o.DeniedExtra, cs.NeverAllow...)

	// Strip never_allow_env from env_allow
	if len(cs.NeverAllowEnv) > 0 {
		blocked := make(map[string]bool, len(cs.NeverAllowEnv))
		for _, e := range cs.NeverAllowEnv {
			blocked[e] = true
		}
		var filtered []string
		for _, e := range o.EnvAllow {
			if !blocked[e] {
				filtered = append(filtered, e)
			}
		}
		o.EnvAllow = filtered
	}

	o.Unguard = dedup(o.Unguard)
	o.ReadableExtra = dedup(o.ReadableExtra)
	o.WritableExtra = dedup(o.WritableExtra)
	o.DeniedExtra = dedup(o.DeniedExtra)
	o.EnvAllow = dedup(o.EnvAllow)

	return o
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/capability/ -v`
Expected: All pass

- [ ] **Step 5: Commit**

Stage: `git add internal/capability/capability.go internal/capability/capability_test.go`
Run: `/commit --style classic add ResolveAll and ToSandboxOverrides for merging capabilities into sandbox policy`

---

### Task 3: Built-in Capability Definitions

Register the 13 built-in capabilities.

**Files:**
- Create: `internal/capability/builtin.go`
- Create: `internal/capability/builtin_test.go`

- [ ] **Step 1: Write the failing test**

```go
package capability

import "testing"

func TestBuiltins_AllPresent(t *testing.T) {
	expected := []string{
		"aws", "gcp", "azure", "digitalocean", "oci",
		"docker", "k8s", "helm",
		"terraform", "vault",
		"ssh", "npm",
	}
	for _, name := range expected {
		if _, ok := Builtins()[name]; !ok {
			t.Errorf("missing built-in capability %q", name)
		}
	}
}

func TestBuiltins_Count(t *testing.T) {
	if len(Builtins()) != 13 {
		t.Errorf("expected 13 built-in capabilities, got %d", len(Builtins()))
	}
}

func TestBuiltins_EachResolvable(t *testing.T) {
	registry := Builtins()
	for name := range registry {
		_, err := ResolveOne(name, registry)
		if err != nil {
			t.Errorf("built-in %q failed to resolve: %v", name, err)
		}
	}
}

func TestBuiltin_K8s_HasCorrectGuard(t *testing.T) {
	k8s := Builtins()["k8s"]
	if len(k8s.Unguard) != 1 || k8s.Unguard[0] != "kubernetes" {
		t.Errorf("k8s should unguard [kubernetes], got %v", k8s.Unguard)
	}
}

func TestBuiltin_Helm_ExtendsK8sGuard(t *testing.T) {
	helm := Builtins()["helm"]
	if len(helm.Unguard) != 1 || helm.Unguard[0] != "kubernetes" {
		t.Errorf("helm should unguard [kubernetes], got %v", helm.Unguard)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/capability/ -run TestBuiltins -v`

- [ ] **Step 3: Implement built-in definitions**

Create `internal/capability/builtin.go`:

```go
package capability

// builtins holds all built-in capability definitions.
var builtins map[string]Capability

func init() {
	builtins = map[string]Capability{
		// Cloud providers
		"aws": {
			Name:        "aws",
			Description: "AWS CLI and credentials",
			Unguard:     []string{"cloud-aws"},
			Readable:    []string{"~/.aws"},
			EnvAllow: []string{
				"AWS_PROFILE", "AWS_REGION", "AWS_DEFAULT_REGION",
				"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN",
				"AWS_CONFIG_FILE", "AWS_SHARED_CREDENTIALS_FILE",
			},
		},
		"gcp": {
			Name:        "gcp",
			Description: "Google Cloud CLI and credentials",
			Unguard:     []string{"cloud-gcp"},
			Readable:    []string{"~/.config/gcloud"},
			EnvAllow:    []string{"CLOUDSDK_CONFIG", "GOOGLE_APPLICATION_CREDENTIALS", "GCLOUD_PROJECT"},
		},
		"azure": {
			Name:        "azure",
			Description: "Azure CLI and credentials",
			Unguard:     []string{"cloud-azure"},
			Readable:    []string{"~/.azure"},
			EnvAllow:    []string{"AZURE_CONFIG_DIR", "AZURE_SUBSCRIPTION_ID"},
		},
		"digitalocean": {
			Name:        "digitalocean",
			Description: "DigitalOcean CLI credentials",
			Unguard:     []string{"cloud-digitalocean"},
			Readable:    []string{"~/.config/doctl"},
			EnvAllow:    []string{"DIGITALOCEAN_ACCESS_TOKEN"},
		},
		"oci": {
			Name:        "oci",
			Description: "Oracle Cloud CLI credentials",
			Unguard:     []string{"cloud-oci"},
			Readable:    []string{"~/.oci"},
			EnvAllow:    []string{"OCI_CLI_CONFIG_FILE"},
		},

		// Containers
		"docker": {
			Name:        "docker",
			Description: "Docker daemon and registry credentials",
			Unguard:     []string{"docker"},
			Readable:    []string{"~/.docker"},
			EnvAllow:    []string{"DOCKER_CONFIG", "DOCKER_HOST"},
		},

		// Orchestration
		"k8s": {
			Name:        "k8s",
			Description: "Kubernetes cluster access",
			Unguard:     []string{"kubernetes"},
			Readable:    []string{"~/.kube"},
			EnvAllow:    []string{"KUBECONFIG"},
		},
		"helm": {
			Name:        "helm",
			Description: "Helm charts and releases",
			Unguard:     []string{"kubernetes"},
			Readable:    []string{"~/.kube", "~/.config/helm", "~/.cache/helm"},
			EnvAllow:    []string{"HELM_HOME", "KUBECONFIG"},
		},

		// Infrastructure as Code
		"terraform": {
			Name:        "terraform",
			Description: "Terraform state and providers",
			Unguard:     []string{"terraform"},
			Readable:    []string{"~/.terraform.d"},
			EnvAllow:    []string{"TF_CLI_CONFIG_FILE"},
		},
		"vault": {
			Name:        "vault",
			Description: "HashiCorp Vault access",
			Unguard:     []string{"vault"},
			Readable:    []string{"~/.vault-token"},
			EnvAllow:    []string{"VAULT_ADDR", "VAULT_TOKEN", "VAULT_TOKEN_FILE"},
		},

		// SSH
		"ssh": {
			Name:        "ssh",
			Description: "SSH keys and agent",
			Unguard:     []string{"ssh-keys"},
			Readable:    []string{"~/.ssh"},
			EnvAllow:    []string{"SSH_AUTH_SOCK"},
		},

		// Package registries
		"npm": {
			Name:        "npm",
			Description: "npm and yarn registry credentials",
			Unguard:     []string{"npm", "netrc"},
			Readable:    []string{"~/.npmrc", "~/.yarnrc"},
			EnvAllow:    []string{"NPM_TOKEN", "NODE_AUTH_TOKEN"},
		},
	}
}

// Builtins returns a copy of the built-in capability registry.
func Builtins() map[string]Capability {
	out := make(map[string]Capability, len(builtins))
	for k, v := range builtins {
		out[k] = v
	}
	return out
}

// MergedRegistry returns a registry combining built-ins with user-defined
// capabilities. User-defined capabilities override built-ins with the same name.
func MergedRegistry(userDefined map[string]Capability) map[string]Capability {
	merged := Builtins()
	for k, v := range userDefined {
		merged[k] = v
	}
	return merged
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/capability/ -v`
Expected: All pass

- [ ] **Step 5: Commit**

Stage: `git add internal/capability/builtin.go internal/capability/builtin_test.go`
Run: `/commit --style classic add 13 built-in capability definitions for cloud, container, IaC, and toolchain access`

---

### Task 4: Config Schema Changes

Add capability-related fields to config types.

**Files:**
- Modify: `internal/config/schema.go`
- Modify: `internal/config/schema_test.go` (if exists)

- [ ] **Step 1: Write the failing test**

Create or extend config test to verify new fields parse from YAML:

```go
func TestConfig_CapabilityFields(t *testing.T) {
	yamlData := `
capabilities:
  k8s-dev:
    extends: k8s
    readable: ["~/.kube/dev-config"]
    deny: ["~/.kube/prod-config"]
    env_allow: [KUBECONFIG]
never_allow:
  - "~/.kube/prod-config"
never_allow_env:
  - VAULT_ROOT_TOKEN
contexts:
  work:
    agent: claude
    capabilities: [k8s-dev, docker]
`
	var cfg Config
	if err := yaml.Unmarshal([]byte(yamlData), &cfg); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(cfg.Capabilities) != 1 {
		t.Errorf("expected 1 capability def, got %d", len(cfg.Capabilities))
	}
	if len(cfg.NeverAllow) != 1 {
		t.Errorf("expected 1 never_allow, got %d", len(cfg.NeverAllow))
	}
	if len(cfg.NeverAllowEnv) != 1 {
		t.Errorf("expected 1 never_allow_env, got %d", len(cfg.NeverAllowEnv))
	}
	work := cfg.Contexts["work"]
	if len(work.Capabilities) != 2 {
		t.Errorf("expected 2 capabilities on context, got %d", len(work.Capabilities))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

- [ ] **Step 3: Add fields to Config and Context structs**

In `internal/config/schema.go`, add to `Config`:

```go
// Capability definitions (user-defined, extends/combines built-ins)
Capabilities  map[string]CapabilityDef `yaml:"capabilities,omitempty"`
NeverAllow    []string                 `yaml:"never_allow,omitempty"`
NeverAllowEnv []string                 `yaml:"never_allow_env,omitempty"`
```

Add to `Context`:

```go
Capabilities []string `yaml:"capabilities,omitempty"`
```

Add to `ProjectOverride`:

```go
Capabilities []string `yaml:"capabilities,omitempty"`
```

Add new type:

```go
// CapabilityDef defines a user-configured capability in YAML.
type CapabilityDef struct {
	Extends     string   `yaml:"extends,omitempty"`
	Combines    []string `yaml:"combines,omitempty"`
	Description string   `yaml:"description,omitempty"`
	Readable    []string `yaml:"readable,omitempty"`
	Writable    []string `yaml:"writable,omitempty"`
	Deny        []string `yaml:"deny,omitempty"`
	EnvAllow    []string `yaml:"env_allow,omitempty"`
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/config/ -v`
Expected: All pass

- [ ] **Step 5: Commit**

Stage: `git add internal/config/schema.go internal/config/*_test.go`
Run: `/commit --style classic add capability config fields to Config, Context, and ProjectOverride`

---

### Task 5: Config-to-Capability Conversion

Bridge between config YAML types and the capability engine types.

**Files:**
- Create: `internal/capability/config.go`
- Create: `internal/capability/config_test.go`

- [ ] **Step 1: Write the failing test**

```go
package capability

import (
	"testing"

	"github.com/jskswamy/aide/internal/config"
)

func TestFromConfigDefs(t *testing.T) {
	defs := map[string]config.CapabilityDef{
		"k8s-dev": {
			Extends:  "k8s",
			Readable: []string{"~/.kube/dev-config"},
			Deny:     []string{"~/.kube/prod-config"},
			EnvAllow: []string{"KUBECONFIG"},
		},
	}

	caps := FromConfigDefs(defs)
	if len(caps) != 1 {
		t.Fatalf("expected 1 capability, got %d", len(caps))
	}
	k8sDev := caps["k8s-dev"]
	if k8sDev.Extends != "k8s" {
		t.Errorf("expected extends k8s, got %s", k8sDev.Extends)
	}
}
```

- [ ] **Step 2: Implement FromConfigDefs**

Create `internal/capability/config.go`:

```go
package capability

import "github.com/jskswamy/aide/internal/config"

// FromConfigDefs converts YAML config capability definitions to Capability types.
func FromConfigDefs(defs map[string]config.CapabilityDef) map[string]Capability {
	out := make(map[string]Capability, len(defs))
	for name, def := range defs {
		out[name] = Capability{
			Name:        name,
			Description: def.Description,
			Extends:     def.Extends,
			Combines:    def.Combines,
			Readable:    def.Readable,
			Writable:    def.Writable,
			Deny:        def.Deny,
			EnvAllow:    def.EnvAllow,
		}
	}
	return out
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/capability/ -v`

- [ ] **Step 4: Commit**

Stage: `git add internal/capability/config.go internal/capability/config_test.go`
Run: `/commit --style classic add config-to-capability bridge for YAML capability definitions`

---

### Task 6: CLI Flags — `--with` and `--without`

Add the flags to the root command.

**Files:**
- Modify: `cmd/aide/main.go`

- [ ] **Step 1: Add flags to root command**

In `main.go`, add after the existing flag definitions (around line 59-63):

```go
var withCaps []string
var withoutCaps []string

// In the flag section:
rootCmd.Flags().StringSliceVar(&withCaps, "with", nil, "Activate capabilities for this session (e.g., --with k8s,docker)")
rootCmd.Flags().StringSliceVar(&withoutCaps, "without", nil, "Disable context capabilities for this session")
```

Pass `withCaps` and `withoutCaps` to `launcher.Launch()` — this requires adding parameters to the Launch function signature.

- [ ] **Step 2: Also add `--auto-approve` flag (replacing `--yolo`)**

```go
rootCmd.Flags().BoolVar(&autoApprove, "auto-approve", false, "Run agent without permission checks")
rootCmd.Flags().BoolVar(&noAutoApprove, "no-auto-approve", false, "Override config: require permission checks")
```

Keep existing `--yolo`/`--no-yolo` flags for backwards compat but mark as hidden:

```go
rootCmd.Flags().MarkHidden("yolo")
rootCmd.Flags().MarkHidden("no-yolo")
```

- [ ] **Step 3: Verify compilation**

Run: `go build ./cmd/aide/`

- [ ] **Step 4: Commit**

Stage: `git add cmd/aide/main.go`
Run: `/commit --style classic add --with, --without, and --auto-approve CLI flags`

---

### Task 7: Launcher Integration

Wire capability resolution into the launch pipeline.

**Files:**
- Modify: `internal/launcher/launcher.go`

- [ ] **Step 1: Add capability resolution to Launch()**

After context resolution (around line 80) and before sandbox resolution (around line 200), add:

```go
// Merge capabilities: context + CLI --with, minus CLI --without
capNames := rc.Context.Capabilities
capNames = append(capNames, withCaps...)
if len(withoutCaps) > 0 {
	excluded := make(map[string]bool, len(withoutCaps))
	for _, name := range withoutCaps {
		excluded[name] = true
	}
	var filtered []string
	for _, name := range capNames {
		if !excluded[name] {
			filtered = append(filtered, name)
		}
	}
	capNames = filtered
}

// Resolve capabilities to sandbox overrides
if len(capNames) > 0 {
	userDefs := capability.FromConfigDefs(cfg.Capabilities)
	registry := capability.MergedRegistry(userDefs)

	capSet, err := capability.ResolveAll(capNames, registry, cfg.NeverAllow, cfg.NeverAllowEnv)
	if err != nil {
		return fmt.Errorf("resolving capabilities: %w", err)
	}

	overrides := capSet.ToSandboxOverrides()

	// Ensure sandbox config exists
	if sandboxCfg == nil {
		sandboxCfg = &config.SandboxPolicy{}
	}

	// Merge capability overrides into sandbox config
	sandboxCfg.Unguard = append(sandboxCfg.Unguard, overrides.Unguard...)
	sandboxCfg.ReadableExtra = append(sandboxCfg.ReadableExtra, overrides.ReadableExtra...)
	sandboxCfg.WritableExtra = append(sandboxCfg.WritableExtra, overrides.WritableExtra...)
	sandboxCfg.DeniedExtra = append(sandboxCfg.DeniedExtra, overrides.DeniedExtra...)
}
```

- [ ] **Step 2: Handle env_allow in environment setup**

After environment resolution, filter environment variables through `env_allow`:

```go
// If capabilities specify env_allow and clean_env is true,
// add the allowed env vars to the passthrough list
```

Note: env_allow primarily matters when `clean_env: true`. When clean_env is false, all env vars pass through anyway. But the banner should always show which credential-bearing env vars are exposed.

- [ ] **Step 3: Verify compilation and existing tests**

Run: `go build ./cmd/aide/`
Run: `go test ./internal/launcher/ -v`

- [ ] **Step 4: Commit**

Stage: `git add internal/launcher/launcher.go`
Run: `/commit --style classic integrate capability resolution into launch pipeline`

---

### Task 8: Context Resolver — Merge Project Override Capabilities

Ensure capabilities from `.aide.yaml` project overrides flow through.

**Files:**
- Modify: `internal/context/resolver.go`

- [ ] **Step 1: Add capability merging to applyProjectOverride**

In `applyProjectOverride()`, add after the sandbox merge (around line 120-134):

```go
if len(override.Capabilities) > 0 {
	rc.Context.Capabilities = override.Capabilities
}
```

- [ ] **Step 2: Write test for project override capabilities**

```go
func TestResolve_ProjectOverrideCapabilities(t *testing.T) {
	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"work": {
				Agent:        "claude",
				Capabilities: []string{"docker"},
				Match:        []config.MatchRule{{Path: "*"}},
			},
		},
		ProjectOverride: &config.ProjectOverride{
			Capabilities: []string{"k8s", "aws"},
		},
	}
	rc, err := Resolve(cfg, "/tmp/test", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Project override replaces context capabilities
	if len(rc.Context.Capabilities) != 2 {
		t.Errorf("expected 2 capabilities from project override, got %d: %v",
			len(rc.Context.Capabilities), rc.Context.Capabilities)
	}
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/context/ -v`

- [ ] **Step 4: Commit**

Stage: `git add internal/context/resolver.go internal/context/*_test.go`
Run: `/commit --style classic merge project override capabilities into context resolution`

---

### Task 9: Contract Tests for Capabilities

Verify end-to-end: capability config → sandbox policy → rendered profile.

**Files:**
- Modify: `internal/sandbox/policy_contract_test.go`

- [ ] **Step 1: Write contract tests**

```go
func TestContract_CapabilityUnguardProducesRule(t *testing.T) {
	// A capability that unguards cloud-aws should result in AWS paths
	// not being denied in the profile (cloud-aws guard inactive).
	home, _ := os.UserHomeDir()
	awsDir := filepath.Join(home, ".aws")
	if _, err := os.Stat(awsDir); err != nil {
		t.Skip("~/.aws not found")
	}

	profile := renderProfileFromConfig(t, &config.SandboxPolicy{
		Unguard: []string{"cloud-aws"},
	})
	// cloud-aws guard's deny rules should NOT appear
	if strings.Contains(profile, "deny") && strings.Contains(profile, ".aws/credentials") {
		t.Error("unguarding cloud-aws should remove .aws deny rules from profile")
	}
}

func TestContract_NeverAllowOverridesCapability(t *testing.T) {
	// Even with cloud-aws unguarded, never_allow should still deny the path
	profile := renderProfileFromConfig(t, &config.SandboxPolicy{
		Unguard:    []string{"cloud-aws"},
		DeniedExtra: []string{"~/.aws/credentials"},
	})
	if !strings.Contains(profile, ".aws/credentials") {
		t.Error("never_allow (via denied_extra) should still deny .aws/credentials")
	}
}
```

- [ ] **Step 2: Run contract tests**

Run: `go test ./internal/sandbox/ -run TestContract -v`

- [ ] **Step 3: Commit**

Stage: `git add internal/sandbox/policy_contract_test.go`
Run: `/commit --style classic add contract tests for capability unguard and never_allow override`

---

### Task 10: Full Test Suite and Vet

- [ ] **Step 1: Run full test suite**

Run: `go test ./...`
Expected: All pass

- [ ] **Step 2: Run vet**

Run: `go vet ./...`
Expected: No issues

- [ ] **Step 3: Run lint**

Run: `golangci-lint run ./...`
Expected: No issues (or only pre-existing)

- [ ] **Step 4: Manual verification**

Verify `aide --with k8s docker` produces the correct sandbox policy by checking the rendered profile:

```bash
aide --with k8s docker sandbox test
```

Expected: Profile contains unguarded kubernetes + docker guards, ~/.kube and ~/.docker in readable paths.

- [ ] **Step 5: Commit any fixups**

Run: `/commit --style classic <description of fixups>`
