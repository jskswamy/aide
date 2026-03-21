# Env Remove + Default Context UX Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `aide env remove` command and improve default context UX so the transition from minimal to multi-context config doesn't break existing workflows.

**Architecture:** Two independent features in `cmd/aide/commands.go`. The env remove command follows the existing `env set` pattern. The default context changes touch `commands.go` (new command + auto-set logic) and `context list` output formatting.

**Tech Stack:** Go, cobra CLI

---

### Task 1: Add `aide env remove` Command

**Files:**
- Modify: `cmd/aide/commands.go`

- [ ] **Step 1: Register envRemoveCmd in envCmd()**

In the `envCmd()` function, add:
```go
cmd.AddCommand(envRemoveCmd())
```

- [ ] **Step 2: Implement envRemoveCmd**

Add after the `envListCmd` function:

```go
func envRemoveCmd() *cobra.Command {
	var contextName string

	cmd := &cobra.Command{
		Use:          "remove KEY",
		Short:        "Remove an environment variable from a context",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]

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
			fmt.Fprintf(cmd.OutOrStdout(), "Removed %s from context %q\n", key, ctxName)
			return nil
		},
	}

	cmd.Flags().StringVar(&contextName, "context", "", "Target context (default: CWD-matched)")
	return cmd
}
```

- [ ] **Step 3: Build and test**

Run: `go build ./... && go test ./...`

- [ ] **Step 4: Commit**

```
Add aide env remove command
```

---

### Task 2: Add `aide context set-default` Command

**Files:**
- Modify: `cmd/aide/commands.go`

- [ ] **Step 1: Register contextSetDefaultCmd in contextCmd()**

In the `contextCmd()` function, add:
```go
cmd.AddCommand(contextSetDefaultCmd())
```

- [ ] **Step 2: Implement contextSetDefaultCmd**

Add after the `contextRemoveSecretCmd` function:

```go
func contextSetDefaultCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "set-default [context-name]",
		Short:        "Set a context as the default fallback",
		Long: `Set a context as the default fallback when no match rules apply.

If no context name is given, the CWD-matched context is used.`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				cwd = "."
			}
			cfg, err := config.Load(config.ConfigDir(), cwd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			var ctxName string
			if len(args) > 0 {
				ctxName = args[0]
			} else {
				remoteURL := aidectx.DetectRemote(cwd, "origin")
				rc, err := aidectx.Resolve(cfg, cwd, remoteURL)
				if err != nil {
					return fmt.Errorf("resolving context: %w", err)
				}
				ctxName = rc.Name
			}

			if _, ok := cfg.Contexts[ctxName]; !ok {
				return fmt.Errorf("context %q not found", ctxName)
			}

			cfg.DefaultContext = ctxName
			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Set default context to %q\n", ctxName)
			return nil
		},
	}
}
```

- [ ] **Step 3: Build and test**

Run: `go build ./... && go test ./...`

- [ ] **Step 4: Commit**

```
Add aide context set-default command
```

---

### Task 3: Auto-Set Default Context on First Context Creation

**Files:**
- Modify: `cmd/aide/commands.go`

When `aide use` creates the first explicit context and no `default_context` is set, automatically set it. This preserves the "works everywhere" behavior when transitioning from minimal to full format.

- [ ] **Step 1: Add auto-set logic to useCmd**

In the `useCmd` RunE, after `cfg.Contexts[ctxName] = ctx` and before `config.WriteConfig(cfg)`, add:

```go
// Auto-set default_context on first context creation
if cfg.DefaultContext == "" {
    cfg.DefaultContext = ctxName
}
```

- [ ] **Step 2: Add auto-set logic to setupCreateNew**

In the `setupCreateNew` function, after `cfg.Contexts[ctxName] = ctx` and before `config.WriteConfig(cfg)`, add the same:

```go
if cfg.DefaultContext == "" {
    cfg.DefaultContext = ctxName
}
```

- [ ] **Step 3: Add auto-set logic to contextAddCmd**

In the `contextAddCmd` RunE, after `cfg.Contexts[name] = newCtx` and before `config.WriteConfig(cfg)`, add:

```go
if cfg.DefaultContext == "" {
    cfg.DefaultContext = name
}
```

- [ ] **Step 4: Build and test**

Run: `go build ./... && go test ./...`

- [ ] **Step 5: Commit**

```
Auto-set default_context on first context creation
```

---

### Task 4: Show Default Context in `context list` Output

**Files:**
- Modify: `cmd/aide/commands.go`

- [ ] **Step 1: Update contextListCmd to mark default**

In the `contextListCmd` RunE, where context names are printed, add a `(default)` marker:

Find the line:
```go
fmt.Fprintln(out, name)
```

Replace with:
```go
if name == cfg.DefaultContext {
    fmt.Fprintf(out, "%s (default)\n", name)
} else {
    fmt.Fprintln(out, name)
}
```

- [ ] **Step 2: Build and test**

Run: `go build ./... && go test ./...`

- [ ] **Step 3: Commit**

```
Show default context marker in context list output
```
