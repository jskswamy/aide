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

	"github.com/jskswamy/aide/internal/capability"
	"github.com/jskswamy/aide/internal/config"
	aidectx "github.com/jskswamy/aide/internal/context"
	"github.com/jskswamy/aide/internal/display"
	"github.com/jskswamy/aide/internal/launcher"
	"github.com/jskswamy/aide/internal/sandbox"
	"github.com/jskswamy/aide/internal/secrets"
	"github.com/jskswamy/aide/internal/ui"
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
	rootCmd.AddCommand(trustCmd())
	rootCmd.AddCommand(denyCmd())
	rootCmd.AddCommand(untrustCmd())
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
				if err := os.WriteFile(bakPath, data, 0o600); err != nil {
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
			if err := os.MkdirAll(configDir, 0o750); err != nil {
				return fmt.Errorf("creating config directory: %w", err)
			}

			if err := os.WriteFile(configPath, []byte(yamlContent), 0o600); err != nil {
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
					source, _ := display.ClassifyEnvSource(v)
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
						_, secretKey := display.ClassifyEnvSource(v)
						displayVal := display.ResolveEnvDisplay(v, "", secretKey, secretMap)
						data.EnvResolved[k] = display.RedactValue(displayVal)
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
						Network: display.NetworkDisplayName(string(policy.Network)),
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

// resolveProjectOverrideForMutation loads the global config and project override
// for mutation. Returns the global config (for validation), the project override
// (empty if .aide.yaml doesn't exist), and the path to write .aide.yaml to.
func resolveProjectOverrideForMutation() (*config.Config, *config.ProjectOverride, string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, nil, "", fmt.Errorf("getting working directory: %w", err)
	}
	cfg, err := config.Load(config.Dir(), cwd)
	if err != nil {
		return nil, nil, "", fmt.Errorf("loading config: %w", err)
	}
	poPath := config.FindProjectConfigForWrite(cwd)
	po := cfg.ProjectOverride
	if po == nil {
		po = &config.ProjectOverride{}
	}
	return cfg, po, poPath, nil
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
