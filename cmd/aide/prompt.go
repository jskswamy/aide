package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/jskswamy/aide/internal/config"
	aidectx "github.com/jskswamy/aide/internal/context"
	"github.com/jskswamy/aide/internal/launcher"
	"github.com/jskswamy/aide/internal/trust"
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

			remoteURL := aidectx.DetectRemote(cwd, "origin")
			resolved, err := aidectx.Resolve(cfg, cwd, remoteURL)
			if err != nil {
				// No context matched — Starship hides the module on non-zero exit.
				return err
			}

			trustStatus := promptTrustStatus(cfg)
			sbDisabled := resolved.Context.Sandbox != nil && resolved.Context.Sandbox.Disabled

			ctxIcon := resolved.Context.Icon
			var agentDef *config.AgentDef
			if def, ok := cfg.Agents[resolved.Context.Agent]; ok {
				agentDef = &def
			}
			agentIcon := launcher.ResolveAgentIcon(resolved.Context.Agent, agentDef)

			line := formatPromptLine(resolved.Name, ctxIcon, agentIcon, sbDisabled, trustStatus, compact)
			fmt.Fprintln(cmd.OutOrStdout(), line)
			return nil
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

// promptTrustStatus returns the trust status of the current project configuration.
// Returns:
//   - "trusted" if no project override is configured
//   - "denied" if the project override file is denied
//   - "untrusted" if the project override file is untrusted
//   - "trusted" otherwise
func promptTrustStatus(cfg *config.Config) string {
	// No project override configured means trusted by default
	if cfg.ProjectConfigPath == "" || cfg.ProjectOverride == nil {
		return "trusted"
	}

	// Read the project override file and check its trust status
	contents, err := os.ReadFile(cfg.ProjectConfigPath)
	if err != nil {
		// If we can't read the file, treat as untrusted
		return "untrusted"
	}

	status := trust.DefaultStore().Check(cfg.ProjectConfigPath, contents)
	return status.String()
}
