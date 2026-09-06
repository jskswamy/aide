# Skip Secret Decryption in `aide sync` When Nothing Changed — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `aide sync` should never discover the age key or decrypt anything when neither `config.yaml` nor the context's encrypted secrets file changed since the last successful sync, while still applying secret-related updates exactly as today whenever something real changed.

**Architecture:** Add a `SecretsHash` field to `ContextState` (sha256 of the raw encrypted-secrets-file bytes, computed with the existing `provision.ConfigHash` helper), alongside the existing `ConfigHash`. Before resolving secrets, `runSync` checks both hashes against state; if both match, it substitutes each secret-templated MCP env value with whatever is already installed (proven safe by the invariant that an unchanged config+secrets pair means today's true resolved value is exactly what the last successful sync already applied) instead of decrypting. If any templated key has no installed counterpart, or either hash differs, it falls back to full decrypt+resolve exactly as today. A `--force-secrets` flag bypasses the gate on demand.

**Tech Stack:** Go, cobra (CLI), existing `internal/provision`, `internal/secrets`, `internal/config` packages. No new dependencies.

## Global Constraints

- No per-server or per-env-key granularity added to the diff/apply engine (`internal/provision/plan.go`, `internal/provision/engine.go`) — those stay untouched.
- The consolidated decrypt helper must produce identical error text to what `internal/launcher/launcher.go` already produces today (existing test `TestLauncher_WithSecrets` and the decrypt-failure test at `launcher_test.go:467` assert on `"decrypting secrets"` — must keep passing unmodified).
- Both `ConfigHash` and `SecretsHash` are only ever persisted together, and only after a fully successful sync (mirrors existing `ConfigHash` semantics in `updateStateAfterSync`, `cmd/aide/sync.go:245-321`).
- Design reference: `docs/superpowers/specs/2026-09-06-sync-secrets-hash-gate-design.md`.

---

### Task 1: Add `SecretsHash` to `ContextState`

**Files:**
- Modify: `internal/provision/state.go:39-47`
- Test: `internal/provision/state_test.go`

**Interfaces:**
- Produces: `provision.ContextState.SecretsHash string` (json tag `secrets_hash,omitempty`), readable/writable like the existing `ConfigHash` field.

- [ ] **Step 1: Write the failing test**

Add to `internal/provision/state_test.go`:

```go
func TestSaveStateRoundTripWithSecretsHash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "managed.json")
	st := &provision.ManagedState{
		Version: 1,
		Contexts: map[string]*provision.ContextState{
			"work": {
				ConfigHash:  "sha256:abc",
				SecretsHash: "sha256:def",
			},
		},
	}
	if err := provision.SaveState(path, st); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	got, err := provision.LoadState(path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if got.Contexts["work"].SecretsHash != "sha256:def" {
		t.Errorf("SecretsHash = %q, want %q", got.Contexts["work"].SecretsHash, "sha256:def")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/provision/... -run TestSaveStateRoundTripWithSecretsHash -v`
Expected: FAIL to compile — `ContextState` has no field `SecretsHash`.

- [ ] **Step 3: Add the field**

In `internal/provision/state.go`, change:

```go
type ContextState struct {
	ConfigHash     string                 `json:"config_hash,omitempty"`
	HookConfigHash string                 `json:"hook_config_hash,omitempty"`
	SyncedAt       time.Time              `json:"synced_at,omitempty"`
	Plugins        map[string]ManagedItem `json:"plugins,omitempty"`
	MCPServers     map[string]ManagedItem `json:"mcp_servers,omitempty"`
	Marketplaces   map[string]ManagedItem `json:"marketplaces,omitempty"`
	Hooks          []ManagedHook          `json:"hooks,omitempty"`
}
```

to:

```go
type ContextState struct {
	ConfigHash     string                 `json:"config_hash,omitempty"`
	HookConfigHash string                 `json:"hook_config_hash,omitempty"`
	// SecretsHash is sha256 of the context's encrypted secrets file
	// bytes (ciphertext only, never plaintext) — lets aide sync skip
	// decryption when neither this nor ConfigHash changed since the
	// last successful sync. Empty when the context has no secret file.
	SecretsHash  string                 `json:"secrets_hash,omitempty"`
	SyncedAt     time.Time              `json:"synced_at,omitempty"`
	Plugins      map[string]ManagedItem `json:"plugins,omitempty"`
	MCPServers   map[string]ManagedItem `json:"mcp_servers,omitempty"`
	Marketplaces map[string]ManagedItem `json:"marketplaces,omitempty"`
	Hooks        []ManagedHook          `json:"hooks,omitempty"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/provision/... -run TestSaveStateRoundTripWithSecretsHash -v`
Expected: PASS

- [ ] **Step 5: Run the full package test suite to check for regressions**

Run: `go test ./internal/provision/...`
Expected: PASS (no other test constructs `ContextState` with positional fields, so this is additive-only)

- [ ] **Step 6: Commit**

```bash
git add internal/provision/state.go internal/provision/state_test.go
git commit -m "Add SecretsHash field to ContextState"
```

---

### Task 2: Consolidate age-key-discovery + decrypt into `internal/secrets.LoadSecretsMap`

**Files:**
- Create: `internal/secrets/template.go`
- Test: `internal/secrets/template_test.go`
- Modify: `internal/launcher/launcher.go:311-325`

**Interfaces:**
- Consumes: `secrets.DiscoverAgeKey() (*AgeIdentity, error)` (`internal/secrets/age.go:36`), `secrets.DecryptSecretsFile(filePath string, identity *AgeIdentity) (map[string]string, error)` (`internal/secrets/sops.go:20`) — both already exist, unchanged.
- Produces: `secrets.LoadSecretsMap(secretsPath string) (map[string]string, error)` — used by Task 3 (sync.go) and this task's launcher.go change.

- [ ] **Step 1: Write the failing tests**

Create `internal/secrets/template_test.go`:

```go
package secrets_test

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/secrets"
)

func repoTestdataDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("unable to determine test file path")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "testdata")
}

func TestLoadSecretsMap_NoKeyAvailable(t *testing.T) {
	t.Setenv("PATH", "")
	t.Setenv("SOPS_AGE_KEY", "")
	t.Setenv("SOPS_AGE_KEY_FILE", "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	_, err := secrets.LoadSecretsMap("/nonexistent/secrets.enc.yaml")
	if err == nil {
		t.Fatal("expected error when no age identity is discoverable")
	}
	if !strings.Contains(err.Error(), "discovering age key") {
		t.Errorf("expected discovering age key error, got: %v", err)
	}
}

func TestLoadSecretsMap_Success(t *testing.T) {
	td := repoTestdataDir(t)
	keyFile := filepath.Join(td, "age-key.txt")
	encFile := filepath.Join(td, "test-secrets.enc.yaml")
	t.Setenv("SOPS_AGE_KEY_FILE", keyFile)

	got, err := secrets.LoadSecretsMap(encFile)
	if err != nil {
		t.Fatalf("LoadSecretsMap: %v", err)
	}
	if got["anthropic_api_key"] == "" {
		t.Errorf("expected anthropic_api_key to be decrypted, got map: %+v", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/secrets/... -run TestLoadSecretsMap -v`
Expected: FAIL to compile — `secrets.LoadSecretsMap` does not exist yet.

- [ ] **Step 3: Implement `LoadSecretsMap`**

Create `internal/secrets/template.go`:

```go
package secrets

import "fmt"

// LoadSecretsMap discovers the local age identity and decrypts the
// sops-encrypted file at secretsPath, returning its plaintext
// key/value map. Shared by `aide launch` (env injection at process
// start) and `aide sync` (MCP server env template resolution) —
// previously each independently reimplemented this discover+decrypt
// sequence.
func LoadSecretsMap(secretsPath string) (map[string]string, error) {
	identity, err := DiscoverAgeKey()
	if err != nil {
		return nil, fmt.Errorf("discovering age key: %w", err)
	}
	secretsMap, err := DecryptSecretsFile(secretsPath, identity)
	if err != nil {
		return nil, fmt.Errorf("decrypting secrets: %w", err)
	}
	return secretsMap, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/secrets/... -run TestLoadSecretsMap -v`
Expected: PASS. (If `TestLoadSecretsMap_Success` skips or fails because the testdata fixtures are missing, verify `testdata/age-key.txt` and `testdata/test-secrets.enc.yaml` exist at the repo root — they're already used by `internal/launcher/launcher_test.go`.)

- [ ] **Step 5: Wire `internal/launcher/launcher.go` to use it**

In `internal/launcher/launcher.go`, replace (around line 311-325):

```go
	// 8. Decrypt secrets if context has Secret
	var secretsMap map[string]string
	if rc.Context.Secret != "" {
		secretsPath := config.ResolveSecretPath(rc.Context.Secret)
		identity, err := secrets.DiscoverAgeKey()
		if err != nil {
			cleanup()
			return fmt.Errorf("discovering age key: %w", err)
		}
		secretsMap, err = secrets.DecryptSecretsFile(secretsPath, identity)
		if err != nil {
			cleanup()
			return fmt.Errorf("decrypting secrets: %w", err)
		}
	}
```

with:

```go
	// 8. Decrypt secrets if context has Secret
	var secretsMap map[string]string
	if rc.Context.Secret != "" {
		secretsPath := config.ResolveSecretPath(rc.Context.Secret)
		var err error
		secretsMap, err = secrets.LoadSecretsMap(secretsPath)
		if err != nil {
			cleanup()
			return err
		}
	}
```

- [ ] **Step 6: Run the launcher package tests to confirm no regressions**

Run: `go test ./internal/launcher/...`
Expected: PASS, including `TestLauncher_WithSecrets` and the decrypt-failure test asserting `"decrypting secrets"` in the error — both must pass unmodified since `LoadSecretsMap` produces identical wrapped error text.

- [ ] **Step 7: Commit**

```bash
git add internal/secrets/template.go internal/secrets/template_test.go internal/launcher/launcher.go
git commit -m "Consolidate age-key-discovery+decrypt into secrets.LoadSecretsMap"
```

---

### Task 3: Persist `SecretsHash` after sync, and switch sync's decrypt call to `LoadSecretsMap`

**Files:**
- Modify: `cmd/aide/sync.go` (`resolveMCPSecretsForSync` at lines 341-372, `updateStateAfterSync` at lines 245-321)
- Test: `cmd/aide/sync_test.go`

**Interfaces:**
- Consumes: `secrets.LoadSecretsMap` (Task 2), `provision.ConfigHash(path string) (string, error)` (`internal/provision/confighash.go` — already exists, returns `("", nil)` for a missing file), `ContextState.SecretsHash` (Task 1).
- Produces: `contextSecretsHash(ctx config.Context) (string, error)` — used again in Task 4's gate check.

- [ ] **Step 1: Write the failing test**

Add to `cmd/aide/sync_test.go` (add `"runtime"` to the import block — needed for the new `repoTestdataDir` helper):

```go
// repoTestdataDir mirrors internal/launcher's helper of the same name —
// cmd/aide is the same two directories below the repo root.
func repoTestdataDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("unable to determine test file path")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "testdata")
}

func TestSync_PersistsSecretsHash(t *testing.T) {
	fakeProvReset(t)
	theFakeProv.RequireTTY = false
	td := repoTestdataDir(t)
	keyFile := filepath.Join(td, "age-key.txt")
	encFile := filepath.Join(td, "test-secrets.enc.yaml")
	if _, err := os.Stat(keyFile); err != nil {
		t.Skipf("test age key not found at %s: %v", keyFile, err)
	}
	t.Setenv("SOPS_AGE_KEY_FILE", keyFile)

	home := isolatedConfigDir(t)
	cwd, _ := os.Getwd()
	yaml := fmt.Sprintf(`mcp_servers:
  github:
    command: github-mcp-server
    env:
      TOKEN: "{{ .secrets.anthropic_api_key }}"
contexts:
  work:
    agent: fakeagent
    secret: %s
    match:
      - path: %s
    mcp_servers:
      - github
`, encFile, cwd)
	cfgPath := filepath.Join(home, "xdg", "aide", "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runSyncCmd(t, "", "--context", "work", "--yes")
	if err != nil {
		t.Fatalf("execute: %v\n%s", err, out)
	}

	wantHash, err := provision.ConfigHash(encFile)
	if err != nil {
		t.Fatal(err)
	}
	st, err := provision.LoadState(provision.DefaultStatePath(home))
	if err != nil {
		t.Fatal(err)
	}
	cs := st.Contexts["work"]
	if cs == nil {
		t.Fatal("context state missing")
	}
	if cs.SecretsHash != wantHash {
		t.Errorf("SecretsHash = %q, want %q", cs.SecretsHash, wantHash)
	}
}

func TestSync_NoSecretContextHasEmptySecretsHash(t *testing.T) {
	fakeProvReset(t)
	theFakeProv.RequireTTY = false
	home := setupProvisionConfig(t,
		[]string{"linear"}, nil,
		map[string]string{"linear": "linear@1.2"}, nil,
	)
	if _, err := runSyncCmd(t, "", "--context", "work", "--yes"); err != nil {
		t.Fatal(err)
	}
	st, err := provision.LoadState(provision.DefaultStatePath(home))
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Contexts["work"].SecretsHash; got != "" {
		t.Errorf("SecretsHash = %q, want empty for a context with no secret configured", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/aide/... -run TestSync_PersistsSecretsHash -v`
Expected: FAIL — `cs.SecretsHash` is always `""` today (field exists per Task 1, but nothing populates it yet).

- [ ] **Step 3: Add `contextSecretsHash` and wire it into `updateStateAfterSync`, and switch to `LoadSecretsMap`**

In `cmd/aide/sync.go`, add near `resolveMCPSecretsForSync`:

```go
// contextSecretsHash returns the sha256 hash of ctx's encrypted secrets
// file (via provision.ConfigHash, which already treats a missing file
// as "" — no separate sentinel needed), or "" if the context has no
// secret configured. Used both to persist a drift signal after a
// successful sync and, in a later change, to decide whether a sync run
// can skip decryption entirely.
func contextSecretsHash(ctx config.Context) (string, error) {
	if ctx.Secret == "" {
		return "", nil
	}
	return provision.ConfigHash(config.ResolveSecretPath(ctx.Secret))
}
```

In `updateStateAfterSync`, change:

```go
func updateStateAfterSync(env *provisionEnv, desired provision.Desired, plan provision.Plan) error {
	hash, err := provision.ConfigHash(config.FilePath())
	if err != nil {
		return err
	}
```

to:

```go
func updateStateAfterSync(env *provisionEnv, desired provision.Desired, plan provision.Plan) error {
	hash, err := provision.ConfigHash(config.FilePath())
	if err != nil {
		return err
	}
	secretsHash, err := contextSecretsHash(env.ctx)
	if err != nil {
		return err
	}
```

and change:

```go
	cs.ConfigHash = hash
	cs.SyncedAt = now
```

to:

```go
	cs.ConfigHash = hash
	cs.SecretsHash = secretsHash
	cs.SyncedAt = now
```

Also switch `resolveMCPSecretsForSync` to use the consolidated helper from Task 2 — change:

```go
func resolveMCPSecretsForSync(env *provisionEnv, desired *provision.Desired) error {
	var td *config.TemplateData
	if env.ctx.Secret != "" {
		secretsPath := config.ResolveSecretPath(env.ctx.Secret)
		identity, err := secrets.DiscoverAgeKey()
		if err != nil {
			return fmt.Errorf("discovering age key for context %q: %w", env.contextName, err)
		}
		secretsMap, err := secrets.DecryptSecretsFile(secretsPath, identity)
		if err != nil {
			return fmt.Errorf("decrypting secrets for context %q: %w", env.contextName, err)
		}
		cwd, _ := os.Getwd()
		td = &config.TemplateData{
			Secrets:     secretsMap,
			ProjectRoot: cwd,
		}
	}
	return provision.ResolveSecretsInMCPEnv(desired, td)
}
```

to:

```go
func resolveMCPSecretsForSync(env *provisionEnv, desired *provision.Desired) error {
	var td *config.TemplateData
	if env.ctx.Secret != "" {
		secretsPath := config.ResolveSecretPath(env.ctx.Secret)
		secretsMap, err := secrets.LoadSecretsMap(secretsPath)
		if err != nil {
			return fmt.Errorf("resolving secrets for context %q: %w", env.contextName, err)
		}
		cwd, _ := os.Getwd()
		td = &config.TemplateData{
			Secrets:     secretsMap,
			ProjectRoot: cwd,
		}
	}
	return provision.ResolveSecretsInMCPEnv(desired, td)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/aide/... -run 'TestSync_PersistsSecretsHash|TestSync_NoSecretContextHasEmptySecretsHash' -v`
Expected: PASS

- [ ] **Step 5: Run the full `cmd/aide` test suite to check for regressions**

Run: `go test ./cmd/aide/...`
Expected: PASS, including `TestSync_MCPSecretTemplate_NoSecretsFileErrors` (still exercises the nil-`TemplateData` branch, untouched by this change) and `TestSync_Yes_AppliesAndUpdatesState` (no `secret:` field, `SecretsHash` stays `""`, doesn't break existing assertions since that test never checked `SecretsHash`).

- [ ] **Step 6: Commit**

```bash
git add cmd/aide/sync.go cmd/aide/sync_test.go
git commit -m "Persist SecretsHash after sync; use consolidated secrets.LoadSecretsMap"
```

---

### Task 4: Reorder `runSync` to compute `installed` before secret resolution, and add `--force-secrets`

This task is a **pure reorder + flag plumbing** with no gating behavior yet — it exists as its own reviewable checkpoint because Task 5's substitution logic needs `installed.MCPServers` available before `resolveMCPSecretsForSync` runs, and a reviewer should be able to confirm "nothing changed behaviorally" here before Task 5 adds new logic on top.

**Files:**
- Modify: `cmd/aide/sync.go` (`runSync` at lines 48-171, `syncCmd` at lines 21-46)
- Test: `cmd/aide/sync_test.go`

**Interfaces:**
- Produces: `runSync(out io.Writer, in io.Reader, contextName string, planOnly, yes, forceSecrets bool) error` — new trailing `forceSecrets` parameter, unused until Task 5.

- [ ] **Step 1: Write the failing test**

Add to `cmd/aide/sync_test.go`:

```go
func TestSync_ForceSecretsFlagAccepted(t *testing.T) {
	fakeProvReset(t)
	theFakeProv.RequireTTY = false
	setupProvisionConfig(t,
		[]string{"linear"}, nil,
		map[string]string{"linear": "linear@1.2"}, nil,
	)
	// No secret configured, so --force-secrets is a no-op here — this
	// test only pins that the flag exists and parses without error.
	out, err := runSyncCmd(t, "", "--context", "work", "--yes", "--force-secrets")
	if err != nil {
		t.Fatalf("execute: %v\n%s", err, out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/aide/... -run TestSync_ForceSecretsFlagAccepted -v`
Expected: FAIL — `unknown flag: --force-secrets`

- [ ] **Step 3: Reorder `runSync` and add the flag**

In `cmd/aide/sync.go`, change `syncCmd`:

```go
func syncCmd() *cobra.Command {
	var contextName string
	var planOnly bool
	var yes bool
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Reconcile declared plugins and MCP servers for a context",
		Long: `aide sync inspects the agent's installed state, computes the diff
against the context's declared plugins and MCP servers, and applies
the changes through the agent's CLI / config file.

Flags:
  --plan   Print the plan and exit without making changes.
  --yes    Non-interactive mode: apply with the default actions and
           skip the confirmation prompt. Unmanaged items are left
           in place. Fails fast if the agent's plugin install path
           requires a TTY.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSync(cmd.OutOrStdout(), cmd.InOrStdin(), contextName, planOnly, yes)
		},
	}
	cmd.Flags().StringVar(&contextName, "context", "", "Context name (default: matched by CWD)")
	cmd.Flags().BoolVar(&planOnly, "plan", false, "Show the plan and exit without applying")
	cmd.Flags().BoolVar(&yes, "yes", false, "Apply without prompting")
	return cmd
}
```

to:

```go
func syncCmd() *cobra.Command {
	var contextName string
	var planOnly bool
	var yes bool
	var forceSecrets bool
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Reconcile declared plugins and MCP servers for a context",
		Long: `aide sync inspects the agent's installed state, computes the diff
against the context's declared plugins and MCP servers, and applies
the changes through the agent's CLI / config file.

Flags:
  --plan            Print the plan and exit without making changes.
  --yes             Non-interactive mode: apply with the default
                     actions and skip the confirmation prompt.
                     Unmanaged items are left in place. Fails fast if
                     the agent's plugin install path requires a TTY.
  --force-secrets   Force re-resolving secret-templated MCP env values
                     even if neither config.yaml nor the encrypted
                     secrets file changed. Sync normally skips
                     decryption in that case, trusting the agent's
                     already-installed values; use this to repair a
                     manually-edited/corrupted installed value.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSync(cmd.OutOrStdout(), cmd.InOrStdin(), contextName, planOnly, yes, forceSecrets)
		},
	}
	cmd.Flags().StringVar(&contextName, "context", "", "Context name (default: matched by CWD)")
	cmd.Flags().BoolVar(&planOnly, "plan", false, "Show the plan and exit without applying")
	cmd.Flags().BoolVar(&yes, "yes", false, "Apply without prompting")
	cmd.Flags().BoolVar(&forceSecrets, "force-secrets", false, "Force re-resolving secret-templated MCP env values, bypassing the skip-if-unchanged gate")
	return cmd
}
```

Change `runSync`'s signature and reorder the body — from:

```go
func runSync(out io.Writer, in io.Reader, contextName string, planOnly, yes bool) error {
	env, err := loadProvisionEnv(contextName)
	if err != nil {
		return err
	}
	agentDir := provision.ResolveAgentDir(env.prov, env.provCtx)
	desired, err := provision.ResolveDesired(env.cfg, env.contextName, agentDir, env.provCtx.HomeDir)
	if err != nil {
		return err
	}

	// Template-resolve MCP env values against context secrets so the
	// plan diff compares already-resolved values against the agent's
	// installed state. Without this, a desired `{{ .secrets.X }}` would
	// never equal the agent's "real-value" installed state and aide
	// sync would propose an unnecessary update every run. Plan output
	// itself only prints op kind + name (not env values), so resolving
	// here does not leak secrets to stdout / journal files.
	if err := resolveMCPSecretsForSync(env, &desired); err != nil {
		return err
	}

	// Warn and filter Desired fields for capabilities the agent doesn't support.
	// Sync continues for supported capabilities.
	warnAndFilterDesired(out, env.prov, &desired)

	installed := provision.Installed{
		MCPServers:   map[string]provision.MCPServer{},
		Marketplaces: map[string]provision.Marketplace{},
	}
	if hi, ok := env.prov.(provision.HookInstaller); ok {
		hooks, err := hi.ReadHooks(env.provCtx)
		if err != nil {
			return fmt.Errorf("listing installed hooks: %w", err)
		}
		installed.Hooks = hooks
	}
	if env.prov.SupportsPlugins() {
		got, err := env.prov.InstalledPlugins(env.provCtx)
		if err != nil {
			return fmt.Errorf("listing installed plugins: %w", err)
		}
		for _, p := range got {
			installed.Plugins = append(installed.Plugins, p.Key)
		}
		// Marketplaces are an attribute of plugin-supporting agents.
		mks, err := env.prov.InstalledMarketplaces(env.provCtx)
		if err != nil {
			return fmt.Errorf("listing installed marketplaces: %w", err)
		}
		for _, m := range mks {
			installed.Marketplaces[m.Key] = m
		}
	}
	var managedCtxState provision.ContextState
	if cs, ok := env.state.Contexts[env.contextName]; ok && cs != nil {
		managedCtxState = *cs
	}

	if env.prov.SupportsMCP() {
		names := provision.MCPQueryNames(desired.MCPServers, managedCtxState.MCPServers)
		got, err := provision.ReadInstalledMCP(env.prov, env.provCtx, names)
		if err != nil {
			return err
		}
		installed.MCPServers = got
	}

	plan := provision.ComputePlan(env.provCtx, desired, installed, managedCtxState)
	renderPlan(out, plan)
```

to:

```go
func runSync(out io.Writer, in io.Reader, contextName string, planOnly, yes, forceSecrets bool) error {
	env, err := loadProvisionEnv(contextName)
	if err != nil {
		return err
	}
	agentDir := provision.ResolveAgentDir(env.prov, env.provCtx)
	desired, err := provision.ResolveDesired(env.cfg, env.contextName, agentDir, env.provCtx.HomeDir)
	if err != nil {
		return err
	}

	var managedCtxState provision.ContextState
	if cs, ok := env.state.Contexts[env.contextName]; ok && cs != nil {
		managedCtxState = *cs
	}

	// Installed state is fetched before secret resolution so a later
	// gate can substitute already-installed values for secret-templated
	// env keys instead of decrypting, when nothing that could affect
	// them has changed. See resolveMCPSecretsForSync.
	installed := provision.Installed{
		MCPServers:   map[string]provision.MCPServer{},
		Marketplaces: map[string]provision.Marketplace{},
	}
	if hi, ok := env.prov.(provision.HookInstaller); ok {
		hooks, err := hi.ReadHooks(env.provCtx)
		if err != nil {
			return fmt.Errorf("listing installed hooks: %w", err)
		}
		installed.Hooks = hooks
	}
	if env.prov.SupportsPlugins() {
		got, err := env.prov.InstalledPlugins(env.provCtx)
		if err != nil {
			return fmt.Errorf("listing installed plugins: %w", err)
		}
		for _, p := range got {
			installed.Plugins = append(installed.Plugins, p.Key)
		}
		// Marketplaces are an attribute of plugin-supporting agents.
		mks, err := env.prov.InstalledMarketplaces(env.provCtx)
		if err != nil {
			return fmt.Errorf("listing installed marketplaces: %w", err)
		}
		for _, m := range mks {
			installed.Marketplaces[m.Key] = m
		}
	}
	if env.prov.SupportsMCP() {
		names := provision.MCPQueryNames(desired.MCPServers, managedCtxState.MCPServers)
		got, err := provision.ReadInstalledMCP(env.prov, env.provCtx, names)
		if err != nil {
			return err
		}
		installed.MCPServers = got
	}

	// Template-resolve MCP env values against context secrets so the
	// plan diff compares already-resolved values against the agent's
	// installed state. Without this, a desired `{{ .secrets.X }}` would
	// never equal the agent's "real-value" installed state and aide
	// sync would propose an unnecessary update every run. Plan output
	// itself only prints op kind + name (not env values), so resolving
	// here does not leak secrets to stdout / journal files.
	_ = forceSecrets // used starting Task 5
	if err := resolveMCPSecretsForSync(env, &desired); err != nil {
		return err
	}

	// Warn and filter Desired fields for capabilities the agent doesn't support.
	// Sync continues for supported capabilities.
	warnAndFilterDesired(out, env.prov, &desired)

	plan := provision.ComputePlan(env.provCtx, desired, installed, managedCtxState)
	renderPlan(out, plan)
```

(The rest of `runSync` — `planOnly` check onward — is unchanged; `installed` and `managedCtxState` are used exactly as before, just defined earlier.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/aide/... -run TestSync_ForceSecretsFlagAccepted -v`
Expected: PASS

- [ ] **Step 5: Run the full `cmd/aide` test suite to confirm the reorder changed nothing observable**

Run: `go test ./cmd/aide/...`
Expected: PASS, identical results to before this task — this step is the reviewer's confirmation that the reorder is behavior-preserving.

- [ ] **Step 6: Commit**

```bash
git add cmd/aide/sync.go cmd/aide/sync_test.go
git commit -m "Reorder runSync to fetch installed state before secret resolution; add --force-secrets flag"
```

---

### Task 5: Implement the hash gate and installed-value substitution

**Files:**
- Modify: `cmd/aide/sync.go`
- Test: `cmd/aide/sync_test.go`

**Interfaces:**
- Consumes: `contextSecretsHash` (Task 3), `provision.ConfigHash` (existing), `config.IsTemplate(s string) bool` (`internal/config/template.go:49`, already exists), `provision.Installed`, `provision.ContextState` (existing).
- Produces: `secretsGateOK(env *provisionEnv, cs provision.ContextState) (bool, error)`, `mcpTemplatesSatisfiedByInstalled(desired *provision.Desired, installed provision.Installed) bool`, `substituteFromInstalled(desired *provision.Desired, installed provision.Installed)`, and the updated `resolveMCPSecretsForSync(env *provisionEnv, desired *provision.Desired, installed provision.Installed, cs provision.ContextState, force bool) error` (signature changes from Task 3/4's two-argument form).

This task's tests rely on directly seeding `theFakeProv.mcpInstalled` and a hand-built `provision.ManagedState` to simulate "a previous sync already succeeded," rather than requiring two real `runSyncCmd` invocations to chain state — `fakeMCPHandler.Write` (`cmd/aide/provision_list_test.go:79-90`) does not write back into `theFakeProv.mcpInstalled`, so a real prior run's output isn't visible to a second run in this test harness. Seeding state directly is simpler and exercises the same code path.

- [ ] **Step 1: Write the failing tests**

Add to `cmd/aide/sync_test.go`:

```go
// setupSecretGateFixture writes a config.yaml declaring one MCP server
// ("github") whose env references {{ .secrets.token }}, with context
// "work" carrying `secret: <secretsFile>`. It also seeds state as if a
// previous sync already ran successfully and installed the server with
// installedTokenValue (empty string means "key missing from installed",
// for the fallback-to-decrypt test). Returns the home dir and the
// secrets file path.
func setupSecretGateFixture(t *testing.T, secretsBytes []byte, installedTokenValue string, hasInstalledKey bool) (home, secretsPath string) {
	t.Helper()
	fakeProvReset(t)
	theFakeProv.RequireTTY = false
	home = isolatedConfigDir(t)
	cwd, _ := os.Getwd()

	secretsPath = filepath.Join(home, "secret.enc.yaml")
	if err := os.WriteFile(secretsPath, secretsBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	cfgYAML := fmt.Sprintf(`mcp_servers:
  github:
    command: github-mcp-server
    env:
      TOKEN: "{{ .secrets.token }}"
contexts:
  work:
    agent: fakeagent
    secret: %s
    match:
      - path: %s
    mcp_servers:
      - github
`, secretsPath, cwd)
	cfgPath := filepath.Join(home, "xdg", "aide", "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	installedEnv := map[string]string{}
	if hasInstalledKey {
		installedEnv["TOKEN"] = installedTokenValue
	}
	theFakeProv.mcpInstalled = map[string]provision.MCPServer{
		"github": {Command: "github-mcp-server", Env: installedEnv},
	}

	configHash, err := provision.ConfigHash(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	secretsHash, err := provision.ConfigHash(secretsPath)
	if err != nil {
		t.Fatal(err)
	}
	st := &provision.ManagedState{
		Version: provision.StateVersion,
		Contexts: map[string]*provision.ContextState{
			"work": {
				ConfigHash:  configHash,
				SecretsHash: secretsHash,
				MCPServers:  map[string]provision.ManagedItem{"github": {}},
			},
		},
	}
	if err := provision.SaveState(provision.DefaultStatePath(home), st); err != nil {
		t.Fatal(err)
	}

	// No age identity discoverable: any real decrypt attempt must fail,
	// which is exactly how these tests detect "did sync try to decrypt".
	t.Setenv("PATH", "")
	t.Setenv("SOPS_AGE_KEY", "")
	t.Setenv("SOPS_AGE_KEY_FILE", "")

	return home, secretsPath
}

func TestSync_SkipsDecryptWhenNothingChanged(t *testing.T) {
	setupSecretGateFixture(t, []byte("dummy-ciphertext-v1"), "already-correct-token", true)

	out, err := runSyncCmd(t, "", "--context", "work", "--yes")
	if err != nil {
		t.Fatalf("expected sync to succeed without decrypting (no age key available), got: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Nothing to apply") {
		t.Errorf("expected a no-op plan (installed already matches), got:\n%s", out)
	}
}

func TestSync_DecryptsWhenSecretsFileChanged(t *testing.T) {
	_, secretsPath := setupSecretGateFixture(t, []byte("dummy-ciphertext-v1"), "already-correct-token", true)
	// Rotate: change the encrypted file's bytes without touching config.yaml.
	if err := os.WriteFile(secretsPath, []byte("dummy-ciphertext-v2-rotated"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := runSyncCmd(t, "", "--context", "work", "--yes")
	if err == nil {
		t.Fatal("expected sync to attempt decryption (secrets file changed) and fail with no age key available")
	}
	if !strings.Contains(err.Error(), "age key") {
		t.Errorf("expected an age-key discovery error, got: %v", err)
	}
}

func TestSync_DecryptsWhenConfigChanged(t *testing.T) {
	home, _ := setupSecretGateFixture(t, []byte("dummy-ciphertext-v1"), "already-correct-token", true)
	// Change config.yaml (add an unrelated declared plugin) without
	// touching the secrets file.
	cfgPath := filepath.Join(home, "xdg", "aide", "config.yaml")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, append(data, []byte("plugins:\n  extra: \"extra@1.0\"\n")...), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = runSyncCmd(t, "", "--context", "work", "--yes")
	if err == nil {
		t.Fatal("expected sync to attempt decryption (config.yaml changed) and fail with no age key available")
	}
	if !strings.Contains(err.Error(), "age key") {
		t.Errorf("expected an age-key discovery error, got: %v", err)
	}
}

func TestSync_ForceSecretsBypassesGate(t *testing.T) {
	setupSecretGateFixture(t, []byte("dummy-ciphertext-v1"), "already-correct-token", true)

	_, err := runSyncCmd(t, "", "--context", "work", "--yes", "--force-secrets")
	if err == nil {
		t.Fatal("expected --force-secrets to force a decrypt attempt and fail with no age key available")
	}
	if !strings.Contains(err.Error(), "age key") {
		t.Errorf("expected an age-key discovery error, got: %v", err)
	}
}

func TestSync_FallsBackToDecryptWhenInstalledMissingKey(t *testing.T) {
	// Installed server exists but is missing the TOKEN key entirely
	// (e.g. manually deleted) — substitution has nothing to copy, so
	// sync must fall back to a real decrypt rather than silently
	// leaving the template unresolved.
	setupSecretGateFixture(t, []byte("dummy-ciphertext-v1"), "", false)

	_, err := runSyncCmd(t, "", "--context", "work", "--yes")
	if err == nil {
		t.Fatal("expected fallback to decrypt (installed missing templated key) and fail with no age key available")
	}
	if !strings.Contains(err.Error(), "age key") {
		t.Errorf("expected an age-key discovery error, got: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/aide/... -run 'TestSync_SkipsDecryptWhenNothingChanged|TestSync_DecryptsWhenSecretsFileChanged|TestSync_DecryptsWhenConfigChanged|TestSync_ForceSecretsBypassesGate|TestSync_FallsBackToDecryptWhenInstalledMissingKey' -v`
Expected: `TestSync_SkipsDecryptWhenNothingChanged` FAILs (today's code always decrypts, so it errors with "no age identity found" even though nothing changed); the other four currently PASS already (today's code always decrypts regardless of the gate, so they already fail as expected) — that's fine, they'll stay green through Step 3; the meaningful new coverage is the first test.

- [ ] **Step 3: Implement the gate and substitution**

In `cmd/aide/sync.go`, add:

```go
// secretsGateOK reports whether ctx's config.yaml and secrets file are
// both byte-identical to the last successful sync recorded in cs — the
// only two things that can make a secret-templated MCP env value need
// re-resolution (see docs/superpowers/specs/2026-09-06-sync-secrets-hash-gate-design.md).
// When true, resolveMCPSecretsForSync can skip decryption entirely and
// substitute already-installed values instead.
func secretsGateOK(env *provisionEnv, cs provision.ContextState) (bool, error) {
	configHash, err := provision.ConfigHash(config.FilePath())
	if err != nil {
		return false, err
	}
	secretsHash, err := contextSecretsHash(env.ctx)
	if err != nil {
		return false, err
	}
	return configHash == cs.ConfigHash && secretsHash == cs.SecretsHash, nil
}

// mcpTemplatesSatisfiedByInstalled reports whether every {{ }}-templated
// MCP env value in desired has a matching key already present in
// installed — the precondition for skipping decryption safely. Pure
// check, no mutation: callers must not apply a partial substitution
// when this returns false, since the fallback decrypt path needs the
// original {{ .secrets.X }} literals intact to re-resolve everything
// from scratch (see substituteFromInstalled).
func mcpTemplatesSatisfiedByInstalled(desired *provision.Desired, installed provision.Installed) bool {
	for name, server := range desired.MCPServers {
		if len(server.Env) == 0 {
			continue
		}
		inst, instOK := installed.MCPServers[name]
		for k, v := range server.Env {
			if !config.IsTemplate(v) {
				continue
			}
			if !instOK {
				return false
			}
			if _, exists := inst.Env[k]; !exists {
				return false
			}
		}
	}
	return true
}

// substituteFromInstalled fills every {{ }}-templated MCP env value in
// desired with the matching key's value from installed, mutating
// desired in place. Callers must only call this after
// mcpTemplatesSatisfiedByInstalled has returned true for the same
// (desired, installed) pair — it does not re-check.
func substituteFromInstalled(desired *provision.Desired, installed provision.Installed) {
	for name, server := range desired.MCPServers {
		if len(server.Env) == 0 {
			continue
		}
		inst := installed.MCPServers[name]
		for k, v := range server.Env {
			if config.IsTemplate(v) {
				server.Env[k] = inst.Env[k]
			}
		}
		desired.MCPServers[name] = server
	}
}
```

Change `resolveMCPSecretsForSync`'s signature and body from:

```go
func resolveMCPSecretsForSync(env *provisionEnv, desired *provision.Desired) error {
	var td *config.TemplateData
	if env.ctx.Secret != "" {
		secretsPath := config.ResolveSecretPath(env.ctx.Secret)
		secretsMap, err := secrets.LoadSecretsMap(secretsPath)
		if err != nil {
			return fmt.Errorf("resolving secrets for context %q: %w", env.contextName, err)
		}
		cwd, _ := os.Getwd()
		td = &config.TemplateData{
			Secrets:     secretsMap,
			ProjectRoot: cwd,
		}
	}
	return provision.ResolveSecretsInMCPEnv(desired, td)
}
```

to:

```go
// resolveMCPSecretsForSync resolves {{ .secrets.X }} placeholders in
// desired's MCP server env maps so the plan diff compares already-
// resolved values against the agent's installed state. See
// docs/superpowers/specs/2026-09-06-sync-secrets-hash-gate-design.md.
//
// When force is false and secretsGateOK holds, decryption is skipped
// entirely: every templated env key is filled in from installed
// instead — sound because an unchanged config+secrets pair guarantees
// today's true resolved value is exactly what the last successful sync
// already applied. If any templated key has no installed counterpart
// (e.g. manually deleted), that guarantee doesn't hold and the code
// falls back to decrypting for real.
func resolveMCPSecretsForSync(env *provisionEnv, desired *provision.Desired, installed provision.Installed, cs provision.ContextState, force bool) error {
	if !force {
		ok, err := secretsGateOK(env, cs)
		if err != nil {
			return err
		}
		if ok && mcpTemplatesSatisfiedByInstalled(desired, installed) {
			substituteFromInstalled(desired, installed)
			return nil
		}
	}

	var td *config.TemplateData
	if env.ctx.Secret != "" {
		secretsPath := config.ResolveSecretPath(env.ctx.Secret)
		secretsMap, err := secrets.LoadSecretsMap(secretsPath)
		if err != nil {
			return fmt.Errorf("resolving secrets for context %q: %w", env.contextName, err)
		}
		cwd, _ := os.Getwd()
		td = &config.TemplateData{
			Secrets:     secretsMap,
			ProjectRoot: cwd,
		}
	}
	return provision.ResolveSecretsInMCPEnv(desired, td)
}
```

Update the call site in `runSync` (from Task 4) — change:

```go
	_ = forceSecrets // used starting Task 5
	if err := resolveMCPSecretsForSync(env, &desired); err != nil {
		return err
	}
```

to:

```go
	if err := resolveMCPSecretsForSync(env, &desired, installed, managedCtxState, forceSecrets); err != nil {
		return err
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/aide/... -run 'TestSync_SkipsDecryptWhenNothingChanged|TestSync_DecryptsWhenSecretsFileChanged|TestSync_DecryptsWhenConfigChanged|TestSync_ForceSecretsBypassesGate|TestSync_FallsBackToDecryptWhenInstalledMissingKey' -v`
Expected: All PASS.

- [ ] **Step 5: Run the full test suite**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: PASS across the whole module — this change only touches `cmd/aide` and the two packages from Tasks 1-2, all of which have their own passing suites already confirmed in earlier tasks.

- [ ] **Step 6: Commit**

```bash
git add cmd/aide/sync.go cmd/aide/sync_test.go
git commit -m "Skip secret decryption in aide sync when config and secrets are unchanged"
```

---

## Post-Implementation Note

Update the "Known Limitations" section of `docs/superpowers/specs/2026-09-06-sync-secrets-hash-gate-design.md` is already accurate as written — no changes needed there. Consider a short mention in `docs/secrets.md` (if it documents `aide sync`'s behavior around secrets) that routine syncs no longer require the age key when nothing changed, and that `--force-secrets` exists as an escape hatch — check that file's current content before deciding whether it needs an update.
