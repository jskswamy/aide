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
	"github.com/jskswamy/aide/internal/envscrub"
	"github.com/jskswamy/aide/internal/output"
	"github.com/jskswamy/aide/internal/secrets"
	"github.com/jskswamy/aide/internal/trust"
)

// Test seams. Production code uses the real implementations; tests
// override these to avoid real SOPS encryption.
var (
	discoverAgeKey     = secrets.DiscoverAgeKey
	decryptSecretsFile = secrets.DecryptSecretsFile
)

type envVarEntry struct {
	Key        string `json:"key"`
	Annotation string `json:"annotation"`
}

type envListResult struct {
	Context string        `json:"context"`
	Vars    []envVarEntry `json:"vars"`
}

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
		main        bool
	)

	cmd := &cobra.Command{
		Use:   "set KEY [VALUE]",
		Short: "Set an environment variable (project-level by default)",
		Long: `Set an environment variable on a context.

Provider API keys and tokens (ANTHROPIC_API_KEY, OPENAI_API_KEY, ...)
are refused: declare them as credentials: bindings on the profile.
Provider selectors and endpoints such as CLAUDE_CODE_USE_BEDROCK or
ANTHROPIC_BASE_URL are ordinary env, but only with --global.

Examples:
  aide env set CLAUDE_CODE_USE_BEDROCK 1 --global                        # literal value
  aide env set GITHUB_TOKEN --secret-key github_token --global           # key in bound store
  aide env set GITHUB_TOKEN --secret-store firmus --secret-key gh --global
  aide env set GITHUB_TOKEN --pick --global                              # interactive picker
  aide env set SENTRY_DSN --secret-key sentry --context work --global`,
		Args:         cobra.RangeArgs(1, 2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			hasValueArg := len(args) == 2
			out := cmd.OutOrStdout()
			reader := bufio.NewReader(os.Stdin)

			useSecret := secretKey != "" || pick || secretStore != ""
			if envscrub.IsProviderSecretName(key) {
				return providerSecretEnvError(key)
			}
			if !global && envscrub.IsProviderSelectorName(key) {
				return fmt.Errorf("%s is a provider selector or endpoint, which repository config (.aide.yaml) cannot set; "+
					"set it on your profile with --global", key)
			}

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
			if err := validateContextScope(global, contextName); err != nil {
				return err
			}
			if err := validateMainScope(global, main); err != nil {
				return err
			}
			if useSecret && !global {
				return fmt.Errorf("secret references require --global (secrets are context-scoped)")
			}

			// Project path: literal KEY VALUE only.
			if !global {
				value := args[1]
				_, po, poPath, err := resolveProjectOverrideForMutation(main)
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
				resolved, err := aidectx.Resolve(cfg, cwd, aidectx.GitRemotes(cwd))
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
							"Pass --secret-store <name>, or run: aide profile set-secret <name> --profile %s",
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
	cmd.Flags().StringVar(&contextName, "context", "", "Target profile (default: CWD-matched)")
	addProfileFlag(cmd, &contextName)
	cmd.Flags().BoolVar(&global, "global", false, "Apply to user-level config instead of project")
	addMainFlag(cmd, &main)
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
				resolved, err := aidectx.Resolve(cfg, cwd, aidectx.GitRemotes(cwd))
				if err != nil {
					return err
				}
				targetName = resolved.Name
				envMap = resolved.Context.Env
			}

			keys := make([]string, 0, len(envMap))
			for k := range envMap {
				keys = append(keys, k)
			}
			sort.Strings(keys)

			result := envListResult{Context: targetName, Vars: make([]envVarEntry, 0, len(keys))}
			for _, k := range keys {
				result.Vars = append(result.Vars, envVarEntry{Key: k, Annotation: display.EnvAnnotation(envMap[k])})
			}

			out := cmd.OutOrStdout()
			format, ferr := output.FromCmd(cmd)
			if ferr != nil {
				return ferr
			}
			return output.Emit(out, format, result, func(w io.Writer) error {
				fmt.Fprintf(w, "Profile: %s\n", result.Context)
				if len(result.Vars) == 0 {
					fmt.Fprintln(w, "  (no env vars)")
					return nil
				}
				maxKeyLen := 0
				for _, v := range result.Vars {
					if len(v.Key) > maxKeyLen {
						maxKeyLen = len(v.Key)
					}
				}
				for _, v := range result.Vars {
					fmt.Fprintf(w, "  %-*s   %s\n", maxKeyLen, v.Key, v.Annotation)
				}
				return nil
			})
		},
	}

	cmd.Flags().StringVar(&contextName, "context", "", "Target profile (default: CWD-matched)")
	addProfileFlag(cmd, &contextName)
	return cmd
}

func envRemoveCmd() *cobra.Command {
	var contextName string
	var global, main bool

	cmd := &cobra.Command{
		Use:          "remove KEY",
		Short:        "Remove an environment variable (project-level by default)",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			if err := validateContextScope(global, contextName); err != nil {
				return err
			}
			if err := validateMainScope(global, main); err != nil {
				return err
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
			_, po, poPath, err := resolveProjectOverrideForMutation(main)
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
	addMainFlag(cmd, &main)
	cmd.Flags().StringVar(&contextName, "context", "", "Target profile (requires --global)")
	addProfileFlag(cmd, &contextName)
	return cmd
}

// providerSecretEnvError refuses a provider credential in env: and
// shows the credentials: binding to declare instead.
func providerSecretEnvError(key string) error {
	return fmt.Errorf("%s is a provider credential and cannot be set with aide env set; "+
		"store it in an age secret store (aide secrets edit <store>) and declare a binding on the profile "+
		"in your aide config:\n    credentials:\n      <name>:\n        backend: age\n        store: <store>\n        key: <key>\n"+
		"        env: [%s]\n        harnesses: [<agent>]\n    default_credentials: [<name>]", key, key)
}
