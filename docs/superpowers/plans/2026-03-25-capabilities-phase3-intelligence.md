# Capabilities Phase 3: Intelligence Layer

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the smart UX layer: capability-oriented banner, `aide status`, project detection, and sandbox error interception via plugin hook.

**Architecture:** Banner rendering shifts from guard-centric to capability-centric. Project detection scans for tool markers and suggests capabilities. A new `aide cap suggest-for-path` CLI command enables the Claude Code plugin hook to map sandbox errors to capability suggestions.

**Tech Stack:** Go, existing `internal/ui`, `internal/capability`, Claude Code plugin hooks

**Spec:** `docs/superpowers/specs/2026-03-25-capabilities-design.md`

**Depends on:** Phase 1 (Foundation) and Phase 2 (Management) must be complete.

---

### Task 1: Banner Data Model Update

Replace guard-centric `SandboxInfo` with capability-centric display data.

**Files:**
- Modify: `internal/ui/banner.go` — add `CapabilityDisplay`, update `BannerData`

- [ ] **Step 1: Add new types to banner.go**

```go
// CapabilityDisplay holds per-capability information for banner rendering.
type CapabilityDisplay struct {
	Name     string
	Paths    []string // readable/writable paths granted
	EnvVars  []string // env vars passed through
	Source   string   // "context config", "--with", "--without"
	Disabled bool     // true if --without excluded this
}

// Update BannerData to add:
type BannerData struct {
	// ... existing fields ...
	Capabilities  []CapabilityDisplay
	DisabledCaps  []CapabilityDisplay // --without caps
	NeverAllow    []string
	CredWarnings  []string // "AWS_SECRET_ACCESS_KEY (via aws)"
	CompWarnings  []string // composition warnings
	AutoApprove   bool     // replaces Yolo for banner display
}
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./internal/ui/`

- [ ] **Step 3: Commit**

Run: `/commit --style classic add capability display types to banner data model`

---

### Task 2: Banner Rendering — Capability View

Update all three banner styles to show capabilities instead of guards when capabilities are active.

**Files:**
- Modify: `internal/ui/banner.go` — update RenderCompact, RenderBoxed, RenderClean
- Modify: `internal/ui/banner_test.go`

- [ ] **Step 1: Add renderCapabilitySection() helper**

```go
func renderCapabilitySection(w io.Writer, data *BannerData, prefix string) {
	// Active capabilities (green ✓)
	for _, cap := range data.Capabilities {
		boldGreen.Fprintf(w, "%s✓ %-10s %s\n", prefix, cap.Name,
			truncateList(cap.Paths, 3))
		if cap.Source != "" && cap.Source != "context config" {
			dim.Fprintf(w, "%s             ← %s\n", prefix, cap.Source)
		}
	}

	// Disabled capabilities (dim ○)
	for _, cap := range data.DisabledCaps {
		dim.Fprintf(w, "%s○ %-10s disabled for this session", prefix, cap.Name)
		fmt.Fprintf(w, "  ← --without\n")
	}

	// Never-allow (red ✗)
	for _, path := range data.NeverAllow {
		red.Fprintf(w, "%s✗ denied    %s (never-allow)\n", prefix, path)
	}

	// Credential warnings
	if len(data.CredWarnings) > 0 {
		fmt.Fprintln(w)
		yellow.Fprintf(w, "%s⚠ credentials exposed: %s\n", prefix,
			strings.Join(data.CredWarnings, ", "))
	}

	// Composition warnings
	for _, w2 := range data.CompWarnings {
		yellow.Fprintf(w, "%s⚠ %s\n", prefix, w2)
	}
}
```

- [ ] **Step 2: Update RenderCompact to use capabilities when present**

If `len(data.Capabilities) > 0`, use `renderCapabilitySection()` instead of `renderGuardSection()`. Fall back to guard display when no capabilities are active (pure code-only mode shows "code-only" label).

- [ ] **Step 3: Update RenderBoxed and RenderClean similarly**

- [ ] **Step 4: Add auto-approve rendering (last line, red bold)**

All three styles render auto-approve as the final line:

```go
if data.AutoApprove {
	red := color.New(color.FgRed, color.Bold)
	red.Fprintf(w, "%s⚡ AUTO-APPROVE — all agent actions execute without confirmation\n", prefix)
}
```

- [ ] **Step 5: Write tests for capability banner rendering**

Test that:
- Capability names appear with ✓
- Never-allow paths appear with ✗
- Disabled caps appear with ○
- Credential warnings appear
- Auto-approve is always the last line
- Falls back to guard display when no capabilities

- [ ] **Step 6: Commit**

Run: `/commit --style classic update banner rendering to show capabilities instead of guards`

---

### Task 3: Launcher — Populate Capability Banner Data

Wire the capability resolution in the launcher to populate the new banner fields.

**Files:**
- Modify: `internal/launcher/launcher.go` — update `buildBannerData()`

- [ ] **Step 1: Pass resolved capabilities to buildBannerData**

After capability resolution in `Launch()`, pass the resolved capability set and the `--with`/`--without` lists to `buildBannerData()`.

- [ ] **Step 2: Build CapabilityDisplay entries**

For each resolved capability, create a `CapabilityDisplay` with:
- `Name` from the capability
- `Paths` from readable + writable (merged)
- `EnvVars` from env_allow
- `Source` annotation: "context config" if from context.Capabilities, "--with" if from CLI flag

For `--without` caps, create `DisabledCaps` entries.

- [ ] **Step 3: Populate credential and composition warnings**

Use `capability.CredentialWarnings()` and `capability.CompositionWarnings()` from Phase 2 Task 7.

- [ ] **Step 4: Set AutoApprove field**

Replace `Yolo` field usage with `AutoApprove`.

- [ ] **Step 5: Run tests and verify**

Run: `go test ./internal/launcher/ -v`
Manual: `aide --with k8s docker` — verify banner shows capabilities

- [ ] **Step 6: Commit**

Run: `/commit --style classic populate capability banner data from resolved capabilities in launcher`

---

### Task 4: `aide status` Command

Full detailed view of current context and capabilities.

**Files:**
- Modify: `cmd/aide/commands.go` — add `statusCmd()`

- [ ] **Step 1: Implement statusCmd()**

Loads config, resolves context, resolves capabilities, and displays:
- Context name, agent, match reason
- Secret name and key count
- Each active capability with full details (readable, writable, deny, env_allow, source, inheritance chain)
- Never-allow paths and env vars
- Credential warnings
- Network mode and sandbox rule count

Output format:

```
aide status
────────────────────────────────────────
Context:      work
Agent:        claude → /usr/local/bin/claude
Matched:      remote github.com/acme/*
Secret:       work (3 keys)

Capabilities:
  k8s-dev (extends k8s)
    readable:  ~/.kube/dev-config, ~/.kube/staging-config
    env:       KUBECONFIG
    source:    context config

  docker
    readable:  ~/.docker/config.json
    env:       DOCKER_CONFIG
    source:    --with flag

Never-allow:
  ~/.kube/prod-config

Credentials exposed:
  ⚠ AWS_SECRET_ACCESS_KEY (via aws)

Network: outbound only (all ports)
Sandbox: active (28 rules)
Auto-approve: no
────────────────────────────────────────
```

- [ ] **Step 2: Register in registerCommands()**

- [ ] **Step 3: Verify and commit**

Run: `aide status`
Run: `/commit --style classic add aide status command for detailed context and capability view`

---

### Task 5: Project Detection

Scan project files for tool markers and suggest capabilities.

**Files:**
- Create: `internal/capability/detect.go`
- Create: `internal/capability/detect_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestDetectProject_Dockerfile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM alpine"), 0644)

	suggestions := DetectProject(dir)
	found := false
	for _, s := range suggestions {
		if s == "docker" {
			found = true
		}
	}
	if !found {
		t.Error("expected docker suggestion for project with Dockerfile")
	}
}

func TestDetectProject_K8sManifests(t *testing.T) {
	dir := t.TempDir()
	k8sDir := filepath.Join(dir, "k8s")
	os.MkdirAll(k8sDir, 0755)
	os.WriteFile(filepath.Join(k8sDir, "deployment.yaml"),
		[]byte("apiVersion: apps/v1\nkind: Deployment"), 0644)

	suggestions := DetectProject(dir)
	found := false
	for _, s := range suggestions {
		if s == "k8s" {
			found = true
		}
	}
	if !found {
		t.Error("expected k8s suggestion for project with k8s manifests")
	}
}

func TestDetectProject_NoMarkers(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)

	suggestions := DetectProject(dir)
	if len(suggestions) != 0 {
		t.Errorf("expected no suggestions, got %v", suggestions)
	}
}
```

- [ ] **Step 2: Implement DetectProject()**

```go
// DetectProject scans the project root for tool markers and returns
// suggested capability names. Does not auto-enable — suggestions only.
func DetectProject(projectRoot string) []string {
	var suggestions []string

	// Docker
	if fileExists(filepath.Join(projectRoot, "Dockerfile")) ||
		fileExists(filepath.Join(projectRoot, "docker-compose.yaml")) ||
		fileExists(filepath.Join(projectRoot, "docker-compose.yml")) {
		suggestions = append(suggestions, "docker")
	}

	// Kubernetes
	if dirExists(filepath.Join(projectRoot, "k8s")) ||
		dirExists(filepath.Join(projectRoot, "manifests")) ||
		hasYAMLWithAPIVersion(projectRoot) {
		suggestions = append(suggestions, "k8s")
	}

	// Terraform
	if hasFileWithExtension(projectRoot, ".tf") {
		suggestions = append(suggestions, "terraform")
	}

	// AWS SDK detection
	if containsInFile(projectRoot, "go.mod", "aws-sdk-go") ||
		containsInFile(projectRoot, "requirements.txt", "boto3") ||
		containsInFile(projectRoot, "package.json", "aws-sdk") {
		suggestions = append(suggestions, "aws")
	}

	// GCP SDK detection
	if containsInFile(projectRoot, "go.mod", "cloud.google.com") ||
		containsInFile(projectRoot, "requirements.txt", "google-cloud") ||
		containsInFile(projectRoot, "package.json", "@google-cloud") {
		suggestions = append(suggestions, "gcp")
	}

	// npm
	if fileExists(filepath.Join(projectRoot, "package.json")) {
		suggestions = append(suggestions, "npm")
	}

	// Vault
	if fileExists(filepath.Join(projectRoot, "vault.hcl")) ||
		hasFileWithExtension(projectRoot, ".vault") {
		suggestions = append(suggestions, "vault")
	}

	return suggestions
}
```

- [ ] **Step 3: Run tests and commit**

Run: `go test ./internal/capability/ -run TestDetect -v`
Run: `/commit --style classic add project detection for capability suggestions`

---

### Task 6: Detection Integration in Launcher

Show detection suggestions on first run.

**Files:**
- Modify: `internal/launcher/launcher.go`

- [ ] **Step 1: Add detection to first-run path**

After context resolution, if no capabilities are active (neither from context nor `--with`), call `capability.DetectProject(projectRoot)`. If suggestions are found, display them in the banner:

```
Detected: Dockerfile, k8s manifests, AWS SDK
Suggested: aide --with docker k8s aws
```

Detection runs every session but the suggestion is non-intrusive (just a banner line). No prompting, no auto-enabling.

- [ ] **Step 2: Commit**

Run: `/commit --style classic show capability suggestions from project detection in banner`

---

### Task 7: `aide cap suggest-for-path` CLI Command

Enable the Claude Code plugin hook to map sandbox errors to capabilities.

**Files:**
- Modify: `cmd/aide/commands.go`
- Create: `internal/capability/suggest.go`

- [ ] **Step 1: Implement path-to-capability mapping**

```go
// SuggestForPath returns capability names that would grant access to the given path.
func SuggestForPath(path string, registry map[string]Capability) []string {
	var suggestions []string
	for name, cap := range registry {
		for _, readable := range cap.Readable {
			if strings.HasPrefix(path, expandTilde(readable)) {
				suggestions = append(suggestions, name)
			}
		}
		for _, writable := range cap.Writable {
			if strings.HasPrefix(path, expandTilde(writable)) {
				suggestions = append(suggestions, name)
			}
		}
	}
	return suggestions
}
```

- [ ] **Step 2: Implement capSuggestForPathCmd()**

```bash
aide cap suggest-for-path ~/.kube/config
# Output: k8s, helm
```

Outputs capability names, one per line. Designed for machine consumption by the plugin hook.

- [ ] **Step 3: Write tests and commit**

Run: `/commit --style classic add aide cap suggest-for-path for plugin hook integration`

---

### Task 8: Auto-approve Config Migration

Replace `yolo` config field with `auto_approve`, keeping backwards compat.

**Files:**
- Modify: `internal/config/schema.go`
- Modify: `internal/context/resolver.go`
- Modify: `internal/launcher/launcher.go`

- [ ] **Step 1: Add auto_approve field to Config and Context**

```go
AutoApprove *bool `yaml:"auto_approve,omitempty"`
```

In resolution, check `auto_approve` first, fall back to `yolo` for backwards compat:

```go
func resolveAutoApprove(prefs, ctx, project) bool {
	// auto_approve takes precedence over yolo
	if autoApprove != nil { return *autoApprove }
	if yolo != nil { return *yolo }
	return false
}
```

- [ ] **Step 2: Update launcher to use AutoApprove**

Map `--auto-approve` flag to the same agent flag as `--yolo` (`--dangerously-skip-permissions` for Claude Code).

- [ ] **Step 3: Commit**

Run: `/commit --style classic add auto_approve config field with yolo backwards compatibility`

---

### Task 9: Full Test Suite and Final Verification

- [ ] **Step 1: Run full test suite**

Run: `go test ./...`

- [ ] **Step 2: Run lint**

Run: `golangci-lint run ./...`

- [ ] **Step 3: Manual verification of all banner styles**

Test each scenario:
- `aide` (no capabilities) — shows "code-only"
- `aide --with k8s docker` — shows capabilities with ← --with
- `aide --with k8s --without docker` — shows disabled cap with ← --without
- `aide --auto-approve --with aws` — shows auto-approve as last line
- Context with `capabilities: [k8s]` — shows capability from context config

- [ ] **Step 4: Commit fixups**

Run: `/commit --style classic <description>`
