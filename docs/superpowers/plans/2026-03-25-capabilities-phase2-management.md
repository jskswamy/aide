# Capabilities Phase 2: Management Commands

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add CLI commands for viewing, creating, editing, and managing capabilities (`aide cap list/show/create/edit/enable/disable/never-allow/check/audit`).

**Architecture:** New `aide cap` command tree in `cmd/aide/commands.go`. Interactive creation uses filesystem discovery from `internal/capability/discover.go`. All commands operate on the config file — no runtime state.

**Tech Stack:** Go, Cobra CLI, existing `internal/config`, `internal/capability`, `internal/ui`

**Spec:** `docs/superpowers/specs/2026-03-25-capabilities-design.md`

**Depends on:** Phase 1 (Foundation) must be complete.

---

### Task 1: `aide cap list` and `aide cap show`

**Files:**
- Modify: `cmd/aide/commands.go` — add `capCmd()` with `list` and `show` subcommands
- Create: `internal/capability/display.go` — formatting helpers for capability display

- [ ] **Step 1: Write capCmd() with list subcommand**

Register `capCmd()` in `registerCommands()`. The `list` subcommand loads config, merges built-in + user-defined capabilities, and displays all with source annotation (built-in / custom / extends).

```go
func capCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cap",
		Short: "Manage capabilities",
	}
	cmd.AddCommand(capListCmd())
	cmd.AddCommand(capShowCmd())
	return cmd
}
```

- [ ] **Step 2: Implement capListCmd()**

List all capabilities (built-in + user-defined) with name, description, and source.

- [ ] **Step 3: Implement capShowCmd()**

Show a single capability's full resolved details: name, description, extends/combines chain, unguard, readable, writable, deny, env_allow. If the capability uses `extends` or `combines`, show the fully resolved result and the inheritance chain via `Sources`.

- [ ] **Step 4: Verify and commit**

Run: `go build ./cmd/aide/ && aide cap list && aide cap show k8s`
Stage and run: `/commit --style classic add aide cap list and aide cap show commands`

---

### Task 2: `aide cap create` (expert mode — flags)

**Files:**
- Modify: `cmd/aide/commands.go` — add `capCreateCmd()`

- [ ] **Step 1: Implement flag-based creation**

Flags: `--extends`, `--combines`, `--readable`, `--writable`, `--deny`, `--env-allow`, `--description`. Validates inputs (extends and combines mutually exclusive, referenced capabilities must exist), writes to config file.

```go
func capCreateCmd() *cobra.Command {
	var extends string
	var combines, readable, writable, deny, envAllow []string
	var description string
	// ...
}
```

- [ ] **Step 2: Write to config**

Use existing `config.Write()` pattern to persist the new capability definition.

- [ ] **Step 3: Verify and commit**

Run: `aide cap create test-cap --extends k8s --deny "~/.kube/prod" && aide cap show test-cap`
Run: `/commit --style classic add aide cap create with expert flag mode`

---

### Task 3: `aide cap create` (interactive mode)

**Files:**
- Create: `internal/capability/discover.go` — filesystem scanning
- Modify: `cmd/aide/commands.go` — interactive flow when no flags

- [ ] **Step 1: Implement discover.go**

Scan well-known paths (`~/.kube/`, `~/.aws/`, `~/.docker/`, etc.) and return what exists on disk with content summaries. Also detect relevant environment variables from `os.Environ()`.

```go
package capability

// DiscoveredPaths scans the filesystem for paths relevant to a capability.
func DiscoveredPaths(home string, capName string) []DiscoveredPath { ... }

// DiscoveredEnvVars returns env vars from the current shell relevant to a capability.
func DiscoveredEnvVars(capName string) []string { ... }

type DiscoveredPath struct {
	Path    string
	Exists  bool
	Summary string // e.g., "3 kubeconfig contexts found"
}
```

- [ ] **Step 2: Implement interactive flow**

When `aide cap create` is called with no name argument, enter interactive mode:
1. Prompt for name
2. Prompt for base (list built-ins as choices, or "from scratch")
3. Call `DiscoveredPaths()` for the selected base, present as checkboxes
4. Prompt for deny paths
5. Call `DiscoveredEnvVars()`, present as checkboxes
6. Show summary, confirm

Use existing `internal/ui` prompt patterns or `bufio.Scanner` for input.

- [ ] **Step 3: Verify and commit**

Test interactive flow manually.
Run: `/commit --style classic add interactive aide cap create with filesystem discovery`

---

### Task 4: `aide cap edit`

**Files:**
- Modify: `cmd/aide/commands.go`

- [ ] **Step 1: Implement capEditCmd()**

Flags: `--add-readable`, `--add-writable`, `--add-deny`, `--remove-deny`, `--add-env-allow`, `--remove-env-allow`, `--description`. Loads config, finds the capability by name (must be user-defined, not built-in), applies changes, writes config.

- [ ] **Step 2: Verify and commit**

Run: `aide cap edit test-cap --add-readable "~/.kube/staging" && aide cap show test-cap`
Run: `/commit --style classic add aide cap edit for modifying user-defined capabilities`

---

### Task 5: `aide cap enable` and `aide cap disable`

**Files:**
- Modify: `cmd/aide/commands.go`

- [ ] **Step 1: Implement capEnableCmd()**

Adds a capability name to the current context's `capabilities` list in config. Uses the same `resolveContextForMutation()` pattern as `sandboxGuardCmd()`.

- [ ] **Step 2: Implement capDisableCmd()**

Removes a capability name from the current context's `capabilities` list.

- [ ] **Step 3: Verify and commit**

Run: `aide cap enable k8s && aide which` (verify k8s in capabilities)
Run: `aide cap disable k8s && aide which`
Run: `/commit --style classic add aide cap enable and disable for context-scoped capability management`

---

### Task 6: `aide cap never-allow`

**Files:**
- Modify: `cmd/aide/commands.go`

- [ ] **Step 1: Implement capNeverAllowCmd()**

Subcommand tree:
- `aide cap never-allow <path>` — add path to `never_allow`
- `aide cap never-allow --env <var>` — add env var to `never_allow_env`
- `aide cap never-allow --list` — show all never-allow entries
- `aide cap never-allow --remove <path>` — remove a path

Modifies the top-level `Config.NeverAllow` / `Config.NeverAllowEnv` fields.

- [ ] **Step 2: Verify and commit**

Run: `aide cap never-allow "~/.kube/prod" && aide cap never-allow --list`
Run: `/commit --style classic add aide cap never-allow for global deny management`

---

### Task 7: `aide cap check` and `aide cap audit`

**Files:**
- Modify: `cmd/aide/commands.go`
- Create: `internal/capability/audit.go`

- [ ] **Step 1: Implement capCheckCmd()**

Preview what a set of capabilities would grant without launching:

```bash
aide cap check aws k8s docker
```

Resolves the capabilities, shows the merged sandbox overrides (unguard, readable, writable, deny, env_allow), and any warnings (credential + network composition).

- [ ] **Step 2: Implement capAuditCmd()**

Show the current context's active capabilities with fully resolved permissions. Similar to `check` but reads from config rather than CLI args.

- [ ] **Step 3: Implement credential/composition warnings**

In `internal/capability/audit.go`, add logic to detect:
- Credential-bearing env vars (AWS_SECRET_ACCESS_KEY, VAULT_TOKEN, etc.)
- Combinations of credential access + network egress

```go
// CredentialWarnings returns env var names that are known credential bearers.
func CredentialWarnings(envAllow []string) []string { ... }

// CompositionWarnings checks if capabilities combine credential + network access.
func CompositionWarnings(caps []ResolvedCapability) []string { ... }
```

- [ ] **Step 4: Verify and commit**

Run: `aide cap check aws k8s docker`
Run: `/commit --style classic add aide cap check and audit for capability composition preview`

---

### Task 8: Shell Tab Completion

**Files:**
- Modify: `cmd/aide/main.go` or completion setup

- [ ] **Step 1: Register completion for --with and --without flags**

Use Cobra's `RegisterFlagCompletionFunc` to provide capability name completion:

```go
rootCmd.RegisterFlagCompletionFunc("with", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return capabilityNames(), cobra.ShellCompDirectiveNoFileComp
})
```

- [ ] **Step 2: Verify and commit**

Run: `/commit --style classic add shell tab completion for capability names`

---

### Task 9: Full Test Suite

- [ ] **Step 1: Run full suite**

Run: `go test ./...`

- [ ] **Step 2: Run lint**

Run: `golangci-lint run ./...`

- [ ] **Step 3: Commit fixups**

Run: `/commit --style classic <description>`
