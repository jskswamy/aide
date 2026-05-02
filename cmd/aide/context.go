// cmd/aide/context.go
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/jskswamy/aide/internal/config"
	aidectx "github.com/jskswamy/aide/internal/context"
	"github.com/jskswamy/aide/internal/launcher"
)

func contextCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "context",
		Short: "Manage aide contexts",
	}
	cmd.AddCommand(contextListCmd())
	cmd.AddCommand(contextBindCmd())
	cmd.AddCommand(contextRenameCmd())
	cmd.AddCommand(contextRemoveCmd())
	cmd.AddCommand(contextSetSecretCmd())
	cmd.AddCommand(contextRemoveSecretCmd())
	cmd.AddCommand(contextSetDefaultCmd())
	return cmd
}

func contextListCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "list",
		Short:        "List all configured contexts",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			env, err := cmdEnv(cmd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			cfg := env.Config()
			if len(cfg.Contexts) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No contexts configured.")
				return nil
			}

			out := cmd.OutOrStdout()
			names := make([]string, 0, len(cfg.Contexts))
			for name := range cfg.Contexts {
				names = append(names, name)
			}
			sort.Strings(names)

			for i, name := range names {
				ctx := cfg.Contexts[name]
				if name == cfg.DefaultContext {
					fmt.Fprintf(out, "%s (default)\n", name)
				} else {
					fmt.Fprintln(out, name)
				}
				fmt.Fprintf(out, "  Agent:    %s\n", ctx.Agent)
				if ctx.Secret != "" {
					fmt.Fprintf(out, "  Secret:   %s\n", ctx.Secret)
				}
				for _, rule := range ctx.Match {
					if rule.Path != "" {
						fmt.Fprintf(out, "  Match:    %s\n", rule.Path)
					}
					if rule.Remote != "" {
						fmt.Fprintf(out, "  Match:    %s (remote)\n", rule.Remote)
					}
				}
				if len(ctx.Env) > 0 {
					keys := make([]string, 0, len(ctx.Env))
					for k := range ctx.Env {
						keys = append(keys, k)
					}
					sort.Strings(keys)
					fmt.Fprintf(out, "  Env:      %s\n", strings.Join(keys, ", "))
				}
				if i < len(names)-1 {
					fmt.Fprintln(out)
				}
			}
			return nil
		},
	}
}

func contextBindCmd() *cobra.Command {
	var (
		forcePath   bool
		forceRemote bool
	)

	cmd := &cobra.Command{
		Use:   "bind [name]",
		Short: "Attach this folder to an existing context",
		Long: `Attach the current folder to an existing context.

Examples:
  aide context bind work               # auto-detect: git remote if repo, else folder path
  aide context bind work --path        # force exact folder path match
  aide context bind work --remote      # force git remote match (errors if not a git repo)
  aide context bind                    # interactive picker over existing contexts`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if forcePath && forceRemote {
				return fmt.Errorf("--path and --remote are mutually exclusive")
			}

			out := cmd.OutOrStdout()
			reader := bufio.NewReader(os.Stdin)

			env, err := cmdEnv(cmd)
			if err != nil {
				return err
			}
			cwd := env.CWD()
			cfg := env.Config()

			var name string
			if len(args) == 1 {
				name = args[0]
			} else {
				picked, err := pickExistingContext(out, reader, cfg)
				if err != nil {
					return err
				}
				name = picked
			}

			ctx, ok := cfg.Contexts[name]
			if !ok {
				// TTY: offer to create. Non-TTY: hard error.
				if isStdinTTY() {
					fmt.Fprintf(out, "Context %q doesn't exist. Create it now? [y/N]: ", name)
					ans, _ := reader.ReadString('\n')
					if strings.EqualFold(strings.TrimSpace(ans), "y") {
						return runCreateWizard(cmd, name, createOptions{here: tristateYes})
					}
				}
				return fmt.Errorf("context %q not found.\nRun: aide context create %s", name, name)
			}

			var rule config.MatchRule
			var desc string
			switch {
			case forceRemote:
				remote := aidectx.DetectRemote(cwd, "origin")
				if remote == "" {
					return fmt.Errorf("--remote requires the current folder to be a git repo with an 'origin' remote (not a git repo or no origin)")
				}
				rule = config.MatchRule{Remote: remote}
				desc = fmt.Sprintf("by remote %s", remote)
			case forcePath:
				rule = config.MatchRule{Path: cwd}
				desc = fmt.Sprintf("by path %s", cwd)
			default:
				rule, desc = autoDetectMatchRule(cwd)
			}

			ctx.Match = append(ctx.Match, rule)
			cfg.Contexts[name] = ctx
			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}
			fmt.Fprintf(out, "Bound this folder to context %q (matched %s)\n", name, desc)
			return nil
		},
	}

	cmd.Flags().BoolVar(&forcePath, "path", false, "Force exact folder path match")
	cmd.Flags().BoolVar(&forceRemote, "remote", false, "Force git remote match (errors if not a git repo)")
	return cmd
}

// pickExistingContext shows a numbered menu of existing contexts and
// returns the chosen name. Returns an error in non-TTY mode (the
// caller is expected to require a positional name in that case).
func pickExistingContext(out io.Writer, reader *bufio.Reader, cfg *config.Config) (string, error) {
	if !isStdinTTY() {
		return "", fmt.Errorf("a context name is required in non-interactive mode")
	}
	if len(cfg.Contexts) == 0 {
		return "", fmt.Errorf("no contexts configured. Run: aide context create <name>")
	}
	names := make([]string, 0, len(cfg.Contexts))
	for n := range cfg.Contexts {
		names = append(names, n)
	}
	sort.Strings(names)

	fmt.Fprintln(out, "Existing contexts:")
	for i, n := range names {
		fmt.Fprintf(out, "  [%d] %s\n", i+1, n)
	}
	fmt.Fprint(out, "Choose [1]: ")
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("reading selection: %w", err)
	}
	input = strings.TrimSpace(input)
	choice := 1
	if input != "" {
		n, err := strconv.Atoi(input)
		if err != nil || n < 1 || n > len(names) {
			return "", fmt.Errorf("invalid selection: %q", input)
		}
		choice = n
	}
	return names[choice-1], nil
}

// isStdinTTY reports whether stdin is connected to a terminal. Used by
// commands that need to choose between interactive prompting and a
// non-TTY hard error.
func isStdinTTY() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

type createTristate int

const (
	tristateUnset createTristate = iota
	tristateYes
	tristateNo
)

type createOptions struct {
	agent  string
	secret string
	here   createTristate
}

// runCreateWizard creates a new context and (optionally) binds cwd.
// Implemented in Task 3.
func runCreateWizard(cmd *cobra.Command, prefilledName string, opts createOptions) error {
	return fmt.Errorf("runCreateWizard not implemented yet")
}

// Ensure launcher import is used (IsKnownAgent is used by contextAddCmd
// which was deleted; keep the import live via a reference here until
// Task 3 adds contextCreateCmd that uses launcher directly).
var _ = launcher.KnownAgents

func contextRenameCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "rename <old-name> <new-name>",
		Short:        "Rename a context",
		Args:         cobra.ExactArgs(2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			oldName := args[0]
			newName := args[1]

			env, err := cmdEnv(cmd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			cfg := env.Config()

			ctx, ok := cfg.Contexts[oldName]
			if !ok {
				return fmt.Errorf("context %q not found", oldName)
			}
			if _, exists := cfg.Contexts[newName]; exists {
				return fmt.Errorf("context %q already exists", newName)
			}

			cfg.Contexts[newName] = ctx
			delete(cfg.Contexts, oldName)

			if cfg.DefaultContext == oldName {
				cfg.DefaultContext = newName
			}

			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Renamed context %q -> %q\n", oldName, newName)
			return nil
		},
	}
}

func contextRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "remove <name>",
		Short:        "Remove a context from configuration",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			env, err := cmdEnv(cmd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			cfg := env.Config()
			ctx, ok := cfg.Contexts[name]
			if !ok {
				return fmt.Errorf("context %q not found", name)
			}
			removedAgent := ctx.Agent
			delete(cfg.Contexts, name)
			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Removed context %q\n", name)

			// Check for orphaned agent
			if removedAgent != "" {
				stillUsed := false
				for _, c := range cfg.Contexts {
					if c.Agent == removedAgent {
						stillUsed = true
						break
					}
				}
				if !stillUsed {
					fmt.Fprintf(out, "\nAgent %q is no longer used by any context.\n", removedAgent)
					fmt.Fprintf(out, "Remove it with: aide agents remove %s\n", removedAgent)
				}
			}
			return nil
		},
	}
}

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

func contextRemoveSecretCmd() *cobra.Command {
	var contextName string

	cmd := &cobra.Command{
		Use:          "remove-secret",
		Short:        "Remove the secret from the current context",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
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

func contextSetDefaultCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set-default [context-name]",
		Short: "Set a context as the default fallback",
		Long: `Set a context as the default fallback when no match rules apply.

If no context name is given, the CWD-matched context is used.`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := cmdEnv(cmd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			cwd := env.CWD()
			cfg := env.Config()

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
