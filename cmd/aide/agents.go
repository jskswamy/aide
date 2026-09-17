// Package main provides the aide agents commands.
package main

import (
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/launcher"
	"github.com/jskswamy/aide/internal/output"
)

type agentEntry struct {
	Name       string   `json:"name"`
	Path       string   `json:"path"`
	Configured bool     `json:"configured"`
	UsedBy     []string `json:"used_by,omitempty"`
}

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

			env, _ := cmdEnv(cmd)
			cfg := env.Config()
			if cfg.Agents == nil {
				cfg.Agents = make(map[string]config.AgentDef)
			}
			if cfg.Contexts == nil {
				cfg.Contexts = make(map[string]config.Context)
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
			env, err := cmdEnv(cmd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			cfg := env.Config()
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

			env, err := cmdEnv(cmd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			cfg := env.Config()
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
			env, _ := cmdEnv(cmd)
			out := cmd.OutOrStdout()
			cfg := env.Config()

			configured := make(map[string]bool)
			entries := make([]agentEntry, 0)

			if len(cfg.Agents) > 0 {
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
					var usedBy []string
					if ctxs, ok := agentContexts[name]; ok && len(ctxs) > 0 {
						sort.Strings(ctxs)
						usedBy = ctxs
					}
					entries = append(entries, agentEntry{Name: name, Path: resolvedPath, Configured: true, UsedBy: usedBy})
				}
			}

			result := launcher.ScanAgents(exec.LookPath)
			var unconfiguredNames []string
			for name := range result.Found {
				if !configured[name] {
					unconfiguredNames = append(unconfiguredNames, name)
				}
			}
			sort.Strings(unconfiguredNames)
			for _, name := range unconfiguredNames {
				entries = append(entries, agentEntry{Name: name, Path: result.Found[name], Configured: false})
			}

			format, ferr := output.FromCmd(cmd)
			if ferr != nil {
				return ferr
			}
			return output.Emit(out, format, entries, func(w io.Writer) error {
				configuredCount := 0
				for _, e := range entries {
					if !e.Configured {
						continue
					}
					configuredCount++
					usedBy := ""
					if len(e.UsedBy) > 0 {
						usedBy = fmt.Sprintf("  (used by: %s)", strings.Join(e.UsedBy, ", "))
					}
					fmt.Fprintf(w, "%-10s %s%s\n", e.Name, e.Path, usedBy)
				}
				unconfiguredCount := len(entries) - configuredCount
				if unconfiguredCount > 0 {
					if configuredCount > 0 {
						fmt.Fprintln(w)
					}
					for _, e := range entries {
						if e.Configured {
							continue
						}
						fmt.Fprintf(w, "%-10s %s  (not configured)\n", e.Name, e.Path)
					}
				}
				if len(entries) == 0 {
					fmt.Fprintln(w, "No agents configured or found on PATH.")
					fmt.Fprintln(w, "Run `aide init` to get started.")
				}
				return nil
			})
		},
	}
}
