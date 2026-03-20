package main

import (
	"fmt"
	"os"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/launcher"
	"github.com/spf13/cobra"
)

var version = "dev"

func main() {
	var agentFlag string
	var cleanEnv bool
	var yolo bool

	rootCmd := &cobra.Command{
		Use:   "aide [flags] [-- agent-args...]",
		Short: "Universal Coding Agent Context Manager",
		Long: `aide resolves context, decrypts secrets, and launches your coding agent
with the right environment. Without a config file, it auto-detects
agents on your PATH.`,
		Version:            version,
		DisableFlagParsing: false,
		SilenceUsage:       true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if yolo {
				fmt.Fprintln(os.Stderr, "\033[1;33mWARNING:\033[0m --yolo mode enabled")
				fmt.Fprintln(os.Stderr, "  Agent permission checks are disabled.")
				fmt.Fprintln(os.Stderr, "  OS sandbox is active with default policy (use `aide sandbox show` to inspect).")
				fmt.Fprintln(os.Stderr, "")
			}

			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}

			l := &launcher.Launcher{
				Execer: &launcher.SyscallExecer{},
				Yolo:   yolo,
			}

			// Check if a config file exists.
			configFile := config.ConfigFilePath()
			if _, err := os.Stat(configFile); os.IsNotExist(err) {
				return l.Passthrough(cwd, agentFlag, args)
			}

			// Config exists — use full launcher flow.
			return l.Launch(cwd, agentFlag, args, cleanEnv)
		},
	}

	rootCmd.Flags().StringVar(&agentFlag, "agent", "", "Override which agent to launch")
	rootCmd.Flags().BoolVar(&cleanEnv, "clean-env", false, "Start agent with only essential environment variables")
	rootCmd.Flags().BoolVar(&yolo, "yolo", false, "Launch agent with skip-permissions (agent-specific, sandbox still applies)")

	registerCommands(rootCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
