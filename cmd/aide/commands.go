package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jskswamy/aide/internal/config"
	aidectx "github.com/jskswamy/aide/internal/context"
	"github.com/jskswamy/aide/internal/sandbox"
	"github.com/jskswamy/aide/internal/secrets"
	"github.com/spf13/cobra"
)

func registerCommands(rootCmd *cobra.Command) {
	rootCmd.AddCommand(initCmd())
	rootCmd.AddCommand(whichCmd())
	rootCmd.AddCommand(validateCmd())
	rootCmd.AddCommand(secretsCmd())
}

func initCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "init",
		Short:        "Initialize aide configuration",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath := config.ConfigFilePath()
			if _, err := os.Stat(configPath); err == nil {
				return fmt.Errorf("config already exists: %s", configPath)
			}

			reader := bufio.NewReader(os.Stdin)

			fmt.Fprint(cmd.OutOrStdout(), "Agent binary name (e.g. claude): ")
			agentName, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("reading agent name: %w", err)
			}
			agentName = strings.TrimSpace(agentName)
			if agentName == "" {
				return fmt.Errorf("agent name cannot be empty")
			}

			yamlContent := fmt.Sprintf("agent: %s\n", agentName)

			fmt.Fprint(cmd.OutOrStdout(), "Set up secrets? (y/N): ")
			answer, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("reading response: %w", err)
			}
			answer = strings.TrimSpace(strings.ToLower(answer))

			if answer == "y" || answer == "yes" {
				fmt.Fprint(cmd.OutOrStdout(), "Age public key: ")
				ageKey, err := reader.ReadString('\n')
				if err != nil {
					return fmt.Errorf("reading age key: %w", err)
				}
				ageKey = strings.TrimSpace(ageKey)
				if ageKey == "" {
					return fmt.Errorf("age public key cannot be empty")
				}

				fmt.Fprint(cmd.OutOrStdout(), "Secrets file name (e.g. personal): ")
				secretsName, err := reader.ReadString('\n')
				if err != nil {
					return fmt.Errorf("reading secrets name: %w", err)
				}
				secretsName = strings.TrimSpace(secretsName)
				if secretsName == "" {
					return fmt.Errorf("secrets file name cannot be empty")
				}

				yamlContent += fmt.Sprintf("secrets_file: %s.enc.yaml\n", secretsName)

				// Create the secrets file
				secretsDir := config.SecretsDir()
				mgr := secrets.NewManager(secretsDir, os.TempDir())
				if err := mgr.Create(secretsName, secretsDir, ageKey); err != nil {
					return fmt.Errorf("creating secrets: %w", err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Created secrets/%s.enc.yaml\n", secretsName)
			}

			// Ensure config directory exists
			configDir := config.ConfigDir()
			if err := os.MkdirAll(configDir, 0o755); err != nil {
				return fmt.Errorf("creating config directory: %w", err)
			}

			if err := os.WriteFile(configPath, []byte(yamlContent), 0o644); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Created %s\n", configPath)
			return nil
		},
	}
}

func whichCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "which",
		Short:        "Show which context matches the current directory",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}

			cfg, err := config.Load(config.ConfigDir(), cwd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			remoteURL := aidectx.DetectRemote(cwd, "origin")
			resolved, err := aidectx.Resolve(cfg, cwd, remoteURL)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Context:  %s\n", resolved.Name)
			fmt.Fprintf(out, "Matched:  %s\n", resolved.MatchReason)
			fmt.Fprintf(out, "Agent:    %s\n", resolved.Context.Agent)

			if resolved.Context.SecretsFile != "" {
				fmt.Fprintf(out, "Secrets:  %s\n", resolved.Context.SecretsFile)
			}

			if len(resolved.Context.Env) > 0 {
				keys := make([]string, 0, len(resolved.Context.Env))
				for k := range resolved.Context.Env {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				fmt.Fprintf(out, "Env:      %s\n", strings.Join(keys, ", "))
			}

			return nil
		},
	}
}

func validateCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "validate",
		Short:        "Validate aide configuration",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}

			cfg, err := config.Load(config.ConfigDir(), cwd)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Config error: %s\n", err)
				return err
			}

			var issues []string

			// Check agent references in contexts
			for ctxName, ctx := range cfg.Contexts {
				if ctx.Agent != "" && len(cfg.Agents) > 0 {
					if _, ok := cfg.Agents[ctx.Agent]; !ok {
						issues = append(issues, fmt.Sprintf(
							"context %q references unknown agent %q", ctxName, ctx.Agent,
						))
					}
				}

				// Check secrets files exist on disk
				if ctx.SecretsFile != "" {
					path := config.ResolveSecretsFilePath(ctx.SecretsFile)
					if _, err := os.Stat(path); os.IsNotExist(err) {
						issues = append(issues, fmt.Sprintf(
							"context %q references secrets file %q which does not exist", ctxName, ctx.SecretsFile,
						))
					}
				}

				// Validate sandbox config
				if ctx.Sandbox != nil {
					if err := sandbox.ValidateSandboxConfig(ctx.Sandbox); err != nil {
						issues = append(issues, fmt.Sprintf(
							"context %q has invalid sandbox config: %s", ctxName, err,
						))
					}
				}
			}

			out := cmd.OutOrStdout()
			if len(issues) == 0 {
				fmt.Fprintln(out, "OK")
				return nil
			}

			sort.Strings(issues)
			for _, issue := range issues {
				fmt.Fprintf(out, "- %s\n", issue)
			}
			return fmt.Errorf("validation found %d issue(s)", len(issues))
		},
	}
}

func secretsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Manage encrypted secrets",
	}
	cmd.AddCommand(secretsCreateCmd())
	cmd.AddCommand(secretsEditCmd())
	cmd.AddCommand(secretsListCmd())
	cmd.AddCommand(secretsRotateCmd())
	return cmd
}

func secretsCreateCmd() *cobra.Command {
	var ageKey string

	cmd := &cobra.Command{
		Use:          "create <name>",
		Short:        "Create a new encrypted secrets file",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			secretsDir := config.SecretsDir()
			runtimeDir := os.TempDir()
			mgr := secrets.NewManager(secretsDir, runtimeDir)
			if err := mgr.Create(name, secretsDir, ageKey); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created secrets/%s.enc.yaml\n", name)
			return nil
		},
	}

	cmd.Flags().StringVar(&ageKey, "age-key", "", "Age public key for encryption (required)")
	_ = cmd.MarkFlagRequired("age-key")
	return cmd
}

func secretsEditCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "edit <name>",
		Short:        "Edit an encrypted secrets file",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			secretsDir := config.SecretsDir()
			runtimeDir := os.TempDir()
			mgr := secrets.NewManager(secretsDir, runtimeDir)
			if err := mgr.Edit(name, secretsDir); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Updated secrets/%s.enc.yaml\n", name)
			return nil
		},
	}
}

func secretsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "list",
		Short:        "List encrypted secrets files",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			secretsDir := config.SecretsDir()
			entries, err := filepath.Glob(filepath.Join(secretsDir, "*.enc.yaml"))
			if err != nil {
				return fmt.Errorf("scanning secrets directory: %w", err)
			}

			if len(entries) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No secrets files found.")
				return nil
			}

			// Load config to find context references
			cwd, err := os.Getwd()
			if err != nil {
				cwd = "."
			}
			cfg, _ := config.Load(config.ConfigDir(), cwd)

			// Build a map of secrets file -> context names
			secretsToContexts := make(map[string][]string)
			if cfg != nil {
				for ctxName, ctx := range cfg.Contexts {
					if ctx.SecretsFile != "" {
						secretsToContexts[ctx.SecretsFile] = append(
							secretsToContexts[ctx.SecretsFile], ctxName,
						)
					}
				}
			}

			out := cmd.OutOrStdout()
			sort.Strings(entries)
			for i, entry := range entries {
				baseName := filepath.Base(entry)
				fmt.Fprintf(out, "secrets/%s\n", baseName)

				recipients, err := secrets.ListRecipients(entry)
				if err != nil {
					fmt.Fprintf(out, "  Recipients: (error: %s)\n", err)
				} else if len(recipients) > 0 {
					fmt.Fprintf(out, "  Recipients: %s\n", strings.Join(recipients, ", "))
				}

				if ctxNames, ok := secretsToContexts[baseName]; ok {
					sort.Strings(ctxNames)
					fmt.Fprintf(out, "  Used by: %s\n", strings.Join(ctxNames, ", "))
				}

				if i < len(entries)-1 {
					fmt.Fprintln(out)
				}
			}

			return nil
		},
	}
}

func secretsRotateCmd() *cobra.Command {
	var addKeys []string
	var removeKeys []string

	cmd := &cobra.Command{
		Use:          "rotate <name>",
		Short:        "Rotate age recipients for a secrets file",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if len(addKeys) == 0 && len(removeKeys) == 0 {
				return fmt.Errorf("at least one of --add-key or --remove-key is required")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			filePath := config.ResolveSecretsFilePath(name + ".enc.yaml")
			if err := secrets.Rotate(filePath, addKeys, removeKeys); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Rotated secrets/%s.enc.yaml\n", name)
			return nil
		},
	}

	cmd.Flags().StringSliceVar(&addKeys, "add-key", nil, "Age public key to add as recipient")
	cmd.Flags().StringSliceVar(&removeKeys, "remove-key", nil, "Age public key to remove as recipient")
	return cmd
}
