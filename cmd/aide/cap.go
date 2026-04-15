// cmd/aide/cap.go
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/jskswamy/aide/internal/capability"
	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/consent"
	"github.com/jskswamy/aide/internal/display"
	"github.com/jskswamy/aide/internal/trust"
)

func capCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cap",
		Short: "Manage capabilities",
	}
	cmd.AddCommand(capListCmd())
	cmd.AddCommand(capShowCmd())
	cmd.AddCommand(capCreateCmd())
	cmd.AddCommand(capEditCmd())
	cmd.AddCommand(capEnableCmd())
	cmd.AddCommand(capDisableCmd())
	cmd.AddCommand(capNeverAllowCmd())
	cmd.AddCommand(capCheckCmd())
	cmd.AddCommand(capAuditCmd())
	cmd.AddCommand(capSuggestForPathCmd())
	cmd.AddCommand(capVariantsCmd())
	cmd.AddCommand(capConsentCmd())
	return cmd
}

func capConsentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "consent",
		Short: "Manage detection consents (granted variant selections)",
	}
	cmd.AddCommand(capConsentListCmd())
	cmd.AddCommand(capConsentRevokeCmd())
	return cmd
}

func capConsentListCmd() *cobra.Command {
	var projectFlag string
	cmd := &cobra.Command{
		Use:          "list",
		Short:        "List granted consents for a project (default: current directory)",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			project := projectFlag
			if project == "" {
				cwd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("getting working directory: %w", err)
				}
				project = cwd
			}
			store := consent.DefaultStore()
			grants, err := store.List(project)
			if err != nil {
				return fmt.Errorf("listing consents: %w", err)
			}
			if len(grants) == 0 {
				fmt.Fprintf(out, "no consents recorded for %s\n", project)
				return nil
			}
			for _, g := range grants {
				fmt.Fprintf(out, "%s  variants=%s  confirmed_at=%s  markers=%s\n",
					g.Capability,
					strings.Join(g.Variants, ","),
					g.ConfirmedAt.Format(time.RFC3339),
					g.Summary,
				)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&projectFlag, "project", "", "Project root (defaults to current directory)")
	return cmd
}

func capConsentRevokeCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "revoke <capability>",
		Short:        "Revoke all consents for a capability in the current project",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}
			store := consent.DefaultStore()
			if err := store.Revoke(cwd, args[0]); err != nil {
				return fmt.Errorf("revoking: %w", err)
			}
			fmt.Fprintf(out, "revoked all %s consents for %s\n", args[0], cwd)
			return nil
		},
	}
}

func capListCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "list",
		Short:        "List all capabilities (built-in and user-defined)",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}

			cfg, err := config.Load(config.Dir(), cwd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			userCaps := capability.FromConfigDefs(cfg.Capabilities)
			registry := capability.MergedRegistry(userCaps)
			builtins := capability.Builtins()

			// Collect and sort names
			names := make([]string, 0, len(registry))
			for name := range registry {
				names = append(names, name)
			}
			sort.Strings(names)

			fmt.Fprintf(out, "%-20s %-12s %s\n", "NAME", "SOURCE", "DESCRIPTION")
			for _, name := range names {
				entry := registry[name]
				source := "built-in"
				if _, isBuiltin := builtins[name]; !isBuiltin {
					switch {
					case entry.Extends != "":
						source = "extends"
					case len(entry.Combines) > 0:
						source = "combines"
					default:
						source = "custom"
					}
				} else if _, isUser := userCaps[name]; isUser {
					// User override of a built-in
					source = "custom"
				}
				desc := entry.Description
				if len(entry.Variants) > 0 {
					names := make([]string, len(entry.Variants))
					for i, v := range entry.Variants {
						names[i] = v.Name
					}
					desc = fmt.Sprintf("%s (%d variants: %s)", desc, len(entry.Variants), strings.Join(names, ", "))
				}
				fmt.Fprintf(out, "%-20s %-12s %s\n", name, source, desc)
			}

			return nil
		},
	}
}

func capShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "show <name>",
		Short:             "Show detailed information about a capability",
		Args:              cobra.ExactArgs(1),
		SilenceUsage:      true,
		ValidArgsFunction: capabilityCompletionFunc,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			name := args[0]

			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}

			cfg, err := config.Load(config.Dir(), cwd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			userCaps := capability.FromConfigDefs(cfg.Capabilities)
			registry := capability.MergedRegistry(userCaps)

			if _, ok := registry[name]; !ok {
				return fmt.Errorf("unknown capability: %q", name)
			}

			resolved, err := capability.ResolveOne(name, registry)
			if err != nil {
				return fmt.Errorf("resolving capability: %w", err)
			}

			entry := registry[name]
			fmt.Fprintf(out, "Name:        %s\n", name)
			fmt.Fprintf(out, "Description: %s\n", entry.Description)

			if len(resolved.Sources) > 1 {
				fmt.Fprintf(out, "Sources:     %s\n", strings.Join(resolved.Sources, " -> "))
			}

			capShowSection(out, "Unguard", resolved.Unguard)
			capShowSection(out, "Readable", resolved.Readable)
			capShowSection(out, "Writable", resolved.Writable)
			capShowSection(out, "Deny", resolved.Deny)
			capShowSection(out, "EnvAllow", resolved.EnvAllow)

			if len(entry.Variants) > 0 {
				fmt.Fprintln(out, "")
				fmt.Fprintln(out, "Variants:")
				for _, v := range entry.Variants {
					fmt.Fprintf(out, "  %s", v.Name)
					if v.Description != "" {
						fmt.Fprintf(out, " — %s", v.Description)
					}
					fmt.Fprintln(out)
					for _, m := range v.Markers {
						fmt.Fprintf(out, "    marker: %s\n", m.MatchSummary())
					}
					if len(v.Readable) > 0 {
						fmt.Fprintf(out, "    readable: %s\n", strings.Join(v.Readable, ", "))
					}
					if len(v.Writable) > 0 {
						fmt.Fprintf(out, "    writable: %s\n", strings.Join(v.Writable, ", "))
					}
					if len(v.EnvAllow) > 0 {
						fmt.Fprintf(out, "    env: %s\n", strings.Join(v.EnvAllow, ", "))
					}
				}
				if len(entry.DefaultVariants) > 0 {
					fmt.Fprintf(out, "\nDefault variants: %s\n", strings.Join(entry.DefaultVariants, ", "))
				}
			}

			return nil
		},
	}
}

func capVariantsCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "variants",
		Short:        "List every (capability/variant) pair across all capabilities",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}
			cfg, err := config.Load(config.Dir(), cwd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			userCaps := capability.FromConfigDefs(cfg.Capabilities)
			registry := capability.MergedRegistry(userCaps)
			pairs := make([]string, 0)
			for capName, entry := range registry {
				for _, v := range entry.Variants {
					pairs = append(pairs, capName+"/"+v.Name)
				}
			}
			sort.Strings(pairs)
			for _, p := range pairs {
				fmt.Fprintln(out, p)
			}
			return nil
		},
	}
}

func capShowSection(out io.Writer, label string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(out, "%-12s %s\n", label+":", strings.Join(items, ", "))
}

func capCreateCmd() *cobra.Command {
	var extends string
	var combines, readable, writable, deny, envAllow []string
	var description string

	cmd := &cobra.Command{
		Use:          "create <name>",
		Short:        "Create a new capability definition",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			name := args[0]

			// Validate: name must not conflict with a built-in capability
			builtins := capability.Builtins()
			if _, isBuiltin := builtins[name]; isBuiltin {
				return fmt.Errorf("capability %q is a built-in capability and cannot be overridden", name)
			}

			// Validate: --extends and --combines are mutually exclusive
			if extends != "" && len(combines) > 0 {
				return fmt.Errorf("--extends and --combines are mutually exclusive; use one or the other")
			}

			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}

			cfg, err := config.Load(config.Dir(), cwd)
			if err != nil {
				cfg = &config.Config{}
			}
			if cfg.Capabilities == nil {
				cfg.Capabilities = make(map[string]config.CapabilityDef)
			}

			if _, exists := cfg.Capabilities[name]; exists {
				return fmt.Errorf("capability %q already exists (use 'aide cap edit' to modify)", name)
			}

			// Build a lookup of all known capabilities (built-in + user-defined)
			allKnown := make(map[string]bool, len(builtins)+len(cfg.Capabilities))
			for k := range builtins {
				allKnown[k] = true
			}
			for k := range cfg.Capabilities {
				allKnown[k] = true
			}

			// Validate: referenced capabilities must exist
			if extends != "" {
				if !allKnown[extends] {
					return fmt.Errorf("parent capability %q does not exist in built-in or user-defined registry", extends)
				}
			}
			for _, c := range combines {
				if !allKnown[c] {
					return fmt.Errorf("combined capability %q does not exist in built-in or user-defined registry", c)
				}
			}

			capDef := config.CapabilityDef{
				Extends:     extends,
				Combines:    combines,
				Description: description,
				Readable:    readable,
				Writable:    writable,
				Deny:        deny,
				EnvAllow:    envAllow,
			}

			cfg.Capabilities[name] = capDef

			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}

			fmt.Fprintf(out, "Created capability %q\n", name)
			return nil
		},
	}

	cmd.Flags().StringVar(&extends, "extends", "", "Parent capability to extend")
	cmd.Flags().StringSliceVar(&combines, "combines", nil, "Capabilities to combine")
	cmd.Flags().StringSliceVar(&readable, "readable", nil, "Readable paths")
	cmd.Flags().StringSliceVar(&writable, "writable", nil, "Writable paths")
	cmd.Flags().StringSliceVar(&deny, "deny", nil, "Denied paths")
	cmd.Flags().StringSliceVar(&envAllow, "env-allow", nil, "Environment variables to pass through")
	cmd.Flags().StringVar(&description, "description", "", "Human-readable description")

	return cmd
}

func capEditCmd() *cobra.Command {
	var addReadable, addWritable, addDeny, removeDeny, addEnvAllow, removeEnvAllow []string
	var description string

	cmd := &cobra.Command{
		Use:          "edit <name>",
		Short:        "Edit a user-defined capability",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			name := args[0]

			// Must not be a built-in capability
			builtins := capability.Builtins()
			if _, isBuiltin := builtins[name]; isBuiltin {
				return fmt.Errorf("capability %q is a built-in capability and cannot be edited", name)
			}

			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}

			cfg, err := config.Load(config.Dir(), cwd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			if cfg.Capabilities == nil {
				return fmt.Errorf("capability %q not found in user-defined capabilities", name)
			}

			capDef, exists := cfg.Capabilities[name]
			if !exists {
				return fmt.Errorf("capability %q not found in user-defined capabilities", name)
			}

			// Apply description change
			if cmd.Flags().Changed("description") {
				capDef.Description = description
			}

			// Apply additive changes
			capDef.Readable = append(capDef.Readable, addReadable...)
			capDef.Writable = append(capDef.Writable, addWritable...)
			capDef.Deny = append(capDef.Deny, addDeny...)
			capDef.EnvAllow = append(capDef.EnvAllow, addEnvAllow...)

			// Apply removals
			for _, r := range removeDeny {
				capDef.Deny = display.RemoveFromSlice(capDef.Deny, r)
			}
			for _, r := range removeEnvAllow {
				capDef.EnvAllow = display.RemoveFromSlice(capDef.EnvAllow, r)
			}

			cfg.Capabilities[name] = capDef

			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}

			fmt.Fprintf(out, "Updated capability %q\n", name)
			return nil
		},
	}

	cmd.Flags().StringSliceVar(&addReadable, "add-readable", nil, "Readable paths to add")
	cmd.Flags().StringSliceVar(&addWritable, "add-writable", nil, "Writable paths to add")
	cmd.Flags().StringSliceVar(&addDeny, "add-deny", nil, "Denied paths to add")
	cmd.Flags().StringSliceVar(&removeDeny, "remove-deny", nil, "Denied paths to remove")
	cmd.Flags().StringSliceVar(&addEnvAllow, "add-env-allow", nil, "Environment variables to add")
	cmd.Flags().StringSliceVar(&removeEnvAllow, "remove-env-allow", nil, "Environment variables to remove")
	cmd.Flags().StringVar(&description, "description", "", "Update the description")

	return cmd
}

func capEnableCmd() *cobra.Command {
	var contextName string
	var global bool
	cmd := &cobra.Command{
		Use:               "enable <capability>[,capability...]",
		Short:             "Enable capabilities (project-level by default)",
		Args:              cobra.ExactArgs(1),
		SilenceUsage:      true,
		ValidArgsFunction: capabilityCompletionFunc,
		RunE: func(cmd *cobra.Command, args []string) error {
			capNames := display.SplitCommaList(args[0])

			if !global && contextName != "" {
				return fmt.Errorf("the --context flag requires --global")
			}

			if global {
				cfg, ctxName, ctx, err := resolveContextForMutation(contextName)
				if err != nil {
					return err
				}
				userCaps := capability.FromConfigDefs(cfg.Capabilities)
				registry := capability.MergedRegistry(userCaps)
				for _, capName := range capNames {
					if _, ok := registry[capName]; !ok {
						return fmt.Errorf("unknown capability: %q", capName)
					}
				}
				for _, capName := range capNames {
					already := false
					for _, c := range ctx.Capabilities {
						if c == capName {
							already = true
							break
						}
					}
					if already {
						fmt.Fprintf(cmd.OutOrStdout(), "Capability %q is already enabled for context %q\n", capName, ctxName)
						continue
					}
					ctx.Capabilities = append(ctx.Capabilities, capName)
					fmt.Fprintf(cmd.OutOrStdout(), "Capability %q enabled for context %q (global)\n", capName, ctxName)
				}
				cfg.Contexts[ctxName] = ctx
				return config.WriteConfig(cfg)
			}

			// Project path: write to .aide.yaml
			cfg, po, poPath, err := resolveProjectOverrideForMutation()
			if err != nil {
				return err
			}
			userCaps := capability.FromConfigDefs(cfg.Capabilities)
			registry := capability.MergedRegistry(userCaps)
			for _, capName := range capNames {
				if _, ok := registry[capName]; !ok {
					return fmt.Errorf("unknown capability: %q", capName)
				}
			}
			for _, capName := range capNames {
				// Remove from disabled if present
				po.DisabledCapabilities = display.RemoveFromSlice(po.DisabledCapabilities, capName)
				// Add if not already present
				already := false
				for _, c := range po.Capabilities {
					if c == capName {
						already = true
						break
					}
				}
				if already {
					fmt.Fprintf(cmd.OutOrStdout(), "Capability %q is already enabled in project\n", capName)
					continue
				}
				po.Capabilities = append(po.Capabilities, capName)
				fmt.Fprintf(cmd.OutOrStdout(), "Capability %q enabled in project (%s)\n", capName, poPath)
			}
			return config.WriteProjectOverrideWithTrust(poPath, po, trust.DefaultStore())
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "Apply to user-level config instead of project")
	cmd.Flags().StringVar(&contextName, "context", "", "Target context name (requires --global)")
	return cmd
}

func capDisableCmd() *cobra.Command {
	var contextName string
	var global bool
	cmd := &cobra.Command{
		Use:               "disable <capability>[,capability...]",
		Short:             "Disable capabilities (project-level by default)",
		Args:              cobra.ExactArgs(1),
		SilenceUsage:      true,
		ValidArgsFunction: capabilityCompletionFunc,
		RunE: func(cmd *cobra.Command, args []string) error {
			capNames := display.SplitCommaList(args[0])

			if !global && contextName != "" {
				return fmt.Errorf("the --context flag requires --global")
			}

			if global {
				cfg, ctxName, ctx, err := resolveContextForMutation(contextName)
				if err != nil {
					return err
				}
				for _, capName := range capNames {
					found := false
					for _, c := range ctx.Capabilities {
						if c == capName {
							found = true
							break
						}
					}
					if !found {
						fmt.Fprintf(cmd.ErrOrStderr(), "warning: capability %q is not enabled for context %q\n", capName, ctxName)
						continue
					}
					ctx.Capabilities = display.RemoveFromSlice(ctx.Capabilities, capName)
					fmt.Fprintf(cmd.OutOrStdout(), "Capability %q disabled for context %q (global)\n", capName, ctxName)
				}
				cfg.Contexts[ctxName] = ctx
				return config.WriteConfig(cfg)
			}

			// Project path
			_, po, poPath, err := resolveProjectOverrideForMutation()
			if err != nil {
				return err
			}
			for _, capName := range capNames {
				removed := false
				for _, c := range po.Capabilities {
					if c == capName {
						removed = true
						break
					}
				}
				if removed {
					po.Capabilities = display.RemoveFromSlice(po.Capabilities, capName)
					fmt.Fprintf(cmd.OutOrStdout(), "Capability %q removed from project (%s)\n", capName, poPath)
				} else {
					// Not in project caps — add to disabled to negate global
					already := false
					for _, c := range po.DisabledCapabilities {
						if c == capName {
							already = true
							break
						}
					}
					if already {
						fmt.Fprintf(cmd.OutOrStdout(), "Capability %q is already disabled in project\n", capName)
						continue
					}
					po.DisabledCapabilities = append(po.DisabledCapabilities, capName)
					fmt.Fprintf(cmd.OutOrStdout(), "Capability %q disabled in project (%s)\n", capName, poPath)
				}
			}
			return config.WriteProjectOverrideWithTrust(poPath, po, trust.DefaultStore())
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "Apply to user-level config instead of project")
	cmd.Flags().StringVar(&contextName, "context", "", "Target context name (requires --global)")
	return cmd
}

func capNeverAllowCmd() *cobra.Command {
	var envMode bool
	var list bool
	var remove bool

	cmd := &cobra.Command{
		Use:   "never-allow [path or env var]",
		Short: "Manage global never-allow paths and environment variables",
		Long: `Manage the global never_allow and never_allow_env lists.

These entries are always denied regardless of capability configuration.

Examples:
  aide cap never-allow ~/.kube/prod-config            Add path to never_allow
  aide cap never-allow --env VAULT_ROOT_TOKEN          Add env var to never_allow_env
  aide cap never-allow --list                          Show all entries
  aide cap never-allow --remove ~/.kube/prod-config    Remove a path
  aide cap never-allow --remove --env VAULT_ROOT_TOKEN Remove an env var`,
		SilenceUsage: true,
		Args:         cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}

			cfg, err := config.Load(config.Dir(), cwd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			// --list mode: show all entries
			if list {
				if len(cfg.NeverAllow) == 0 && len(cfg.NeverAllowEnv) == 0 {
					fmt.Fprintln(out, "No never-allow entries configured.")
					return nil
				}
				if len(cfg.NeverAllow) > 0 {
					fmt.Fprintln(out, "never_allow paths:")
					for _, p := range cfg.NeverAllow {
						fmt.Fprintf(out, "  %s\n", p)
					}
				}
				if len(cfg.NeverAllowEnv) > 0 {
					fmt.Fprintln(out, "never_allow_env variables:")
					for _, e := range cfg.NeverAllowEnv {
						fmt.Fprintf(out, "  %s\n", e)
					}
				}
				return nil
			}

			// All other modes require an argument
			if len(args) == 0 {
				return fmt.Errorf("a path or environment variable name is required (use --list to show entries)")
			}
			entry := args[0]

			if remove {
				// --remove mode
				if envMode {
					before := len(cfg.NeverAllowEnv)
					cfg.NeverAllowEnv = display.RemoveFromSlice(cfg.NeverAllowEnv, entry)
					if len(cfg.NeverAllowEnv) == before {
						return fmt.Errorf("env var %q not found in never_allow_env", entry)
					}
				} else {
					before := len(cfg.NeverAllow)
					cfg.NeverAllow = display.RemoveFromSlice(cfg.NeverAllow, entry)
					if len(cfg.NeverAllow) == before {
						return fmt.Errorf("path %q not found in never_allow", entry)
					}
				}
				if err := config.WriteConfig(cfg); err != nil {
					return fmt.Errorf("writing config: %w", err)
				}
				if envMode {
					fmt.Fprintf(out, "Removed env var %q from never_allow_env\n", entry)
				} else {
					fmt.Fprintf(out, "Removed path %q from never_allow\n", entry)
				}
				return nil
			}

			// Add mode (default)
			if envMode {
				for _, e := range cfg.NeverAllowEnv {
					if e == entry {
						return fmt.Errorf("env var %q is already in never_allow_env", entry)
					}
				}
				cfg.NeverAllowEnv = append(cfg.NeverAllowEnv, entry)
			} else {
				for _, p := range cfg.NeverAllow {
					if p == entry {
						return fmt.Errorf("path %q is already in never_allow", entry)
					}
				}
				cfg.NeverAllow = append(cfg.NeverAllow, entry)
			}
			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}
			if envMode {
				fmt.Fprintf(out, "Added env var %q to never_allow_env\n", entry)
			} else {
				fmt.Fprintf(out, "Added path %q to never_allow\n", entry)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&envMode, "env", false, "Operate on environment variables instead of paths")
	cmd.Flags().BoolVar(&list, "list", false, "List all never-allow entries")
	cmd.Flags().BoolVar(&remove, "remove", false, "Remove an entry instead of adding")

	return cmd
}

func capCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check <capability>[,capability...]",
		Short: "Preview merged sandbox overrides for given capabilities",
		Long: `Resolve one or more capabilities and display the merged sandbox overrides
that would be applied, along with any credential or composition warnings.
This is a preview — nothing is launched or modified.`,
		Args:              cobra.ExactArgs(1),
		SilenceUsage:      true,
		ValidArgsFunction: capabilityCompletionFunc,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			capNames := display.SplitCommaList(args[0])

			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}

			cfg, err := config.Load(config.Dir(), cwd)
			if err != nil {
				// Allow check to work even without config (built-ins only)
				cfg = &config.Config{}
			}

			userCaps := capability.FromConfigDefs(cfg.Capabilities)
			registry := capability.MergedRegistry(userCaps)

			set, err := capability.ResolveAll(capNames, registry, cfg.NeverAllow, cfg.NeverAllowEnv)
			if err != nil {
				return err
			}

			printCapabilityReport(out, set)
			return nil
		},
	}
}

func capAuditCmd() *cobra.Command {
	var contextName string
	cmd := &cobra.Command{
		Use:          "audit",
		Short:        "Show resolved capabilities for the current context",
		Long:         `Reads the active context's capabilities and displays the merged sandbox overrides and any warnings.`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()

			cfg, ctxName, ctx, err := resolveContextForMutation(contextName)
			if err != nil {
				return err
			}

			if len(ctx.Capabilities) == 0 {
				fmt.Fprintf(out, "Context %q has no capabilities enabled.\n", ctxName)
				return nil
			}

			userCaps := capability.FromConfigDefs(cfg.Capabilities)
			registry := capability.MergedRegistry(userCaps)

			set, err := capability.ResolveAll(ctx.Capabilities, registry, cfg.NeverAllow, cfg.NeverAllowEnv)
			if err != nil {
				return err
			}

			fmt.Fprintf(out, "Context: %s\n\n", ctxName)
			printCapabilityReport(out, set)
			return nil
		},
	}
	cmd.Flags().StringVar(&contextName, "context", "", "target context name")
	return cmd
}

func capSuggestForPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "suggest-for-path <path>",
		Short:        "Suggest capabilities that would grant access to a path",
		Long:         `Outputs capability names (one per line) that would grant access to the given path. Designed for machine consumption by plugin hooks.`,
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			targetPath := args[0]

			// Expand ~ in the target path
			if strings.HasPrefix(targetPath, "~/") {
				home, err := os.UserHomeDir()
				if err != nil {
					return fmt.Errorf("getting home directory: %w", err)
				}
				targetPath = filepath.Join(home, targetPath[2:])
			}

			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}

			cfg, err := config.Load(config.Dir(), cwd)
			if err != nil {
				// Allow suggest to work even without config (built-ins only)
				cfg = &config.Config{}
			}

			userCaps := capability.FromConfigDefs(cfg.Capabilities)
			registry := capability.MergedRegistry(userCaps)

			suggestions := capability.SuggestForPath(targetPath, registry)
			sort.Strings(suggestions)
			for _, name := range suggestions {
				fmt.Fprintln(out, name)
			}
			return nil
		},
	}
}

// printCapabilityReport displays the merged sandbox overrides and warnings for a CapabilitySet.
func printCapabilityReport(out io.Writer, set *capability.Set) {
	overrides := set.ToSandboxOverrides()

	// Show per-capability sources
	fmt.Fprintln(out, "Capabilities:")
	for _, cap := range set.Capabilities {
		if len(cap.Sources) > 1 {
			fmt.Fprintf(out, "  %s (via %s)\n", cap.Name, strings.Join(cap.Sources[1:], " -> "))
		} else {
			fmt.Fprintf(out, "  %s\n", cap.Name)
		}
	}
	fmt.Fprintln(out)

	// Show merged overrides
	fmt.Fprintln(out, "Merged sandbox overrides:")
	capReportSection(out, "Unguard", overrides.Unguard)
	capReportSection(out, "Readable", overrides.ReadableExtra)
	capReportSection(out, "Writable", overrides.WritableExtra)
	capReportSection(out, "Denied", overrides.DeniedExtra)
	capReportSection(out, "EnvAllow", overrides.EnvAllow)

	if len(overrides.Unguard) == 0 && len(overrides.ReadableExtra) == 0 &&
		len(overrides.WritableExtra) == 0 && len(overrides.DeniedExtra) == 0 &&
		len(overrides.EnvAllow) == 0 {
		fmt.Fprintln(out, "  (none)")
	}

	// Show warnings
	credWarnings := capability.CredentialWarnings(overrides.EnvAllow)
	compWarnings := capability.CompositionWarnings(set.Capabilities)

	if len(credWarnings) > 0 || len(compWarnings) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Warnings:")
		for _, env := range credWarnings {
			fmt.Fprintf(out, "  [credential] %s is a known credential-bearing env var\n", env)
		}
		for _, w := range compWarnings {
			fmt.Fprintf(out, "  [composition] %s\n", w)
		}
	}
}

func capReportSection(out io.Writer, label string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(out, "  %-12s %s\n", label+":", strings.Join(items, ", "))
}

// capabilityCompletionFunc is a cobra completion function that returns all
// available capability names.
func capabilityCompletionFunc(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return capabilityNamesForCompletion(), cobra.ShellCompDirectiveNoFileComp
}
