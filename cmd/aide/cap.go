// cmd/aide/cap.go
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/jskswamy/aide/internal/capability"
	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/consent"
	"github.com/jskswamy/aide/internal/display"
	"github.com/jskswamy/aide/internal/fsutil"
	"github.com/jskswamy/aide/internal/homepath"
	"github.com/jskswamy/aide/internal/output"
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

type capConsentEntry struct {
	Capability  string    `json:"capability"`
	Variants    []string  `json:"variants,omitempty"`
	ConfirmedAt time.Time `json:"confirmed_at"`
	Markers     string    `json:"markers,omitempty"`
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
				env, err := cmdEnv(cmd)
				if err != nil {
					return fmt.Errorf("loading config: %w", err)
				}
				project = env.CWD()
			}
			store := consent.DefaultStore()
			grants, err := store.List(project)
			if err != nil {
				return fmt.Errorf("listing consents: %w", err)
			}
			entries := make([]capConsentEntry, 0, len(grants))
			for _, g := range grants {
				entries = append(entries, capConsentEntry{
					Capability:  g.Capability,
					Variants:    g.Variants,
					ConfirmedAt: g.ConfirmedAt,
					Markers:     g.Summary,
				})
			}

			format, ferr := output.FromCmd(cmd)
			if ferr != nil {
				return ferr
			}
			return output.Emit(out, format, entries, func(w io.Writer) error {
				if len(entries) == 0 {
					fmt.Fprintf(w, "no consents recorded for %s\n", project)
					return nil
				}
				for _, e := range entries {
					fmt.Fprintf(w, "%s  variants=%s  confirmed_at=%s  markers=%s\n",
						e.Capability,
						strings.Join(e.Variants, ","),
						e.ConfirmedAt.Format(time.RFC3339),
						e.Markers,
					)
				}
				return nil
			})
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
			env, err := cmdEnv(cmd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			store := consent.DefaultStore()
			if err := store.Revoke(env.CWD(), args[0]); err != nil {
				return fmt.Errorf("revoking: %w", err)
			}
			fmt.Fprintf(out, "revoked all %s consents for %s\n", args[0], env.CWD())
			return nil
		},
	}
}

type capListEntry struct {
	Name        string `json:"name"`
	Status      string `json:"status"`
	Source      string `json:"source"`
	Description string `json:"description"`
}

func capListCmd() *cobra.Command {
	var contextName string
	cmd := &cobra.Command{
		Use:          "list",
		Short:        "List all capabilities (built-in and user-defined)",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			env, err := cmdEnv(cmd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			registry := env.Registry()
			userCaps := capability.FromConfigDefs(env.Config().Capabilities)
			builtins := capability.Builtins()

			home, _ := os.UserHomeDir()
			_, enabled, err := resolveEffectiveCapabilities(env.Config(), env.CWD(), contextName, home)
			if err != nil {
				return err
			}
			enabledSet := make(map[string]bool, len(enabled))
			for _, name := range enabled {
				enabledSet[name] = true
			}

			var disabledSet map[string]bool
			if contextName == "" && env.Config().ProjectOverride != nil {
				disabled := env.Config().ProjectOverride.DisabledCapabilities
				disabledSet = make(map[string]bool, len(disabled))
				for _, name := range disabled {
					disabledSet[name] = true
				}
			}

			suggested := capability.DetectProject(os.DirFS(env.CWD()))
			suggestedSet := make(map[string]bool, len(suggested))
			for _, name := range suggested {
				suggestedSet[name] = true
			}

			// Collect and sort names
			names := make([]string, 0, len(registry))
			for name := range registry {
				names = append(names, name)
			}
			sort.Strings(names)

			entries := make([]capListEntry, 0, len(names))
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
					source = "custom"
				}
				desc := entry.Description
				if len(entry.Variants) > 0 {
					vnames := make([]string, len(entry.Variants))
					for i, v := range entry.Variants {
						vnames[i] = v.Name
					}
					desc = fmt.Sprintf("%s (%d variants: %s)", desc, len(entry.Variants), strings.Join(vnames, ", "))
				}
				status := capListStatus(name, enabledSet, disabledSet, suggestedSet)
				entries = append(entries, capListEntry{Name: name, Status: status, Source: source, Description: desc})
			}

			format, ferr := output.FromCmd(cmd)
			if ferr != nil {
				return ferr
			}
			return output.Emit(out, format, entries, func(w io.Writer) error {
				fmt.Fprintf(w, "%-20s %-12s %-12s %s\n", "NAME", "STATUS", "SOURCE", "DESCRIPTION")
				for _, e := range entries {
					fmt.Fprintf(w, "%-20s %-12s %-12s %s\n", e.Name, e.Status, e.Source, e.Description)
				}
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&contextName, "context", "", "target context name")
	return cmd
}

// capListStatus reports a capability's activation state for the current
// project: "enabled" if active, "disabled" if explicitly negated by a
// project override, "suggested" if its markers match this project but
// it isn't enabled, or "-" if none apply. Precedence: enabled > disabled
// > suggested (in practice these never overlap, since suggested is only
// computed for names that aren't enabled, and a name can't be both
// enabled and disabled given how the merge in resolveEffectiveCapabilities
// works).
func capListStatus(name string, enabled, disabled, suggested map[string]bool) string {
	switch {
	case enabled[name]:
		return "enabled"
	case disabled[name]:
		return "disabled"
	case suggested[name]:
		return "suggested"
	default:
		return "-"
	}
}

type capPathEntry struct {
	Declared    string `json:"declared"`
	Resolved    string `json:"resolved,omitempty"`
	OutsideHome bool   `json:"outside_home,omitempty"`
}

// resolvePathEntries mirrors the per-item symlink-resolution logic that
// used to live inline in capShowPathSection. Shared by the JSON result
// builder and (refactored) capShowPathSection so both surfaces agree.
func resolvePathEntries(items []string, home string) []capPathEntry {
	entries := make([]capPathEntry, 0, len(items))
	for _, p := range items {
		entry := capPathEntry{Declared: p}
		expanded := homepath.Expand(p, home)
		resolved, changed, underHome := fsutil.ResolveWidening(expanded, home)
		if changed {
			entry.Resolved = resolved
			entry.OutsideHome = !underHome
		}
		entries = append(entries, entry)
	}
	return entries
}

type capVariantEntry struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Markers     []string `json:"markers,omitempty"`
	Readable    []string `json:"readable,omitempty"`
	Writable    []string `json:"writable,omitempty"`
	EnvAllow    []string `json:"env_allow,omitempty"`
}

type capShowResult struct {
	Name            string            `json:"name"`
	Description     string            `json:"description"`
	Sources         []string          `json:"sources,omitempty"`
	Unguard         []string          `json:"unguard,omitempty"`
	Readable        []capPathEntry    `json:"readable,omitempty"`
	Writable        []capPathEntry    `json:"writable,omitempty"`
	Deny            []capPathEntry    `json:"deny,omitempty"`
	EnvAllow        []string          `json:"env_allow,omitempty"`
	Allow           []string          `json:"allow,omitempty"`
	Variants        []capVariantEntry `json:"variants,omitempty"`
	DefaultVariants []string          `json:"default_variants,omitempty"`
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

			env, err := cmdEnv(cmd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			registry := env.Registry()

			if _, ok := registry[name]; !ok {
				return fmt.Errorf("unknown capability: %q", name)
			}

			resolved, err := capability.ResolveOne(name, registry)
			if err != nil {
				return fmt.Errorf("resolving capability: %w", err)
			}

			entry := registry[name]
			home, _ := os.UserHomeDir()
			result := capShowResult{
				Name:        name,
				Description: entry.Description,
			}
			if len(resolved.Sources) > 1 {
				result.Sources = resolved.Sources
			}
			result.Unguard = resolved.Unguard
			result.Readable = resolvePathEntries(resolved.Readable, home)
			result.Writable = resolvePathEntries(resolved.Writable, home)
			result.Deny = resolvePathEntries(resolved.Deny, home)
			result.EnvAllow = resolved.EnvAllow
			result.Allow = resolved.Allow
			for _, v := range entry.Variants {
				ve := capVariantEntry{
					Name:        v.Name,
					Description: v.Description,
					Readable:    v.Readable,
					Writable:    v.Writable,
					EnvAllow:    v.EnvAllow,
				}
				for _, m := range v.Markers {
					ve.Markers = append(ve.Markers, m.MatchSummary())
				}
				result.Variants = append(result.Variants, ve)
			}
			result.DefaultVariants = entry.DefaultVariants

			format, ferr := output.FromCmd(cmd)
			if ferr != nil {
				return ferr
			}
			return output.Emit(out, format, result, func(w io.Writer) error {
				fmt.Fprintf(w, "Name:        %s\n", result.Name)
				fmt.Fprintf(w, "Description: %s\n", result.Description)
				if len(result.Sources) > 1 {
					fmt.Fprintf(w, "Sources:     %s\n", strings.Join(result.Sources, " -> "))
				}
				capShowSection(w, "Unguard", result.Unguard)
				capShowPathSection(w, "Readable", resolved.Readable, home)
				capShowPathSection(w, "Writable", resolved.Writable, home)
				capShowPathSection(w, "Deny", resolved.Deny, home)
				capShowSection(w, "EnvAllow", result.EnvAllow)
				capShowSection(w, "Allow", result.Allow)
				if len(entry.Variants) > 0 {
					fmt.Fprintln(w, "")
					fmt.Fprintln(w, "Variants:")
					for _, v := range entry.Variants {
						fmt.Fprintf(w, "  %s", v.Name)
						if v.Description != "" {
							fmt.Fprintf(w, " — %s", v.Description)
						}
						fmt.Fprintln(w)
						for _, m := range v.Markers {
							fmt.Fprintf(w, "    marker: %s\n", m.MatchSummary())
						}
						if len(v.Readable) > 0 {
							fmt.Fprintf(w, "    readable: %s\n", strings.Join(v.Readable, ", "))
						}
						if len(v.Writable) > 0 {
							fmt.Fprintf(w, "    writable: %s\n", strings.Join(v.Writable, ", "))
						}
						if len(v.EnvAllow) > 0 {
							fmt.Fprintf(w, "    env: %s\n", strings.Join(v.EnvAllow, ", "))
						}
					}
					if len(entry.DefaultVariants) > 0 {
						fmt.Fprintf(w, "\nDefault variants: %s\n", strings.Join(entry.DefaultVariants, ", "))
					}
				}
				return nil
			})
		},
	}
}

type capVariantPair struct {
	Capability string `json:"capability"`
	Variant    string `json:"variant"`
}

func capVariantsCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "variants",
		Short:        "List every (capability/variant) pair across all capabilities",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			env, err := cmdEnv(cmd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			registry := env.Registry()
			pairs := make([]capVariantPair, 0)
			for capName, entry := range registry {
				for _, v := range entry.Variants {
					pairs = append(pairs, capVariantPair{Capability: capName, Variant: v.Name})
				}
			}
			sort.Slice(pairs, func(i, j int) bool {
				if pairs[i].Capability != pairs[j].Capability {
					return pairs[i].Capability < pairs[j].Capability
				}
				return pairs[i].Variant < pairs[j].Variant
			})

			format, ferr := output.FromCmd(cmd)
			if ferr != nil {
				return ferr
			}
			return output.Emit(out, format, pairs, func(w io.Writer) error {
				for _, p := range pairs {
					fmt.Fprintln(w, p.Capability+"/"+p.Variant)
				}
				return nil
			})
		},
	}
}

func capShowSection(out io.Writer, label string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(out, "%-12s %s\n", label+":", strings.Join(items, ", "))
}

// capShowPathSection renders a path-bearing field (Readable / Writable /
// Deny) with symlink-resolution annotation. For each entry:
//
//   - declared path is always shown
//   - if it resolves to a different path via filepath.EvalSymlinks, the
//     resolved target follows after " → "
//   - if the resolved target falls outside home, a "⚠ outside $HOME"
//     marker trails — the sandbox refuses to silently widen there
//     (AIDE-46h) and writes through the symlink will EPERM until the
//     user opts in via the escape hatch (AIDE-mu8).
//
// When the section has no entries it emits nothing. When no entry has a
// resolution annotation, format collapses to the single-line "Label:
// a, b, c" shape used by capShowSection so casual output stays compact.
func capShowPathSection(out io.Writer, label string, items []string, home string) {
	if len(items) == 0 {
		return
	}
	rows := resolvePathEntries(items, home)
	hasResolution := false
	for _, r := range rows {
		if r.Resolved != "" {
			hasResolution = true
			break
		}
	}
	if !hasResolution {
		fmt.Fprintf(out, "%-12s %s\n", label+":", strings.Join(items, ", "))
		return
	}
	fmt.Fprintf(out, "%s:\n", label)
	for _, r := range rows {
		switch {
		case r.OutsideHome:
			fmt.Fprintf(out, "  %s  →  %s  ⚠ outside $HOME (resolved target will not be widened; use AIDE-mu8 escape hatch to opt in)\n", r.Declared, r.Resolved)
		case r.Resolved != "":
			fmt.Fprintf(out, "  %s  →  %s\n", r.Declared, r.Resolved)
		default:
			fmt.Fprintf(out, "  %s\n", r.Declared)
		}
	}
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

			env, _ := cmdEnv(cmd)
			cfg := env.Config()
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

			env, err := cmdEnv(cmd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			cfg := env.Config()

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

			if err := validateContextScope(global, contextName); err != nil {
				return err
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
					if slices.Contains(ctx.Capabilities, capName) {
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
				if slices.Contains(po.Capabilities, capName) {
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

			if err := validateContextScope(global, contextName); err != nil {
				return err
			}

			if global {
				cfg, ctxName, ctx, err := resolveContextForMutation(contextName)
				if err != nil {
					return err
				}
				for _, capName := range capNames {
					if !slices.Contains(ctx.Capabilities, capName) {
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
				if slices.Contains(po.Capabilities, capName) {
					po.Capabilities = display.RemoveFromSlice(po.Capabilities, capName)
					fmt.Fprintf(cmd.OutOrStdout(), "Capability %q removed from project (%s)\n", capName, poPath)
				} else {
					// Not in project caps — add to disabled to negate global
					if slices.Contains(po.DisabledCapabilities, capName) {
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

			env, err := cmdEnv(cmd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			cfg := env.Config()

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
				if slices.Contains(cfg.NeverAllowEnv, entry) {
					return fmt.Errorf("env var %q is already in never_allow_env", entry)
				}
				cfg.NeverAllowEnv = append(cfg.NeverAllowEnv, entry)
			} else {
				if slices.Contains(cfg.NeverAllow, entry) {
					return fmt.Errorf("path %q is already in never_allow", entry)
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

			// Allow check to work even without config (built-ins only)
			env, _ := cmdEnv(cmd)
			cfg := env.Config()
			registry := env.Registry()

			set, err := capability.ResolveAll(capNames, registry, cfg.NeverAllow, cfg.NeverAllowEnv)
			if err != nil {
				return err
			}

			result := buildCapReportResult(set, "")
			format, ferr := output.FromCmd(cmd)
			if ferr != nil {
				return ferr
			}
			return output.Emit(out, format, result, func(w io.Writer) error {
				renderCapReportHuman(w, result)
				return nil
			})
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

			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}
			cfg, err := config.Load(config.Dir(), cwd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			home, _ := os.UserHomeDir()
			ctxName, caps, err := resolveEffectiveCapabilities(cfg, cwd, contextName, home)
			if err != nil {
				return err
			}

			if len(caps) == 0 {
				fmt.Fprintf(out, "Context %q has no capabilities enabled.\n", ctxName)
				return nil
			}

			userCaps := capability.FromConfigDefs(cfg.Capabilities)
			registry := capability.MergedRegistry(userCaps)

			set, err := capability.ResolveAll(caps, registry, cfg.NeverAllow, cfg.NeverAllowEnv)
			if err != nil {
				return err
			}

			result := buildCapReportResult(set, ctxName)
			format, ferr := output.FromCmd(cmd)
			if ferr != nil {
				return ferr
			}
			return output.Emit(out, format, result, func(w io.Writer) error {
				fmt.Fprintf(w, "Context: %s\n\n", ctxName)
				renderCapReportHuman(w, result)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&contextName, "context", "", "target context name")
	return cmd
}

type capSuggestResult struct {
	Path        string   `json:"path"`
	Suggestions []string `json:"suggestions"`
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

			// Allow suggest to work even without config (built-ins only)
			env, _ := cmdEnv(cmd)
			registry := env.Registry()

			suggestions := capability.SuggestForPath(targetPath, registry)
			sort.Strings(suggestions)
			result := capSuggestResult{Path: targetPath, Suggestions: suggestions}

			format, ferr := output.FromCmd(cmd)
			if ferr != nil {
				return ferr
			}
			return output.Emit(out, format, result, func(w io.Writer) error {
				for _, name := range result.Suggestions {
					fmt.Fprintln(w, name)
				}
				return nil
			})
		},
	}
}

type capReportEntry struct {
	Name    string   `json:"name"`
	Sources []string `json:"sources,omitempty"`
}

type capReportResult struct {
	Context             string           `json:"context,omitempty"`
	Capabilities        []capReportEntry `json:"capabilities"`
	Unguard             []string         `json:"unguard,omitempty"`
	Readable            []string         `json:"readable,omitempty"`
	Writable            []string         `json:"writable,omitempty"`
	Denied              []string         `json:"denied,omitempty"`
	EnvAllow            []string         `json:"env_allow,omitempty"`
	CredentialWarnings  []string         `json:"credential_warnings,omitempty"`
	CompositionWarnings []string         `json:"composition_warnings,omitempty"`
}

// buildCapReportResult mirrors what printCapabilityReport used to
// print, as data. context is "" for cap check (no context concept).
func buildCapReportResult(set *capability.Set, context string) capReportResult {
	overrides := set.ToSandboxOverrides()
	entries := make([]capReportEntry, 0, len(set.Capabilities))
	for _, cap := range set.Capabilities {
		e := capReportEntry{Name: cap.Name}
		if len(cap.Sources) > 1 {
			e.Sources = cap.Sources[1:]
		}
		entries = append(entries, e)
	}
	return capReportResult{
		Context:             context,
		Capabilities:        entries,
		Unguard:             overrides.Unguard,
		Readable:            overrides.ReadableExtra,
		Writable:            overrides.WritableExtra,
		Denied:              overrides.DeniedExtra,
		EnvAllow:            overrides.EnvAllow,
		CredentialWarnings:  capability.CredentialWarnings(overrides.EnvAllow),
		CompositionWarnings: capability.CompositionWarnings(set.Capabilities),
	}
}

// renderCapReportHuman renders r exactly as printCapabilityReport used
// to. Does not print r.Context — callers that need a "Context: %s\n\n"
// header (capAuditCmd) print it themselves before calling this.
func renderCapReportHuman(out io.Writer, r capReportResult) {
	fmt.Fprintln(out, "Capabilities:")
	for _, e := range r.Capabilities {
		if len(e.Sources) > 0 {
			fmt.Fprintf(out, "  %s (via %s)\n", e.Name, strings.Join(e.Sources, " -> "))
		} else {
			fmt.Fprintf(out, "  %s\n", e.Name)
		}
	}
	fmt.Fprintln(out)

	fmt.Fprintln(out, "Merged sandbox overrides:")
	capReportSection(out, "Unguard", r.Unguard)
	capReportSection(out, "Readable", r.Readable)
	capReportSection(out, "Writable", r.Writable)
	capReportSection(out, "Denied", r.Denied)
	capReportSection(out, "EnvAllow", r.EnvAllow)

	if len(r.Unguard) == 0 && len(r.Readable) == 0 &&
		len(r.Writable) == 0 && len(r.Denied) == 0 && len(r.EnvAllow) == 0 {
		fmt.Fprintln(out, "  (none)")
	}

	if len(r.CredentialWarnings) > 0 || len(r.CompositionWarnings) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Warnings:")
		for _, env := range r.CredentialWarnings {
			fmt.Fprintf(out, "  [credential] %s is a known credential-bearing env var\n", env)
		}
		for _, w := range r.CompositionWarnings {
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
