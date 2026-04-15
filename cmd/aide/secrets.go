// Package main provides the aide CLI commands.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/secrets"
)

func secretsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Manage encrypted secrets",
	}
	cmd.AddCommand(secretsCreateCmd())
	cmd.AddCommand(secretsEditCmd())
	cmd.AddCommand(secretsKeysCmd())
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
			out := cmd.OutOrStdout()

			// Capture keys before edit for diff.
			filePath := config.ResolveSecretPath(name + ".enc.yaml")
			var keysBefore map[string]bool
			if identity, err := secrets.DiscoverAgeKey(); err == nil {
				if data, err := secrets.DecryptSecretsFile(filePath, identity); err == nil {
					keysBefore = make(map[string]bool, len(data))
					for k := range data {
						keysBefore[k] = true
					}
				}
			}

			mgr := secrets.NewManager(secretsDir, runtimeDir)
			if err := mgr.Edit(name, secretsDir); err != nil {
				return err
			}

			fmt.Fprintf(out, "Updated secrets/%s.enc.yaml\n", name)

			// Show key diff if we had keys before.
			if keysBefore != nil {
				if identity, err := secrets.DiscoverAgeKey(); err == nil {
					if data, err := secrets.DecryptSecretsFile(filePath, identity); err == nil {
						var added, removed []string
						keysAfter := make(map[string]bool, len(data))
						for k := range data {
							keysAfter[k] = true
							if !keysBefore[k] {
								added = append(added, k)
							}
						}
						for k := range keysBefore {
							if !keysAfter[k] {
								removed = append(removed, k)
							}
						}
						sort.Strings(added)
						sort.Strings(removed)

						if len(added) > 0 || len(removed) > 0 {
							fmt.Fprintln(out)
						}
						for _, k := range added {
							fmt.Fprintf(out, "  + %s (new)\n", k)
						}
						for _, k := range removed {
							fmt.Fprintf(out, "  - %s (removed)\n", k)
						}

						// Tip for new keys
						if len(added) > 0 {
							fmt.Fprintf(out, "\nTip: Wire new keys to env vars:\n")
							for _, k := range added {
								fmt.Fprintf(out, "  aide env set MY_VAR --from-secret %s\n", k)
							}
						}
					}
				}
			}

			return nil
		},
	}
}

func secretsKeysCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "keys <name>",
		Short:        "List key names in an encrypted secrets file",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			filePath := config.ResolveSecretPath(name + ".enc.yaml")

			if _, err := os.Stat(filePath); os.IsNotExist(err) {
				return fmt.Errorf("secrets/%s.enc.yaml not found", name)
			}

			identity, err := secrets.DiscoverAgeKey()
			if err != nil {
				return fmt.Errorf("discovering age key: %w", err)
			}

			data, err := secrets.DecryptSecretsFile(filePath, identity)
			if err != nil {
				return fmt.Errorf("decrypting secrets file: %w", err)
			}

			keys := make([]string, 0, len(data))
			for k := range data {
				keys = append(keys, k)
			}
			sort.Strings(keys)

			out := cmd.OutOrStdout()
			for _, k := range keys {
				fmt.Fprintln(out, k)
			}
			fmt.Fprintf(out, "\n%d keys in secrets/%s.enc.yaml\n", len(keys), name)
			return nil
		},
	}
}

func secretsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "list",
		Short:        "List encrypted secrets files",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
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
			cfg, _ := config.Load(config.Dir(), cwd)

			// Build a map of secret -> context names
			secretsToContexts := make(map[string][]string)
			if cfg != nil {
				for ctxName, ctx := range cfg.Contexts {
					if ctx.Secret != "" {
						// Normalize bare name to filename for matching
						key := ctx.Secret
						if !strings.HasSuffix(key, ".enc.yaml") {
							key += ".enc.yaml"
						}
						secretsToContexts[key] = append(
							secretsToContexts[key], ctxName,
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
		PreRunE: func(_ *cobra.Command, _ []string) error {
			if len(addKeys) == 0 && len(removeKeys) == 0 {
				return fmt.Errorf("at least one of --add-key or --remove-key is required")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			filePath := config.ResolveSecretPath(name + ".enc.yaml")
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
