// Package main provides the aide CLI commands.
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"

	"github.com/jskswamy/aide/internal/capability"
	"github.com/jskswamy/aide/internal/config"
	aidectx "github.com/jskswamy/aide/internal/context"
	"github.com/jskswamy/aide/internal/launcher"
	"github.com/jskswamy/aide/internal/sandbox"
	"github.com/jskswamy/aide/internal/secrets"
	"github.com/jskswamy/aide/internal/ui"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
	"github.com/spf13/cobra"
)

func registerCommands(rootCmd *cobra.Command) {
	rootCmd.AddCommand(initCmd())
	rootCmd.AddCommand(whichCmd())
	rootCmd.AddCommand(validateCmd())
	rootCmd.AddCommand(secretsCmd())
	rootCmd.AddCommand(configCmd())
	rootCmd.AddCommand(agentsCmd())
	rootCmd.AddCommand(useCmd())
	rootCmd.AddCommand(contextCmd())
	rootCmd.AddCommand(envCmd())
	rootCmd.AddCommand(setupCmd())
	rootCmd.AddCommand(sandboxCmd())
	rootCmd.AddCommand(capCmd())
	rootCmd.AddCommand(statusCmd())
}

func initCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:          "init",
		Short:        "Initialize aide configuration",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			configPath := config.FilePath()

			// Check existing config
			if _, err := os.Stat(configPath); err == nil {
				if !force {
					return fmt.Errorf("config already exists: %s\nUse --force to overwrite (creates .bak backup)", configPath)
				}
				// Backup existing config
				bakPath := configPath + ".bak"
				data, err := os.ReadFile(configPath)
				if err != nil {
					return fmt.Errorf("reading existing config for backup: %w", err)
				}
				if err := os.WriteFile(bakPath, data, 0o644); err != nil {
					return fmt.Errorf("writing backup: %w", err)
				}
				fmt.Fprintf(out, "Backed up existing config to %s\n\n", bakPath)
			}

			fmt.Fprintln(out, "Welcome to aide! Let's set up your configuration.")
			fmt.Fprintln(out)

			reader := bufio.NewReader(os.Stdin)

			// Detect agents on PATH
			result := launcher.ScanAgents(exec.LookPath)
			var agentName string

			if len(result.Found) > 0 {
				// Collect and sort found agent names
				var foundNames []string
				for name := range result.Found {
					foundNames = append(foundNames, name)
				}
				sort.Strings(foundNames)

				fmt.Fprintf(out, "Detected agents on PATH: %s\n", strings.Join(foundNames, ", "))

				// Pick default: prefer "claude" if found, otherwise first alphabetically
				defaultAgent := foundNames[0]
				for _, name := range foundNames {
					if name == "claude" {
						defaultAgent = name
						break
					}
				}

				fmt.Fprintf(out, "Primary agent (default: %s): ", defaultAgent)
				input, err := reader.ReadString('\n')
				if err != nil {
					return fmt.Errorf("reading agent name: %w", err)
				}
				agentName = strings.TrimSpace(input)
				if agentName == "" {
					agentName = defaultAgent
				}
			} else {
				fmt.Fprint(out, "Agent binary name: ")
				input, err := reader.ReadString('\n')
				if err != nil {
					return fmt.Errorf("reading agent name: %w", err)
				}
				agentName = strings.TrimSpace(input)
				if agentName == "" {
					return fmt.Errorf("agent name cannot be empty")
				}
				if !launcher.IsKnownAgent(agentName) {
					return fmt.Errorf(
						"unknown agent %q.\n\nSupported agents: %s",
						agentName, strings.Join(launcher.KnownAgents, ", "),
					)
				}
			}

			fmt.Fprintln(out)

			yamlContent := fmt.Sprintf("agent: %s\n", agentName)

			// Secrets setup (optional)
			fmt.Fprint(out, "Set up secrets? (y/N): ")
			answer, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("reading response: %w", err)
			}
			answer = strings.TrimSpace(strings.ToLower(answer))

			if answer == "y" || answer == "yes" {
				fmt.Fprint(out, "Age public key: ")
				ageKey, err := reader.ReadString('\n')
				if err != nil {
					return fmt.Errorf("reading age key: %w", err)
				}
				ageKey = strings.TrimSpace(ageKey)
				if ageKey == "" {
					return fmt.Errorf("age public key cannot be empty")
				}

				fmt.Fprint(out, "Secrets file name (e.g. personal): ")
				secretsName, err := reader.ReadString('\n')
				if err != nil {
					return fmt.Errorf("reading secrets name: %w", err)
				}
				secretsName = strings.TrimSpace(secretsName)
				if secretsName == "" {
					return fmt.Errorf("secrets file name cannot be empty")
				}

				yamlContent += fmt.Sprintf("secret: %s\n", secretsName)

				// Create the secrets file
				secretsDir := config.SecretsDir()
				mgr := secrets.NewManager(secretsDir, os.TempDir())
				if err := mgr.Create(secretsName, secretsDir, ageKey); err != nil {
					return fmt.Errorf("creating secrets: %w", err)
				}
				fmt.Fprintf(out, "Created secrets/%s.enc.yaml\n", secretsName)
			}

			// Ensure config directory exists
			configDir := config.Dir()
			if err := os.MkdirAll(configDir, 0o755); err != nil {
				return fmt.Errorf("creating config directory: %w", err)
			}

			if err := os.WriteFile(configPath, []byte(yamlContent), 0o644); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}

			// Show generated config
			fmt.Fprintln(out)
			fmt.Fprintf(out, "Created %s:\n\n", configPath)
			for _, line := range strings.Split(strings.TrimRight(yamlContent, "\n"), "\n") {
				fmt.Fprintf(out, "  %s\n", line)
			}

			// Next steps
			fmt.Fprintln(out)
			fmt.Fprintln(out, "Next steps:")
			fmt.Fprintf(out, "  aide                     Launch %s\n", agentName)
			fmt.Fprintln(out, "  aide use <agent>         Bind a folder to an agent")
			fmt.Fprintln(out, "  aide secrets create      Set up encrypted secrets")
			fmt.Fprintln(out, "  aide agents add <name>   Register another agent")

			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing config (creates .bak backup)")

	return cmd
}

func whichCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "which",
		Short:        "Show which context matches the current directory",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			resolve, _ := cmd.Flags().GetBool("resolve")

			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}

			cfg, err := config.Load(config.Dir(), cwd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			remoteURL := aidectx.DetectRemote(cwd, "origin")
			resolved, err := aidectx.Resolve(cfg, cwd, remoteURL)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			prefs := resolved.Preferences

			// Build BannerData
			agentPath, lookErr := exec.LookPath(resolved.Context.Agent)
			if lookErr != nil {
				agentPath = ""
			}

			data := &ui.BannerData{
				ContextName: resolved.Name,
				MatchReason: resolved.MatchReason,
				AgentName:   resolved.Context.Agent,
				AgentPath:   agentPath,
				SecretName:  resolved.Context.Secret,
			}

			// Build env annotations
			if len(resolved.Context.Env) > 0 {
				data.Env = make(map[string]string, len(resolved.Context.Env))
				for k, v := range resolved.Context.Env {
					source, _ := classifyEnvSource(v)
					data.Env[k] = "← " + source
				}
			}

			// In resolve mode, populate detailed fields
			var secretMap map[string]string
			if resolve {
				// Resolve secret keys
				if resolved.Context.Secret != "" {
					filePath := config.ResolveSecretPath(resolved.Context.Secret)
					identity, idErr := secrets.DiscoverAgeKey()
					if idErr == nil {
						decrypted, decErr := secrets.DecryptSecretsFile(filePath, identity)
						if decErr == nil {
							secretMap = decrypted
							data.SecretKeys = make([]string, 0, len(decrypted))
							for k := range decrypted {
								data.SecretKeys = append(data.SecretKeys, k)
							}
							sort.Strings(data.SecretKeys)
						}
					}
				}

				// Resolve env values
				if len(resolved.Context.Env) > 0 {
					data.EnvResolved = make(map[string]string, len(resolved.Context.Env))
					for k, v := range resolved.Context.Env {
						_, secretKey := classifyEnvSource(v)
						displayVal := resolveEnvDisplay(v, "", secretKey, secretMap)
						data.EnvResolved[k] = redactValue(displayVal)
					}
				}
			}

			// Build sandbox info
			homeDir, _ := os.UserHomeDir()
			resolvedSandbox, sbDisabled, _ := sandbox.ResolveSandboxRef(resolved.Context.Sandbox, cfg.Sandboxes)
			if sbDisabled {
				data.Sandbox = &ui.SandboxInfo{Disabled: true}
			} else {
				tempDir := os.TempDir()
				policy, pathWarnings, policyErr := sandbox.PolicyFromConfig(resolvedSandbox, aidectx.ProjectRoot(cwd), "/tmp/aide-preview", homeDir, tempDir)
				if policyErr == nil && policy != nil {
					guardResults := sandbox.EvaluateGuards(policy)
					availableNames := sandbox.AvailableGuardNames(policy.Guards)
					si := &ui.SandboxInfo{
						Network: networkDisplayName(string(policy.Network)),
					}
					if len(policy.AllowPorts) > 0 {
						portStrs := make([]string, len(policy.AllowPorts))
						for i, p := range policy.AllowPorts {
							portStrs[i] = strconv.Itoa(p)
						}
						si.Ports = strings.Join(portStrs, ", ")
					}
					for _, g := range guardResults {
						if len(g.Rules) > 0 {
							display := ui.GuardDisplay{
								Name:      g.Name,
								Protected: g.Protected,
								Allowed:   g.Allowed,
							}
							for _, o := range g.Overrides {
								display.Overrides = append(display.Overrides, ui.GuardOverride{
									EnvVar:      o.EnvVar,
									Value:       o.Value,
									DefaultPath: o.DefaultPath,
								})
							}
							si.Active = append(si.Active, display)
						} else if len(g.Skipped) > 0 {
							si.Skipped = append(si.Skipped, ui.GuardDisplay{
								Name:   g.Name,
								Reason: strings.Join(g.Skipped, "; "),
							})
						}
					}
					si.Available = availableNames
					data.Sandbox = si
					data.Warnings = append(data.Warnings, pathWarnings...)
				}
			}

			// aide which always renders regardless of show_info
			if err := ui.RenderBanner(out, prefs.InfoStyle, data); err != nil {
				return fmt.Errorf("rendering banner: %w", err)
			}
			return nil
		},
	}

	return cmd
}

// networkDisplayName converts raw network mode to user-friendly display.
func networkDisplayName(mode string) string {
	switch mode {
	case "outbound":
		return "outbound only"
	case "none":
		return "none"
	case "unrestricted":
		return "unrestricted"
	default:
		return mode
	}
}

func classifyEnvSource(val string) (source string, secretKey string) {
	reSecretsDot := regexp.MustCompile(`\{\{\s*\.secrets\.(\w+)\s*\}\}`)
	reSecretsIndex := regexp.MustCompile(`\{\{\s*index\s+\.secrets\s+"(\w+)"\s*\}\}`)

	if m := reSecretsDot.FindStringSubmatch(val); m != nil {
		return fmt.Sprintf("from secrets.%s", m[1]), m[1]
	}
	if m := reSecretsIndex.FindStringSubmatch(val); m != nil {
		return fmt.Sprintf("from secrets.%s", m[1]), m[1]
	}
	if strings.Contains(val, ".project_root") {
		return "from project_root", ""
	}
	if strings.Contains(val, ".runtime_dir") {
		return "from runtime_dir", ""
	}
	if strings.Contains(val, "{{") {
		return "template", ""
	}
	return "literal", ""
}

func resolveEnvDisplay(val, _, secretKey string, secretMap map[string]string) string {
	if secretKey != "" && secretMap != nil {
		if sv, ok := secretMap[secretKey]; ok {
			return redactValue(sv)
		}
	}
	return val
}

func redactValue(s string) string {
	if len(s) <= 8 {
		return s + "***"
	}
	return s[:8] + "***"
}

func validateCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "validate",
		Short:        "Validate aide configuration",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}

			cfg, err := config.Load(config.Dir(), cwd)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Config error: %s\n", err)
				return err
			}

			var errors []string
			var warnings []string
			secretsCount := make(map[string]bool)

			for ctxName, ctx := range cfg.Contexts {
				if ctx.Agent != "" && len(cfg.Agents) > 0 {
					if _, ok := cfg.Agents[ctx.Agent]; !ok {
						errors = append(errors, fmt.Sprintf(
							"context %q references unknown agent %q", ctxName, ctx.Agent,
						))
					}
				}

				if ctx.Secret != "" {
					secretsCount[ctx.Secret] = true
					path := config.ResolveSecretPath(ctx.Secret)
					if _, err := os.Stat(path); os.IsNotExist(err) {
						errors = append(errors, fmt.Sprintf(
							"context %q references secret %q which does not exist", ctxName, ctx.Secret,
						))
					}
				}

				if ctx.Sandbox != nil {
					if err := sandbox.ValidateSandboxRef(ctx.Sandbox, cfg.Sandboxes); err != nil {
						errors = append(errors, fmt.Sprintf(
							"context %q has invalid sandbox config: %s", ctxName, err,
						))
					}
				}

				if ctxName != cfg.DefaultContext && ctxName != "default" {
					if len(ctx.Match) == 0 {
						warnings = append(warnings, fmt.Sprintf(
							"context %q has no match rules (will never activate)", ctxName,
						))
					}
				}

				for envKey, envVal := range ctx.Env {
					if strings.Contains(envVal, "{{") {
						if _, tmplErr := template.New("").Parse(envVal); tmplErr != nil {
							errors = append(errors, fmt.Sprintf(
								"context %q env var %q has invalid template syntax: %s", ctxName, envKey, tmplErr,
							))
						} else if strings.Contains(envVal, ".secrets.") && ctx.Secret == "" {
							errors = append(errors, fmt.Sprintf(
								"context %q env var %q references secrets but no secret is configured", ctxName, envKey,
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
				fmt.Fprintf(out, "OK (%d contexts, %d agents, %d secrets)\n",
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

func configCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "View and edit aide configuration",
	}
	cmd.AddCommand(configShowCmd())
	cmd.AddCommand(configEditCmd())
	return cmd
}

func configShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "show",
		Short:        "Print the config file contents",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			configPath := config.FilePath()
			data, err := os.ReadFile(configPath)
			if err != nil {
				if os.IsNotExist(err) {
					fmt.Fprintln(cmd.OutOrStdout(), "No config file found. Run `aide init` to create one.")
					return nil
				}
				return fmt.Errorf("reading config file: %w", err)
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "# %s\n", configPath)
			fmt.Fprint(out, string(data))
			return nil
		},
	}
}

func configEditCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "edit",
		Short:        "Open the config file in your editor",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			configPath := config.FilePath()
			if _, err := os.Stat(configPath); os.IsNotExist(err) {
				return fmt.Errorf("no config file found. Run `aide init` to create one")
			}

			editor := os.Getenv("EDITOR")
			if editor == "" {
				editor = os.Getenv("VISUAL")
			}
			if editor == "" {
				editor = "vi"
			}

			editorCmd := exec.Command(editor, configPath)
			editorCmd.Stdin = os.Stdin
			editorCmd.Stdout = os.Stdout
			editorCmd.Stderr = os.Stderr
			if err := editorCmd.Run(); err != nil {
				return fmt.Errorf("editor exited with error: %w", err)
			}

			cwd, err := os.Getwd()
			if err != nil {
				cwd = "."
			}

			out := cmd.OutOrStdout()
			if _, err := config.Load(config.Dir(), cwd); err != nil {
				fmt.Fprintf(out, "Saved. Validation failed: %s\n", err)
			} else {
				fmt.Fprintln(out, "Saved. Validation: OK")
			}
			return nil
		},
	}
}

func agentsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agents",
		Short: "Manage coding agents",
	}
	cmd.AddCommand(agentsListCmd())
	cmd.AddCommand(agentsAddCmd())
	cmd.AddCommand(agentsRemoveCmd())
	cmd.AddCommand(agentsEditCmd())
	return cmd
}

func agentsAddCmd() *cobra.Command {
	var binary string

	cmd := &cobra.Command{
		Use:          "add <name>",
		Short:        "Register a new agent",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if binary == "" {
				binary = name
			}

			cwd, err := os.Getwd()
			if err != nil {
				cwd = "."
			}
			cfg, loadErr := config.Load(config.Dir(), cwd)
			if loadErr != nil {
				cfg = &config.Config{
					Agents:   make(map[string]config.AgentDef),
					Contexts: make(map[string]config.Context),
				}
			}
			if cfg.Agents == nil {
				cfg.Agents = make(map[string]config.AgentDef)
			}

			if _, ok := cfg.Agents[name]; ok {
				return fmt.Errorf("agent %q already exists. Use 'aide agents edit %s --binary <path>' to update it", name, name)
			}

			cfg.Agents[name] = config.AgentDef{Binary: binary}

			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Added agent %q (binary: %s)\n", name, binary)

			// Check if binary is on PATH
			if _, err := exec.LookPath(binary); err != nil {
				fmt.Fprintf(out, "Warning: %q not found on PATH\n", binary)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&binary, "binary", "", "Binary name or path (defaults to agent name)")
	return cmd
}

func agentsRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "remove <name>",
		Short:        "Remove an agent from configuration",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			cwd, err := os.Getwd()
			if err != nil {
				cwd = "."
			}
			cfg, err := config.Load(config.Dir(), cwd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			if _, ok := cfg.Agents[name]; !ok {
				return fmt.Errorf("agent %q not found", name)
			}

			// Warn if contexts reference this agent
			var refs []string
			for ctxName, ctx := range cfg.Contexts {
				if ctx.Agent == name {
					refs = append(refs, ctxName)
				}
			}

			out := cmd.OutOrStdout()
			if len(refs) > 0 {
				sort.Strings(refs)
				fmt.Fprintf(out, "Warning: agent %q is used by contexts: %s\n", name, strings.Join(refs, ", "))
				fmt.Fprintln(out, "Those contexts will need a different agent.")
			}

			delete(cfg.Agents, name)
			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}
			fmt.Fprintf(out, "Removed agent %q\n", name)
			return nil
		},
	}
}

func agentsEditCmd() *cobra.Command {
	var binary string

	cmd := &cobra.Command{
		Use:          "edit <name>",
		Short:        "Update an agent's binary path",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if binary == "" {
				return fmt.Errorf("--binary flag is required")
			}

			cwd, err := os.Getwd()
			if err != nil {
				cwd = "."
			}
			cfg, err := config.Load(config.Dir(), cwd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			if _, ok := cfg.Agents[name]; !ok {
				return fmt.Errorf("agent %q not found. Use 'aide agents add %s' to create it", name, name)
			}

			cfg.Agents[name] = config.AgentDef{Binary: binary}
			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Updated agent %q (binary: %s)\n", name, binary)
			if _, err := exec.LookPath(binary); err != nil {
				fmt.Fprintf(out, "Warning: %q not found on PATH\n", binary)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&binary, "binary", "", "New binary name or path (required)")
	return cmd
}

func agentsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "list",
		Short:        "List configured and available agents",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				cwd = "."
			}

			out := cmd.OutOrStdout()
			cfg, _ := config.Load(config.Dir(), cwd)

			configured := make(map[string]bool)

			if cfg != nil && len(cfg.Agents) > 0 {
				agentContexts := make(map[string][]string)
				for ctxName, ctx := range cfg.Contexts {
					agentContexts[ctx.Agent] = append(agentContexts[ctx.Agent], ctxName)
				}

				var names []string
				for name := range cfg.Agents {
					names = append(names, name)
				}
				sort.Strings(names)

				for _, name := range names {
					configured[name] = true
					agent := cfg.Agents[name]
					binary := agent.Binary
					if binary == "" {
						binary = name
					}
					resolvedPath, lookErr := exec.LookPath(binary)
					if lookErr != nil {
						resolvedPath = "(not found)"
					}

					usedBy := ""
					if ctxs, ok := agentContexts[name]; ok && len(ctxs) > 0 {
						sort.Strings(ctxs)
						usedBy = fmt.Sprintf("  (used by: %s)", strings.Join(ctxs, ", "))
					}

					fmt.Fprintf(out, "%-10s %s%s\n", name, resolvedPath, usedBy)
				}
			}

			result := launcher.ScanAgents(exec.LookPath)
			var unconfigured []string
			for name, path := range result.Found {
				if !configured[name] {
					unconfigured = append(unconfigured, fmt.Sprintf("%-10s %s  (not configured)", name, path))
				}
			}
			if len(unconfigured) > 0 {
				if len(configured) > 0 {
					fmt.Fprintln(out)
				}
				sort.Strings(unconfigured)
				for _, line := range unconfigured {
					fmt.Fprintln(out, line)
				}
			}

			if len(configured) == 0 && len(unconfigured) == 0 {
				fmt.Fprintln(out, "No agents configured or found on PATH.")
				fmt.Fprintln(out, "Run `aide init` to get started.")
			}

			return nil
		},
	}
}

func useCmd() *cobra.Command {
	var matchPattern string
	var contextName string
	var secretFlag string
	var sandboxProfile string

	cmd := &cobra.Command{
		Use:   "use [agent-name]",
		Short: "Bind current directory to an agent or context",
		Long: `Bind current directory (or a glob pattern) to an agent/context in global config.

Examples:
  aide use claude                       # Bind CWD to claude
  aide use claude --match "~/work/*"    # Bind a glob pattern
  aide use --context myproject          # Add CWD match to existing context
  aide use claude --secret personal     # Also set secret
  aide use claude --sandbox strict      # Use a named sandbox profile`,
		SilenceUsage: true,
		Args:         cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			agentName := ""
			if len(args) > 0 {
				agentName = args[0]
			}

			if agentName == "" && contextName == "" {
				return fmt.Errorf("either an agent name or --context is required")
			}

			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}

			matchPath := cwd
			if matchPattern != "" {
				matchPath = matchPattern
			}

			cfg, err := config.Load(config.Dir(), cwd)
			if err != nil {
				cfg = &config.Config{
					Agents:   make(map[string]config.AgentDef),
					Contexts: make(map[string]config.Context),
				}
			}
			if cfg.Agents == nil {
				cfg.Agents = make(map[string]config.AgentDef)
			}
			if cfg.Contexts == nil {
				cfg.Contexts = make(map[string]config.Context)
			}

			out := cmd.OutOrStdout()
			newRule := config.MatchRule{Path: matchPath}

			if contextName != "" {
				ctx, ok := cfg.Contexts[contextName]
				if !ok {
					return fmt.Errorf("context %q not found in config", contextName)
				}
				ctx.Match = append(ctx.Match, newRule)
				if secretFlag != "" {
					ctx.Secret = secretFlag
					resolvedPath := config.ResolveSecretPath(ctx.Secret)
					if _, err := os.Stat(resolvedPath); os.IsNotExist(err) {
						fmt.Fprintf(out, "Warning: secret %q does not exist yet.\n", ctx.Secret)
						fmt.Fprintf(out, "Create it with: aide secrets create %s --age-key <key>\n\n", secretFlag)
					}
				}
				cfg.Contexts[contextName] = ctx

				if err := config.WriteConfig(cfg); err != nil {
					return fmt.Errorf("writing config: %w", err)
				}

				fmt.Fprintf(out, "Added match rule to context %q:\n", contextName)
				fmt.Fprintf(out, "  path: %s\n", matchPath)
				if secretFlag != "" {
					fmt.Fprintf(out, "  secret: %s\n", secretFlag)
				}
				return nil
			}

			// Accept agent if: known, already in config, or resolvable on PATH
			_, inConfig := cfg.Agents[agentName]
			if !launcher.IsKnownAgent(agentName) && !inConfig {
				if _, lookErr := exec.LookPath(agentName); lookErr != nil {
					return fmt.Errorf(
						"unknown agent %q (not in known agents, config, or PATH).\n\n"+
							"Register it first: aide agents add %s --binary /path/to/binary\n"+
							"Known agents: %s",
						agentName, agentName, strings.Join(launcher.KnownAgents, ", "),
					)
				}
			}

			ctxName := filepath.Base(cwd)
			ctx, exists := cfg.Contexts[ctxName]
			if !exists {
				ctx = config.Context{
					Agent: agentName,
					Match: []config.MatchRule{newRule},
				}
			} else {
				ctx.Agent = agentName
				found := false
				for _, r := range ctx.Match {
					if r.Path == matchPath {
						found = true
						break
					}
				}
				if !found {
					ctx.Match = append(ctx.Match, newRule)
				}
			}

			if secretFlag != "" {
				ctx.Secret = secretFlag
				// Validate secrets file exists
				resolvedPath := config.ResolveSecretPath(ctx.Secret)
				if _, err := os.Stat(resolvedPath); os.IsNotExist(err) {
					fmt.Fprintf(out, "Warning: secret %q does not exist yet.\n", ctx.Secret)
					fmt.Fprintf(out, "Create it with: aide secrets create %s --age-key <key>\n\n", secretFlag)
				}
			}
			if sandboxProfile != "" {
				if sandboxProfile == "false" || sandboxProfile == "none" {
					ctx.Sandbox = &config.SandboxRef{Disabled: sandboxProfile == "false", ProfileName: ""}
					if sandboxProfile == "none" {
						ctx.Sandbox = &config.SandboxRef{ProfileName: "none"}
					}
				} else {
					ctx.Sandbox = &config.SandboxRef{ProfileName: sandboxProfile}
				}
			}
			cfg.Contexts[ctxName] = ctx

			if _, ok := cfg.Agents[agentName]; !ok {
				cfg.Agents[agentName] = config.AgentDef{Binary: agentName}
			}

			if cfg.DefaultContext == "" {
				cfg.DefaultContext = ctxName
			}

			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}

			if exists {
				fmt.Fprintf(out, "Updated context %q:\n", ctxName)
			} else {
				fmt.Fprintf(out, "Created context %q:\n", ctxName)
			}
			fmt.Fprintf(out, "  agent: %s\n", agentName)
			fmt.Fprintf(out, "  match: %s\n", matchPath)
			if secretFlag != "" {
				fmt.Fprintf(out, "  secret: %s\n", secretFlag)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&matchPattern, "match", "", "Glob pattern to match instead of CWD")
	cmd.Flags().StringVar(&contextName, "context", "", "Add match rule to an existing context")
	cmd.Flags().StringVar(&secretFlag, "secret", "", "Secret name (e.g. work)")
	cmd.Flags().StringVar(&sandboxProfile, "sandbox", "", "Sandbox profile name (e.g. strict, none, default)")
	return cmd
}

func contextCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "context",
		Short: "Manage aide contexts",
	}
	cmd.AddCommand(contextListCmd())
	cmd.AddCommand(contextAddCmd())
	cmd.AddCommand(contextAddMatchCmd())
	cmd.AddCommand(contextRenameCmd())
	cmd.AddCommand(contextRemoveCmd())
	cmd.AddCommand(contextSetSecretCmd())
	cmd.AddCommand(contextRemoveSecretCmd())
	cmd.AddCommand(contextSetDefaultCmd())
	return cmd
}

func contextListCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "list",
		Short:        "List all configured contexts",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				cwd = "."
			}
			cfg, err := config.Load(config.Dir(), cwd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			if len(cfg.Contexts) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No contexts configured.")
				return nil
			}

			out := cmd.OutOrStdout()
			names := make([]string, 0, len(cfg.Contexts))
			for name := range cfg.Contexts {
				names = append(names, name)
			}
			sort.Strings(names)

			for i, name := range names {
				ctx := cfg.Contexts[name]
				if name == cfg.DefaultContext {
					fmt.Fprintf(out, "%s (default)\n", name)
				} else {
					fmt.Fprintln(out, name)
				}
				fmt.Fprintf(out, "  Agent:    %s\n", ctx.Agent)
				if ctx.Secret != "" {
					fmt.Fprintf(out, "  Secret:   %s\n", ctx.Secret)
				}
				for _, rule := range ctx.Match {
					if rule.Path != "" {
						fmt.Fprintf(out, "  Match:    %s\n", rule.Path)
					}
					if rule.Remote != "" {
						fmt.Fprintf(out, "  Match:    %s (remote)\n", rule.Remote)
					}
				}
				if len(ctx.Env) > 0 {
					keys := make([]string, 0, len(ctx.Env))
					for k := range ctx.Env {
						keys = append(keys, k)
					}
					sort.Strings(keys)
					fmt.Fprintf(out, "  Env:      %s\n", strings.Join(keys, ", "))
				}
				if i < len(names)-1 {
					fmt.Fprintln(out)
				}
			}
			return nil
		},
	}
}

func contextAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "add",
		Short:        "Add a new context interactively",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			reader := bufio.NewReader(os.Stdin)
			out := cmd.OutOrStdout()

			cwd, err := os.Getwd()
			if err != nil {
				cwd = "."
			}

			fmt.Fprint(out, "Context name: ")
			name, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("reading context name: %w", err)
			}
			name = strings.TrimSpace(name)
			if name == "" {
				return fmt.Errorf("context name cannot be empty")
			}

			fmt.Fprint(out, "Agent: ")
			agent, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("reading agent: %w", err)
			}
			agent = strings.TrimSpace(agent)
			if agent == "" {
				return fmt.Errorf("agent cannot be empty")
			}
			if !launcher.IsKnownAgent(agent) {
				return fmt.Errorf("unknown agent %q.\n\nSupported agents: %s",
					agent, strings.Join(launcher.KnownAgents, ", "))
			}

			matchRule, err := askMatchRule(out, reader, cwd)
			if err != nil {
				return err
			}

			fmt.Fprint(out, "Secret name (optional, press enter to skip): ")
			secretsInput, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("reading secret name: %w", err)
			}
			secretsInput = strings.TrimSpace(secretsInput)

			cfg, loadErr := config.Load(config.Dir(), cwd)
			if loadErr != nil {
				cfg = &config.Config{
					Agents:   make(map[string]config.AgentDef),
					Contexts: make(map[string]config.Context),
				}
			}
			if cfg.Agents == nil {
				cfg.Agents = make(map[string]config.AgentDef)
			}
			if cfg.Contexts == nil {
				cfg.Contexts = make(map[string]config.Context)
			}

			if _, ok := cfg.Agents[agent]; !ok {
				cfg.Agents[agent] = config.AgentDef{Binary: agent}
			}

			newCtx := config.Context{
				Agent: agent,
				Match: []config.MatchRule{matchRule},
			}
			if secretsInput != "" {
				newCtx.Secret = secretsInput
			}
			cfg.Contexts[name] = newCtx

			if cfg.DefaultContext == "" {
				cfg.DefaultContext = name
			}

			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}
			fmt.Fprintf(out, "\nAdded context %q\n", name)
			return nil
		},
	}
}

func contextAddMatchCmd() *cobra.Command {
	var contextName string

	cmd := &cobra.Command{
		Use:          "add-match",
		Short:        "Add a match rule to the current context",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, name, ctx, err := resolveContextForMutation(contextName)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			reader := bufio.NewReader(os.Stdin)

			cwd, cwdErr := os.Getwd()
			if cwdErr != nil {
				cwd = "."
			}

			rule, err := askMatchRule(out, reader, cwd)
			if err != nil {
				return err
			}

			ctx.Match = append(ctx.Match, rule)
			cfg.Contexts[name] = ctx

			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}

			if rule.Path != "" {
				fmt.Fprintf(out, "Added path match to context %q: %s\n", name, rule.Path)
			} else {
				fmt.Fprintf(out, "Added remote match to context %q: %s\n", name, rule.Remote)
			}
			fmt.Fprintln(out, "\nTip: `aide setup` can also do this interactively with more options.")
			return nil
		},
	}

	cmd.Flags().StringVar(&contextName, "context", "", "Target context (default: CWD-matched)")
	return cmd
}

func contextRenameCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "rename <old-name> <new-name>",
		Short:        "Rename a context",
		Args:         cobra.ExactArgs(2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			oldName := args[0]
			newName := args[1]

			cwd, err := os.Getwd()
			if err != nil {
				cwd = "."
			}
			cfg, err := config.Load(config.Dir(), cwd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			ctx, ok := cfg.Contexts[oldName]
			if !ok {
				return fmt.Errorf("context %q not found", oldName)
			}
			if _, exists := cfg.Contexts[newName]; exists {
				return fmt.Errorf("context %q already exists", newName)
			}

			cfg.Contexts[newName] = ctx
			delete(cfg.Contexts, oldName)

			if cfg.DefaultContext == oldName {
				cfg.DefaultContext = newName
			}

			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Renamed context %q -> %q\n", oldName, newName)
			return nil
		},
	}
}

func contextRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "remove <name>",
		Short:        "Remove a context from configuration",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			cwd, err := os.Getwd()
			if err != nil {
				cwd = "."
			}
			cfg, err := config.Load(config.Dir(), cwd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			ctx, ok := cfg.Contexts[name]
			if !ok {
				return fmt.Errorf("context %q not found", name)
			}
			removedAgent := ctx.Agent
			delete(cfg.Contexts, name)
			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Removed context %q\n", name)

			// Check for orphaned agent
			if removedAgent != "" {
				stillUsed := false
				for _, c := range cfg.Contexts {
					if c.Agent == removedAgent {
						stillUsed = true
						break
					}
				}
				if !stillUsed {
					fmt.Fprintf(out, "\nAgent %q is no longer used by any context.\n", removedAgent)
					fmt.Fprintf(out, "Remove it with: aide agents remove %s\n", removedAgent)
				}
			}
			return nil
		},
	}
}

func contextSetSecretCmd() *cobra.Command {
	var contextName string

	cmd := &cobra.Command{
		Use:          "set-secret <secret-name>",
		Short:        "Set the secret on the current context",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			secretName := args[0]

			cfg, ctxName, ctx, err := resolveContextForMutation(contextName)
			if err != nil {
				return err
			}

			// Warn if secret file doesn't exist on disk
			resolvedPath := config.ResolveSecretPath(secretName)
			if _, err := os.Stat(resolvedPath); os.IsNotExist(err) {
				fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %s does not exist yet.\n", resolvedPath)
			}

			ctx.Secret = secretName
			cfg.Contexts[ctxName] = ctx

			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Set secret %q on context %q\n", secretName, ctxName)
			return nil
		},
	}

	cmd.Flags().StringVar(&contextName, "context", "", "Target context (default: CWD-matched)")
	return cmd
}

func contextRemoveSecretCmd() *cobra.Command {
	var contextName string

	cmd := &cobra.Command{
		Use:          "remove-secret",
		Short:        "Remove the secret from the current context",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, ctxName, ctx, err := resolveContextForMutation(contextName)
			if err != nil {
				return err
			}

			oldSecret := ctx.Secret
			if oldSecret == "" {
				return fmt.Errorf("context %q has no secret configured", ctxName)
			}

			// Warn if env vars reference secrets templates
			for envKey, envVal := range ctx.Env {
				if strings.Contains(envVal, ".secrets.") {
					fmt.Fprintf(cmd.ErrOrStderr(), "Warning: env var %q references secrets templates\n", envKey)
				}
			}

			ctx.Secret = ""
			cfg.Contexts[ctxName] = ctx

			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed secret %q from context %q\n", oldSecret, ctxName)
			return nil
		},
	}

	cmd.Flags().StringVar(&contextName, "context", "", "Target context (default: CWD-matched)")
	return cmd
}

func contextSetDefaultCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set-default [context-name]",
		Short: "Set a context as the default fallback",
		Long: `Set a context as the default fallback when no match rules apply.

If no context name is given, the CWD-matched context is used.`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				cwd = "."
			}
			cfg, err := config.Load(config.Dir(), cwd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			var ctxName string
			if len(args) > 0 {
				ctxName = args[0]
			} else {
				remoteURL := aidectx.DetectRemote(cwd, "origin")
				rc, err := aidectx.Resolve(cfg, cwd, remoteURL)
				if err != nil {
					return fmt.Errorf("resolving context: %w", err)
				}
				ctxName = rc.Name
			}

			if _, ok := cfg.Contexts[ctxName]; !ok {
				return fmt.Errorf("context %q not found", ctxName)
			}

			cfg.DefaultContext = ctxName
			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Set default context to %q\n", ctxName)
			return nil
		},
	}
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
	var fromSecret string
	var contextName string

	cmd := &cobra.Command{
		Use:   "set KEY [VALUE]",
		Short: "Set an environment variable on a context",
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

			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}
			cfg, err := config.Load(config.Dir(), cwd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

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
					identity, err := secrets.DiscoverAgeKey()
					if err != nil {
						return err
					}
					decrypted, err := secrets.DecryptSecretsFile(secretsFilePath, identity)
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
	identity, err := secrets.DiscoverAgeKey()
	if err != nil {
		return "", err
	}
	decrypted, err := secrets.DecryptSecretsFile(secretsFilePath, identity)
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

// askMatchRule prompts the user with human-friendly questions to build a match rule.
// cwd is used as the default for "this folder" option.
func askMatchRule(out io.Writer, reader *bufio.Reader, cwd string) (config.MatchRule, error) {
	fmt.Fprintln(out, "  How should aide recognize this context?")
	fmt.Fprintf(out, "    [1] This folder (%s)\n", cwd)
	fmt.Fprintln(out, "    [2] A folder path or pattern")
	fmt.Fprintln(out, "    [3] By git repository URL")
	fmt.Fprint(out, "  Select [1]: ")

	input, err := reader.ReadString('\n')
	if err != nil {
		return config.MatchRule{}, fmt.Errorf("reading selection: %w", err)
	}
	input = strings.TrimSpace(input)

	choice := 1
	if input != "" {
		choice, err = strconv.Atoi(input)
		if err != nil || choice < 1 || choice > 3 {
			return config.MatchRule{}, fmt.Errorf("invalid selection: %q", input)
		}
	}

	switch choice {
	case 1:
		path := cwd
		fmt.Fprint(out, "  Include all subfolders? (Y/n): ")
		answer, err := reader.ReadString('\n')
		if err != nil {
			return config.MatchRule{}, fmt.Errorf("reading response: %w", err)
		}
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer == "" || answer == "y" || answer == "yes" {
			path = cwd + "/**"
		}
		return config.MatchRule{Path: path}, nil

	case 2:
		fmt.Fprint(out, "  Folder path: ")
		pathInput, err := reader.ReadString('\n')
		if err != nil {
			return config.MatchRule{}, fmt.Errorf("reading path: %w", err)
		}
		path := strings.TrimSpace(pathInput)
		if path == "" {
			return config.MatchRule{}, fmt.Errorf("path cannot be empty")
		}
		fmt.Fprint(out, "  Include all subfolders? (Y/n): ")
		answer, err := reader.ReadString('\n')
		if err != nil {
			return config.MatchRule{}, fmt.Errorf("reading response: %w", err)
		}
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer == "" || answer == "y" || answer == "yes" {
			path = strings.TrimRight(path, "/") + "/**"
		}
		return config.MatchRule{Path: path}, nil

	case 3:
		fmt.Fprintln(out, "  Examples: github.com/company/*, gitlab.com/team/project")
		fmt.Fprint(out, "  Git remote URL pattern: ")
		urlInput, err := reader.ReadString('\n')
		if err != nil {
			return config.MatchRule{}, fmt.Errorf("reading URL: %w", err)
		}
		url := strings.TrimSpace(urlInput)
		if url == "" {
			return config.MatchRule{}, fmt.Errorf("URL pattern cannot be empty")
		}
		return config.MatchRule{Remote: url}, nil
	}

	return config.MatchRule{}, fmt.Errorf("invalid selection")
}

func envListCmd() *cobra.Command {
	var contextName string

	cmd := &cobra.Command{
		Use:          "list",
		Short:        "List environment variables for a context",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}
			cfg, err := config.Load(config.Dir(), cwd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

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
				annotation := envAnnotation(v)
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

	cmd := &cobra.Command{
		Use:          "remove KEY",
		Short:        "Remove an environment variable from a context",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]

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
			fmt.Fprintf(cmd.OutOrStdout(), "Removed %s from context %q\n", key, ctxName)
			return nil
		},
	}

	cmd.Flags().StringVar(&contextName, "context", "", "Target context (default: CWD-matched)")
	return cmd
}

func envAnnotation(val string) string {
	reSecretsDot := regexp.MustCompile(`\{\{\s*\.secrets\.(\w+)\s*\}\}`)
	if m := reSecretsDot.FindStringSubmatch(val); m != nil {
		return fmt.Sprintf("\u2190 secrets.%s", m[1])
	}
	if strings.Contains(val, ".project_root") {
		return "\u2190 project_root"
	}
	if strings.Contains(val, ".runtime_dir") {
		return "\u2190 runtime_dir"
	}
	if strings.Contains(val, "{{") {
		return "\u2190 template"
	}
	return fmt.Sprintf("= %s", val)
}

func setupCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "setup",
		Short:        "Set up aide for the current directory (guided wizard)",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			reader := bufio.NewReader(os.Stdin)

			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}

			fmt.Fprintf(out, "\nSetting up aide for %s\n", cwd)

			cfg, _ := config.Load(config.Dir(), cwd)
			if cfg == nil {
				cfg = &config.Config{
					Agents:   make(map[string]config.AgentDef),
					Contexts: make(map[string]config.Context),
				}
			}
			if cfg.Agents == nil {
				cfg.Agents = make(map[string]config.AgentDef)
			}
			if cfg.Contexts == nil {
				cfg.Contexts = make(map[string]config.Context)
			}

			// If contexts exist, offer reuse/inherit options
			if len(cfg.Contexts) > 0 {
				ctxNames := make([]string, 0, len(cfg.Contexts))
				for name := range cfg.Contexts {
					ctxNames = append(ctxNames, name)
				}
				sort.Strings(ctxNames)

				fmt.Fprintln(out, "\nExisting contexts:")
				for i, name := range ctxNames {
					ctx := cfg.Contexts[name]
					envCount := len(ctx.Env)
					matchCount := len(ctx.Match)
					fmt.Fprintf(out, "  [%d] %-12s (%s, %d match rules, %d env vars)\n",
						i+1, name, ctx.Agent, matchCount, envCount)
				}
				createIdx := len(ctxNames) + 1
				inheritIdx := len(ctxNames) + 2
				fmt.Fprintf(out, "  [%d] Create new context\n", createIdx)
				fmt.Fprintf(out, "  [%d] Inherit from existing + customize\n", inheritIdx)
				fmt.Fprint(out, "Select [1]: ")

				selInput, err := reader.ReadString('\n')
				if err != nil {
					return fmt.Errorf("reading selection: %w", err)
				}
				selInput = strings.TrimSpace(selInput)
				choice := 1
				if selInput != "" {
					choice, err = strconv.Atoi(selInput)
					if err != nil || choice < 1 || choice > inheritIdx {
						return fmt.Errorf("invalid selection: %q", selInput)
					}
				}

				switch {
				case choice <= len(ctxNames):
					// Reuse existing context: just add a match rule
					selectedName := ctxNames[choice-1]
					ctx := cfg.Contexts[selectedName]

					fmt.Fprintf(out, "\nAdding match rule to context %q\n", selectedName)
					rule, err := askMatchRule(out, reader, cwd)
					if err != nil {
						return err
					}
					ctx.Match = append(ctx.Match, rule)
					cfg.Contexts[selectedName] = ctx

					if err := config.WriteConfig(cfg); err != nil {
						return fmt.Errorf("writing config: %w", err)
					}

					fmt.Fprintf(out, "\nUpdated context %q:\n", selectedName)
					fmt.Fprintf(out, "  Agent:    %s\n", ctx.Agent)
					fmt.Fprintf(out, "  Match:    %d rules\n", len(ctx.Match))
					fmt.Fprintf(out, "\nRun `aide` to launch %s.\n", ctx.Agent)
					return nil

				case choice == inheritIdx:
					// Inherit from existing + customize
					fmt.Fprintln(out, "\nWhich context to inherit from?")
					for i, name := range ctxNames {
						fmt.Fprintf(out, "  [%d] %s\n", i+1, name)
					}
					fmt.Fprint(out, "Select [1]: ")

					parentInput, err := reader.ReadString('\n')
					if err != nil {
						return fmt.Errorf("reading selection: %w", err)
					}
					parentInput = strings.TrimSpace(parentInput)
					parentChoice := 1
					if parentInput != "" {
						parentChoice, err = strconv.Atoi(parentInput)
						if err != nil || parentChoice < 1 || parentChoice > len(ctxNames) {
							return fmt.Errorf("invalid selection: %q", parentInput)
						}
					}
					parentName := ctxNames[parentChoice-1]
					parentCtx := cfg.Contexts[parentName]

					// Let user override agent
					agentPrompt := fmt.Sprintf("  Agent [%s]: ", parentCtx.Agent)
					fmt.Fprint(out, agentPrompt)
					agentInput, err := reader.ReadString('\n')
					if err != nil {
						return fmt.Errorf("reading agent: %w", err)
					}
					newAgent := strings.TrimSpace(agentInput)
					if newAgent == "" {
						newAgent = parentCtx.Agent
					}
					if !launcher.IsKnownAgent(newAgent) {
						if _, inCfg := cfg.Agents[newAgent]; !inCfg {
							if _, lookErr := exec.LookPath(newAgent); lookErr != nil {
								return fmt.Errorf("unknown agent %q (not in known agents, config, or PATH)", newAgent)
							}
						}
					}

					// Let user override secret
					newSecrets := parentCtx.Secret
					if parentCtx.Secret != "" {
						secretsPrompt := fmt.Sprintf("  Secret [%s]: ", parentCtx.Secret)
						fmt.Fprint(out, secretsPrompt)
						secretsInput, err := reader.ReadString('\n')
						if err != nil {
							return fmt.Errorf("reading secret: %w", err)
						}
						si := strings.TrimSpace(secretsInput)
						if si != "" {
							newSecrets = si
						}
					}

					// Copy inherited env vars
					newEnv := make(map[string]string)
					if len(parentCtx.Env) > 0 {
						envKeys := make([]string, 0, len(parentCtx.Env))
						for k := range parentCtx.Env {
							envKeys = append(envKeys, k)
						}
						sort.Strings(envKeys)
						fmt.Fprintln(out, "  Inherited env vars:")
						for _, k := range envKeys {
							fmt.Fprintf(out, "    %s = %s\n", k, parentCtx.Env[k])
							newEnv[k] = parentCtx.Env[k]
						}
					}

					fmt.Fprint(out, "  Add more env vars? (y/N): ")
					addMore, err := reader.ReadString('\n')
					if err != nil {
						return fmt.Errorf("reading response: %w", err)
					}
					addMore = strings.TrimSpace(strings.ToLower(addMore))
					if addMore == "y" || addMore == "yes" {
						for {
							fmt.Fprint(out, "  Env var (KEY=value, empty to stop): ")
							kvInput, err := reader.ReadString('\n')
							if err != nil {
								return fmt.Errorf("reading env var: %w", err)
							}
							kv := strings.TrimSpace(kvInput)
							if kv == "" {
								break
							}
							parts := strings.SplitN(kv, "=", 2)
							if len(parts) != 2 {
								fmt.Fprintln(out, "  Invalid format, use KEY=value")
								continue
							}
							newEnv[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
						}
					}

					// Add CWD match rule
					rule, err := askMatchRule(out, reader, cwd)
					if err != nil {
						return err
					}

					ctxName := filepath.Base(cwd)
					newCtx := config.Context{
						Agent: newAgent,
						Match: []config.MatchRule{rule},
					}
					if newSecrets != "" {
						newCtx.Secret = newSecrets
					}
					if len(newEnv) > 0 {
						newCtx.Env = newEnv
					}
					cfg.Contexts[ctxName] = newCtx

					if _, ok := cfg.Agents[newAgent]; !ok {
						cfg.Agents[newAgent] = config.AgentDef{Binary: newAgent}
					}

					if err := config.WriteConfig(cfg); err != nil {
						return fmt.Errorf("writing config: %w", err)
					}

					fmt.Fprintf(out, "\nCreated context %q (inherited from %q):\n", ctxName, parentName)
					fmt.Fprintf(out, "  Agent:    %s\n", newAgent)
					if newSecrets != "" {
						fmt.Fprintf(out, "  Secrets:  %s\n", newSecrets)
					}
					fmt.Fprintf(out, "  Match:    %s\n", setupMatchRuleSummary(rule))
					if len(newEnv) > 0 {
						ek := make([]string, 0, len(newEnv))
						for k := range newEnv {
							ek = append(ek, k)
						}
						sort.Strings(ek)
						fmt.Fprintf(out, "  Env:      %s\n", strings.Join(ek, ", "))
					}
					fmt.Fprintf(out, "\nRun `aide` to launch %s.\n", newAgent)
					return nil

				default:
					// choice == createIdx: fall through to create-new flow below
				}
			}

			// Create-new flow (also used when no contexts exist)
			return setupCreateNew(out, reader, cfg, cwd)
		},
	}
}

func setupMatchRuleSummary(rule config.MatchRule) string {
	if rule.Path != "" {
		return rule.Path
	}
	return rule.Remote
}

func setupCreateNew(out io.Writer, reader *bufio.Reader, cfg *config.Config, cwd string) error {
	// Step 1: Agent
	fmt.Fprintln(out, "\nStep 1: Agent")

	var configuredNames []string
	for name := range cfg.Agents {
		configuredNames = append(configuredNames, name)
	}
	sort.Strings(configuredNames)
	if len(configuredNames) > 0 {
		fmt.Fprintf(out, "  Configured agents: %s\n", strings.Join(configuredNames, ", "))
	}

	result := launcher.ScanAgents(exec.LookPath)
	var detectedNames []string
	for name := range result.Found {
		detectedNames = append(detectedNames, name)
	}
	sort.Strings(detectedNames)
	if len(detectedNames) > 0 {
		fmt.Fprintf(out, "  Detected on PATH: %s\n", strings.Join(detectedNames, ", "))
	}

	seen := make(map[string]bool)
	var allAgents []string
	for _, name := range configuredNames {
		if !seen[name] {
			seen[name] = true
			allAgents = append(allAgents, name)
		}
	}
	for _, name := range detectedNames {
		if !seen[name] {
			seen[name] = true
			allAgents = append(allAgents, name)
		}
	}
	sort.Strings(allAgents)

	defaultAgent := ""
	for _, name := range allAgents {
		if name == "claude" {
			defaultAgent = name
			break
		}
	}
	if defaultAgent == "" && len(configuredNames) > 0 {
		defaultAgent = configuredNames[0]
	}
	if defaultAgent == "" && len(detectedNames) > 0 {
		defaultAgent = detectedNames[0]
	}

	prompt := "  Agent for this folder"
	if defaultAgent != "" {
		prompt += fmt.Sprintf(" [%s]", defaultAgent)
	}
	prompt += ": "
	fmt.Fprint(out, prompt)

	agentInput, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("reading agent selection: %w", err)
	}
	agentName := strings.TrimSpace(agentInput)
	if agentName == "" {
		agentName = defaultAgent
	}
	if agentName == "" {
		return fmt.Errorf("agent name cannot be empty")
	}

	_, inConfig := cfg.Agents[agentName]
	if !launcher.IsKnownAgent(agentName) && !inConfig {
		if _, lookErr := exec.LookPath(agentName); lookErr != nil {
			return fmt.Errorf("unknown agent %q (not in known agents, config, or PATH)", agentName)
		}
	}

	// Step 2: Secrets
	fmt.Fprintln(out, "\nStep 2: Secrets")

	secretsDir := config.SecretsDir()
	matches, _ := filepath.Glob(filepath.Join(secretsDir, "*.enc.yaml"))
	sort.Strings(matches)

	var selectedSecret string

	if len(matches) > 0 {
		fmt.Fprintln(out, "  Available secrets:")
		secretsBaseNames := make([]string, len(matches))
		for i, m := range matches {
			baseName := strings.TrimSuffix(filepath.Base(m), ".enc.yaml")
			secretsBaseNames[i] = baseName
			keyCount := ""
			if identity, idErr := secrets.DiscoverAgeKey(); idErr == nil {
				if data, decErr := secrets.DecryptSecretsFile(m, identity); decErr == nil {
					keyCount = fmt.Sprintf(" (%d keys)", len(data))
				}
			}
			fmt.Fprintf(out, "    [%d] %s%s\n", i+1, baseName, keyCount)
		}
		createIdx := len(matches) + 1
		skipIdx := len(matches) + 2
		fmt.Fprintf(out, "    [%d] Create new secrets file\n", createIdx)
		fmt.Fprintf(out, "    [%d] Skip\n", skipIdx)
		fmt.Fprint(out, "  Select [1]: ")

		selInput, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("reading selection: %w", err)
		}
		selInput = strings.TrimSpace(selInput)
		choice := 1
		if selInput != "" {
			choice, err = strconv.Atoi(selInput)
			if err != nil || choice < 1 || choice > skipIdx {
				return fmt.Errorf("invalid selection: %q", selInput)
			}
		}

		switch { //nolint:staticcheck // switch with no tag used for complex condition matching
		case choice == skipIdx:
			fmt.Fprintln(out, "  Skipping secrets.")
		case choice == createIdx:
			sf, err := setupCreateSecrets(out, reader)
			if err != nil {
				return err
			}
			selectedSecret = sf
		default:
			selectedSecret = secretsBaseNames[choice-1]
		}
	} else {
		fmt.Fprintln(out, "  No secrets files found.")
		fmt.Fprint(out, "  Create one? (y/N): ")
		answer, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("reading response: %w", err)
		}
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer == "y" || answer == "yes" {
			sf, err := setupCreateSecrets(out, reader)
			if err != nil {
				return err
			}
			selectedSecret = sf
		} else {
			fmt.Fprintln(out, "  Skipping secrets.")
		}
	}

	// Step 3: Env wiring
	envMap := make(map[string]string)

	if selectedSecret != "" {
		fmt.Fprintln(out, "\nStep 3: Environment variables")

		secretsFilePath := config.ResolveSecretPath(selectedSecret)
		identity, idErr := secrets.DiscoverAgeKey()
		if idErr != nil {
			fmt.Fprintln(out, "  Could not discover age key; skipping env wiring.")
		} else {
			data, decErr := secrets.DecryptSecretsFile(secretsFilePath, identity)
			switch {
			case decErr != nil:
				fmt.Fprintf(out, "  Could not decrypt: %s\n  Skipping env wiring.\n", decErr)
			case len(data) == 0:
				fmt.Fprintln(out, "  Secrets file has no keys; skipping env wiring.")
			default:
				keys := make([]string, 0, len(data))
				for k := range data {
					keys = append(keys, k)
				}
				sort.Strings(keys)

				fmt.Fprintf(out, "  Keys in %s:\n", selectedSecret)
				for i, k := range keys {
					fmt.Fprintf(out, "    [%d] %s\n", i+1, k)
				}

				fmt.Fprint(out, "  Wire as env vars? (y/N): ")
				wireAnswer, err := reader.ReadString('\n')
				if err != nil {
					return fmt.Errorf("reading response: %w", err)
				}
				wireAnswer = strings.TrimSpace(strings.ToLower(wireAnswer))

				if wireAnswer == "y" || wireAnswer == "yes" {
					fmt.Fprint(out, "  Select keys (comma-separated, or * for all) [*]: ")
					selInput, err := reader.ReadString('\n')
					if err != nil {
						return fmt.Errorf("reading selection: %w", err)
					}
					selInput = strings.TrimSpace(selInput)
					if selInput == "" || selInput == "*" {
						for _, k := range keys {
							envMap[strings.ToUpper(k)] = fmt.Sprintf("{{ .secrets.%s }}", k)
						}
					} else {
						parts := strings.Split(selInput, ",")
						for _, p := range parts {
							p = strings.TrimSpace(p)
							idx, err := strconv.Atoi(p)
							if err != nil || idx < 1 || idx > len(keys) {
								return fmt.Errorf("invalid selection: %q", p)
							}
							k := keys[idx-1]
							envMap[strings.ToUpper(k)] = fmt.Sprintf("{{ .secrets.%s }}", k)
						}
					}

					if len(envMap) > 0 {
						fmt.Fprintln(out)
						envKeys := make([]string, 0, len(envMap))
						for k := range envMap {
							envKeys = append(envKeys, k)
						}
						sort.Strings(envKeys)
						for _, ek := range envKeys {
							reKey := regexp.MustCompile(`\{\{\s*\.secrets\.(\w+)\s*\}\}`)
							if m := reKey.FindStringSubmatch(envMap[ek]); m != nil {
								fmt.Fprintf(out, "  %s <- secrets.%s\n", ek, m[1])
							}
						}
					}
				}
			}
		}
	}

	// Step 4: Sandbox
	fmt.Fprintln(out, "\nStep 4: Sandbox")
	fmt.Fprintln(out, "  Default policy protects SSH keys, cloud credentials, and browser profiles.")
	fmt.Fprintln(out, "  [1] Use defaults (recommended)")
	fmt.Fprintln(out, "  [2] Add extra denied paths")
	fmt.Fprintln(out, "  [3] Disable sandbox (not recommended)")
	fmt.Fprint(out, "  Select [1]: ")

	var selectedSandbox *config.SandboxRef
	sandboxInput, _ := reader.ReadString('\n')
	sandboxInput = strings.TrimSpace(sandboxInput)
	sandboxChoice := 1
	if sandboxInput != "" {
		var parseErr error
		sandboxChoice, parseErr = strconv.Atoi(sandboxInput)
		if parseErr != nil || sandboxChoice < 1 || sandboxChoice > 3 {
			return fmt.Errorf("invalid selection: %q", sandboxInput)
		}
	}

	switch sandboxChoice {
	case 1:
		// nil SandboxRef = use defaults
		fmt.Fprintln(out, "  Using default sandbox policy.")
	case 2:
		fmt.Fprint(out, "  Enter extra denied paths (comma-separated): ")
		pathInput, pathErr := reader.ReadString('\n')
		if pathErr != nil {
			return fmt.Errorf("reading denied paths: %w", pathErr)
		}
		pathInput = strings.TrimSpace(pathInput)
		var deniedPaths []string
		for _, p := range strings.Split(pathInput, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				deniedPaths = append(deniedPaths, p)
			}
		}
		if len(deniedPaths) > 0 {
			selectedSandbox = &config.SandboxRef{Inline: &config.SandboxPolicy{DeniedExtra: deniedPaths}}
			fmt.Fprintf(out, "  Added %d extra denied path(s).\n", len(deniedPaths))
		} else {
			fmt.Fprintln(out, "  No paths provided; using default sandbox policy.")
		}
	case 3:
		selectedSandbox = &config.SandboxRef{Disabled: true}
		fmt.Fprintln(out, "  Sandbox disabled. The agent will have full filesystem and network access.")
	}

	// Step 5: Write config
	ctxName := filepath.Base(cwd)
	ctx := config.Context{
		Agent: agentName,
		Match: []config.MatchRule{{Path: cwd}},
	}
	if selectedSecret != "" {
		ctx.Secret = selectedSecret
	}
	if len(envMap) > 0 {
		ctx.Env = envMap
	}
	if selectedSandbox != nil {
		ctx.Sandbox = selectedSandbox
	}
	cfg.Contexts[ctxName] = ctx

	if _, ok := cfg.Agents[agentName]; !ok {
		cfg.Agents[agentName] = config.AgentDef{Binary: agentName}
	}

	if cfg.DefaultContext == "" {
		cfg.DefaultContext = ctxName
	}

	if err := config.WriteConfig(cfg); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	fmt.Fprintf(out, "\nCreated context %q:\n", ctxName)
	fmt.Fprintf(out, "  Agent:    %s\n", agentName)
	if selectedSecret != "" {
		fmt.Fprintf(out, "  Secret:   %s\n", selectedSecret)
	}
	fmt.Fprintf(out, "  Match:    %s\n", cwd)
	if len(envMap) > 0 {
		envKeys := make([]string, 0, len(envMap))
		for k := range envMap {
			envKeys = append(envKeys, k)
		}
		sort.Strings(envKeys)
		fmt.Fprintf(out, "  Env:      %s\n", strings.Join(envKeys, ", "))
	}

	fmt.Fprintf(out, "\nRun `aide` to launch %s.\n", agentName)
	return nil
}

func setupCreateSecrets(out io.Writer, reader *bufio.Reader) (string, error) {
	fmt.Fprint(out, "  Secrets file name (e.g. personal): ")
	nameInput, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("reading secrets name: %w", err)
	}
	name := strings.TrimSpace(nameInput)
	if name == "" {
		return "", fmt.Errorf("secrets file name cannot be empty")
	}

	fmt.Fprint(out, "  Age public key: ")
	ageKeyInput, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("reading age key: %w", err)
	}
	ageKey := strings.TrimSpace(ageKeyInput)
	if ageKey == "" {
		return "", fmt.Errorf("age public key cannot be empty")
	}

	secretsDir := config.SecretsDir()
	mgr := secrets.NewManager(secretsDir, os.TempDir())
	if err := mgr.Create(name, secretsDir, ageKey); err != nil {
		return "", fmt.Errorf("creating secrets: %w", err)
	}

	fmt.Fprintf(out, "  Created secrets/%s.enc.yaml\n", name)
	return name, nil
}

func sandboxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sandbox",
		Short: "Manage sandbox profiles",
	}
	cmd.AddCommand(sandboxShowCmd())
	cmd.AddCommand(sandboxTestCmd())
	cmd.AddCommand(sandboxListCmd())
	cmd.AddCommand(sandboxCreateCmd())
	cmd.AddCommand(sandboxEditCmd())
	cmd.AddCommand(sandboxRemoveCmd())
	cmd.AddCommand(sandboxDenyCmd())
	cmd.AddCommand(sandboxAllowCmd())
	cmd.AddCommand(sandboxResetCmd())
	cmd.AddCommand(sandboxPortsCmd())
	cmd.AddCommand(sandboxNetworkCmd())
	cmd.AddCommand(sandboxGuardsCmd())
	cmd.AddCommand(sandboxGuardCmd())
	cmd.AddCommand(sandboxUnguardCmd())
	cmd.AddCommand(sandboxTypesCmd())
	return cmd
}

func sandboxNetworkCmd() *cobra.Command {
	var contextName string
	cmd := &cobra.Command{
		Use:          "network <mode>",
		Short:        "Set network mode for a context's sandbox (outbound|none|unrestricted)",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := args[0]
			validModes := map[string]bool{"outbound": true, "none": true, "unrestricted": true}
			if !validModes[mode] {
				return fmt.Errorf("invalid network mode %q (must be outbound, none, or unrestricted)", mode)
			}
			cfg, ctxName, ctx, err := resolveContextForMutation(contextName)
			if err != nil {
				return err
			}
			sp := ensureInlineSandbox(&ctx)
			sp.Network = &config.NetworkPolicy{Mode: mode}
			cfg.Contexts[ctxName] = ctx
			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Set network mode to %q for context %q\n", mode, ctxName)
			return nil
		},
	}
	cmd.Flags().StringVar(&contextName, "context", "", "target context name")
	return cmd
}

func sandboxShowCmd() *cobra.Command {
	var contextName string
	var withCaps, withoutCaps []string

	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show effective sandbox policy for current/named context",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}

			cfg, err := config.Load(config.Dir(), cwd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			// Resolve context
			remoteURL := aidectx.DetectRemote(cwd, "origin")
			rc, err := aidectx.Resolve(cfg, cwd, remoteURL)
			if err != nil {
				return fmt.Errorf("resolving context: %w", err)
			}

			if contextName != "" {
				ctx, ok := cfg.Contexts[contextName]
				if !ok {
					return fmt.Errorf("context %q not found", contextName)
				}
				rc = &aidectx.ResolvedContext{
					Name:    contextName,
					Context: ctx,
				}
			}

			// Resolve sandbox ref
			sandboxCfg, disabled, sbErr := sandbox.ResolveSandboxRef(rc.Context.Sandbox, cfg.Sandboxes)
			if sbErr != nil {
				return fmt.Errorf("resolving sandbox: %w", sbErr)
			}

			if disabled {
				fmt.Fprintf(out, "Sandbox: disabled (context %q)\n", rc.Name)
				return nil
			}

			// Resolve capabilities and merge into sandbox config
			capNames := sandbox.MergeCapNames(rc.Context.Capabilities, withCaps, withoutCaps)
			_, capOverrides, err := sandbox.ResolveCapabilities(capNames, cfg)
			if err != nil {
				return fmt.Errorf("resolving capabilities: %w", err)
			}
			sandbox.ApplyOverrides(&sandboxCfg, capOverrides)

			homeDir, _ := os.UserHomeDir()
			tempDir := os.TempDir()
			projectRoot := aidectx.ProjectRoot(cwd)

			policy, _, err := sandbox.PolicyFromConfig(sandboxCfg, projectRoot, "/tmp/aide-preview", homeDir, tempDir)
			if err != nil {
				return fmt.Errorf("building sandbox policy: %w", err)
			}
			if policy == nil {
				fmt.Fprintf(out, "Sandbox: disabled (context %q)\n", rc.Name)
				return nil
			}

			source := "default"
			if rc.Context.Sandbox != nil {
				if rc.Context.Sandbox.ProfileName != "" {
					source = fmt.Sprintf("profile %q", rc.Context.Sandbox.ProfileName)
				} else if rc.Context.Sandbox.Inline != nil {
					source = "inline"
				}
			}
			fmt.Fprintf(out, "Effective sandbox policy (%s):\n", source)
			fmt.Fprintf(out, "  Guards:     %s\n", strings.Join(policy.Guards, ", "))
			fmt.Fprintf(out, "  Denied:     %s\n", strings.Join(policy.ExtraDenied, ", "))
			fmt.Fprintf(out, "  Network:    %s\n", policy.Network)
			return nil
		},
	}
	cmd.Flags().StringVar(&contextName, "context", "", "show policy for a specific context")
	cmd.Flags().StringSliceVar(&withCaps, "with", nil, "additional capabilities to enable")
	cmd.Flags().StringSliceVar(&withoutCaps, "without", nil, "capabilities to disable")
	return cmd
}

func sandboxTestCmd() *cobra.Command {
	var contextName string
	var withCaps, withoutCaps []string

	cmd := &cobra.Command{
		Use:   "test",
		Short: "Generate and print the platform-specific sandbox profile without launching the agent",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}

			cfg, err := config.Load(config.Dir(), cwd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			// Resolve context
			remoteURL := aidectx.DetectRemote(cwd, "origin")
			rc, err := aidectx.Resolve(cfg, cwd, remoteURL)
			if err != nil {
				return fmt.Errorf("resolving context: %w", err)
			}

			if contextName != "" {
				ctx, ok := cfg.Contexts[contextName]
				if !ok {
					return fmt.Errorf("context %q not found", contextName)
				}
				rc = &aidectx.ResolvedContext{
					Name:    contextName,
					Context: ctx,
				}
			}

			// Resolve sandbox ref
			sandboxCfg, disabled, sbErr := sandbox.ResolveSandboxRef(rc.Context.Sandbox, cfg.Sandboxes)
			if sbErr != nil {
				return fmt.Errorf("resolving sandbox: %w", sbErr)
			}

			if disabled {
				fmt.Fprintf(out, "Sandbox: disabled (context %q)\n", rc.Name)
				return nil
			}

			// Resolve capabilities and merge into sandbox config
			capNames := sandbox.MergeCapNames(rc.Context.Capabilities, withCaps, withoutCaps)
			_, capOverrides, err := sandbox.ResolveCapabilities(capNames, cfg)
			if err != nil {
				return fmt.Errorf("resolving capabilities: %w", err)
			}
			sandbox.ApplyOverrides(&sandboxCfg, capOverrides)

			homeDir, _ := os.UserHomeDir()
			tempDir := os.TempDir()
			projectRoot := aidectx.ProjectRoot(cwd)

			policy, _, err := sandbox.PolicyFromConfig(sandboxCfg, projectRoot, "/tmp/aide-preview", homeDir, tempDir)
			if err != nil {
				return fmt.Errorf("building sandbox policy: %w", err)
			}
			if policy == nil {
				fmt.Fprintf(out, "Sandbox: disabled (context %q)\n", rc.Name)
				return nil
			}

			sb := sandbox.NewSandbox()
			profile, err := sb.GenerateProfile(*policy)
			if err != nil {
				return fmt.Errorf("generating sandbox profile: %w", err)
			}

			fmt.Fprint(out, profile)
			return nil
		},
	}
	cmd.Flags().StringVar(&contextName, "context", "", "generate profile for a specific context")
	cmd.Flags().StringSliceVar(&withCaps, "with", nil, "additional capabilities to enable")
	cmd.Flags().StringSliceVar(&withoutCaps, "without", nil, "capabilities to disable")
	return cmd
}

func sandboxListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List named sandbox profiles",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}

			cfg, err := config.Load(config.Dir(), cwd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			fmt.Fprintf(out, "%-16s %-12s %s\n", "NAME", "SOURCE", "DETAILS")
			fmt.Fprintf(out, "%-16s %-12s %s\n", "default", "(built-in)", "network=outbound")

			if cfg.Sandboxes != nil {
				names := make([]string, 0, len(cfg.Sandboxes))
				for name := range cfg.Sandboxes {
					names = append(names, name)
				}
				sort.Strings(names)
				for _, name := range names {
					sp := cfg.Sandboxes[name]
					details := ""
					if sp.Network != nil && sp.Network.Mode != "" {
						details = fmt.Sprintf("network=%s", sp.Network.Mode)
					}
					if len(sp.DeniedExtra) > 0 {
						if details != "" {
							details += "  "
						}
						details += fmt.Sprintf("denied_extra: %s", strings.Join(sp.DeniedExtra, ", "))
					}
					fmt.Fprintf(out, "%-16s %-12s %s\n", name, "(config)", details)
				}
			}

			return nil
		},
	}
}

func sandboxCreateCmd() *cobra.Command {
	var fromProfile string

	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new sandbox profile",
		Args:  cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			reader := bufio.NewReader(os.Stdin)
			name := args[0]

			if name == "default" || name == "none" {
				return fmt.Errorf("cannot use reserved profile name %q", name)
			}

			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}

			cfg, err := config.Load(config.Dir(), cwd)
			if err != nil {
				cfg = &config.Config{}
			}
			if cfg.Sandboxes == nil {
				cfg.Sandboxes = make(map[string]config.SandboxPolicy)
			}

			if _, exists := cfg.Sandboxes[name]; exists {
				return fmt.Errorf("sandbox profile %q already exists (use 'aide sandbox edit' to modify)", name)
			}

			var sp config.SandboxPolicy

			if fromProfile != "" && fromProfile != "default" {
				base, ok := cfg.Sandboxes[fromProfile]
				if !ok {
					return fmt.Errorf("base profile %q not found", fromProfile)
				}
				sp = base
			}

			// Ask for writable paths
			fmt.Fprint(out, "Additional writable paths (comma-separated, empty to skip):\n> ")
			wrInput, _ := reader.ReadString('\n')
			wrInput = strings.TrimSpace(wrInput)
			if wrInput != "" {
				for _, p := range strings.Split(wrInput, ",") {
					p = strings.TrimSpace(p)
					if p == "" {
						continue
					}
					expanded := expandHome(p)
					if _, err := os.Stat(expanded); err != nil {
						fmt.Fprintf(out, "  ⚠ %s does not exist (added anyway)\n", p)
					} else {
						fmt.Fprintf(out, "  ✓ %s exists\n", p)
					}
					sp.WritableExtra = append(sp.WritableExtra, p)
				}
			}

			// Ask for denied paths
			fmt.Fprint(out, "Additional denied paths (comma-separated, empty to skip):\n> ")
			dnInput, _ := reader.ReadString('\n')
			dnInput = strings.TrimSpace(dnInput)
			if dnInput != "" {
				for _, p := range strings.Split(dnInput, ",") {
					p = strings.TrimSpace(p)
					if p == "" {
						continue
					}
					expanded := expandHome(p)
					if _, err := os.Stat(expanded); err != nil {
						fmt.Fprintf(out, "  ⚠ %s does not exist (added anyway)\n", p)
					} else {
						fmt.Fprintf(out, "  ✓ %s exists\n", p)
					}
					sp.DeniedExtra = append(sp.DeniedExtra, p)
				}
			}

			// Ask for network mode
			fmt.Fprint(out, "Network mode [outbound/none/unrestricted] (default: outbound): ")
			netInput, _ := reader.ReadString('\n')
			netInput = strings.TrimSpace(netInput)
			if netInput == "" {
				netInput = "outbound"
			}
			validModes := map[string]bool{"outbound": true, "none": true, "unrestricted": true}
			if !validModes[netInput] {
				return fmt.Errorf("invalid network mode %q", netInput)
			}
			sp.Network = &config.NetworkPolicy{Mode: netInput}

			cfg.Sandboxes[name] = sp

			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}

			fmt.Fprintf(out, "\nCreated sandbox profile %q\n", name)
			return nil
		},
	}
	cmd.Flags().StringVar(&fromProfile, "from", "", "base profile to inherit from")
	return cmd
}

func sandboxEditCmd() *cobra.Command {
	var addDenied, addWritable, addReadable, removeDenied, removeWritable, removeReadable []string
	var network string

	cmd := &cobra.Command{
		Use:   "edit <name>",
		Short: "Edit an existing sandbox profile",
		Args:  cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			name := args[0]

			if name == "default" || name == "none" {
				return fmt.Errorf("cannot edit built-in profile %q", name)
			}

			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}

			cfg, err := config.Load(config.Dir(), cwd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			if cfg.Sandboxes == nil {
				return fmt.Errorf("sandbox profile %q not found", name)
			}

			sp, ok := cfg.Sandboxes[name]
			if !ok {
				return fmt.Errorf("sandbox profile %q not found", name)
			}

			for _, p := range addWritable {
				expanded := expandHome(p)
				if _, err := os.Stat(expanded); err != nil {
					fmt.Fprintf(out, "  ⚠ %s does not exist (added anyway)\n", p)
				}
				sp.WritableExtra = append(sp.WritableExtra, p)
			}

			for _, p := range addDenied {
				expanded := expandHome(p)
				if _, err := os.Stat(expanded); err != nil {
					fmt.Fprintf(out, "  ⚠ %s does not exist (added anyway)\n", p)
				}
				sp.DeniedExtra = append(sp.DeniedExtra, p)
			}

			for _, p := range removeWritable {
				sp.WritableExtra = removeFromSlice(sp.WritableExtra, p)
			}

			for _, p := range removeDenied {
				sp.DeniedExtra = removeFromSlice(sp.DeniedExtra, p)
			}

			for _, p := range addReadable {
				expanded := expandHome(p)
				if _, err := os.Stat(expanded); err != nil {
					fmt.Fprintf(out, "  ⚠ %s does not exist (added anyway)\n", p)
				}
				sp.ReadableExtra = append(sp.ReadableExtra, p)
			}

			for _, p := range removeReadable {
				sp.ReadableExtra = removeFromSlice(sp.ReadableExtra, p)
			}

			if network != "" {
				validModes := map[string]bool{"outbound": true, "none": true, "unrestricted": true}
				if !validModes[network] {
					return fmt.Errorf("invalid network mode %q", network)
				}
				sp.Network = &config.NetworkPolicy{Mode: network}
			}

			cfg.Sandboxes[name] = sp

			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}

			fmt.Fprintf(out, "Updated sandbox profile %q\n", name)
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&addDenied, "add-denied", nil, "add denied paths")
	cmd.Flags().StringSliceVar(&addWritable, "add-writable", nil, "add writable paths")
	cmd.Flags().StringSliceVar(&addReadable, "add-readable", nil, "add readable paths")
	cmd.Flags().StringSliceVar(&removeDenied, "remove-denied", nil, "remove denied paths")
	cmd.Flags().StringSliceVar(&removeWritable, "remove-writable", nil, "remove writable paths")
	cmd.Flags().StringSliceVar(&removeReadable, "remove-readable", nil, "remove readable paths")
	cmd.Flags().StringVar(&network, "network", "", "set network mode")
	return cmd
}

func sandboxRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a sandbox profile",
		Args:  cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			name := args[0]

			if name == "default" || name == "none" {
				return fmt.Errorf("cannot remove built-in profile %q", name)
			}

			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}

			cfg, err := config.Load(config.Dir(), cwd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			if cfg.Sandboxes == nil {
				return fmt.Errorf("sandbox profile %q not found", name)
			}
			if _, ok := cfg.Sandboxes[name]; !ok {
				return fmt.Errorf("sandbox profile %q not found", name)
			}

			// Warn if any contexts reference this profile
			for ctxName, ctx := range cfg.Contexts {
				if ctx.Sandbox != nil && ctx.Sandbox.ProfileName == name {
					fmt.Fprintf(out, "  Warning: context %q references profile %q\n", ctxName, name)
				}
			}

			delete(cfg.Sandboxes, name)

			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}

			fmt.Fprintf(out, "Removed sandbox profile %q\n", name)
			return nil
		},
	}
}

// splitCommaList splits a comma-separated string into its parts,
// matching the behaviour of pflag's StringSliceVar used by --with.
func splitCommaList(s string) []string {
	var result []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func removeFromSlice(slice []string, item string) []string {
	var result []string
	for _, s := range slice {
		if s != item {
			result = append(result, s)
		}
	}
	return result
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}

// resolveContextForMutation loads config, resolves the context name, and returns
// the config, context name, and context for modification.
func resolveContextForMutation(contextName string) (*config.Config, string, config.Context, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, "", config.Context{}, fmt.Errorf("getting working directory: %w", err)
	}
	cfg, err := config.Load(config.Dir(), cwd)
	if err != nil {
		return nil, "", config.Context{}, fmt.Errorf("loading config: %w", err)
	}
	if contextName == "" {
		remoteURL := aidectx.DetectRemote(cwd, "origin")
		rc, err := aidectx.Resolve(cfg, cwd, remoteURL)
		if err != nil {
			return nil, "", config.Context{}, fmt.Errorf("resolving context: %w", err)
		}
		contextName = rc.Name
	}
	ctx, ok := cfg.Contexts[contextName]
	if !ok {
		return nil, "", config.Context{}, fmt.Errorf("context %q not found", contextName)
	}
	return cfg, contextName, ctx, nil
}

// ensureInlineSandbox ensures the context has an inline SandboxRef with a SandboxPolicy.
func ensureInlineSandbox(ctx *config.Context) *config.SandboxPolicy {
	if ctx.Sandbox == nil {
		ctx.Sandbox = &config.SandboxRef{Inline: &config.SandboxPolicy{}}
	}
	// Clear disabled flag — user is actively configuring the sandbox
	ctx.Sandbox.Disabled = false
	// Clear profile name — switching to inline config
	ctx.Sandbox.ProfileName = ""
	if ctx.Sandbox.Inline == nil {
		ctx.Sandbox.Inline = &config.SandboxPolicy{}
	}
	return ctx.Sandbox.Inline
}

func sandboxDenyCmd() *cobra.Command {
	var contextName string
	cmd := &cobra.Command{
		Use:          "deny <path>",
		Short:        "Add a path to the denied_extra list",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			cfg, ctxName, ctx, err := resolveContextForMutation(contextName)
			if err != nil {
				return err
			}
			sp := ensureInlineSandbox(&ctx)
			sp.DeniedExtra = append(sp.DeniedExtra, path)
			cfg.Contexts[ctxName] = ctx
			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Added %s to denied_extra for context %q\n", path, ctxName)
			return nil
		},
	}
	cmd.Flags().StringVar(&contextName, "context", "", "target context name")
	return cmd
}

func sandboxAllowCmd() *cobra.Command {
	var contextName string
	var write bool
	cmd := &cobra.Command{
		Use:          "allow <path>",
		Short:        "Add a path to readable_extra or writable_extra",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			cfg, ctxName, ctx, err := resolveContextForMutation(contextName)
			if err != nil {
				return err
			}
			sp := ensureInlineSandbox(&ctx)
			listName := "readable_extra"
			if write {
				sp.WritableExtra = append(sp.WritableExtra, path)
				listName = "writable_extra"
			} else {
				sp.ReadableExtra = append(sp.ReadableExtra, path)
			}
			cfg.Contexts[ctxName] = ctx
			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Added %s to %s for context %q\n", path, listName, ctxName)
			return nil
		},
	}
	cmd.Flags().StringVar(&contextName, "context", "", "target context name")
	cmd.Flags().BoolVar(&write, "write", false, "add to writable_extra instead of readable_extra")
	return cmd
}

func sandboxResetCmd() *cobra.Command {
	var contextName string
	cmd := &cobra.Command{
		Use:          "reset",
		Short:        "Reset sandbox to defaults for a context",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, ctxName, ctx, err := resolveContextForMutation(contextName)
			if err != nil {
				return err
			}
			ctx.Sandbox = nil
			cfg.Contexts[ctxName] = ctx
			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Reset sandbox to defaults for context %q\n", ctxName)
			return nil
		},
	}
	cmd.Flags().StringVar(&contextName, "context", "", "target context name")
	return cmd
}

func sandboxPortsCmd() *cobra.Command {
	var contextName string
	cmd := &cobra.Command{
		Use:          "ports <port1> [port2] ...",
		Short:        "Set allowed network ports for a context's sandbox",
		Args:         cobra.MinimumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			var ports []int
			for _, arg := range args {
				p, err := strconv.Atoi(arg)
				if err != nil {
					return fmt.Errorf("invalid port %q: %w", arg, err)
				}
				if p < 1 || p > 65535 {
					return fmt.Errorf("port %d out of range (must be 1-65535)", p)
				}
				ports = append(ports, p)
			}
			cfg, ctxName, ctx, err := resolveContextForMutation(contextName)
			if err != nil {
				return err
			}
			sp := ensureInlineSandbox(&ctx)
			sp.Network = &config.NetworkPolicy{Mode: "outbound", AllowPorts: ports}
			cfg.Contexts[ctxName] = ctx
			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Set allowed ports to %v for context %q\n", ports, ctxName)
			return nil
		},
	}
	cmd.Flags().StringVar(&contextName, "context", "", "target context name")
	return cmd
}

func sandboxGuardsCmd() *cobra.Command {
	var contextName string
	var withCaps, withoutCaps []string
	cmd := &cobra.Command{
		Use:          "guards",
		Short:        "List all guards with type, status, and description",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			allGuards := guards.AllGuards()

			// Resolve the active set from the current context config
			var activeNames []string
			cfg, ctxName, ctx, err := resolveContextForMutation(contextName)
			if err == nil {
				_ = ctxName
				var sandboxCfg *config.SandboxPolicy
				if ctx.Sandbox != nil && !ctx.Sandbox.Disabled {
					if ctx.Sandbox.Inline != nil {
						sandboxCfg = ctx.Sandbox.Inline
					} else if ctx.Sandbox.ProfileName != "" {
						if sp, ok := cfg.Sandboxes[ctx.Sandbox.ProfileName]; ok {
							sandboxCfg = &sp
						}
					}
				}

				// Resolve capabilities and merge into sandbox config
				capNames := sandbox.MergeCapNames(ctx.Capabilities, withCaps, withoutCaps)
				_, capOverrides, capErr := sandbox.ResolveCapabilities(capNames, cfg)
				if capErr == nil {
					sandbox.ApplyOverrides(&sandboxCfg, capOverrides)
				}

				activeNames = sandbox.EffectiveGuards(sandboxCfg)
			} else {
				// Fall back to defaults if config cannot be loaded
				activeNames = guards.DefaultGuardNames()
			}

			activeSet := make(map[string]bool, len(activeNames))
			for _, n := range activeNames {
				activeSet[n] = true
			}

			fmt.Fprintf(out, "%-20s %-12s %-10s %s\n", "GUARD", "TYPE", "STATUS", "DESCRIPTION")
			for _, g := range allGuards {
				status := "inactive"
				if activeSet[g.Name()] {
					status = "active"
				}
				fmt.Fprintf(out, "%-20s %-12s %-10s %s\n", g.Name(), g.Type(), status, g.Description())
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&contextName, "context", "", "target context name")
	cmd.Flags().StringSliceVar(&withCaps, "with", nil, "additional capabilities to enable")
	cmd.Flags().StringSliceVar(&withoutCaps, "without", nil, "capabilities to disable")
	return cmd
}

func sandboxGuardCmd() *cobra.Command {
	var contextName string
	cmd := &cobra.Command{
		Use:          "guard <name>",
		Short:        "Enable an additional guard for a context's sandbox",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			cfg, ctxName, ctx, err := resolveContextForMutation(contextName)
			if err != nil {
				return err
			}
			// Reject named profile references — user must edit the profile directly
			if ctx.Sandbox != nil && !ctx.Sandbox.Disabled && ctx.Sandbox.Inline == nil && ctx.Sandbox.ProfileName != "" {
				return fmt.Errorf("context %q uses a named sandbox profile %q; modify the profile directly", ctxName, ctx.Sandbox.ProfileName)
			}
			sp := ensureInlineSandbox(&ctx)
			r := sandbox.EnableGuard(sp, name)
			for _, w := range r.Warnings {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", w)
			}
			if !r.OK() {
				return fmt.Errorf("%s", r.Errors[0])
			}
			cfg.Contexts[ctxName] = ctx
			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}
			if len(r.Warnings) > 0 {
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Guard %q enabled for context %q\n", name, ctxName)
			return nil
		},
	}
	cmd.Flags().StringVar(&contextName, "context", "", "target context name")
	return cmd
}

func sandboxUnguardCmd() *cobra.Command {
	var contextName string
	cmd := &cobra.Command{
		Use:          "unguard <name>",
		Short:        "Disable a guard for a context's sandbox",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			cfg, ctxName, ctx, err := resolveContextForMutation(contextName)
			if err != nil {
				return err
			}
			// Reject named profile references — user must edit the profile directly
			if ctx.Sandbox != nil && !ctx.Sandbox.Disabled && ctx.Sandbox.Inline == nil && ctx.Sandbox.ProfileName != "" {
				return fmt.Errorf("context %q uses a named sandbox profile %q; modify the profile directly", ctxName, ctx.Sandbox.ProfileName)
			}
			sp := ensureInlineSandbox(&ctx)
			r := sandbox.DisableGuard(sp, name)
			for _, w := range r.Warnings {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", w)
			}
			if !r.OK() {
				return fmt.Errorf("%s", r.Errors[0])
			}
			cfg.Contexts[ctxName] = ctx
			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}
			if len(r.Warnings) > 0 {
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Guard %q disabled for context %q\n", name, ctxName)
			return nil
		},
	}
	cmd.Flags().StringVar(&contextName, "context", "", "target context name")
	return cmd
}

func sandboxTypesCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "types",
		Short:        "List built-in guard types with their default state and description",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%-12s %-10s %s\n", "TYPE", "STATE", "DESCRIPTION")
			fmt.Fprintf(out, "%-12s %-10s %s\n", "always", "on", "Always active; cannot be disabled")
			fmt.Fprintf(out, "%-12s %-10s %s\n", "default", "on", "Active by default; can be disabled with unguard")
			fmt.Fprintf(out, "%-12s %-10s %s\n", "opt-in", "off", "Inactive by default; enable with guard")
			return nil
		},
	}
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "status",
		Short:        "Show detailed view of current context and capabilities",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}

			cfg, err := config.Load(config.Dir(), cwd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			remoteURL := aidectx.DetectRemote(cwd, "origin")
			resolved, err := aidectx.Resolve(cfg, cwd, remoteURL)
			if err != nil {
				return err
			}

			// Resolve agent path
			agentName := resolved.Context.Agent
			agentPath, lookErr := exec.LookPath(agentName)
			if lookErr != nil {
				agentPath = "(not found)"
			}

			// Resolve secret key count
			secretName := resolved.Context.Secret
			var secretKeyCount int
			if secretName != "" {
				filePath := config.ResolveSecretPath(secretName)
				identity, idErr := secrets.DiscoverAgeKey()
				if idErr == nil {
					if data, decErr := secrets.DecryptSecretsFile(filePath, identity); decErr == nil {
						secretKeyCount = len(data)
					}
				}
			}

			// Build capability registry and resolve capabilities
			userCaps := capability.FromConfigDefs(cfg.Capabilities)
			registry := capability.MergedRegistry(userCaps)

			capNames := resolved.Context.Capabilities
			var capSet *capability.Set
			if len(capNames) > 0 {
				capSet, err = capability.ResolveAll(capNames, registry, cfg.NeverAllow, cfg.NeverAllowEnv)
				if err != nil {
					return fmt.Errorf("resolving capabilities: %w", err)
				}
			}

			// Resolve sandbox policy for rule count
			sandboxPolicy, sandboxDisabled, _ := sandbox.ResolveSandboxRef(
				resolved.Context.Sandbox, cfg.Sandboxes,
			)
			var guardCount int
			if !sandboxDisabled {
				homeDir, _ := os.UserHomeDir()
				tempDir := os.TempDir()
				pol, _, _ := sandbox.PolicyFromConfig(sandboxPolicy, cwd, "", homeDir, tempDir)
				if pol != nil {
					guardCount = len(pol.Guards)
				}
			}

			// Determine network mode
			networkMode := "outbound only (all ports)"
			if sandboxPolicy != nil && sandboxPolicy.Network != nil {
				mode := sandboxPolicy.Network.Mode
				switch mode {
				case "none":
					networkMode = "none"
				case "unrestricted":
					networkMode = "unrestricted"
				default:
					networkMode = "outbound only"
				}
				if len(sandboxPolicy.Network.AllowPorts) > 0 {
					ports := make([]string, len(sandboxPolicy.Network.AllowPorts))
					for i, p := range sandboxPolicy.Network.AllowPorts {
						ports[i] = strconv.Itoa(p)
					}
					networkMode += " (ports " + strings.Join(ports, ", ") + ")"
				} else if mode == "" || mode == "outbound" {
					networkMode += " (all ports)"
				}
			}

			// Determine auto-approve
			autoApprove := resolved.Context.Yolo != nil && *resolved.Context.Yolo

			// Print formatted output
			line := strings.Repeat("\u2500", 40)
			fmt.Fprintln(out, line)

			fmt.Fprintf(out, "Context:      %s\n", resolved.Name)
			fmt.Fprintf(out, "Agent:        %s \u2192 %s\n", agentName, agentPath)
			fmt.Fprintf(out, "Matched:      %s\n", resolved.MatchReason)

			if secretName != "" {
				if secretKeyCount > 0 {
					fmt.Fprintf(out, "Secret:       %s (%d keys)\n", secretName, secretKeyCount)
				} else {
					fmt.Fprintf(out, "Secret:       %s\n", secretName)
				}
			}

			// Capabilities section
			if capSet != nil && len(capSet.Capabilities) > 0 {
				fmt.Fprintln(out)
				fmt.Fprintln(out, "Capabilities:")
				for _, cap := range capSet.Capabilities {
					// Show name with inheritance chain
					label := cap.Name
					if len(cap.Sources) > 1 {
						label += " (extends " + strings.Join(cap.Sources[1:], ", ") + ")"
					}
					fmt.Fprintf(out, "  %s\n", label)

					if len(cap.Readable) > 0 {
						fmt.Fprintf(out, "    readable:  %s\n", strings.Join(cap.Readable, ", "))
					}
					if len(cap.Writable) > 0 {
						fmt.Fprintf(out, "    writable:  %s\n", strings.Join(cap.Writable, ", "))
					}
					if len(cap.Deny) > 0 {
						fmt.Fprintf(out, "    deny:      %s\n", strings.Join(cap.Deny, ", "))
					}
					if len(cap.EnvAllow) > 0 {
						fmt.Fprintf(out, "    env:       %s\n", strings.Join(cap.EnvAllow, ", "))
					}
					fmt.Fprintf(out, "    source:    context config\n")
					fmt.Fprintln(out)
				}
			}

			// Never-allow section
			neverAllow := cfg.NeverAllow
			if capSet != nil {
				neverAllow = capSet.NeverAllow
			}
			if len(neverAllow) > 0 {
				fmt.Fprintln(out, "Never-allow:")
				for _, path := range neverAllow {
					fmt.Fprintf(out, "  %s\n", path)
				}
			}

			// Credential warnings
			if capSet != nil && len(capSet.Capabilities) > 0 {
				var allEnvAllow []string
				for _, cap := range capSet.Capabilities {
					allEnvAllow = append(allEnvAllow, cap.EnvAllow...)
				}
				credWarnings := capability.CredentialWarnings(allEnvAllow)
				if len(credWarnings) > 0 {
					fmt.Fprintln(out)
					fmt.Fprintln(out, "Credentials exposed:")
					for _, w := range credWarnings {
						fmt.Fprintf(out, "  \u26a0 %s\n", w)
					}
				}

				compWarnings := capability.CompositionWarnings(capSet.Capabilities)
				if len(compWarnings) > 0 {
					fmt.Fprintln(out)
					for _, w := range compWarnings {
						fmt.Fprintf(out, "\u26a0 %s\n", w)
					}
				}
			}

			fmt.Fprintln(out)
			fmt.Fprintf(out, "Network: %s\n", networkMode)
			if sandboxDisabled {
				fmt.Fprintln(out, "Sandbox: disabled")
			} else {
				fmt.Fprintf(out, "Sandbox: active (%d guards)\n", guardCount)
			}
			if autoApprove {
				fmt.Fprintln(out, "Auto-approve: yes")
			} else {
				fmt.Fprintln(out, "Auto-approve: no")
			}
			fmt.Fprintln(out, line)

			return nil
		},
	}
}

func capCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cap",
		Short: "Manage capabilities",
	}
	cmd.AddCommand(capListCmd())
	cmd.AddCommand(capShowCmd())
	cmd.AddCommand(capCreateCmd())
	cmd.AddCommand(capEditCmd())
	cmd.AddCommand(capEnableCmd())
	cmd.AddCommand(capDisableCmd())
	cmd.AddCommand(capNeverAllowCmd())
	cmd.AddCommand(capCheckCmd())
	cmd.AddCommand(capAuditCmd())
	cmd.AddCommand(capSuggestForPathCmd())
	return cmd
}

func capListCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "list",
		Short:        "List all capabilities (built-in and user-defined)",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}

			cfg, err := config.Load(config.Dir(), cwd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			userCaps := capability.FromConfigDefs(cfg.Capabilities)
			registry := capability.MergedRegistry(userCaps)
			builtins := capability.Builtins()

			// Collect and sort names
			names := make([]string, 0, len(registry))
			for name := range registry {
				names = append(names, name)
			}
			sort.Strings(names)

			fmt.Fprintf(out, "%-20s %-12s %s\n", "NAME", "SOURCE", "DESCRIPTION")
			for _, name := range names {
				entry := registry[name]
				source := "built-in"
				if _, isBuiltin := builtins[name]; !isBuiltin {
					switch {
					case entry.Extends != "":
						source = "extends"
					case len(entry.Combines) > 0:
						source = "combines"
					default:
						source = "custom"
					}
				} else if _, isUser := userCaps[name]; isUser {
					// User override of a built-in
					source = "custom"
				}
				fmt.Fprintf(out, "%-20s %-12s %s\n", name, source, entry.Description)
			}

			return nil
		},
	}
}

func capShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "show <name>",
		Short:             "Show detailed information about a capability",
		Args:              cobra.ExactArgs(1),
		SilenceUsage:      true,
		ValidArgsFunction: capabilityCompletionFunc,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			name := args[0]

			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}

			cfg, err := config.Load(config.Dir(), cwd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			userCaps := capability.FromConfigDefs(cfg.Capabilities)
			registry := capability.MergedRegistry(userCaps)

			if _, ok := registry[name]; !ok {
				return fmt.Errorf("unknown capability: %q", name)
			}

			resolved, err := capability.ResolveOne(name, registry)
			if err != nil {
				return fmt.Errorf("resolving capability: %w", err)
			}

			entry := registry[name]
			fmt.Fprintf(out, "Name:        %s\n", name)
			fmt.Fprintf(out, "Description: %s\n", entry.Description)

			if len(resolved.Sources) > 1 {
				fmt.Fprintf(out, "Sources:     %s\n", strings.Join(resolved.Sources, " -> "))
			}

			capShowSection(out, "Unguard", resolved.Unguard)
			capShowSection(out, "Readable", resolved.Readable)
			capShowSection(out, "Writable", resolved.Writable)
			capShowSection(out, "Deny", resolved.Deny)
			capShowSection(out, "EnvAllow", resolved.EnvAllow)

			return nil
		},
	}
}

func capShowSection(out io.Writer, label string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(out, "%-12s %s\n", label+":", strings.Join(items, ", "))
}

func capCreateCmd() *cobra.Command {
	var extends string
	var combines, readable, writable, deny, envAllow []string
	var description string

	cmd := &cobra.Command{
		Use:          "create <name>",
		Short:        "Create a new capability definition",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			name := args[0]

			// Validate: name must not conflict with a built-in capability
			builtins := capability.Builtins()
			if _, isBuiltin := builtins[name]; isBuiltin {
				return fmt.Errorf("capability %q is a built-in capability and cannot be overridden", name)
			}

			// Validate: --extends and --combines are mutually exclusive
			if extends != "" && len(combines) > 0 {
				return fmt.Errorf("--extends and --combines are mutually exclusive; use one or the other")
			}

			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}

			cfg, err := config.Load(config.Dir(), cwd)
			if err != nil {
				cfg = &config.Config{}
			}
			if cfg.Capabilities == nil {
				cfg.Capabilities = make(map[string]config.CapabilityDef)
			}

			if _, exists := cfg.Capabilities[name]; exists {
				return fmt.Errorf("capability %q already exists (use 'aide cap edit' to modify)", name)
			}

			// Build a lookup of all known capabilities (built-in + user-defined)
			allKnown := make(map[string]bool, len(builtins)+len(cfg.Capabilities))
			for k := range builtins {
				allKnown[k] = true
			}
			for k := range cfg.Capabilities {
				allKnown[k] = true
			}

			// Validate: referenced capabilities must exist
			if extends != "" {
				if !allKnown[extends] {
					return fmt.Errorf("parent capability %q does not exist in built-in or user-defined registry", extends)
				}
			}
			for _, c := range combines {
				if !allKnown[c] {
					return fmt.Errorf("combined capability %q does not exist in built-in or user-defined registry", c)
				}
			}

			capDef := config.CapabilityDef{
				Extends:     extends,
				Combines:    combines,
				Description: description,
				Readable:    readable,
				Writable:    writable,
				Deny:        deny,
				EnvAllow:    envAllow,
			}

			cfg.Capabilities[name] = capDef

			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}

			fmt.Fprintf(out, "Created capability %q\n", name)
			return nil
		},
	}

	cmd.Flags().StringVar(&extends, "extends", "", "Parent capability to extend")
	cmd.Flags().StringSliceVar(&combines, "combines", nil, "Capabilities to combine")
	cmd.Flags().StringSliceVar(&readable, "readable", nil, "Readable paths")
	cmd.Flags().StringSliceVar(&writable, "writable", nil, "Writable paths")
	cmd.Flags().StringSliceVar(&deny, "deny", nil, "Denied paths")
	cmd.Flags().StringSliceVar(&envAllow, "env-allow", nil, "Environment variables to pass through")
	cmd.Flags().StringVar(&description, "description", "", "Human-readable description")

	return cmd
}

func capEditCmd() *cobra.Command {
	var addReadable, addWritable, addDeny, removeDeny, addEnvAllow, removeEnvAllow []string
	var description string

	cmd := &cobra.Command{
		Use:          "edit <name>",
		Short:        "Edit a user-defined capability",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			name := args[0]

			// Must not be a built-in capability
			builtins := capability.Builtins()
			if _, isBuiltin := builtins[name]; isBuiltin {
				return fmt.Errorf("capability %q is a built-in capability and cannot be edited", name)
			}

			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}

			cfg, err := config.Load(config.Dir(), cwd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			if cfg.Capabilities == nil {
				return fmt.Errorf("capability %q not found in user-defined capabilities", name)
			}

			capDef, exists := cfg.Capabilities[name]
			if !exists {
				return fmt.Errorf("capability %q not found in user-defined capabilities", name)
			}

			// Apply description change
			if cmd.Flags().Changed("description") {
				capDef.Description = description
			}

			// Apply additive changes
			capDef.Readable = append(capDef.Readable, addReadable...)
			capDef.Writable = append(capDef.Writable, addWritable...)
			capDef.Deny = append(capDef.Deny, addDeny...)
			capDef.EnvAllow = append(capDef.EnvAllow, addEnvAllow...)

			// Apply removals
			for _, r := range removeDeny {
				capDef.Deny = removeFromSlice(capDef.Deny, r)
			}
			for _, r := range removeEnvAllow {
				capDef.EnvAllow = removeFromSlice(capDef.EnvAllow, r)
			}

			cfg.Capabilities[name] = capDef

			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}

			fmt.Fprintf(out, "Updated capability %q\n", name)
			return nil
		},
	}

	cmd.Flags().StringSliceVar(&addReadable, "add-readable", nil, "Readable paths to add")
	cmd.Flags().StringSliceVar(&addWritable, "add-writable", nil, "Writable paths to add")
	cmd.Flags().StringSliceVar(&addDeny, "add-deny", nil, "Denied paths to add")
	cmd.Flags().StringSliceVar(&removeDeny, "remove-deny", nil, "Denied paths to remove")
	cmd.Flags().StringSliceVar(&addEnvAllow, "add-env-allow", nil, "Environment variables to add")
	cmd.Flags().StringSliceVar(&removeEnvAllow, "remove-env-allow", nil, "Environment variables to remove")
	cmd.Flags().StringVar(&description, "description", "", "Update the description")

	return cmd
}

func capEnableCmd() *cobra.Command {
	var contextName string
	cmd := &cobra.Command{
		Use:               "enable <capability>[,capability...]",
		Short:             "Enable capabilities for the current context",
		Args:              cobra.ExactArgs(1),
		SilenceUsage:      true,
		ValidArgsFunction: capabilityCompletionFunc,
		RunE: func(cmd *cobra.Command, args []string) error {
			capNames := splitCommaList(args[0])

			cfg, ctxName, ctx, err := resolveContextForMutation(contextName)
			if err != nil {
				return err
			}

			// Validate all capabilities exist (built-in or user-defined)
			userCaps := capability.FromConfigDefs(cfg.Capabilities)
			registry := capability.MergedRegistry(userCaps)
			for _, capName := range capNames {
				if _, ok := registry[capName]; !ok {
					return fmt.Errorf("unknown capability: %q", capName)
				}
			}

			for _, capName := range capNames {
				// Check if already enabled
				already := false
				for _, c := range ctx.Capabilities {
					if c == capName {
						already = true
						break
					}
				}
				if already {
					fmt.Fprintf(cmd.OutOrStdout(), "Capability %q is already enabled for context %q\n", capName, ctxName)
					continue
				}

				ctx.Capabilities = append(ctx.Capabilities, capName)
				fmt.Fprintf(cmd.OutOrStdout(), "Capability %q enabled for context %q\n", capName, ctxName)
			}

			cfg.Contexts[ctxName] = ctx
			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}

			return nil
		},
	}
	cmd.Flags().StringVar(&contextName, "context", "", "target context name")
	return cmd
}

func capDisableCmd() *cobra.Command {
	var contextName string
	cmd := &cobra.Command{
		Use:               "disable <capability>[,capability...]",
		Short:             "Disable capabilities for the current context",
		Args:              cobra.ExactArgs(1),
		SilenceUsage:      true,
		ValidArgsFunction: capabilityCompletionFunc,
		RunE: func(cmd *cobra.Command, args []string) error {
			capNames := splitCommaList(args[0])

			cfg, ctxName, ctx, err := resolveContextForMutation(contextName)
			if err != nil {
				return err
			}

			for _, capName := range capNames {
				// Check if the capability is in the list
				found := false
				for _, c := range ctx.Capabilities {
					if c == capName {
						found = true
						break
					}
				}

				if !found {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: capability %q is not enabled for context %q\n", capName, ctxName)
					continue
				}

				ctx.Capabilities = removeFromSlice(ctx.Capabilities, capName)
				fmt.Fprintf(cmd.OutOrStdout(), "Capability %q disabled for context %q\n", capName, ctxName)
			}

			cfg.Contexts[ctxName] = ctx
			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}

			return nil
		},
	}
	cmd.Flags().StringVar(&contextName, "context", "", "target context name")
	return cmd
}

func capNeverAllowCmd() *cobra.Command {
	var envMode bool
	var list bool
	var remove bool

	cmd := &cobra.Command{
		Use:   "never-allow [path or env var]",
		Short: "Manage global never-allow paths and environment variables",
		Long: `Manage the global never_allow and never_allow_env lists.

These entries are always denied regardless of capability configuration.

Examples:
  aide cap never-allow ~/.kube/prod-config            Add path to never_allow
  aide cap never-allow --env VAULT_ROOT_TOKEN          Add env var to never_allow_env
  aide cap never-allow --list                          Show all entries
  aide cap never-allow --remove ~/.kube/prod-config    Remove a path
  aide cap never-allow --remove --env VAULT_ROOT_TOKEN Remove an env var`,
		SilenceUsage: true,
		Args:         cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}

			cfg, err := config.Load(config.Dir(), cwd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			// --list mode: show all entries
			if list {
				if len(cfg.NeverAllow) == 0 && len(cfg.NeverAllowEnv) == 0 {
					fmt.Fprintln(out, "No never-allow entries configured.")
					return nil
				}
				if len(cfg.NeverAllow) > 0 {
					fmt.Fprintln(out, "never_allow paths:")
					for _, p := range cfg.NeverAllow {
						fmt.Fprintf(out, "  %s\n", p)
					}
				}
				if len(cfg.NeverAllowEnv) > 0 {
					fmt.Fprintln(out, "never_allow_env variables:")
					for _, e := range cfg.NeverAllowEnv {
						fmt.Fprintf(out, "  %s\n", e)
					}
				}
				return nil
			}

			// All other modes require an argument
			if len(args) == 0 {
				return fmt.Errorf("a path or environment variable name is required (use --list to show entries)")
			}
			entry := args[0]

			if remove {
				// --remove mode
				if envMode {
					before := len(cfg.NeverAllowEnv)
					cfg.NeverAllowEnv = removeFromSlice(cfg.NeverAllowEnv, entry)
					if len(cfg.NeverAllowEnv) == before {
						return fmt.Errorf("env var %q not found in never_allow_env", entry)
					}
				} else {
					before := len(cfg.NeverAllow)
					cfg.NeverAllow = removeFromSlice(cfg.NeverAllow, entry)
					if len(cfg.NeverAllow) == before {
						return fmt.Errorf("path %q not found in never_allow", entry)
					}
				}
				if err := config.WriteConfig(cfg); err != nil {
					return fmt.Errorf("writing config: %w", err)
				}
				if envMode {
					fmt.Fprintf(out, "Removed env var %q from never_allow_env\n", entry)
				} else {
					fmt.Fprintf(out, "Removed path %q from never_allow\n", entry)
				}
				return nil
			}

			// Add mode (default)
			if envMode {
				for _, e := range cfg.NeverAllowEnv {
					if e == entry {
						return fmt.Errorf("env var %q is already in never_allow_env", entry)
					}
				}
				cfg.NeverAllowEnv = append(cfg.NeverAllowEnv, entry)
			} else {
				for _, p := range cfg.NeverAllow {
					if p == entry {
						return fmt.Errorf("path %q is already in never_allow", entry)
					}
				}
				cfg.NeverAllow = append(cfg.NeverAllow, entry)
			}
			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}
			if envMode {
				fmt.Fprintf(out, "Added env var %q to never_allow_env\n", entry)
			} else {
				fmt.Fprintf(out, "Added path %q to never_allow\n", entry)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&envMode, "env", false, "Operate on environment variables instead of paths")
	cmd.Flags().BoolVar(&list, "list", false, "List all never-allow entries")
	cmd.Flags().BoolVar(&remove, "remove", false, "Remove an entry instead of adding")

	return cmd
}

func capCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check <capability>[,capability...]",
		Short: "Preview merged sandbox overrides for given capabilities",
		Long: `Resolve one or more capabilities and display the merged sandbox overrides
that would be applied, along with any credential or composition warnings.
This is a preview — nothing is launched or modified.`,
		Args:              cobra.ExactArgs(1),
		SilenceUsage:      true,
		ValidArgsFunction: capabilityCompletionFunc,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			capNames := splitCommaList(args[0])

			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}

			cfg, err := config.Load(config.Dir(), cwd)
			if err != nil {
				// Allow check to work even without config (built-ins only)
				cfg = &config.Config{}
			}

			userCaps := capability.FromConfigDefs(cfg.Capabilities)
			registry := capability.MergedRegistry(userCaps)

			set, err := capability.ResolveAll(capNames, registry, cfg.NeverAllow, cfg.NeverAllowEnv)
			if err != nil {
				return err
			}

			printCapabilityReport(out, set)
			return nil
		},
	}
}

func capAuditCmd() *cobra.Command {
	var contextName string
	cmd := &cobra.Command{
		Use:          "audit",
		Short:        "Show resolved capabilities for the current context",
		Long:         `Reads the active context's capabilities and displays the merged sandbox overrides and any warnings.`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()

			cfg, ctxName, ctx, err := resolveContextForMutation(contextName)
			if err != nil {
				return err
			}

			if len(ctx.Capabilities) == 0 {
				fmt.Fprintf(out, "Context %q has no capabilities enabled.\n", ctxName)
				return nil
			}

			userCaps := capability.FromConfigDefs(cfg.Capabilities)
			registry := capability.MergedRegistry(userCaps)

			set, err := capability.ResolveAll(ctx.Capabilities, registry, cfg.NeverAllow, cfg.NeverAllowEnv)
			if err != nil {
				return err
			}

			fmt.Fprintf(out, "Context: %s\n\n", ctxName)
			printCapabilityReport(out, set)
			return nil
		},
	}
	cmd.Flags().StringVar(&contextName, "context", "", "target context name")
	return cmd
}

func capSuggestForPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "suggest-for-path <path>",
		Short:        "Suggest capabilities that would grant access to a path",
		Long:         `Outputs capability names (one per line) that would grant access to the given path. Designed for machine consumption by plugin hooks.`,
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			targetPath := args[0]

			// Expand ~ in the target path
			if strings.HasPrefix(targetPath, "~/") {
				home, err := os.UserHomeDir()
				if err != nil {
					return fmt.Errorf("getting home directory: %w", err)
				}
				targetPath = filepath.Join(home, targetPath[2:])
			}

			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}

			cfg, err := config.Load(config.Dir(), cwd)
			if err != nil {
				// Allow suggest to work even without config (built-ins only)
				cfg = &config.Config{}
			}

			userCaps := capability.FromConfigDefs(cfg.Capabilities)
			registry := capability.MergedRegistry(userCaps)

			suggestions := capability.SuggestForPath(targetPath, registry)
			sort.Strings(suggestions)
			for _, name := range suggestions {
				fmt.Fprintln(out, name)
			}
			return nil
		},
	}
}

// printCapabilityReport displays the merged sandbox overrides and warnings for a CapabilitySet.
func printCapabilityReport(out io.Writer, set *capability.Set) {
	overrides := set.ToSandboxOverrides()

	// Show per-capability sources
	fmt.Fprintln(out, "Capabilities:")
	for _, cap := range set.Capabilities {
		if len(cap.Sources) > 1 {
			fmt.Fprintf(out, "  %s (via %s)\n", cap.Name, strings.Join(cap.Sources[1:], " -> "))
		} else {
			fmt.Fprintf(out, "  %s\n", cap.Name)
		}
	}
	fmt.Fprintln(out)

	// Show merged overrides
	fmt.Fprintln(out, "Merged sandbox overrides:")
	capReportSection(out, "Unguard", overrides.Unguard)
	capReportSection(out, "Readable", overrides.ReadableExtra)
	capReportSection(out, "Writable", overrides.WritableExtra)
	capReportSection(out, "Denied", overrides.DeniedExtra)
	capReportSection(out, "EnvAllow", overrides.EnvAllow)

	if len(overrides.Unguard) == 0 && len(overrides.ReadableExtra) == 0 &&
		len(overrides.WritableExtra) == 0 && len(overrides.DeniedExtra) == 0 &&
		len(overrides.EnvAllow) == 0 {
		fmt.Fprintln(out, "  (none)")
	}

	// Show warnings
	credWarnings := capability.CredentialWarnings(overrides.EnvAllow)
	compWarnings := capability.CompositionWarnings(set.Capabilities)

	if len(credWarnings) > 0 || len(compWarnings) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Warnings:")
		for _, env := range credWarnings {
			fmt.Fprintf(out, "  [credential] %s is a known credential-bearing env var\n", env)
		}
		for _, w := range compWarnings {
			fmt.Fprintf(out, "  [composition] %s\n", w)
		}
	}
}

func capReportSection(out io.Writer, label string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(out, "  %-12s %s\n", label+":", strings.Join(items, ", "))
}

// capabilityNamesForCompletion returns a sorted list of all capability names
// (built-in + user-defined from config) for shell tab completion.
func capabilityNamesForCompletion() []string {
	builtins := capability.Builtins()
	names := make([]string, 0, len(builtins))
	for name := range builtins {
		names = append(names, name)
	}

	// Try to load config for user-defined capabilities.
	cwd, err := os.Getwd()
	if err == nil {
		if cfg, err := config.Load(config.Dir(), cwd); err == nil {
			userCaps := capability.FromConfigDefs(cfg.Capabilities)
			for name := range userCaps {
				// Only add if not already present from builtins.
				if _, exists := builtins[name]; !exists {
					names = append(names, name)
				}
			}
		}
	}

	sort.Strings(names)
	return names
}

// capabilityCompletionFunc is a cobra completion function that returns all
// available capability names.
func capabilityCompletionFunc(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return capabilityNamesForCompletion(), cobra.ShellCompDirectiveNoFileComp
}

