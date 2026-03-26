package main

import (
	"fmt"
	"os"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/launcher"
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

			l := &launcher.Launcher{
				Execer:              &launcher.SyscallExecer{},
				Yolo:                yolo || autoApprove,
				NoYolo:              noYolo || noAutoApprove,
				IgnoreProjectConfig: ignoreProjectConfig,
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
