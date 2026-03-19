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

	"github.com/jskswamy/aide/internal/config"
	aidectx "github.com/jskswamy/aide/internal/context"
	"github.com/jskswamy/aide/internal/launcher"
	"github.com/jskswamy/aide/internal/sandbox"
	"github.com/jskswamy/aide/internal/secrets"
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
}

func initCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:          "init",
		Short:        "Initialize aide configuration",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			configPath := config.ConfigFilePath()

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

				yamlContent += fmt.Sprintf("secrets_file: %s.enc.yaml\n", secretsName)

				// Create the secrets file
				secretsDir := config.SecretsDir()
				mgr := secrets.NewManager(secretsDir, os.TempDir())
				if err := mgr.Create(secretsName, secretsDir, ageKey); err != nil {
					return fmt.Errorf("creating secrets: %w", err)
				}
				fmt.Fprintf(out, "Created secrets/%s.enc.yaml\n", secretsName)
			}

			// Ensure config directory exists
			configDir := config.ConfigDir()
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
	var resolve bool

	cmd := &cobra.Command{
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

			if resolve {
				agentPath, lookErr := exec.LookPath(resolved.Context.Agent)
				if lookErr != nil {
					fmt.Fprintf(out, "Agent:    %s (not found)\n", resolved.Context.Agent)
				} else {
					fmt.Fprintf(out, "Agent:    %s (-> %s)\n", resolved.Context.Agent, agentPath)
				}
			} else {
				fmt.Fprintf(out, "Agent:    %s\n", resolved.Context.Agent)
			}

			var secretKeys []string
			var secretMap map[string]string
			if resolve && resolved.Context.SecretsFile != "" {
				filePath := config.ResolveSecretsFilePath(resolved.Context.SecretsFile)
				identity, idErr := secrets.DiscoverAgeKey()
				if idErr == nil {
					data, decErr := secrets.DecryptSecretsFile(filePath, identity)
					if decErr == nil {
						secretMap = data
						for k := range data {
							secretKeys = append(secretKeys, k)
						}
						sort.Strings(secretKeys)
					}
				}
			}

			if resolved.Context.SecretsFile != "" {
				if resolve && len(secretKeys) > 0 {
					fmt.Fprintf(out, "Secrets:  %s (%d keys: %s)\n",
						resolved.Context.SecretsFile, len(secretKeys), strings.Join(secretKeys, ", "))
				} else {
					fmt.Fprintf(out, "Secrets:  %s\n", resolved.Context.SecretsFile)
				}
			}

			if len(resolved.Context.Env) > 0 {
				keys := make([]string, 0, len(resolved.Context.Env))
				for k := range resolved.Context.Env {
					keys = append(keys, k)
				}
				sort.Strings(keys)

				if resolve {
					maxKeyLen := 0
					for _, k := range keys {
						if len(k) > maxKeyLen {
							maxKeyLen = len(k)
						}
					}
					fmt.Fprintln(out, "Env:")
					for _, k := range keys {
						v := resolved.Context.Env[k]
						source, secretKey := classifyEnvSource(v)
						displayVal := resolveEnvDisplay(v, source, secretKey, secretMap)
						fmt.Fprintf(out, "  %-*s = %s  (%s)\n", maxKeyLen, k, displayVal, source)
					}
				} else {
					fmt.Fprintf(out, "Env:      %s\n", strings.Join(keys, ", "))
				}
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&resolve, "resolve", false, "Show resolved values")
	return cmd
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

func resolveEnvDisplay(val, source, secretKey string, secretMap map[string]string) string {
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

			var errors []string
			var warnings []string
			secretsFiles := make(map[string]bool)

			for ctxName, ctx := range cfg.Contexts {
				if ctx.Agent != "" && len(cfg.Agents) > 0 {
					if _, ok := cfg.Agents[ctx.Agent]; !ok {
						errors = append(errors, fmt.Sprintf(
							"context %q references unknown agent %q", ctxName, ctx.Agent,
						))
					}
				}

				if ctx.SecretsFile != "" {
					secretsFiles[ctx.SecretsFile] = true
					path := config.ResolveSecretsFilePath(ctx.SecretsFile)
					if _, err := os.Stat(path); os.IsNotExist(err) {
						errors = append(errors, fmt.Sprintf(
							"context %q references secrets file %q which does not exist", ctxName, ctx.SecretsFile,
						))
					}
				}

				if ctx.Sandbox != nil {
					if err := sandbox.ValidateSandboxConfig(ctx.Sandbox); err != nil {
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
						} else if strings.Contains(envVal, ".secrets.") && ctx.SecretsFile == "" {
							errors = append(errors, fmt.Sprintf(
								"context %q env var %q references secrets but no secrets_file is configured", ctxName, envKey,
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
				fmt.Fprintf(out, "OK (%d contexts, %d agents, %d secrets files)\n",
					len(cfg.Contexts), len(cfg.Agents), len(secretsFiles))
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
			filePath := config.ResolveSecretsFilePath(name + ".enc.yaml")
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
			filePath := config.ResolveSecretsFilePath(name + ".enc.yaml")

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
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath := config.ConfigFilePath()
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
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath := config.ConfigFilePath()
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
			if _, err := config.Load(config.ConfigDir(), cwd); err != nil {
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
			cfg, loadErr := config.Load(config.ConfigDir(), cwd)
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
			cfg, err := config.Load(config.ConfigDir(), cwd)
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
			cfg, err := config.Load(config.ConfigDir(), cwd)
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
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				cwd = "."
			}

			out := cmd.OutOrStdout()
			cfg, _ := config.Load(config.ConfigDir(), cwd)

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
	var secretsFile string

	cmd := &cobra.Command{
		Use:   "use [agent-name]",
		Short: "Bind current directory to an agent or context",
		Long: `Bind current directory (or a glob pattern) to an agent/context in global config.

Examples:
  aide use claude                       # Bind CWD to claude
  aide use claude --match "~/work/*"    # Bind a glob pattern
  aide use --context myproject          # Add CWD match to existing context
  aide use claude --secrets personal    # Also set secrets_file`,
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

			cfg, err := config.Load(config.ConfigDir(), cwd)
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
				cfg.Contexts[contextName] = ctx

				if err := config.WriteConfig(cfg); err != nil {
					return fmt.Errorf("writing config: %w", err)
				}

				fmt.Fprintf(out, "Added match rule to context %q:\n", contextName)
				fmt.Fprintf(out, "  path: %s\n", matchPath)
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

			if secretsFile != "" {
				ctx.SecretsFile = secretsFile + ".enc.yaml"
				// Validate secrets file exists
				resolvedPath := config.ResolveSecretsFilePath(ctx.SecretsFile)
				if _, err := os.Stat(resolvedPath); os.IsNotExist(err) {
					fmt.Fprintf(out, "Warning: %s does not exist yet.\n", ctx.SecretsFile)
					fmt.Fprintf(out, "Create it with: aide secrets create %s --age-key <key>\n\n", secretsFile)
				}
			}
			cfg.Contexts[ctxName] = ctx

			if _, ok := cfg.Agents[agentName]; !ok {
				cfg.Agents[agentName] = config.AgentDef{Binary: agentName}
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
			if secretsFile != "" {
				fmt.Fprintf(out, "  secrets_file: %s.enc.yaml\n", secretsFile)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&matchPattern, "match", "", "Glob pattern to match instead of CWD")
	cmd.Flags().StringVar(&contextName, "context", "", "Add match rule to an existing context")
	cmd.Flags().StringVar(&secretsFile, "secrets", "", "Secrets file name (without .enc.yaml suffix)")
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
	return cmd
}

func contextListCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "list",
		Short:        "List all configured contexts",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				cwd = "."
			}
			cfg, err := config.Load(config.ConfigDir(), cwd)
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
				fmt.Fprintln(out, name)
				fmt.Fprintf(out, "  Agent:    %s\n", ctx.Agent)
				if ctx.SecretsFile != "" {
					fmt.Fprintf(out, "  Secrets:  %s\n", ctx.SecretsFile)
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
		RunE: func(cmd *cobra.Command, args []string) error {
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

			fmt.Fprint(out, "Secrets file (optional, press enter to skip): ")
			secretsInput, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("reading secrets file: %w", err)
			}
			secretsInput = strings.TrimSpace(secretsInput)

			cfg, loadErr := config.Load(config.ConfigDir(), cwd)
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
				newCtx.SecretsFile = secretsInput
			}
			cfg.Contexts[name] = newCtx

			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}
			fmt.Fprintf(out, "\nAdded context %q\n", name)
			return nil
		},
	}
}

func contextAddMatchCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "add-match <context-name>",
		Short:        "Add a match rule to an existing context",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			out := cmd.OutOrStdout()
			reader := bufio.NewReader(os.Stdin)

			cwd, err := os.Getwd()
			if err != nil {
				cwd = "."
			}
			cfg, err := config.Load(config.ConfigDir(), cwd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			ctx, ok := cfg.Contexts[name]
			if !ok {
				return fmt.Errorf("context %q not found", name)
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
			cfg, err := config.Load(config.ConfigDir(), cwd)
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
			cfg, err := config.Load(config.ConfigDir(), cwd)
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

func envCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Manage environment variables for contexts",
	}
	cmd.AddCommand(envSetCmd())
	cmd.AddCommand(envListCmd())
	return cmd
}

func envSetCmd() *cobra.Command {
	var fromSecret string
	var contextFlag string

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
			cfg, err := config.Load(config.ConfigDir(), cwd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			var targetName string
			if contextFlag != "" {
				targetName = contextFlag
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
				// Auto-detect secrets_file if missing
				if ctx.SecretsFile == "" {
					selected, err := selectSecretsFile(out, reader, config.SecretsDir())
					if err != nil {
						return err
					}
					ctx.SecretsFile = selected
					fmt.Fprintf(out, "Set secrets_file=%q on context %q.\n", selected, targetName)
				}

				var secretKey string
				if isInteractive {
					secretsFilePath := config.ResolveSecretsFilePath(ctx.SecretsFile)
					picked, err := selectSecretKey(out, reader, secretsFilePath)
					if err != nil {
						return err
					}
					secretKey = picked
				} else {
					secretKey = fromSecret
					secretsFilePath := config.ResolveSecretsFilePath(ctx.SecretsFile)
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
							secretKey, ctx.SecretsFile, strings.Join(available, ", "))
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
	cmd.Flags().StringVar(&contextFlag, "context", "", "Target context name (default: CWD-matched)")
	return cmd
}

func selectSecretsFile(out io.Writer, reader *bufio.Reader, secretsDir string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(secretsDir, "*.enc.yaml"))
	if err != nil {
		return "", fmt.Errorf("scanning secrets directory: %w", err)
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no secrets files found.\nCreate one with: aide secrets create <name> --age-key <key>")
	}

	names := make([]string, len(matches))
	for i, m := range matches {
		names[i] = filepath.Base(m)
	}
	sort.Strings(names)

	if len(names) == 1 {
		fmt.Fprintf(out, "Auto-selected secrets file: %s\n", names[0])
		return names[0], nil
	}

	fmt.Fprintln(out, "Available secrets files:")
	for i, name := range names {
		fmt.Fprintf(out, "  [%d] %s\n", i+1, name)
	}
	fmt.Fprint(out, "Select secrets file [1]: ")
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
			path = cwd + "/*"
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
			path = strings.TrimRight(path, "/") + "/*"
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
	var contextFlag string

	cmd := &cobra.Command{
		Use:          "list",
		Short:        "List environment variables for a context",
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

			var targetName string
			var envMap map[string]string
			if contextFlag != "" {
				targetName = contextFlag
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

	cmd.Flags().StringVar(&contextFlag, "context", "", "Context name (default: CWD-matched)")
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
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			reader := bufio.NewReader(os.Stdin)

			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}

			fmt.Fprintf(out, "\nSetting up aide for %s\n", cwd)

			cfg, _ := config.Load(config.ConfigDir(), cwd)
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

					// Let user override secrets file
					newSecrets := parentCtx.SecretsFile
					if parentCtx.SecretsFile != "" {
						secretsPrompt := fmt.Sprintf("  Secrets file [%s]: ", parentCtx.SecretsFile)
						fmt.Fprint(out, secretsPrompt)
						secretsInput, err := reader.ReadString('\n')
						if err != nil {
							return fmt.Errorf("reading secrets file: %w", err)
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
						newCtx.SecretsFile = newSecrets
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

	var selectedSecretsFile string

	if len(matches) > 0 {
		fmt.Fprintln(out, "  Available secrets files:")
		secretsBaseNames := make([]string, len(matches))
		for i, m := range matches {
			baseName := filepath.Base(m)
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

		switch {
		case choice == skipIdx:
			fmt.Fprintln(out, "  Skipping secrets.")
		case choice == createIdx:
			sf, err := setupCreateSecrets(out, reader)
			if err != nil {
				return err
			}
			selectedSecretsFile = sf
		default:
			selectedSecretsFile = secretsBaseNames[choice-1]
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
			selectedSecretsFile = sf
		} else {
			fmt.Fprintln(out, "  Skipping secrets.")
		}
	}

	// Step 3: Env wiring
	envMap := make(map[string]string)

	if selectedSecretsFile != "" {
		fmt.Fprintln(out, "\nStep 3: Environment variables")

		secretsFilePath := config.ResolveSecretsFilePath(selectedSecretsFile)
		identity, idErr := secrets.DiscoverAgeKey()
		if idErr != nil {
			fmt.Fprintln(out, "  Could not discover age key; skipping env wiring.")
		} else {
			data, decErr := secrets.DecryptSecretsFile(secretsFilePath, identity)
			if decErr != nil {
				fmt.Fprintf(out, "  Could not decrypt: %s\n  Skipping env wiring.\n", decErr)
			} else if len(data) == 0 {
				fmt.Fprintln(out, "  Secrets file has no keys; skipping env wiring.")
			} else {
				keys := make([]string, 0, len(data))
				for k := range data {
					keys = append(keys, k)
				}
				sort.Strings(keys)

				fmt.Fprintf(out, "  Keys in %s:\n", selectedSecretsFile)
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

	// Step 4: Write config
	ctxName := filepath.Base(cwd)
	ctx := config.Context{
		Agent: agentName,
		Match: []config.MatchRule{{Path: cwd}},
	}
	if selectedSecretsFile != "" {
		ctx.SecretsFile = selectedSecretsFile
	}
	if len(envMap) > 0 {
		ctx.Env = envMap
	}
	cfg.Contexts[ctxName] = ctx

	if _, ok := cfg.Agents[agentName]; !ok {
		cfg.Agents[agentName] = config.AgentDef{Binary: agentName}
	}

	if err := config.WriteConfig(cfg); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	fmt.Fprintf(out, "\nCreated context %q:\n", ctxName)
	fmt.Fprintf(out, "  Agent:    %s\n", agentName)
	if selectedSecretsFile != "" {
		fmt.Fprintf(out, "  Secrets:  %s\n", selectedSecretsFile)
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

	fileName := name + ".enc.yaml"
	fmt.Fprintf(out, "  Created secrets/%s\n", fileName)
	return fileName, nil
}

