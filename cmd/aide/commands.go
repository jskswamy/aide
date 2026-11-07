// Package main provides the aide CLI commands.
package main

import (
	"fmt"
	"os"
	"sort"

	"github.com/jskswamy/aide/internal/capability"
	"github.com/jskswamy/aide/internal/config"
	aidectx "github.com/jskswamy/aide/internal/context"
	"github.com/jskswamy/aide/internal/launcher"
	"github.com/spf13/cobra"
)

func registerCommands(rootCmd *cobra.Command) {
	rootCmd.AddCommand(initCmd())
	rootCmd.AddCommand(whichCmd())
	rootCmd.AddCommand(validateCmd())
	rootCmd.AddCommand(secretsCmd())
	rootCmd.AddCommand(gatewayCmd())
	rootCmd.AddCommand(configCmd())
	rootCmd.AddCommand(agentsCmd())
	rootCmd.AddCommand(useCmd())
	rootCmd.AddCommand(profileCmd())
	rootCmd.AddCommand(contextCmd())
	rootCmd.AddCommand(projectCmd())
	rootCmd.AddCommand(envCmd())
	rootCmd.AddCommand(setupCmd())
	rootCmd.AddCommand(sandboxCmd())
	rootCmd.AddCommand(capCmd())
	rootCmd.AddCommand(statusCmd())
	rootCmd.AddCommand(trustCmd())
	rootCmd.AddCommand(denyCmd())
	rootCmd.AddCommand(untrustCmd())
	rootCmd.AddCommand(pluginCmd())
	rootCmd.AddCommand(mcpCmd())
	rootCmd.AddCommand(skillshareCmd())
	rootCmd.AddCommand(hookCmd())
	rootCmd.AddCommand(syncCmd())
	rootCmd.AddCommand(adoptCmd())
	rootCmd.AddCommand(promptCmd())
	rootCmd.AddCommand(explainCmd())
	rootCmd.AddCommand(statuslineCmd())
	rootCmd.AddCommand(sandboxApplyCmd())
}

// resolveContextForMutation loads config via cmdEnv, resolves the context name,
// and returns the config, context name, and context for modification.
func resolveContextForMutation(contextName string) (*config.Config, string, config.Context, error) {
	env, err := cmdEnv(nil)
	if err != nil {
		return nil, "", config.Context{}, fmt.Errorf("loading config: %w", err)
	}
	return env.ResolveContextForMutation(contextName)
}

// resolveEffectiveCapabilities returns the resolved context name and its
// effective (fully resolved) capabilities list for cwd.
//
// When contextName is "", it resolves the applicable context via
// aidectx.Resolve, which merges any .aide.yaml project override on top
// of the matched context — this answers "what's actually active here."
//
// When contextName is non-empty, it looks up that context by name
// directly, with no project-override merge: project overrides are
// tied to cwd, not to an arbitrary named context being inspected.
//
// The result also auto-includes "ccstatusline" and "skillshare" when
// their tools are set up (capability.AutoInclude), mirroring what
// internal/launcher.Launch grants at actual agent launch — this is
// the same resolution `aide cap list`/`aide cap audit` use, so what they
// report stays in sync with what a real launch actually grants. The
// project's disabled_capabilities opts out of them, as at launch;
// callers here have no --without-style exclusion concept.
// homeDir is a parameter (rather than resolved internally via
// os.UserHomeDir()) so tests stay hermetic against the real machine's
// home directory, matching internal/capability's auto-include tests.
func resolveEffectiveCapabilities(cfg *config.Config, cwd, contextName, homeDir string) (name string, caps []string, err error) {
	if contextName == "" {
		// What a launch applies: untrusted .aide.yaml files contribute
		// nothing (this also gates cfg for the caller).
		if _, err := launcher.GateProjectConfig(cfg); err != nil {
			return "", nil, err
		}
		rc, err := aidectx.Resolve(cfg, cwd, aidectx.GitRemotes(cwd))
		if err != nil {
			return "", nil, fmt.Errorf("resolving profile: %w", err)
		}
		var disabled []string
		if cfg.ProjectOverride != nil {
			disabled = cfg.ProjectOverride.DisabledCapabilities
		}
		return rc.Name, capability.AutoInclude(rc.Context.Capabilities, disabled, homeDir, os.Getenv), nil
	}
	ctx, ok := cfg.Contexts[contextName]
	if !ok {
		return "", nil, fmt.Errorf("context %q not found", contextName)
	}
	return contextName, capability.AutoInclude(ctx.Capabilities, nil, homeDir, os.Getenv), nil
}

// resolveProjectOverrideForMutation loads the global config via cmdEnv and the
// project file to edit. Returns the global config (for validation),
// that file's override as written (empty if it does not exist; never
// the merged or path-rewritten view a launch uses), and its path. In a
// linked worktree the file is the worktree's own .aide.yaml; main
// selects the main checkout's instead.
func resolveProjectOverrideForMutation(main bool) (*config.Config, *config.ProjectOverride, string, error) {
	env, err := cmdEnv(nil)
	if err != nil {
		return nil, nil, "", fmt.Errorf("loading config: %w", err)
	}
	return env.ResolveProjectOverrideForMutation(main)
}

// validateMainScope rejects --main combined with --global.
func validateMainScope(global, main bool) error {
	if global && main {
		return fmt.Errorf("--main edits the main checkout's .aide.yaml and cannot be combined with --global")
	}
	return nil
}

// addMainFlag adds --main to a project-scope editing command.
func addMainFlag(cmd *cobra.Command, main *bool) {
	cmd.Flags().BoolVar(main, "main", false,
		"In a linked git worktree, edit the main checkout's .aide.yaml instead of the worktree's own")
}

// capabilityNamesForCompletion returns a sorted list of all capability names
// (built-in + user-defined from config) for shell tab completion.
//
// This helper is called from the cobra flag completion callback in main.go,
// which receives (*cobra.Command, []string, string) — the *cobra.Command it
// gets is the one being completed against, and wiring cmdEnv through the
// completion path would push Env construction into every tab-completion call.
// The direct config.Load here is both best-effort (errors swallowed so a
// partially loadable config still yields the built-in completions) and
// performance-sensitive, so it stays inline.
func capabilityNamesForCompletion() []string {
	builtins := capability.Builtins()
	names := make([]string, 0, len(builtins))
	for name := range builtins {
		names = append(names, name)
	}

	// Try to load config for user-defined capabilities.
	cwd, err := os.Getwd()
	if err == nil {
		if cfg, err := config.Load(config.Dir(), cwd); err == nil {
			userCaps := capability.FromConfigDefs(cfg.Capabilities)
			for name := range userCaps {
				// Only add if not already present from builtins.
				if _, exists := builtins[name]; !exists {
					names = append(names, name)
				}
			}
		}
	}

	sort.Strings(names)
	return names
}
