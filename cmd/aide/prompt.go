package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/launcher"
	"github.com/jskswamy/aide/internal/output"
	"github.com/jskswamy/aide/internal/ui"
	"github.com/spf13/cobra"
)

// starshipConfigSnippet is a TOML configuration block for Starship shell prompt.
const starshipConfigSnippet = `# Add to ~/.config/starship.toml
[custom.aide]
command = "aide prompt"
when = true
symbol = ""
timeout = 100
`

type promptResult struct {
	Line string `json:"line"`
}

func promptCmd() *cobra.Command {
	var printStarshipConfig bool
	var compact bool

	cmd := &cobra.Command{
		Use:           "prompt",
		Short:         "Output a compact context line for Starship shell prompt integration",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if printStarshipConfig {
				fmt.Fprint(cmd.OutOrStdout(), starshipConfigSnippet)
				return nil
			}

			env, err := cmdEnv(cmd)
			if err != nil {
				return err
			}
			cwd := env.CWD()
			cfg := env.Config()

			// A launch's selection, trust gate included: untrusted files
			// neither shape the prompt nor go unflagged. Steps a launch
			// stops on after the profile is resolved leave the harness it
			// would otherwise run; it reports them itself.
			home, _ := os.UserHomeDir()
			res, err := (&launcher.Launcher{}).ResolveSelection(cfg, cwd, home, launcher.SelectionInputs{})
			resolved := res.RC
			if resolved == nil {
				// No context matched — Starship hides the module on non-zero exit.
				return err
			}

			trustStatus := res.Gate.Status()
			sbDisabled := resolved.Context.Sandbox != nil && resolved.Context.Sandbox.Disabled

			ctxIcon := resolved.Context.Icon
			var agentDef *config.AgentDef
			if def, ok := cfg.Agents[resolved.Context.Agent]; ok {
				agentDef = &def
			}
			agentIcon := launcher.ResolveAgentIcon(resolved.Context.Agent, agentDef)

			result := promptResult{Line: formatPromptLine(resolved.Name, ctxIcon, agentIcon, sbDisabled, trustStatus, compact)}

			format, ferr := output.FromCmd(cmd)
			if ferr != nil {
				return ferr
			}
			return output.Emit(cmd.OutOrStdout(), format, result, func(w io.Writer) error {
				fmt.Fprintln(w, result.Line)
				return nil
			})
		},
	}

	cmd.Flags().BoolVar(&printStarshipConfig, "starship-config", false, "Print Starship configuration snippet")
	cmd.Flags().BoolVar(&compact, "compact", false, "Remove spaces between prompt segments")

	return cmd
}

// formatPromptLine builds a prompt line from context, icons, and trust status.
// When ctxIcon is set it replaces the name; otherwise the name is shown.
func formatPromptLine(ctx, ctxIcon, agentIcon string, sbDisabled bool, trustStatus string, compact bool) string {
	var parts []string

	if s := ui.SanitizeIcon(ctxIcon); s != "" {
		parts = append(parts, s)
	} else {
		parts = append(parts, ctx)
	}

	if s := ui.SanitizeIcon(agentIcon); s != "" {
		parts = append(parts, s)
	}

	// Append trust/sandbox icons based on trust status
	switch trustStatus {
	case "denied":
		parts = append(parts, "🚫")
	case "untrusted":
		parts = append(parts, "⚠")
	default:
		// "trusted" or anything else
		if !sbDisabled {
			parts = append(parts, "🛡")
		}
	}

	sep := " "
	if compact {
		sep = ""
	}
	return strings.Join(parts, sep)
}
