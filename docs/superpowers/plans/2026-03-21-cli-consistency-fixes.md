# CLI Consistency Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix three CLI consistency issues: context auto-detection, declarative/imperative parity, and output/error patterns.

**Architecture:** All changes are in `cmd/aide/commands.go`. Reuse the existing `resolveContextForMutation` helper for auto-detection. No new packages or structs needed.

**Tech Stack:** Go, cobra CLI

---

### Task 1: Context Auto-Detection for set-secret, remove-secret, add-match

**Files:**
- Modify: `cmd/aide/commands.go`

These three commands currently require an explicit context name as a positional arg. Refactor them to use `--context` flag with CWD auto-detection (same pattern as `sandbox deny`, `sandbox allow`, etc., which use `resolveContextForMutation`).

- [ ] **Step 1: Refactor contextSetSecretCmd**

Change from `aide context set-secret <context-name> <secret-name>` to `aide context set-secret <secret-name> [--context name]`.

```go
func contextSetSecretCmd() *cobra.Command {
	var contextName string

	cmd := &cobra.Command{
		Use:          "set-secret <secret-name>",
		Short:        "Set the secret on the current context",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			secretName := args[0]

			cfg, ctxName, ctx, err := resolveContextForMutation(contextName)
			if err != nil {
				return err
			}

			// Warn if secret file doesn't exist on disk
			resolvedPath := config.ResolveSecretPath(secretName)
			if _, err := os.Stat(resolvedPath); os.IsNotExist(err) {
				fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %s does not exist yet.\n", resolvedPath)
			}

			ctx.Secret = secretName
			cfg.Contexts[ctxName] = ctx

			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Set secret %q on context %q\n", secretName, ctxName)
			return nil
		},
	}

	cmd.Flags().StringVar(&contextName, "context", "", "Target context (default: CWD-matched)")
	return cmd
}
```

- [ ] **Step 2: Refactor contextRemoveSecretCmd**

Change from `aide context remove-secret <context-name>` to `aide context remove-secret [--context name]`.

```go
func contextRemoveSecretCmd() *cobra.Command {
	var contextName string

	cmd := &cobra.Command{
		Use:          "remove-secret",
		Short:        "Remove the secret from the current context",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, ctxName, ctx, err := resolveContextForMutation(contextName)
			if err != nil {
				return err
			}

			oldSecret := ctx.Secret
			if oldSecret == "" {
				return fmt.Errorf("context %q has no secret configured", ctxName)
			}

			// Warn if env vars reference secrets templates
			for envKey, envVal := range ctx.Env {
				if strings.Contains(envVal, ".secrets.") {
					fmt.Fprintf(cmd.ErrOrStderr(), "Warning: env var %q references secrets templates\n", envKey)
				}
			}

			ctx.Secret = ""
			cfg.Contexts[ctxName] = ctx

			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed secret %q from context %q\n", oldSecret, ctxName)
			return nil
		},
	}

	cmd.Flags().StringVar(&contextName, "context", "", "Target context (default: CWD-matched)")
	return cmd
}
```

- [ ] **Step 3: Refactor contextAddMatchCmd**

Change from `aide context add-match <context-name>` to `aide context add-match [--context name]`.

```go
func contextAddMatchCmd() *cobra.Command {
	var contextName string

	cmd := &cobra.Command{
		Use:          "add-match",
		Short:        "Add a match rule to the current context",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, name, ctx, err := resolveContextForMutation(contextName)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			reader := bufio.NewReader(os.Stdin)

			cwd, cwdErr := os.Getwd()
			if cwdErr != nil {
				cwd = "."
			}

			rule, err := askMatchRule(out, reader, cwd)
			if err != nil {
				return err
			}

			ctx.Match = append(ctx.Match, rule)
			cfg.Contexts[name] = ctx

			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}

			if rule.Path != "" {
				fmt.Fprintf(out, "Added path match to context %q: %s\n", name, rule.Path)
			} else {
				fmt.Fprintf(out, "Added remote match to context %q: %s\n", name, rule.Remote)
			}
			fmt.Fprintln(out, "\nTip: `aide setup` can also do this interactively with more options.")
			return nil
		},
	}

	cmd.Flags().StringVar(&contextName, "context", "", "Target context (default: CWD-matched)")
	return cmd
}
```

- [ ] **Step 4: Build and test**

Run: `go build ./... && go test ./...`

- [ ] **Step 5: Commit**

```
Make context subcommands auto-detect CWD context
```

---

### Task 2: Add Missing CLI Commands for Declarative Parity

**Files:**
- Modify: `cmd/aide/commands.go`

Three missing CLI capabilities: (1) set network mode on context sandbox, (2) set denied_extra on named profile, (3) set readable_extra on named profile.

- [ ] **Step 1: Add sandbox network command**

```go
func sandboxNetworkCmd() *cobra.Command {
	var contextName string
	cmd := &cobra.Command{
		Use:          "network <mode>",
		Short:        "Set network mode for a context's sandbox (outbound|none|unrestricted)",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := args[0]
			validModes := map[string]bool{"outbound": true, "none": true, "unrestricted": true}
			if !validModes[mode] {
				return fmt.Errorf("invalid network mode %q (must be outbound, none, or unrestricted)", mode)
			}
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
			fmt.Fprintf(cmd.OutOrStdout(), "Set network mode to %q for context %q\n", mode, ctxName)
			return nil
		},
	}
	cmd.Flags().StringVar(&contextName, "context", "", "target context name")
	return cmd
}
```

Register in `sandboxCmd()`: `cmd.AddCommand(sandboxNetworkCmd())`

- [ ] **Step 2: Add --add-denied and --add-readable flags to sandbox edit command**

The existing `sandboxEditCmd` already has `--add-denied` and `--remove-denied` but they are `StringSlice` vars. Check current flags:

Current `sandboxEditCmd` flags:
- `--add-denied` ✓ exists
- `--add-writable` ✓ exists
- `--remove-denied` ✓ exists
- `--remove-writable` ✓ exists
- `--network` ✓ exists

Missing:
- `--add-readable` — add this flag
- `--remove-readable` — add this flag

Add to `sandboxEditCmd`:

```go
var addReadable, removeReadable []string

// In the RunE, after the addDenied block:
for _, p := range addReadable {
    expanded := expandHome(p)
    if _, err := os.Stat(expanded); err != nil {
        fmt.Fprintf(out, "  ⚠ %s does not exist (added anyway)\n", p)
    }
    sp.ReadableExtra = append(sp.ReadableExtra, p)
}
for _, p := range removeReadable {
    sp.ReadableExtra = removeFromSlice(sp.ReadableExtra, p)
}

// Register flags:
cmd.Flags().StringSliceVar(&addReadable, "add-readable", nil, "add readable paths")
cmd.Flags().StringSliceVar(&removeReadable, "remove-readable", nil, "remove readable paths")
```

- [ ] **Step 3: Build and test**

Run: `go build ./... && go test ./...`

- [ ] **Step 4: Commit**

```
Add missing CLI commands for declarative parity
```

---

### Task 3: Standardize Output and Error Patterns

**Files:**
- Modify: `cmd/aide/commands.go`

This is a polish pass. Standardize variable naming and confirmation messages.

- [ ] **Step 1: Rename contextFlag to contextName in envSetCmd and envListCmd**

In `envSetCmd`: change `var contextFlag string` to `var contextName string` and update all references.
In `envListCmd`: same rename.

This matches the pattern used by `sandbox deny/allow/reset/ports` and the newly refactored context commands.

- [ ] **Step 2: Standardize confirmation message format**

Adopt this pattern across all mutation commands:
```
<Action> <what> on context "<name>"
```

Check these commands produce consistent output:
- `env set` → `Set KEY on context "name"`  ← already correct
- `sandbox deny` → `Added <path> to denied_extra for context "name"` ← fine
- `sandbox allow` → `Added <path> to <list> for context "name"` ← fine
- `sandbox reset` → `Reset sandbox to defaults for context "name"` ← fine
- `context set-secret` → `Set secret "x" on context "name"` ← fine
- `context remove-secret` → `Removed secret "x" from context "name"` ← fine

These are already mostly consistent. Verify no outliers.

- [ ] **Step 3: Build and test**

Run: `go build ./... && go test ./...`

- [ ] **Step 4: Commit**

```
Standardize CLI variable naming and output patterns
```
