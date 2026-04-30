// cmd/aide/env.go
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jskswamy/aide/internal/config"
	aidectx "github.com/jskswamy/aide/internal/context"
	"github.com/jskswamy/aide/internal/display"
	"github.com/jskswamy/aide/internal/secrets"
	"github.com/jskswamy/aide/internal/trust"
)

// Test seams. Production code uses the real implementations; tests
// override these to avoid real SOPS encryption.
var (
	discoverAgeKey     = secrets.DiscoverAgeKey
	decryptSecretsFile = secrets.DecryptSecretsFile
)

func envCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Manage environment variables for contexts",
	}
	cmd.AddCommand(envSetCmd())
	cmd.AddCommand(envListCmd())
	cmd.AddCommand(envRemoveCmd())
	return cmd
}

func envSetCmd() *cobra.Command {
	var (
		secretKey   string
		secretStore string
		pick        bool
		contextName string
		global      bool
	)

	cmd := &cobra.Command{
		Use:   "set KEY [VALUE]",
		Short: "Set an environment variable (project-level by default)",
		Long: `Set an environment variable on a context.

Examples:
  aide env set ANTHROPIC_API_KEY sk-ant-xxx                              # literal value
  aide env set ANTHROPIC_API_KEY --secret-key api_key --global           # key in bound store
  aide env set ANTHROPIC_API_KEY --secret-store firmus --secret-key api_key --global
  aide env set ANTHROPIC_API_KEY --pick --global                         # interactive picker
  aide env set OPENAI_API_KEY --secret-key key --context work --global`,
		Args:         cobra.RangeArgs(1, 2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			hasValueArg := len(args) == 2
			out := cmd.OutOrStdout()
			reader := bufio.NewReader(os.Stdin)

			useSecret := secretKey != "" || pick || secretStore != ""

			// Mutual-exclusion checks.
			if hasValueArg && useSecret {
				return fmt.Errorf("cannot specify both a literal VALUE and a secret flag (--secret-key/--secret-store/--pick)")
			}
			if !hasValueArg && !useSecret {
				return fmt.Errorf("must specify VALUE, --secret-key, or --pick")
			}
			if secretKey != "" && pick {
				return fmt.Errorf("--secret-key and --pick are mutually exclusive")
			}
			if !global && contextName != "" {
				return fmt.Errorf("the --context flag requires --global")
			}
			if useSecret && !global {
				return fmt.Errorf("secret references require --global (secrets are context-scoped)")
			}

			// Project path: literal KEY VALUE only.
			if !global {
				value := args[1]
				_, po, poPath, err := resolveProjectOverrideForMutation()
				if err != nil {
					return err
				}
				if po.Env == nil {
					po.Env = make(map[string]string)
				}
				po.Env[key] = value
				if err := config.WriteProjectOverrideWithTrust(poPath, po, trust.DefaultStore()); err != nil {
					return fmt.Errorf("writing project config: %w", err)
				}
				fmt.Fprintf(out, "Set %s in project (%s)\n", key, poPath)
				return nil
			}

			// Global path.
			env, err := cmdEnv(cmd)
			if err != nil {
				return err
			}
			cwd := env.CWD()
			cfg := env.Config()

			var targetName string
			if contextName != "" {
				targetName = contextName
				if _, ok := cfg.Contexts[targetName]; !ok {
					return fmt.Errorf("context %q not found", targetName)
				}
			} else {
				remoteURL := aidectx.DetectRemote(cwd, "origin")
				resolved, err := aidectx.Resolve(cfg, cwd, remoteURL)
				if err != nil {
					return err
				}
				targetName = resolved.Name
			}

			ctx := cfg.Contexts[targetName]

			var value string
			if useSecret {
				// Resolve store: explicit flag, else context binding. No auto-bind.
				store := secretStore
				if store == "" {
					store = ctx.Secret
				}
				if store == "" {
					return fmt.Errorf(
						"no secret store bound to context %q.\n"+
							"Pass --secret-store <name>, or run: aide context set-secret <name> --context %s --global",
						targetName, targetName,
					)
				}

				// Resolve key.
				resolvedKey := secretKey
				if pick {
					secretsFilePath := config.ResolveSecretPath(store)
					picked, err := selectSecretKey(out, reader, secretsFilePath)
					if err != nil {
						return err
					}
					resolvedKey = picked
				} else {
					// Validate the key exists in the store now, surfacing a
					// helpful error before writing the template.
					secretsFilePath := config.ResolveSecretPath(store)
					identity, err := discoverAgeKey()
					if err != nil {
						return err
					}
					decrypted, err := decryptSecretsFile(secretsFilePath, identity)
					if err != nil {
						return err
					}
					if _, ok := decrypted[resolvedKey]; !ok {
						available := make([]string, 0, len(decrypted))
						for k := range decrypted {
							available = append(available, k)
						}
						sort.Strings(available)
						return fmt.Errorf("key %q not found in %s.\nAvailable keys: %s",
							resolvedKey, store, strings.Join(available, ", "))
					}
				}
				value = fmt.Sprintf("{{ .secrets.%s }}", resolvedKey)
			} else {
				value = args[1]
			}

			if ctx.Env == nil {
				ctx.Env = make(map[string]string)
			}
			ctx.Env[key] = value
			cfg.Contexts[targetName] = ctx

			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}
			fmt.Fprintf(out, "Set %s on context %q.\n", key, targetName)
			return nil
		},
	}

	cmd.Flags().StringVar(&secretKey, "secret-key", "", "Key inside the secret store to reference")
	cmd.Flags().StringVar(&secretStore, "secret-store", "", "Secret store name (defaults to context's bound store)")
	cmd.Flags().BoolVar(&pick, "pick", false, "Interactively pick a key from the resolved store")
	cmd.Flags().StringVar(&contextName, "context", "", "Target context (default: CWD-matched)")
	cmd.Flags().BoolVar(&global, "global", false, "Apply to user-level config instead of project")
	return cmd
}

func selectSecretKey(out io.Writer, reader *bufio.Reader, secretsFilePath string) (string, error) {
	identity, err := discoverAgeKey()
	if err != nil {
		return "", err
	}
	decrypted, err := decryptSecretsFile(secretsFilePath, identity)
	if err != nil {
		return "", err
	}
	if len(decrypted) == 0 {
		return "", fmt.Errorf("secrets file contains no keys")
	}

	keys := make([]string, 0, len(decrypted))
	for k := range decrypted {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	if len(keys) == 1 {
		fmt.Fprintf(out, "Auto-selected secret key: %s\n", keys[0])
		return keys[0], nil
	}

	fmt.Fprintln(out, "Available secret keys:")
	for i, k := range keys {
		fmt.Fprintf(out, "  [%d] %s\n", i+1, k)
	}
	fmt.Fprint(out, "Select secret key [1]: ")
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("reading selection: %w", err)
	}
	input = strings.TrimSpace(input)
	choice := 1
	if input != "" {
		choice, err = strconv.Atoi(input)
		if err != nil || choice < 1 || choice > len(keys) {
			return "", fmt.Errorf("invalid selection: %q", input)
		}
	}
	return keys[choice-1], nil
}

func envListCmd() *cobra.Command {
	var contextName string

	cmd := &cobra.Command{
		Use:          "list",
		Short:        "List environment variables for a context",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			env, err := cmdEnv(cmd)
			if err != nil {
				return err
			}
			cwd := env.CWD()
			cfg := env.Config()

			var targetName string
			var envMap map[string]string
			if contextName != "" {
				targetName = contextName
				ctx, ok := cfg.Contexts[targetName]
				if !ok {
					return fmt.Errorf("context %q not found", targetName)
				}
				envMap = ctx.Env
			} else {
				remoteURL := aidectx.DetectRemote(cwd, "origin")
				resolved, err := aidectx.Resolve(cfg, cwd, remoteURL)
				if err != nil {
					return err
				}
				targetName = resolved.Name
				envMap = resolved.Context.Env
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Context: %s\n", targetName)
			if len(envMap) == 0 {
				fmt.Fprintln(out, "  (no env vars)")
				return nil
			}

			keys := make([]string, 0, len(envMap))
			for k := range envMap {
				keys = append(keys, k)
			}
			sort.Strings(keys)

			maxKeyLen := 0
			for _, k := range keys {
				if len(k) > maxKeyLen {
					maxKeyLen = len(k)
				}
			}

			for _, k := range keys {
				v := envMap[k]
				annotation := display.EnvAnnotation(v)
				fmt.Fprintf(out, "  %-*s   %s\n", maxKeyLen, k, annotation)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&contextName, "context", "", "Target context (default: CWD-matched)")
	return cmd
}

func envRemoveCmd() *cobra.Command {
	var contextName string
	var global bool

	cmd := &cobra.Command{
		Use:          "remove KEY",
		Short:        "Remove an environment variable (project-level by default)",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			if !global && contextName != "" {
				return fmt.Errorf("the --context flag requires --global")
			}
			if global {
				cfg, ctxName, ctx, err := resolveContextForMutation(contextName)
				if err != nil {
					return err
				}
				if ctx.Env == nil || ctx.Env[key] == "" {
					return fmt.Errorf("env var %q not found on context %q", key, ctxName)
				}
				delete(ctx.Env, key)
				cfg.Contexts[ctxName] = ctx
				if err := config.WriteConfig(cfg); err != nil {
					return fmt.Errorf("writing config: %w", err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Removed %s from context %q (global)\n", key, ctxName)
				return nil
			}
			_, po, poPath, err := resolveProjectOverrideForMutation()
			if err != nil {
				return err
			}
			if po.Env == nil || po.Env[key] == "" {
				return fmt.Errorf("env var %q not found in project config", key)
			}
			delete(po.Env, key)
			if err := config.WriteProjectOverrideWithTrust(poPath, po, trust.DefaultStore()); err != nil {
				return fmt.Errorf("writing project config: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed %s from project (%s)\n", key, poPath)
			return nil
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "Apply to user-level config instead of project")
	cmd.Flags().StringVar(&contextName, "context", "", "Target context (requires --global)")
	return cmd
}
