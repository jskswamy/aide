// Package main provides the aide CLI commands.
package main

import (
	"fmt"
	"os"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/explain"
	"github.com/spf13/cobra"
)

func explainCmd() *cobra.Command {
	var format string

	cmd := &cobra.Command{
		Use:   "explain [topic]",
		Short: "Explain how to configure aide, grounded in your current config",
		Long: `Explain aide configuration: embedded recipes plus a redacted,
read-only snapshot of your current config. Secret values are never shown —
only {{ .secrets.X }} references and store names.

Formats: human (default), agent (markdown for injection), json.`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			switch format {
			case "human", "agent", "json":
			default:
				return fmt.Errorf("unknown --format %q (want human, agent, or json)", format)
			}

			recipes, err := explain.LoadRecipes()
			if err != nil {
				return fmt.Errorf("loading recipes: %w", err)
			}

			// Topic argument: print a single recipe and exit.
			if len(args) > 0 {
				topic := args[0]
				for _, r := range recipes {
					if r.Topic == topic {
						fmt.Fprint(cmd.OutOrStdout(), r.Body)
						return nil
					}
				}
				return fmt.Errorf("unknown topic %q (run aide explain to list topics)", topic)
			}

			// Best-effort config load: explain works even with no config.
			var cfg *config.Config
			if cwd, err := os.Getwd(); err == nil {
				if loaded, err := config.Load(config.Dir(), cwd); err == nil {
					cfg = loaded
				}
			}

			doc := explain.Document{
				State:   explain.StateFromConfig(cfg),
				Recipes: recipes,
			}

			out := cmd.OutOrStdout()
			switch format {
			case "json":
				s, err := explain.RenderJSON(doc)
				if err != nil {
					return err
				}
				fmt.Fprintln(out, s)
			case "agent":
				fmt.Fprint(out, explain.RenderAgent(doc))
			default:
				fmt.Fprint(out, explain.RenderHuman(doc))
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&format, "format", "human", "Output format: human, agent, or json")
	return cmd
}
