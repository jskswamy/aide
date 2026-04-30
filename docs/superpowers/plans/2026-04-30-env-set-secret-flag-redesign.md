# env set Secret Flag Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the broken `--from-secret` flag on `aide env set` with `--secret-key`, `--secret-store`, and `--pick`; drop the `NoOptDefVal` parser quirk and the silent `ctx.Secret` auto-bind; back the change with cobra-level parsing tests.

**Architecture:** All changes are scoped to the `env set` command and its callers' user-facing strings. The handler in `cmd/aide/env.go` gains explicit secret-related flags and stricter validation. Two small package-level function variables (`discoverAgeKey`, `decryptSecretsFile`) become test seams so the handler's secret-resolution path can be exercised without real SOPS encryption. Internal docs and error hints that mention `--from-secret` are rewritten in the same change.

**Tech Stack:** Go, cobra/pflag, existing `internal/secrets` package. No new dependencies.

---

## Spec

See `docs/superpowers/specs/2026-04-30-env-set-secret-flag-redesign.md`.

## File Map

- **Modify:** `cmd/aide/env.go` — flag definitions, validation, secret resolution path, help text
- **Modify:** `cmd/aide/secrets.go:121` — `Tip:` line printed after secret edits
- **Modify:** `internal/launcher/launcher.go:387,394,402` — error-hint strings
- **Modify:** `docs/environment.md`, `docs/cli-reference.md` — replace `--from-secret` references
- **Create:** `cmd/aide/env_set_test.go` — cobra-level parsing/validation tests
- **Create:** `cmd/aide/examples_test.go` — examples-as-tests guard

---

## Task 1: Introduce test seams in env.go

The handler currently calls `secrets.DiscoverAgeKey()` and `secrets.DecryptSecretsFile()` directly. To make happy-path tests possible without real age encryption, swap each call to a package-level `var` that tests can override.

**Files:**
- Modify: `cmd/aide/env.go:139-143`

- [ ] **Step 1: Add package-level function variables**

At the top of `cmd/aide/env.go`, just below the imports, add:

```go
// Test seams. Production code uses the real implementations; tests
// override these to avoid real SOPS encryption.
var (
	discoverAgeKey     = secrets.DiscoverAgeKey
	decryptSecretsFile = secrets.DecryptSecretsFile
)
```

- [ ] **Step 2: Replace direct calls in envSetCmd**

In `envSetCmd().RunE`, replace:

```go
identity, err := secrets.DiscoverAgeKey()
...
decrypted, err := secrets.DecryptSecretsFile(secretsFilePath, identity)
```

with:

```go
identity, err := discoverAgeKey()
...
decrypted, err := decryptSecretsFile(secretsFilePath, identity)
```

(There is exactly one call site for each in env.go.)

- [ ] **Step 3: Verify it still builds**

Run: `go build ./...`
Expected: success.

- [ ] **Step 4: Verify existing tests still pass**

Run: `go test ./cmd/aide/... ./internal/...`
Expected: all green (this is a pure refactor — no behavior change yet).

- [ ] **Step 5: Commit**

```bash
git add cmd/aide/env.go
git commit -m "Introduce test seams for env.go secret resolution"
```

---

## Task 2: Add parsing test harness with red tests for the new flag matrix

Create the cobra-level test file. Most cases assert on the error returned by `cmd.Execute()`. Happy-path secret tests use the seams from Task 1.

**Files:**
- Create: `cmd/aide/env_set_test.go`

- [ ] **Step 1: Write the failing tests**

Create `cmd/aide/env_set_test.go`:

```go
// cmd/aide/env_set_test.go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/secrets"
)

// runEnvSet builds a fresh envSetCmd, redirects output, and runs it
// with the given args. It returns combined stdout+stderr and any error.
func runEnvSet(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := envSetCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

// stubSecretSeams replaces the package-level seams for the duration of a test.
// The cleanup is registered via t.Cleanup.
func stubSecretSeams(t *testing.T, decrypted map[string]string) {
	t.Helper()
	origDiscover := discoverAgeKey
	origDecrypt := decryptSecretsFile
	discoverAgeKey = func() (*secrets.AgeIdentity, error) {
		return &secrets.AgeIdentity{}, nil
	}
	decryptSecretsFile = func(_ string, _ *secrets.AgeIdentity) (map[string]string, error) {
		return decrypted, nil
	}
	t.Cleanup(func() {
		discoverAgeKey = origDiscover
		decryptSecretsFile = origDecrypt
	})
}

// projectTempDir creates a tempdir with an empty .aide/project.yaml and
// chdirs into it so cmdEnv resolves to a writable project override.
func projectTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.MkdirAll(filepath.Join(dir, ".aide"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestEnvSet_LiteralValue_WritesProjectOverride(t *testing.T) {
	projectTempDir(t)
	out, err := runEnvSet(t, "FOO", "bar")
	if err != nil {
		t.Fatalf("unexpected error: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Set FOO in project") {
		t.Errorf("expected success message, got: %s", out)
	}
}

func TestEnvSet_NoValueNoFlag_Errors(t *testing.T) {
	projectTempDir(t)
	_, err := runEnvSet(t, "FOO")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "must specify VALUE, --secret-key, or --pick") {
		t.Errorf("wrong error: %v", err)
	}
}

func TestEnvSet_LiteralAndSecretKey_Errors(t *testing.T) {
	projectTempDir(t)
	_, err := runEnvSet(t, "FOO", "bar", "--secret-key", "api_key")
	if err == nil || !strings.Contains(err.Error(), "cannot specify both") {
		t.Errorf("expected mutual-exclusion error, got: %v", err)
	}
}

func TestEnvSet_SecretKeyAndPick_Errors(t *testing.T) {
	projectTempDir(t)
	_, err := runEnvSet(t, "FOO", "--secret-key", "api_key", "--pick")
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("expected mutual-exclusion error, got: %v", err)
	}
}

func TestEnvSet_SecretKey_RequiresGlobal(t *testing.T) {
	projectTempDir(t)
	_, err := runEnvSet(t, "FOO", "--secret-key", "api_key")
	if err == nil || !strings.Contains(err.Error(), "--global") {
		t.Errorf("expected --global hint, got: %v", err)
	}
}

func TestEnvSet_FromSecret_UnknownFlag(t *testing.T) {
	projectTempDir(t)
	// Both space and = forms should fail since the flag is removed.
	for _, form := range [][]string{
		{"FOO", "--from-secret=api_key"},
		{"FOO", "--from-secret", "api_key"},
	} {
		_, err := runEnvSet(t, form...)
		if err == nil || !strings.Contains(err.Error(), "unknown flag") {
			t.Errorf("args %v: expected unknown-flag error, got: %v", form, err)
		}
	}
}

func TestEnvSet_SecretKey_SpaceForm_Parses(t *testing.T) {
	// The whole point of the redesign: space form must work.
	projectTempDir(t)
	_, err := runEnvSet(t, "FOO", "--secret-key", "api_key", "--global")
	// We don't expect success without a configured context+store, but
	// the error must NOT be "cannot specify both a value argument..."
	// (the old NoOptDefVal symptom).
	if err != nil && strings.Contains(err.Error(), "cannot specify both a value argument") {
		t.Errorf("space form still misparsed: %v", err)
	}
}

func TestEnvSet_SecretKey_NoStoreBound_Errors(t *testing.T) {
	projectTempDir(t)
	_, err := runEnvSet(t, "FOO", "--secret-key", "api_key", "--global")
	if err == nil || !strings.Contains(err.Error(), "no secret store bound") {
		t.Errorf("expected no-store-bound error, got: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they all fail (red bar)**

Run: `go test ./cmd/aide/ -run TestEnvSet -v`
Expected: every test fails. The flag set hasn't been changed yet, so `--secret-key` is unknown and `--from-secret` still exists. This confirms the harness works and the surface area is uncovered.

- [ ] **Step 3: Commit the red tests**

```bash
git add cmd/aide/env_set_test.go
git commit -m "Add cobra parsing tests for env set flag matrix"
```

---

## Task 3: Replace --from-secret with --secret-key, --secret-store, --pick

The substantive change. Drop `NoOptDefVal`. Drop the `ctx.Secret` auto-bind. Rewrite validation and resolution. Update help text.

**Files:**
- Modify: `cmd/aide/env.go` (the entire `envSetCmd` function)

- [ ] **Step 1: Rewrite envSetCmd**

Replace the existing `envSetCmd` function (env.go:34-181) with:

```go
func envSetCmd() *cobra.Command {
	var (
		secretKey   string
		secretStore string
		pick        bool
		contextName string
		global      bool
	)

	cmd := &cobra.Command{
		Use:   "set KEY [VALUE]",
		Short: "Set an environment variable (project-level by default)",
		Long: `Set an environment variable on a context.

Examples:
  aide env set ANTHROPIC_API_KEY sk-ant-xxx                              # literal value
  aide env set ANTHROPIC_API_KEY --secret-key api_key --global           # key in bound store
  aide env set ANTHROPIC_API_KEY --secret-store firmus --secret-key api_key --global
  aide env set ANTHROPIC_API_KEY --pick --global                         # interactive picker
  aide env set OPENAI_API_KEY --secret-key key --context work --global`,
		Args:         cobra.RangeArgs(1, 2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			hasValueArg := len(args) == 2
			out := cmd.OutOrStdout()
			reader := bufio.NewReader(os.Stdin)

			useSecret := secretKey != "" || pick || secretStore != ""

			// Mutual-exclusion checks.
			if hasValueArg && useSecret {
				return fmt.Errorf("cannot specify both a literal VALUE and a secret flag (--secret-key/--secret-store/--pick)")
			}
			if !hasValueArg && !useSecret {
				return fmt.Errorf("must specify VALUE, --secret-key, or --pick")
			}
			if secretKey != "" && pick {
				return fmt.Errorf("--secret-key and --pick are mutually exclusive")
			}
			if !global && contextName != "" {
				return fmt.Errorf("the --context flag requires --global")
			}
			if useSecret && !global {
				return fmt.Errorf("secret references require --global (secrets are context-scoped)")
			}

			// Project path: literal KEY VALUE only.
			if !global {
				value := args[1]
				_, po, poPath, err := resolveProjectOverrideForMutation()
				if err != nil {
					return err
				}
				if po.Env == nil {
					po.Env = make(map[string]string)
				}
				po.Env[key] = value
				if err := config.WriteProjectOverrideWithTrust(poPath, po, trust.DefaultStore()); err != nil {
					return fmt.Errorf("writing project config: %w", err)
				}
				fmt.Fprintf(out, "Set %s in project (%s)\n", key, poPath)
				return nil
			}

			// Global path.
			env, err := cmdEnv(cmd)
			if err != nil {
				return err
			}
			cwd := env.CWD()
			cfg := env.Config()

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
			if useSecret {
				// Resolve store: explicit flag, else context binding. No auto-bind.
				store := secretStore
				if store == "" {
					store = ctx.Secret
				}
				if store == "" {
					return fmt.Errorf(
						"no secret store bound to context %q.\n"+
							"Pass --secret-store <name>, or run: aide context set-secret <name> --context %s --global",
						targetName, targetName,
					)
				}

				// Resolve key.
				resolvedKey := secretKey
				if pick {
					secretsFilePath := config.ResolveSecretPath(store)
					picked, err := selectSecretKey(out, reader, secretsFilePath)
					if err != nil {
						return err
					}
					resolvedKey = picked
				} else {
					// Validate the key exists in the store now, surfacing a
					// helpful error before writing the template.
					secretsFilePath := config.ResolveSecretPath(store)
					identity, err := discoverAgeKey()
					if err != nil {
						return err
					}
					decrypted, err := decryptSecretsFile(secretsFilePath, identity)
					if err != nil {
						return err
					}
					if _, ok := decrypted[resolvedKey]; !ok {
						available := make([]string, 0, len(decrypted))
						for k := range decrypted {
							available = append(available, k)
						}
						sort.Strings(available)
						return fmt.Errorf("key %q not found in %s.\nAvailable keys: %s",
							resolvedKey, store, strings.Join(available, ", "))
					}
				}
				value = fmt.Sprintf("{{ .secrets.%s }}", resolvedKey)
			} else {
				value = args[1]
			}

			if ctx.Env == nil {
				ctx.Env = make(map[string]string)
			}
			ctx.Env[key] = value
			cfg.Contexts[targetName] = ctx

			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}
			fmt.Fprintf(out, "Set %s on context %q.\n", key, targetName)
			return nil
		},
	}

	cmd.Flags().StringVar(&secretKey, "secret-key", "", "Key inside the secret store to reference")
	cmd.Flags().StringVar(&secretStore, "secret-store", "", "Secret store name (defaults to context's bound store)")
	cmd.Flags().BoolVar(&pick, "pick", false, "Interactively pick a key from the resolved store")
	cmd.Flags().StringVar(&contextName, "context", "", "Target context (default: CWD-matched)")
	cmd.Flags().BoolVar(&global, "global", false, "Apply to user-level config instead of project")
	return cmd
}
```

Key differences from the old code:
- No `NoOptDefVal`.
- Three explicit flags (`--secret-key`, `--secret-store`, `--pick`) replace the overloaded `--from-secret`.
- The `ctx.Secret == ""` auto-select/persist block is gone — replaced by an explicit error pointing at `aide context set-secret`.
- `selectSecret` (the store picker) is no longer called from `env set`; that helper stays in the file because nothing else has changed about it (other callers still use it).

- [ ] **Step 2: Verify the helpful but now-unused selectSecret func is still referenced**

Run: `grep -n "selectSecret\b" cmd/aide/`
Expected: at least one other caller (or none, in which case it can be deleted). If unused, delete `selectSecret` and remove the unused helper to keep the file clean.

- [ ] **Step 3: Run the parsing tests**

Run: `go test ./cmd/aide/ -run TestEnvSet -v`
Expected: all green.

- [ ] **Step 4: Run the full test suite**

Run: `go test ./...`
Expected: green. If any test references `--from-secret` or the old behavior, it will surface here.

- [ ] **Step 5: Commit**

```bash
git add cmd/aide/env.go
git commit -m "Replace --from-secret with --secret-key, --secret-store, --pick"
```

---

## Task 4: Add examples-as-tests guard

Walk every `cobra.Command` registered under the root command, parse the `Long:` field for `aide ...` lines, and assert each parses without a flag-or-arg error against the matching command. This catches future drift between help and implementation.

**Files:**
- Create: `cmd/aide/examples_test.go`

The codebase already has `registerCommands(rootCmd *cobra.Command)` in `cmd/aide/commands.go`, which adds every subcommand. Tests can build a bare root and call it directly — no main.go change needed.

- [ ] **Step 1: Write the test**

Create `cmd/aide/examples_test.go`:

```go
// cmd/aide/examples_test.go
package main

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// exampleLine matches lines that start with `aide ` (after optional
// leading whitespace). Trailing comments after `#` are stripped.
var exampleLine = regexp.MustCompile(`^\s*aide\s+(.+)$`)

func walkCommands(c *cobra.Command, fn func(*cobra.Command)) {
	fn(c)
	for _, child := range c.Commands() {
		walkCommands(child, fn)
	}
}

// buildTestRoot returns a cobra root command with every subcommand
// registered. It reuses production registerCommands() so the test surface
// is exactly main()'s subcommand tree.
func buildTestRoot() *cobra.Command {
	root := &cobra.Command{Use: "aide"}
	registerCommands(root)
	return root
}

func TestHelpExamplesParse(t *testing.T) {
	root := buildTestRoot()

	walkCommands(root, func(c *cobra.Command) {
		if c.Long == "" {
			return
		}
		for _, raw := range strings.Split(c.Long, "\n") {
			m := exampleLine.FindStringSubmatch(raw)
			if m == nil {
				continue
			}
			// Strip trailing # comment.
			line := m[1]
			if idx := strings.Index(line, "#"); idx >= 0 {
				line = strings.TrimSpace(line[:idx])
			}
			fields := strings.Fields(line)
			if len(fields) == 0 {
				continue
			}

			t.Run(line, func(t *testing.T) {
				// Build a fresh root per example to avoid flag-state bleed.
				freshRoot := buildTestRoot()
				freshRoot.SetArgs(append(fields, "--help"))
				var buf bytes.Buffer
				freshRoot.SetOut(&buf)
				freshRoot.SetErr(&buf)
				// --help short-circuits the handler, so this only validates
				// the command path and flag names are recognized.
				if err := freshRoot.Execute(); err != nil {
					t.Errorf("example %q failed to parse: %v\noutput: %s", line, err, buf.String())
				}
			})
		}
	})
}
```

Note: this guard doesn't run the example with real values (which would require fixtures). It runs the command with `--help` appended, which exercises cobra's argument and flag-name parsing without invoking the handler. That catches the class of bug we just hit (`--from-secret` not being a real flag, `--secret-key VALUE` being misparsed, etc.) without coupling the test to runtime state.

- [ ] **Step 2: Confirm the test passes for the new help text**

Run: `go test ./cmd/aide/ -run TestHelpExamplesParse -v`
Expected: green. The new examples in env.go's `Long:` use `--secret-key`, `--secret-store`, `--pick`, all of which exist.

- [ ] **Step 3: Confirm the test would have caught the original bug**

Temporarily revert env.go's help to use `--from-secret api_key` and re-run.
Expected: failure. Restore the new help.

(Optional but worth doing once to verify the guard is real.)

- [ ] **Step 4: Commit**

```bash
git add cmd/aide/examples_test.go
git commit -m "Guard against drift between help examples and CLI surface"
```

---

## Task 5: Update internal user-facing strings

Three call sites still print `--from-secret` to users.

**Files:**
- Modify: `cmd/aide/secrets.go:121`
- Modify: `internal/launcher/launcher.go:387,394,402`

- [ ] **Step 1: Update secrets.go tip**

In `cmd/aide/secrets.go`, change:

```go
fmt.Fprintf(out, "  aide env set MY_VAR --from-secret %s\n", k)
```

to:

```go
fmt.Fprintf(out, "  aide env set MY_VAR --secret-key %s --global\n", k)
```

- [ ] **Step 2: Update launcher.go error hints**

In `internal/launcher/launcher.go`, change all three occurrences of:

```
Fix with: aide env set <KEY> --from-secret
```

(and the `Re-wire:` variant) to:

```
Fix with: aide env set <KEY> --secret-key <KEY_NAME> --global
```

The full diff in `wrapTemplateError`:

```go
// First branch: secret == ""
return fmt.Errorf(
    "context %q references secrets in env vars but has no secret configured.\n\n"+
        "Fix with: aide context set-secret <name> --context %s --global",
    contextName, contextName,
)

// Second branch: key not found
return fmt.Errorf(
    "context %q: secret key not found in %s.\n\n"+
        "Available keys: aide secrets keys %s\n"+
        "Re-wire:        aide env set <KEY> --secret-key <KEY_NAME> --global",
    contextName, secret, secret,
)

// Third branch: nil pointer / can't evaluate
return fmt.Errorf(
    "context %q references secrets but has no secret configured.\n\n"+
        "Fix with: aide context set-secret <name> --context %s --global",
    contextName, contextName,
)
```

The first and third branches now point at `aide context set-secret` because that's where users bind a store to a context — which is exactly what's missing in those error scenarios.

- [ ] **Step 3: Run launcher tests**

Run: `go test ./internal/launcher/...`
Expected: green. If any test asserts on the exact error string, update it to match the new wording.

- [ ] **Step 4: Verify no other --from-secret references remain in non-doc code**

Run: `grep -rn "from-secret" --include="*.go" .`
Expected: zero matches.

- [ ] **Step 5: Commit**

```bash
git add cmd/aide/secrets.go internal/launcher/launcher.go internal/launcher/launcher_test.go
git commit -m "Rewrite --from-secret hints to use new flag set"
```

---

## Task 6: Sweep documentation

Replace `--from-secret` references in user-facing docs.

**Files:**
- Modify: `docs/environment.md`
- Modify: `docs/cli-reference.md`

- [ ] **Step 1: Read each file and rewrite the affected sections**

`docs/environment.md` (around line 65) currently has a `## The --from-secret Flag` section with examples like:

```
aide env set ANTHROPIC_API_KEY --from-secret anthropic_key
aide env set ANTHROPIC_API_KEY --from-secret anthropic_key --context work
aide env set ANTHROPIC_API_KEY --from-secret
```

Rewrite the section heading to `## Referencing Secrets in Env Vars` and update examples to:

```
aide env set ANTHROPIC_API_KEY --secret-key anthropic_key --global
aide env set ANTHROPIC_API_KEY --secret-key anthropic_key --context work --global
aide env set ANTHROPIC_API_KEY --pick --global
```

Add a sentence noting that the context must already have a secret store bound (via `aide context set-secret`) unless `--secret-store` is passed explicitly.

`docs/cli-reference.md` (around line 426) has a flag table entry for `--from-secret [key]`. Replace with three rows:

| Flag | Description |
| ---- | ----------- |
| `--secret-key <key>` | Reference a key inside the bound (or explicit) secret store |
| `--secret-store <name>` | Override which secret store to read from (without changing the context binding) |
| `--pick` | Interactively pick a key from the resolved store |

Update the example below the table to:

```
aide env set ANTHROPIC_API_KEY --secret-key api_key --context work --global
```

- [ ] **Step 2: Verify no --from-secret references remain in docs**

Run: `grep -rn "from-secret" docs/ README.md 2>/dev/null`
Expected: zero matches.

- [ ] **Step 3: Commit**

```bash
git add docs/environment.md docs/cli-reference.md
git commit -m "Update docs for env set secret flag redesign"
```

---

## Task 7: Final verification

- [ ] **Step 1: Full test suite**

Run: `go test ./...`
Expected: green.

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 3: Run preflight if available**

Run: `make preflight 2>/dev/null || go vet ./...`
Expected: green.

- [ ] **Step 4: Manual smoke test**

```bash
go run ./cmd/aide env set --help
```

Expected output: examples show `--secret-key`, `--secret-store`, `--pick`. No `--from-secret`.

```bash
go run ./cmd/aide env set FOO --secret-key bar
```

Expected: clear error about `--global` requirement. **Crucially:** not "cannot specify both a value argument and --from-secret".

```bash
go run ./cmd/aide env set FOO --from-secret bar
```

Expected: `Error: unknown flag: --from-secret`.

- [ ] **Step 5: No final commit** — Task 6 was the last commit. Push at session end per the SessionStart protocol.
