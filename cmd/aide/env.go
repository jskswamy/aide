// cmd/aide/env.go
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	var fromSecret string
	var contextName string
	var global bool

	cmd := &cobra.Command{
		Use:   "set KEY [VALUE]",
		Short: "Set an environment variable (project-level by default)",
		Long: `Set an environment variable on a context.

Examples:
  aide env set ANTHROPIC_API_KEY sk-ant-xxx              # literal value
  aide env set ANTHROPIC_API_KEY --from-secret api_key   # explicit key
  aide env set ANTHROPIC_API_KEY --from-secret            # interactive picker
  aide env set OPENAI_API_KEY --from-secret key --context work`,
		Args:         cobra.RangeArgs(1, 2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			hasValueArg := len(args) == 2
			out := cmd.OutOrStdout()
			reader := bufio.NewReader(os.Stdin)

			isFromSecret := cmd.Flags().Changed("from-secret")
			isInteractive := isFromSecret && strings.TrimSpace(fromSecret) == ""

			if hasValueArg && isFromSecret {
				return fmt.Errorf("cannot specify both a value argument and --from-secret")
			}
			if !hasValueArg && !isFromSecret {
				return fmt.Errorf("must specify either a value argument or --from-secret")
			}
			if !global && contextName != "" {
				return fmt.Errorf("the --context flag requires --global")
			}
			if isFromSecret && !global {
				return fmt.Errorf("--from-secret requires --global (secrets are context-scoped)")
			}

			// Project path: simple KEY VALUE only (--from-secret requires --global)
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
				fmt.Fprintf(cmd.OutOrStdout(), "Set %s in project (%s)\n", key, poPath)
				return nil
			}

			// Global path: existing logic below (handles --from-secret)
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
			if isFromSecret {
				// Auto-detect secret if missing
				if ctx.Secret == "" {
					selected, err := selectSecret(out, reader, config.SecretsDir())
					if err != nil {
						return err
					}
					ctx.Secret = selected
					fmt.Fprintf(out, "Set secret=%q on context %q.\n", selected, targetName)
				}

				var secretKey string
				if isInteractive {
					secretsFilePath := config.ResolveSecretPath(ctx.Secret)
					picked, err := selectSecretKey(out, reader, secretsFilePath)
					if err != nil {
						return err
					}
					secretKey = picked
				} else {
					secretKey = fromSecret
					secretsFilePath := config.ResolveSecretPath(ctx.Secret)
					identity, err := discoverAgeKey()
					if err != nil {
						return err
					}
					decrypted, err := decryptSecretsFile(secretsFilePath, identity)
					if err != nil {
						return err
					}
					if _, ok := decrypted[secretKey]; !ok {
						available := make([]string, 0, len(decrypted))
						for k := range decrypted {
							available = append(available, k)
						}
						sort.Strings(available)
						return fmt.Errorf("key %q not found in %s.\nAvailable keys: %s",
							secretKey, ctx.Secret, strings.Join(available, ", "))
					}
				}
				value = fmt.Sprintf("{{ .secrets.%s }}", secretKey)
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

	cmd.Flags().StringVar(&fromSecret, "from-secret", "", "Generate template referencing a secret key")
	cmd.Flags().Lookup("from-secret").NoOptDefVal = " "
	cmd.Flags().StringVar(&contextName, "context", "", "Target context (default: CWD-matched)")
	cmd.Flags().BoolVar(&global, "global", false, "Apply to user-level config instead of project")
	return cmd
}

func selectSecret(out io.Writer, reader *bufio.Reader, secretsDir string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(secretsDir, "*.enc.yaml"))
	if err != nil {
		return "", fmt.Errorf("scanning secrets directory: %w", err)
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no secrets found.\nCreate one with: aide secrets create <name> --age-key <key>")
	}

	names := make([]string, len(matches))
	for i, m := range matches {
		names[i] = strings.TrimSuffix(filepath.Base(m), ".enc.yaml")
	}
	sort.Strings(names)

	if len(names) == 1 {
		fmt.Fprintf(out, "Auto-selected secret: %s\n", names[0])
		return names[0], nil
	}

	fmt.Fprintln(out, "Available secrets:")
	for i, name := range names {
		fmt.Fprintf(out, "  [%d] %s\n", i+1, name)
	}
	fmt.Fprint(out, "Select secret [1]: ")
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("reading selection: %w", err)
	}
	input = strings.TrimSpace(input)
	choice := 1
	if input != "" {
		choice, err = strconv.Atoi(input)
		if err != nil || choice < 1 || choice > len(names) {
			return "", fmt.Errorf("invalid selection: %q", input)
		}
	}
	return names[choice-1], nil
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
