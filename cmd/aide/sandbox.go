// Package main provides the aide CLI commands.
package main

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/jskswamy/aide/internal/config"
	aidectx "github.com/jskswamy/aide/internal/context"
	"github.com/jskswamy/aide/internal/display"
	"github.com/jskswamy/aide/internal/homepath"
	"github.com/jskswamy/aide/internal/launcher"
	"github.com/jskswamy/aide/internal/sandbox"
	"github.com/jskswamy/aide/internal/trust"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
	"github.com/spf13/cobra"
)

func sandboxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sandbox",
		Short: "Manage sandbox profiles",
	}
	cmd.AddCommand(sandboxShowCmd())
	cmd.AddCommand(sandboxTestCmd())
	cmd.AddCommand(sandboxListCmd())
	cmd.AddCommand(sandboxCreateCmd())
	cmd.AddCommand(sandboxEditCmd())
	cmd.AddCommand(sandboxRemoveCmd())
	cmd.AddCommand(sandboxDenyCmd())
	cmd.AddCommand(sandboxAllowCmd())
	cmd.AddCommand(sandboxResetCmd())
	cmd.AddCommand(sandboxPortsCmd())
	cmd.AddCommand(sandboxNetworkCmd())
	cmd.AddCommand(sandboxGuardsCmd())
	cmd.AddCommand(sandboxGuardCmd())
	cmd.AddCommand(sandboxUnguardCmd())
	cmd.AddCommand(sandboxTypesCmd())
	return cmd
}

func sandboxNetworkCmd() *cobra.Command {
	var contextName string
	var global bool
	cmd := &cobra.Command{
		Use:          "network <mode>",
		Short:        "Set network mode for a context's sandbox (outbound|none|unrestricted) (project-level by default)",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := args[0]
			if err := config.ValidateNetworkMode(mode); err != nil {
				return err
			}
			return runScopedMutation(cmd.OutOrStdout(), global, contextName, scopedMutation{
				contextMutate: func(ctx *config.Context) error {
					ensureInlineSandbox(ctx).Network = &config.NetworkPolicy{Mode: mode}
					return nil
				},
				projectMutate: func(po *config.ProjectOverride) error {
					ensureProjectSandbox(po).Network = &config.NetworkPolicy{Mode: mode}
					return nil
				},
				successGlobal:  func(ctxName string) string { return fmt.Sprintf("Set network mode to %q for context %q (global)", mode, ctxName) },
				successProject: func(poPath string) string { return fmt.Sprintf("Set network mode to %q in project (%s)", mode, poPath) },
			})
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "Apply to user-level config instead of project")
	cmd.Flags().StringVar(&contextName, "context", "", "Target context name (requires --global)")
	return cmd
}

func sandboxShowCmd() *cobra.Command {
	var contextName string
	var withCaps, withoutCaps []string

	cmd := &cobra.Command{
		Use:          "show",
		Short:        "Show effective sandbox policy for current/named context",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			env, err := cmdEnv(cmd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			cwd := env.CWD()
			cfg := env.Config()

			// Resolve context
			remoteURL := aidectx.DetectRemote(cwd, "origin")
			rc, err := aidectx.Resolve(cfg, cwd, remoteURL)
			if err != nil {
				return fmt.Errorf("resolving context: %w", err)
			}

			if contextName != "" {
				ctx, ok := cfg.Contexts[contextName]
				if !ok {
					return fmt.Errorf("context %q not found", contextName)
				}
				rc = &aidectx.ResolvedContext{
					Name:    contextName,
					Context: ctx,
				}
			}

			// Resolve sandbox ref
			sandboxCfg, disabled, sbErr := sandbox.ResolveSandboxRef(rc.Context.Sandbox, cfg.Sandboxes)
			if sbErr != nil {
				return fmt.Errorf("resolving sandbox: %w", sbErr)
			}

			if disabled {
				fmt.Fprintf(out, "Sandbox: disabled (context %q)\n", rc.Name)
				return nil
			}

			// Resolve capabilities and merge into sandbox config
			capNames := sandbox.MergeCapNames(rc.Context.Capabilities, withCaps, withoutCaps)
			_, capOverrides, err := sandbox.ResolveCapabilities(capNames, cfg)
			if err != nil {
				return fmt.Errorf("resolving capabilities: %w", err)
			}
			sandbox.ApplyOverrides(&sandboxCfg, capOverrides)

			homeDir, _ := os.UserHomeDir()
			tempDir := os.TempDir()
			projectRoot := aidectx.ProjectRoot(cwd)

			policy, _, err := sandbox.PolicyFromConfig(sandboxCfg, sandbox.Paths{ProjectRoot: projectRoot, RuntimeDir: "/tmp/aide-preview", HomeDir: homeDir, TempDir: tempDir})
			if err != nil {
				return fmt.Errorf("building sandbox policy: %w", err)
			}
			if policy == nil {
				fmt.Fprintf(out, "Sandbox: disabled (context %q)\n", rc.Name)
				return nil
			}

			source := "default"
			if rc.Context.Sandbox != nil {
				if rc.Context.Sandbox.ProfileName != "" {
					source = fmt.Sprintf("profile %q", rc.Context.Sandbox.ProfileName)
				} else if rc.Context.Sandbox.Inline != nil {
					source = "inline"
				}
			}

			tier := sandbox.PlatformIsolationTier(*policy)
			fmt.Fprintf(out, "Isolation tier:  %s\n", tier.Tier)
			fmt.Fprintf(out, "Backend:         %s\n", tier.Backend)
			fmt.Fprintf(out, "Port filtering:  %s\n", tier.PortFiltering)
			if tier.Reason != "" {
				fmt.Fprintf(out, "Reason:          %s\n", tier.Reason)
			}
			fmt.Fprintln(out)

			fmt.Fprintf(out, "Effective sandbox policy (%s):\n", source)
			fmt.Fprintf(out, "  Guards:     %s\n", strings.Join(policy.Guards, ", "))

			gps := sandbox.DeriveGrantedPathSet(*policy)
			if len(gps.Writable) > 0 {
				fmt.Fprintln(out, "  Writable:")
				for _, p := range gps.Writable {
					origin := gps.OriginGuard[p]
					if origin != "" {
						fmt.Fprintf(out, "    %s  [%s]\n", p, origin)
					} else {
						fmt.Fprintf(out, "    %s\n", p)
					}
				}
			}
			if len(gps.Readable) > 0 {
				fmt.Fprintln(out, "  Readable:")
				for _, p := range gps.Readable {
					origin := gps.OriginGuard[p]
					if origin != "" {
						fmt.Fprintf(out, "    %s  [%s]\n", p, origin)
					} else {
						fmt.Fprintf(out, "    %s\n", p)
					}
				}
			}
			if len(gps.Denied) > 0 {
				fmt.Fprintln(out, "  Denied:")
				for _, p := range gps.Denied {
					origin := gps.OriginGuard[p]
					if origin != "" {
						fmt.Fprintf(out, "    %s  [%s]\n", p, origin)
					} else {
						fmt.Fprintf(out, "    %s\n", p)
					}
				}
			}

			fmt.Fprintf(out, "  Network:    %s\n", policy.Network)
			return nil
		},
	}
	cmd.Flags().StringVar(&contextName, "context", "", "show policy for a specific context")
	cmd.Flags().StringSliceVar(&withCaps, "with", nil, "additional capabilities to enable")
	cmd.Flags().StringSliceVar(&withoutCaps, "without", nil, "capabilities to disable")
	return cmd
}

func sandboxTestCmd() *cobra.Command {
	var contextName string
	var withCaps, withoutCaps []string

	cmd := &cobra.Command{
		Use:          "test",
		Short:        "Generate and print the platform-specific sandbox profile without launching the agent",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			env, err := cmdEnv(cmd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			cwd := env.CWD()
			cfg := env.Config()

			// Resolve context
			remoteURL := aidectx.DetectRemote(cwd, "origin")
			rc, err := aidectx.Resolve(cfg, cwd, remoteURL)
			if err != nil {
				return fmt.Errorf("resolving context: %w", err)
			}

			if contextName != "" {
				ctx, ok := cfg.Contexts[contextName]
				if !ok {
					return fmt.Errorf("context %q not found", contextName)
				}
				rc = &aidectx.ResolvedContext{
					Name:    contextName,
					Context: ctx,
				}
			}

			// Resolve sandbox ref
			sandboxCfg, disabled, sbErr := sandbox.ResolveSandboxRef(rc.Context.Sandbox, cfg.Sandboxes)
			if sbErr != nil {
				return fmt.Errorf("resolving sandbox: %w", sbErr)
			}

			if disabled {
				fmt.Fprintf(out, "Sandbox: disabled (context %q)\n", rc.Name)
				return nil
			}

			// Resolve capabilities and merge into sandbox config
			capNames := sandbox.MergeCapNames(rc.Context.Capabilities, withCaps, withoutCaps)
			_, capOverrides, err := sandbox.ResolveCapabilities(capNames, cfg)
			if err != nil {
				return fmt.Errorf("resolving capabilities: %w", err)
			}
			sandbox.ApplyOverrides(&sandboxCfg, capOverrides)

			homeDir, _ := os.UserHomeDir()
			tempDir := os.TempDir()
			projectRoot := aidectx.ProjectRoot(cwd)

			policy, _, err := sandbox.PolicyFromConfig(sandboxCfg, sandbox.Paths{ProjectRoot: projectRoot, RuntimeDir: "/tmp/aide-preview", HomeDir: homeDir, TempDir: tempDir})
			if err != nil {
				return fmt.Errorf("building sandbox policy: %w", err)
			}
			if policy == nil {
				fmt.Fprintf(out, "Sandbox: disabled (context %q)\n", rc.Name)
				return nil
			}

			// Wire the agent-specific seatbelt module so the printed
			// profile reflects what the launched agent will actually
			// see. Without this, `aide sandbox test` shows the base
			// profile only and the diagnostic misleads when debugging
			// agent-specific path issues (e.g. claude's
			// CLAUDE_CONFIG_DIR override).
			if rc.Context.Agent != "" {
				if m := launcher.ResolveAgentModule(rc.Context.Agent); m != nil {
					policy.AgentModule = m
				}
			}

			// Project the context's env (with tilde-expansion already
			// applied by the loader) into policy.Env so the agent
			// module's EnvLookup can see CLAUDE_CONFIG_DIR and similar
			// per-context overrides. Without this, the test command's
			// diagnostic output diverges from what the launcher
			// actually generates at run time.
			if len(rc.Context.Env) > 0 {
				envSlice := make([]string, 0, len(rc.Context.Env))
				for k, v := range rc.Context.Env {
					envSlice = append(envSlice, k+"="+homepath.Expand(v, homeDir))
				}
				policy.Env = envSlice
			}

			sb := sandbox.NewSandbox()
			profile, err := sb.GenerateProfile(*policy)
			if err != nil {
				return fmt.Errorf("generating sandbox profile: %w", err)
			}

			fmt.Fprint(out, profile)
			return nil
		},
	}
	cmd.Flags().StringVar(&contextName, "context", "", "generate profile for a specific context")
	cmd.Flags().StringSliceVar(&withCaps, "with", nil, "additional capabilities to enable")
	cmd.Flags().StringSliceVar(&withoutCaps, "without", nil, "capabilities to disable")
	return cmd
}

func sandboxListCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "list",
		Short:        "List named sandbox profiles",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			env, err := cmdEnv(cmd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			cfg := env.Config()

			fmt.Fprintf(out, "%-16s %-12s %s\n", "NAME", "SOURCE", "DETAILS")
			fmt.Fprintf(out, "%-16s %-12s %s\n", "default", "(built-in)", "network=outbound")

			if cfg.Sandboxes != nil {
				names := make([]string, 0, len(cfg.Sandboxes))
				for name := range cfg.Sandboxes {
					names = append(names, name)
				}
				sort.Strings(names)
				for _, name := range names {
					sp := cfg.Sandboxes[name]
					details := ""
					if sp.Network != nil && sp.Network.Mode != "" {
						details = fmt.Sprintf("network=%s", sp.Network.Mode)
					}
					if len(sp.DeniedExtra) > 0 {
						if details != "" {
							details += "  "
						}
						details += fmt.Sprintf("denied_extra: %s", strings.Join(sp.DeniedExtra, ", "))
					}
					fmt.Fprintf(out, "%-16s %-12s %s\n", name, "(config)", details)
				}
			}

			return nil
		},
	}
}

func sandboxCreateCmd() *cobra.Command {
	var fromProfile string

	cmd := &cobra.Command{
		Use:          "create <name>",
		Short:        "Create a new sandbox profile",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			reader := bufio.NewReader(os.Stdin)
			name := args[0]

			if name == "default" || name == "none" {
				return fmt.Errorf("cannot use reserved profile name %q", name)
			}

			env, _ := cmdEnv(cmd)
			cfg := env.Config()
			if cfg.Sandboxes == nil {
				cfg.Sandboxes = make(map[string]config.SandboxPolicy)
			}

			if _, exists := cfg.Sandboxes[name]; exists {
				return fmt.Errorf("sandbox profile %q already exists (use 'aide sandbox edit' to modify)", name)
			}

			var sp config.SandboxPolicy

			if fromProfile != "" && fromProfile != "default" {
				base, ok := cfg.Sandboxes[fromProfile]
				if !ok {
					return fmt.Errorf("base profile %q not found", fromProfile)
				}
				sp = base
			}

			// Ask for writable paths
			fmt.Fprint(out, "Additional writable paths (comma-separated, empty to skip):\n> ")
			wrInput, _ := reader.ReadString('\n')
			wrInput = strings.TrimSpace(wrInput)
			if wrInput != "" {
				for _, p := range strings.Split(wrInput, ",") {
					p = strings.TrimSpace(p)
					if p == "" {
						continue
					}
					expanded := homepath.Expand(p, "")
					if _, err := os.Stat(expanded); err != nil {
						fmt.Fprintf(out, "  ⚠ %s does not exist (added anyway)\n", p)
					} else {
						fmt.Fprintf(out, "  ✓ %s exists\n", p)
					}
					sp.WritableExtra = append(sp.WritableExtra, p)
				}
			}

			// Ask for denied paths
			fmt.Fprint(out, "Additional denied paths (comma-separated, empty to skip):\n> ")
			dnInput, _ := reader.ReadString('\n')
			dnInput = strings.TrimSpace(dnInput)
			if dnInput != "" {
				for _, p := range strings.Split(dnInput, ",") {
					p = strings.TrimSpace(p)
					if p == "" {
						continue
					}
					expanded := homepath.Expand(p, "")
					if _, err := os.Stat(expanded); err != nil {
						fmt.Fprintf(out, "  ⚠ %s does not exist (added anyway)\n", p)
					} else {
						fmt.Fprintf(out, "  ✓ %s exists\n", p)
					}
					sp.DeniedExtra = append(sp.DeniedExtra, p)
				}
			}

			// Ask for network mode
			fmt.Fprint(out, "Network mode [outbound/none/unrestricted] (default: outbound): ")
			netInput, _ := reader.ReadString('\n')
			netInput = strings.TrimSpace(netInput)
			if netInput == "" {
				netInput = "outbound"
			}
			if err := config.ValidateNetworkMode(netInput); err != nil {
				return err
			}
			sp.Network = &config.NetworkPolicy{Mode: netInput}

			cfg.Sandboxes[name] = sp

			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}

			fmt.Fprintf(out, "\nCreated sandbox profile %q\n", name)
			return nil
		},
	}
	cmd.Flags().StringVar(&fromProfile, "from", "", "base profile to inherit from")
	return cmd
}

func sandboxEditCmd() *cobra.Command {
	var addDenied, addWritable, addReadable, removeDenied, removeWritable, removeReadable []string
	var network string

	cmd := &cobra.Command{
		Use:          "edit <name>",
		Short:        "Edit an existing sandbox profile",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			name := args[0]

			if name == "default" || name == "none" {
				return fmt.Errorf("cannot edit built-in profile %q", name)
			}

			env, err := cmdEnv(cmd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			cfg := env.Config()
			if cfg.Sandboxes == nil {
				return fmt.Errorf("sandbox profile %q not found", name)
			}

			sp, ok := cfg.Sandboxes[name]
			if !ok {
				return fmt.Errorf("sandbox profile %q not found", name)
			}

			for _, p := range addWritable {
				expanded := homepath.Expand(p, "")
				if _, err := os.Stat(expanded); err != nil {
					fmt.Fprintf(out, "  ⚠ %s does not exist (added anyway)\n", p)
				}
				sp.WritableExtra = append(sp.WritableExtra, p)
			}

			for _, p := range addDenied {
				expanded := homepath.Expand(p, "")
				if _, err := os.Stat(expanded); err != nil {
					fmt.Fprintf(out, "  ⚠ %s does not exist (added anyway)\n", p)
				}
				sp.DeniedExtra = append(sp.DeniedExtra, p)
			}

			for _, p := range removeWritable {
				sp.WritableExtra = display.RemoveFromSlice(sp.WritableExtra, p)
			}

			for _, p := range removeDenied {
				sp.DeniedExtra = display.RemoveFromSlice(sp.DeniedExtra, p)
			}

			for _, p := range addReadable {
				expanded := homepath.Expand(p, "")
				if _, err := os.Stat(expanded); err != nil {
					fmt.Fprintf(out, "  ⚠ %s does not exist (added anyway)\n", p)
				}
				sp.ReadableExtra = append(sp.ReadableExtra, p)
			}

			for _, p := range removeReadable {
				sp.ReadableExtra = display.RemoveFromSlice(sp.ReadableExtra, p)
			}

			if network != "" {
				if err := config.ValidateNetworkMode(network); err != nil {
					return err
				}
				sp.Network = &config.NetworkPolicy{Mode: network}
			}

			cfg.Sandboxes[name] = sp

			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}

			fmt.Fprintf(out, "Updated sandbox profile %q\n", name)
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&addDenied, "add-denied", nil, "add denied paths")
	cmd.Flags().StringSliceVar(&addWritable, "add-writable", nil, "add writable paths")
	cmd.Flags().StringSliceVar(&addReadable, "add-readable", nil, "add readable paths")
	cmd.Flags().StringSliceVar(&removeDenied, "remove-denied", nil, "remove denied paths")
	cmd.Flags().StringSliceVar(&removeWritable, "remove-writable", nil, "remove writable paths")
	cmd.Flags().StringSliceVar(&removeReadable, "remove-readable", nil, "remove readable paths")
	cmd.Flags().StringVar(&network, "network", "", "set network mode")
	return cmd
}

func sandboxRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "remove <name>",
		Short:        "Remove a sandbox profile",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			name := args[0]

			if name == "default" || name == "none" {
				return fmt.Errorf("cannot remove built-in profile %q", name)
			}

			env, err := cmdEnv(cmd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			cfg := env.Config()

			if cfg.Sandboxes == nil {
				return fmt.Errorf("sandbox profile %q not found", name)
			}
			if _, ok := cfg.Sandboxes[name]; !ok {
				return fmt.Errorf("sandbox profile %q not found", name)
			}

			// Warn if any contexts reference this profile
			for ctxName, ctx := range cfg.Contexts {
				if ctx.Sandbox != nil && ctx.Sandbox.ProfileName == name {
					fmt.Fprintf(out, "  Warning: context %q references profile %q\n", ctxName, name)
				}
			}

			delete(cfg.Sandboxes, name)

			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}

			fmt.Fprintf(out, "Removed sandbox profile %q\n", name)
			return nil
		},
	}
}

// ensureInlineSandbox ensures the context has an inline SandboxRef with a SandboxPolicy.
func ensureInlineSandbox(ctx *config.Context) *config.SandboxPolicy {
	if ctx.Sandbox == nil {
		ctx.Sandbox = &config.SandboxRef{Inline: &config.SandboxPolicy{}}
	}
	// Clear disabled flag — user is actively configuring the sandbox
	ctx.Sandbox.Disabled = false
	// Clear profile name — switching to inline config
	ctx.Sandbox.ProfileName = ""
	if ctx.Sandbox.Inline == nil {
		ctx.Sandbox.Inline = &config.SandboxPolicy{}
	}
	return ctx.Sandbox.Inline
}

// ensureProjectSandbox ensures the project override has a non-nil SandboxPolicy.
func ensureProjectSandbox(po *config.ProjectOverride) *config.SandboxPolicy {
	if po.Sandbox == nil {
		po.Sandbox = &config.SandboxPolicy{}
	}
	return po.Sandbox
}

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
			return runScopedMutation(cmd.OutOrStdout(), global, contextName, scopedMutation{
				contextMutate: func(ctx *config.Context) error {
					sp := ensureInlineSandbox(ctx)
					sp.DeniedExtra = append(sp.DeniedExtra, path)
					return nil
				},
				projectMutate: func(po *config.ProjectOverride) error {
					sp := ensureProjectSandbox(po)
					sp.DeniedExtra = append(sp.DeniedExtra, path)
					return nil
				},
				successGlobal:  func(ctxName string) string { return fmt.Sprintf("Added %s to denied_extra for context %q (global)", path, ctxName) },
				successProject: func(poPath string) string { return fmt.Sprintf("Added %s to denied_extra in project (%s)", path, poPath) },
			})
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "Apply to user-level config instead of project")
	cmd.Flags().StringVar(&contextName, "context", "", "Target context name (requires --global)")
	return cmd
}

func sandboxAllowCmd() *cobra.Command {
	var contextName string
	var global bool
	var write bool
	cmd := &cobra.Command{
		Use:          "allow <path>",
		Short:        "Add a path to readable_extra or writable_extra (project-level by default)",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			listName := "readable_extra"
			if write {
				listName = "writable_extra"
			}
			apply := func(sp *config.SandboxPolicy) {
				if write {
					sp.WritableExtra = append(sp.WritableExtra, path)
				} else {
					sp.ReadableExtra = append(sp.ReadableExtra, path)
				}
			}
			return runScopedMutation(cmd.OutOrStdout(), global, contextName, scopedMutation{
				contextMutate: func(ctx *config.Context) error {
					apply(ensureInlineSandbox(ctx))
					return nil
				},
				projectMutate: func(po *config.ProjectOverride) error {
					apply(ensureProjectSandbox(po))
					return nil
				},
				successGlobal:  func(ctxName string) string { return fmt.Sprintf("Added %s to %s for context %q (global)", path, listName, ctxName) },
				successProject: func(poPath string) string { return fmt.Sprintf("Added %s to %s in project (%s)", path, listName, poPath) },
			})
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "Apply to user-level config instead of project")
	cmd.Flags().StringVar(&contextName, "context", "", "Target context name (requires --global)")
	cmd.Flags().BoolVar(&write, "write", false, "add to writable_extra instead of readable_extra")
	return cmd
}

func sandboxResetCmd() *cobra.Command {
	var contextName string
	var global bool
	cmd := &cobra.Command{
		Use:          "reset",
		Short:        "Reset sandbox to defaults (project-level by default)",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runScopedMutation(cmd.OutOrStdout(), global, contextName, scopedMutation{
				contextMutate: func(ctx *config.Context) error { ctx.Sandbox = nil; return nil },
				projectMutate: func(po *config.ProjectOverride) error { po.Sandbox = nil; return nil },
				successGlobal: func(ctxName string) string {
					return fmt.Sprintf("Reset sandbox to defaults for context %q (global)", ctxName)
				},
				successProject: func(poPath string) string {
					return fmt.Sprintf("Reset sandbox to defaults in project (%s)", poPath)
				},
			})
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "Apply to user-level config instead of project")
	cmd.Flags().StringVar(&contextName, "context", "", "Target context name (requires --global)")
	return cmd
}

func sandboxPortsCmd() *cobra.Command {
	var contextName string
	var global bool
	cmd := &cobra.Command{
		Use:          "ports <port1> [port2] ...",
		Short:        "Set allowed network ports for a sandbox (project-level by default)",
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
			return runScopedMutation(cmd.OutOrStdout(), global, contextName, scopedMutation{
				contextMutate: func(ctx *config.Context) error {
					ensureInlineSandbox(ctx).Network = &config.NetworkPolicy{Mode: "outbound", AllowPorts: ports}
					return nil
				},
				projectMutate: func(po *config.ProjectOverride) error {
					sp := ensureProjectSandbox(po)
					if sp.Network == nil {
						sp.Network = &config.NetworkPolicy{Mode: "outbound"}
					}
					sp.Network.AllowPorts = ports
					return nil
				},
				successGlobal:  func(ctxName string) string { return fmt.Sprintf("Set allowed ports to %v for context %q (global)", ports, ctxName) },
				successProject: func(poPath string) string { return fmt.Sprintf("Set allowed ports to %v in project (%s)", ports, poPath) },
			})
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "Apply to user-level config instead of project")
	cmd.Flags().StringVar(&contextName, "context", "", "Target context name (requires --global)")
	return cmd
}

func sandboxGuardsCmd() *cobra.Command {
	var contextName string
	var withCaps, withoutCaps []string
	cmd := &cobra.Command{
		Use:          "guards",
		Short:        "List all guards with type, status, and description",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			allGuards := guards.AllGuards()

			// Resolve the active set from the current context config
			var activeNames []string
			cfg, ctxName, ctx, err := resolveContextForMutation(contextName)
			if err == nil {
				_ = ctxName
				var sandboxCfg *config.SandboxPolicy
				if ctx.Sandbox != nil && !ctx.Sandbox.Disabled {
					if ctx.Sandbox.Inline != nil {
						sandboxCfg = ctx.Sandbox.Inline
					} else if ctx.Sandbox.ProfileName != "" {
						if sp, ok := cfg.Sandboxes[ctx.Sandbox.ProfileName]; ok {
							sandboxCfg = &sp
						}
					}
				}

				// Resolve capabilities and merge into sandbox config
				capNames := sandbox.MergeCapNames(ctx.Capabilities, withCaps, withoutCaps)
				_, capOverrides, capErr := sandbox.ResolveCapabilities(capNames, cfg)
				if capErr == nil {
					sandbox.ApplyOverrides(&sandboxCfg, capOverrides)
				}

				activeNames = sandbox.EffectiveGuards(sandboxCfg)
			} else {
				// Fall back to defaults if config cannot be loaded
				activeNames = guards.DefaultGuardNames()
			}

			activeSet := make(map[string]bool, len(activeNames))
			for _, n := range activeNames {
				activeSet[n] = true
			}

			fmt.Fprintf(out, "%-20s %-12s %-10s %s\n", "GUARD", "TYPE", "STATUS", "DESCRIPTION")
			for _, g := range allGuards {
				status := "inactive"
				if activeSet[g.Name()] {
					status = "active"
				}
				fmt.Fprintf(out, "%-20s %-12s %-10s %s\n", g.Name(), g.Type(), status, g.Description())
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&contextName, "context", "", "target context name")
	cmd.Flags().StringSliceVar(&withCaps, "with", nil, "additional capabilities to enable")
	cmd.Flags().StringSliceVar(&withoutCaps, "without", nil, "capabilities to disable")
	return cmd
}

func sandboxGuardCmd() *cobra.Command {
	var contextName string
	var global bool
	cmd := &cobra.Command{
		Use:          "guard <name>",
		Short:        "Enable an additional guard for a sandbox (project-level by default)",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := validateContextScope(global, contextName); err != nil {
				return err
			}
			if global {
				cfg, ctxName, ctx, err := resolveContextForMutation(contextName)
				if err != nil {
					return err
				}
				// Reject named profile references — user must edit the profile directly
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
				if len(r.Warnings) > 0 {
					return nil
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Guard %q enabled for context %q (global)\n", name, ctxName)
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
			if err := config.WriteProjectOverrideWithTrust(poPath, po, trust.DefaultStore()); err != nil {
				return fmt.Errorf("writing project config: %w", err)
			}
			if len(r.Warnings) > 0 {
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Guard %q enabled in project (%s)\n", name, poPath)
			return nil
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "Apply to user-level config instead of project")
	cmd.Flags().StringVar(&contextName, "context", "", "Target context name (requires --global)")
	return cmd
}

func sandboxUnguardCmd() *cobra.Command {
	var contextName string
	var global bool
	cmd := &cobra.Command{
		Use:          "unguard <name>",
		Short:        "Disable a guard for a sandbox (project-level by default)",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := validateContextScope(global, contextName); err != nil {
				return err
			}
			if global {
				cfg, ctxName, ctx, err := resolveContextForMutation(contextName)
				if err != nil {
					return err
				}
				// Reject named profile references — user must edit the profile directly
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
				if len(r.Warnings) > 0 {
					return nil
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Guard %q disabled for context %q (global)\n", name, ctxName)
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
			if err := config.WriteProjectOverrideWithTrust(poPath, po, trust.DefaultStore()); err != nil {
				return fmt.Errorf("writing project config: %w", err)
			}
			if len(r.Warnings) > 0 {
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Guard %q disabled in project (%s)\n", name, poPath)
			return nil
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "Apply to user-level config instead of project")
	cmd.Flags().StringVar(&contextName, "context", "", "Target context name (requires --global)")
	return cmd
}

func sandboxTypesCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "types",
		Short:        "List built-in guard types with their default state and description",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%-12s %-10s %s\n", "TYPE", "STATE", "DESCRIPTION")
			fmt.Fprintf(out, "%-12s %-10s %s\n", "always", "on", "Always active; cannot be disabled")
			fmt.Fprintf(out, "%-12s %-10s %s\n", "default", "on", "Active by default; can be disabled with unguard")
			fmt.Fprintf(out, "%-12s %-10s %s\n", "opt-in", "off", "Inactive by default; enable with guard")
			return nil
		},
	}
}
