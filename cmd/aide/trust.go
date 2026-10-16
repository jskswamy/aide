// Package main provides the aide CLI commands.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"text/template"

	"github.com/spf13/cobra"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/sandbox"
	"github.com/jskswamy/aide/internal/trust"
)

func validateCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "validate",
		Short:        "Validate aide configuration",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			env, err := cmdEnv(cmd)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Config error: %s\n", err)
				return err
			}
			cfg := env.Config()

			var errors []string
			var warnings []string
			secretsCount := make(map[string]bool)

			for ctxName, ctx := range cfg.Contexts {
				if ctx.Agent != "" && len(cfg.Agents) > 0 {
					if _, ok := cfg.Agents[ctx.Agent]; !ok {
						errors = append(errors, fmt.Sprintf(
							"profile %q references unknown agent %q", ctxName, ctx.Agent,
						))
					}
				}

				if ctx.Secret != "" {
					secretsCount[ctx.Secret] = true
					path := config.ResolveSecretPath(ctx.Secret)
					if _, err := os.Stat(path); os.IsNotExist(err) {
						errors = append(errors, fmt.Sprintf(
							"profile %q references secret %q which does not exist", ctxName, ctx.Secret,
						))
					}
				}

				if ctx.Sandbox != nil {
					if err := sandbox.ValidateSandboxRef(ctx.Sandbox, cfg.Sandboxes); err != nil {
						errors = append(errors, fmt.Sprintf(
							"profile %q has invalid sandbox config: %s", ctxName, err,
						))
					}
				}

				if ctxName != cfg.DefaultContext && ctxName != "default" {
					if len(ctx.Match) == 0 {
						warnings = append(warnings, fmt.Sprintf(
							"profile %q has no match rules (will never activate)", ctxName,
						))
					}
				}

				for envKey, envVal := range ctx.Env {
					if strings.Contains(envVal, "{{") {
						if _, tmplErr := template.New("").Parse(envVal); tmplErr != nil {
							errors = append(errors, fmt.Sprintf(
								"profile %q env var %q has invalid template syntax: %s", ctxName, envKey, tmplErr,
							))
						} else if strings.Contains(envVal, ".secrets.") && ctx.Secret == "" {
							errors = append(errors, fmt.Sprintf(
								"profile %q env var %q references secrets but no secret is configured", ctxName, envKey,
							))
						}
					}
				}
			}

			for agentName, agentDef := range cfg.Agents {
				binary := agentDef.Binary
				if binary == "" {
					binary = agentName
				}
				if _, err := exec.LookPath(binary); err != nil {
					warnings = append(warnings, fmt.Sprintf(
						"agent %q binary %q not found on PATH", agentName, binary,
					))
				}
			}

			out := cmd.OutOrStdout()
			if len(errors) == 0 && len(warnings) == 0 {
				fmt.Fprintf(out, "OK (%d profiles, %d agents, %d secrets)\n",
					len(cfg.Contexts), len(cfg.Agents), len(secretsCount))
				return nil
			}

			if len(errors) > 0 {
				sort.Strings(errors)
				fmt.Fprintln(out, "Errors:")
				for _, e := range errors {
					fmt.Fprintf(out, "  - %s\n", e)
				}
			}
			if len(warnings) > 0 {
				sort.Strings(warnings)
				if len(errors) > 0 {
					fmt.Fprintln(out)
				}
				fmt.Fprintln(out, "Warnings:")
				for _, w := range warnings {
					fmt.Fprintf(out, "  - %s\n", w)
				}
			}

			fmt.Fprintf(out, "\n%d errors, %d warnings\n", len(errors), len(warnings))
			if len(errors) > 0 {
				return fmt.Errorf("validation found %d error(s)", len(errors))
			}
			return nil
		},
	}
}

// trustTarget is the .aide.yaml a trust command acts on: the path
// argument when given, else .aide.yaml in the current directory.
func trustTarget(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return config.ProjectConfigFileName
}

func trustCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "trust [path]",
		Short: "Trust a .aide.yaml (default: the one in the current directory)",
		Long: `Trust a .aide.yaml at its current contents. Trust is recorded per file,
keyed on its location (parent directories symlink-resolved) and the
contents: changing the file withdraws the approval. A symlinked
.aide.yaml is approved at its own location, never via its target. In a linked git worktree, the main checkout's
.aide.yaml is a separate file with its own approval; pass its path.`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			target := trustTarget(args)
			fc, err := trust.DefaultStore().CheckFile(target)
			if err != nil {
				return fmt.Errorf("%s not found: %w", target, err)
			}
			if err := trust.DefaultStore().Trust(fc.Path, fc.Contents); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Trusted: %s\n", fc.Path)
			return nil
		},
	}
}

func denyCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "deny [path]",
		Short:        "Deny a .aide.yaml (default: the one in the current directory)",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// A missing file can still be denied ahead of time; it is
			// recorded at its absolute path.
			path, _ := trust.CanonicalPath(trustTarget(args))
			if err := trust.DefaultStore().Deny(path); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Denied: %s\n", path)
			return nil
		},
	}
}

func untrustCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "untrust [path]",
		Short:        "Remove trust for a .aide.yaml without denying",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			target := trustTarget(args)
			fc, err := trust.DefaultStore().CheckFile(target)
			if err != nil {
				return fmt.Errorf("%s not found: %w", target, err)
			}
			if err := trust.DefaultStore().Untrust(fc.Path, fc.Contents); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Untrusted: %s\n", fc.Path)
			return nil
		},
	}
}
