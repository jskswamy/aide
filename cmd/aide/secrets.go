// Package main provides the aide CLI commands.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/output"
	"github.com/jskswamy/aide/internal/secrets"
)

type secretsKeysResult struct {
	File string   `json:"file"`
	Keys []string `json:"keys"`
}

type secretsFileEntry struct {
	File       string   `json:"file"`
	Recipients []string `json:"recipients,omitempty"`
	UsedBy     []string `json:"used_by,omitempty"`
}

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
								fmt.Fprintf(out, "  aide env set MY_VAR --secret-key %s --global\n", k)
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

			result := secretsKeysResult{File: fmt.Sprintf("secrets/%s.enc.yaml", name), Keys: keys}
			out := cmd.OutOrStdout()
			format, ferr := output.FromCmd(cmd)
			if ferr != nil {
				return ferr
			}
			return output.Emit(out, format, result, func(w io.Writer) error {
				for _, k := range result.Keys {
					fmt.Fprintln(w, k)
				}
				fmt.Fprintf(w, "\n%d keys in %s\n", len(result.Keys), result.File)
				return nil
			})
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
			paths, err := filepath.Glob(filepath.Join(secretsDir, "*.enc.yaml"))
			if err != nil {
				return fmt.Errorf("scanning secrets directory: %w", err)
			}
			sort.Strings(paths)

			env, _ := cmdEnv(cmd)
			cfg := env.Config()
			secretsToContexts := make(map[string][]string)
			if cfg != nil {
				for ctxName, ctx := range cfg.Contexts {
					if ctx.Secret != "" {
						key := ctx.Secret
						if !strings.HasSuffix(key, ".enc.yaml") {
							key += ".enc.yaml"
						}
						secretsToContexts[key] = append(secretsToContexts[key], ctxName)
					}
				}
			}

			entries := make([]secretsFileEntry, 0, len(paths))
			for _, p := range paths {
				baseName := filepath.Base(p)
				e := secretsFileEntry{File: fmt.Sprintf("secrets/%s", baseName)}
				if recipients, err := secrets.ListRecipients(p); err == nil {
					e.Recipients = recipients
				}
				if ctxNames, ok := secretsToContexts[baseName]; ok {
					sort.Strings(ctxNames)
					e.UsedBy = ctxNames
				}
				entries = append(entries, e)
			}

			out := cmd.OutOrStdout()
			format, ferr := output.FromCmd(cmd)
			if ferr != nil {
				return ferr
			}
			return output.Emit(out, format, entries, func(w io.Writer) error {
				if len(entries) == 0 {
					fmt.Fprintln(w, "No secrets files found.")
					return nil
				}
				for i, e := range entries {
					fmt.Fprintln(w, e.File)
					if len(e.Recipients) > 0 {
						fmt.Fprintf(w, "  Recipients: %s\n", strings.Join(e.Recipients, ", "))
					}
					if len(e.UsedBy) > 0 {
						fmt.Fprintf(w, "  Used by: %s\n", strings.Join(e.UsedBy, ", "))
					}
					if i < len(entries)-1 {
						fmt.Fprintln(w)
					}
				}
				return nil
			})
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
