// Package main provides the aide agents commands.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/launcher"
)

func agentsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agents",
		Short: "Manage coding agents",
	}
	cmd.AddCommand(agentsListCmd())
	cmd.AddCommand(agentsAddCmd())
	cmd.AddCommand(agentsRemoveCmd())
	cmd.AddCommand(agentsEditCmd())
	return cmd
}

func agentsAddCmd() *cobra.Command {
	var binary string

	cmd := &cobra.Command{
		Use:          "add <name>",
		Short:        "Register a new agent",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if binary == "" {
				binary = name
			}

			cwd, err := os.Getwd()
			if err != nil {
				cwd = "."
			}
			cfg, loadErr := config.Load(config.Dir(), cwd)
			if loadErr != nil {
				cfg = &config.Config{
					Agents:   make(map[string]config.AgentDef),
					Contexts: make(map[string]config.Context),
				}
			}
			if cfg.Agents == nil {
				cfg.Agents = make(map[string]config.AgentDef)
			}

			if _, ok := cfg.Agents[name]; ok {
				return fmt.Errorf("agent %q already exists. Use 'aide agents edit %s --binary <path>' to update it", name, name)
			}

			cfg.Agents[name] = config.AgentDef{Binary: binary}

			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Added agent %q (binary: %s)\n", name, binary)

			// Check if binary is on PATH
			if _, err := exec.LookPath(binary); err != nil {
				fmt.Fprintf(out, "Warning: %q not found on PATH\n", binary)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&binary, "binary", "", "Binary name or path (defaults to agent name)")
	return cmd
}

func agentsRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "remove <name>",
		Short:        "Remove an agent from configuration",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			cwd, err := os.Getwd()
			if err != nil {
				cwd = "."
			}
			cfg, err := config.Load(config.Dir(), cwd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			if _, ok := cfg.Agents[name]; !ok {
				return fmt.Errorf("agent %q not found", name)
			}

			// Warn if contexts reference this agent
			var refs []string
			for ctxName, ctx := range cfg.Contexts {
				if ctx.Agent == name {
					refs = append(refs, ctxName)
				}
			}

			out := cmd.OutOrStdout()
			if len(refs) > 0 {
				sort.Strings(refs)
				fmt.Fprintf(out, "Warning: agent %q is used by contexts: %s\n", name, strings.Join(refs, ", "))
				fmt.Fprintln(out, "Those contexts will need a different agent.")
			}

			delete(cfg.Agents, name)
			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}
			fmt.Fprintf(out, "Removed agent %q\n", name)
			return nil
		},
	}
}

func agentsEditCmd() *cobra.Command {
	var binary string

	cmd := &cobra.Command{
		Use:          "edit <name>",
		Short:        "Update an agent's binary path",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if binary == "" {
				return fmt.Errorf("--binary flag is required")
			}

			cwd, err := os.Getwd()
			if err != nil {
				cwd = "."
			}
			cfg, err := config.Load(config.Dir(), cwd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			if _, ok := cfg.Agents[name]; !ok {
				return fmt.Errorf("agent %q not found. Use 'aide agents add %s' to create it", name, name)
			}

			cfg.Agents[name] = config.AgentDef{Binary: binary}
			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Updated agent %q (binary: %s)\n", name, binary)
			if _, err := exec.LookPath(binary); err != nil {
				fmt.Fprintf(out, "Warning: %q not found on PATH\n", binary)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&binary, "binary", "", "New binary name or path (required)")
	return cmd
}

func agentsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "list",
		Short:        "List configured and available agents",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				cwd = "."
			}

			out := cmd.OutOrStdout()
			cfg, _ := config.Load(config.Dir(), cwd)

			configured := make(map[string]bool)

			if cfg != nil && len(cfg.Agents) > 0 {
				agentContexts := make(map[string][]string)
				for ctxName, ctx := range cfg.Contexts {
					agentContexts[ctx.Agent] = append(agentContexts[ctx.Agent], ctxName)
				}

				var names []string
				for name := range cfg.Agents {
					names = append(names, name)
				}
				sort.Strings(names)

				for _, name := range names {
					configured[name] = true
					agent := cfg.Agents[name]
					binary := agent.Binary
					if binary == "" {
						binary = name
					}
					resolvedPath, lookErr := exec.LookPath(binary)
					if lookErr != nil {
						resolvedPath = "(not found)"
					}

					usedBy := ""
					if ctxs, ok := agentContexts[name]; ok && len(ctxs) > 0 {
						sort.Strings(ctxs)
						usedBy = fmt.Sprintf("  (used by: %s)", strings.Join(ctxs, ", "))
					}

					fmt.Fprintf(out, "%-10s %s%s\n", name, resolvedPath, usedBy)
				}
			}

			result := launcher.ScanAgents(exec.LookPath)
			var unconfigured []string
			for name, path := range result.Found {
				if !configured[name] {
					unconfigured = append(unconfigured, fmt.Sprintf("%-10s %s  (not configured)", name, path))
				}
			}
			if len(unconfigured) > 0 {
				if len(configured) > 0 {
					fmt.Fprintln(out)
				}
				sort.Strings(unconfigured)
				for _, line := range unconfigured {
					fmt.Fprintln(out, line)
				}
			}

			if len(configured) == 0 && len(unconfigured) == 0 {
				fmt.Fprintln(out, "No agents configured or found on PATH.")
				fmt.Fprintln(out, "Run `aide init` to get started.")
			}

			return nil
		},
	}
}
