package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/consent"
	"github.com/jskswamy/aide/internal/launcher"
	"github.com/jskswamy/aide/internal/ui"
	"github.com/spf13/cobra"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	var agentFlag string
	var cleanEnv bool
	var yolo bool
	var noYolo bool
	var autoApprove bool
	var noAutoApprove bool
	var resolve bool
	var withCaps []string
	var withoutCaps []string
	var ignoreProjectConfig bool
	var unrestrictedNetwork bool
	var variantFlag []string
	var autoYes bool

	rootCmd := &cobra.Command{
		Use:   "aide [flags] [-- agent-args...]",
		Short: "Universal Coding Agent Context Manager",
		Long: `aide resolves context, decrypts secrets, and launches your coding agent
with the right environment. Without a config file, it auto-detects
agents on your PATH.`,
		Version:            version,
		DisableFlagParsing: false,
		SilenceUsage:       true,
		RunE: func(_ *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}

			variantOverrides, verr := parseVariantFlag(variantFlag, withCaps)
			if verr != nil {
				return verr
			}

			l := &launcher.Launcher{
				Execer:              &launcher.SyscallExecer{},
				Yolo:                yolo || autoApprove,
				NoYolo:              noYolo || noAutoApprove,
				IgnoreProjectConfig: ignoreProjectConfig,
				UnrestrictedNetwork: unrestrictedNetwork,
				VariantOverrides:    variantOverrides,
				AutoYes:             autoYes,
				Interactive:         isInteractiveTerminal(os.Stdin),
				ConsentStore:        consent.DefaultStore(),
			}
			if l.Interactive {
				l.Prompter = ui.NewTTYPrompter(os.Stdin, os.Stderr)
			}

			// Check if a config file exists.
			configFile := config.FilePath()
			if _, err := os.Stat(configFile); os.IsNotExist(err) {
				return l.Passthrough(cwd, agentFlag, args)
			}

			// Config exists — use full launcher flow.
			return l.Launch(cwd, agentFlag, args, cleanEnv, resolve, withCaps, withoutCaps)
		},
	}

	rootCmd.SetVersionTemplate("aide " + version + " (commit: " + commit + ", built: " + date + ")\n")

	rootCmd.Flags().StringVar(&agentFlag, "agent", "", "Override which agent to launch")
	rootCmd.Flags().BoolVar(&cleanEnv, "clean-env", false, "Start agent with only essential environment variables")
	rootCmd.Flags().BoolVar(&yolo, "yolo", false, "Launch agent with skip-permissions (agent-specific, sandbox still applies)")
	rootCmd.Flags().BoolVar(&noYolo, "no-yolo", false, "Disable yolo mode (overrides config and --yolo flag)")
	_ = rootCmd.Flags().MarkHidden("yolo")
	_ = rootCmd.Flags().MarkHidden("no-yolo")
	rootCmd.Flags().BoolVar(&autoApprove, "auto-approve", false, "Run agent without permission checks (sandbox still applies)")
	rootCmd.Flags().BoolVar(&noAutoApprove, "no-auto-approve", false, "Override config: require permission checks")
	rootCmd.Flags().StringSliceVar(&withCaps, "with", nil, "Activate capabilities for this session (e.g., --with k8s,docker)")
	rootCmd.Flags().StringSliceVar(&withoutCaps, "without", nil, "Disable context capabilities for this session")
	rootCmd.Flags().BoolVar(&ignoreProjectConfig, "ignore-project-config", false, "Launch without applying .aide.yaml")
	rootCmd.Flags().BoolVarP(&unrestrictedNetwork, "unrestricted-network", "N", false,
		"Allow unrestricted network access, ignoring config port rules")
	rootCmd.Flags().StringSliceVar(&variantFlag, "variant", nil,
		"Pin variants for capabilities in --with (format: capability=variant). Repeatable. Must match a --with capability.")
	rootCmd.Flags().BoolVar(&autoYes, "yes", false, "Auto-approve variant consent prompts (non-interactive workflows).")
	rootCmd.PersistentFlags().BoolVar(&resolve, "resolve", false, "Show detailed startup info")

	registerCommands(rootCmd)

	_ = rootCmd.RegisterFlagCompletionFunc("with", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return capabilityNamesForCompletion(), cobra.ShellCompDirectiveNoFileComp
	})
	_ = rootCmd.RegisterFlagCompletionFunc("without", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return capabilityNamesForCompletion(), cobra.ShellCompDirectiveNoFileComp
	})

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// parseVariantFlag turns ["python=uv", "node=pnpm"] into a map keyed
// by capability name. Returns an error if a capability is not active
// in activeCaps, or an entry is malformed.
func parseVariantFlag(raw []string, activeCaps []string) (map[string][]string, error) {
	active := make(map[string]bool, len(activeCaps))
	for _, c := range activeCaps {
		active[c] = true
	}
	out := make(map[string][]string)
	for _, entry := range raw {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return nil, fmt.Errorf("--variant %q: expected capability=variant", entry)
		}
		capName, variant := parts[0], parts[1]
		if !active[capName] {
			return nil, fmt.Errorf("--variant %s=%s requires --with %s", capName, variant, capName)
		}
		out[capName] = append(out[capName], variant)
	}
	return out, nil
}

// isInteractiveTerminal reports whether f is a character device (TTY).
// Returns false when stdin is a pipe or redirected file, so CI runs
// default to non-interactive variant selection.
func isInteractiveTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

