// Package main provides the aide CLI commands.
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/jskswamy/aide/internal/capability"
	"github.com/jskswamy/aide/internal/config"
	aidectx "github.com/jskswamy/aide/internal/context"
	"github.com/jskswamy/aide/internal/display"
	"github.com/jskswamy/aide/internal/fsutil"
	"github.com/jskswamy/aide/internal/harness"
	"github.com/jskswamy/aide/internal/launcher"
	"github.com/jskswamy/aide/internal/output"
	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/sandbox"
	"github.com/jskswamy/aide/internal/secrets"
	"github.com/jskswamy/aide/internal/ui"
	"github.com/spf13/cobra"
)

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
				if err := fsutil.AtomicWrite(bakPath, data); err != nil {
					return fmt.Errorf("writing backup: %w", err)
				}
				fmt.Fprintf(out, "Backed up existing config to %s\n\n", bakPath)
			}

			fmt.Fprintln(out, "Welcome to aide! Let's set up your configuration.")
			fmt.Fprintln(out)

			reader := bufio.NewReader(cmd.InOrStdin())

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

			var secretsName string

			// Secrets setup (optional)
			fmt.Fprint(out, "Set up secrets? (y/N): ")
			answer, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("reading response: %w", err)
			}
			answer = strings.TrimSpace(strings.ToLower(answer))

			if answer == "y" || answer == "yes" {
				fmt.Fprint(out, "Secrets file name (default: default): ")
				secretsInput, err := reader.ReadString('\n')
				if err != nil {
					return fmt.Errorf("reading secrets name: %w", err)
				}
				secretsName = strings.TrimSpace(secretsInput)
				if secretsName == "" {
					secretsName = "default"
				}

				ageKey, err := secrets.ResolveRecipient("", reader, out, isInputTTY(cmd))
				if err != nil {
					return err
				}

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

			defaultCtx := config.Context{Agent: agentName}
			if secretsName != "" {
				defaultCtx.Secret = secretsName
			}
			agentsMap := make(map[string]config.AgentDef, len(result.Found))
			for name := range result.Found {
				agentsMap[name] = config.AgentDef{Binary: name}
			}
			if len(agentsMap) == 0 {
				agentsMap[agentName] = config.AgentDef{Binary: agentName}
			}
			cfg := &config.Config{
				ProfileSchema:  true,
				DefaultContext: "default",
				Agents:         agentsMap,
				Contexts: map[string]config.Context{
					"default": defaultCtx,
				},
			}
			if err := config.WriteConfigTo(cfg, configPath); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}

			// Show generated config
			profilePath := filepath.Join(filepath.Dir(configPath), "profiles", "default.yaml")
			fmt.Fprintln(out)
			fmt.Fprintf(out, "Created %s and %s\n", configPath, profilePath)

			// Next steps
			fmt.Fprintln(out)
			fmt.Fprintln(out, "Next steps:")
			fmt.Fprintf(out, "  aide                     Launch %s\n", agentName)
			fmt.Fprintln(out, "  aide profile bind <name> Attach this folder to an existing profile")
			fmt.Fprintln(out, "  aide secrets create      Set up encrypted secrets")
			fmt.Fprintln(out, "  aide agents add <name>   Register another agent")

			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing config (creates .bak backup)")

	return cmd
}

func whichCmd() *cobra.Command {
	var sf selectionFlags
	cmd := &cobra.Command{
		Use:          "which",
		Short:        "Show which profile matches the current directory",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			resolve, _ := cmd.Flags().GetBool("resolve")

			env, err := cmdEnv(cmd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			cwd := env.CWD()
			cfg := env.Config()

			homeDir, _ := os.UserHomeDir()
			preview, err := previewSelection(cmd, cfg, cwd, homeDir, &sf)
			if err != nil {
				return err
			}
			resolved, inputs, sel, notices := preview.RC, preview.Inputs, preview.Sel, preview.Notices

			out := cmd.OutOrStdout()
			prefs := resolved.Preferences

			// Credentials by binding and env var name; translated legacy
			// env: credentials leave the env listing.
			credCtx, _, credPlan, credErr := launcher.PlanProfileCredentials(cfg, resolved, inputs, &sel)
			resolved.Context = credCtx

			// Build BannerData
			agentName := resolved.Context.Agent
			agentBinary := agentName
			if agentDef, ok := cfg.Agents[agentName]; ok && agentDef.Binary != "" {
				agentBinary = agentDef.Binary
			} else if d, ok := harness.Lookup(agentName); ok && d.Binary != "" {
				agentBinary = d.Binary
			}
			agentPath := agentBinary
			if !filepath.IsAbs(agentPath) {
				if p, lookErr := exec.LookPath(agentBinary); lookErr == nil {
					agentPath = p
				} else {
					agentPath = ""
				}
			}

			data := &ui.BannerData{
				ContextName: resolved.Name,
				MatchReason: resolved.MatchReason,
				AgentName:   agentName,
				AgentPath:   agentPath,
				SecretName:  resolved.Context.Secret,
			}

			launcher.SetBannerCredentials(data, credPlan)
			launcher.SetBannerSelection(data, sel)
			launcher.EnrichBannerHarnessesAndGateways(data, cfg, resolved, agentName, homeDir, exec.LookPath)
			preview.Gate.SetBanner(data)
			data.Warnings = append(data.Warnings, notices...)
			if credErr != nil {
				data.Warnings = append(data.Warnings, credErr.Error())
			}

			// Add context and agent icons
			data.ContextIcon = resolved.Context.Icon
			if agentDef, ok := cfg.Agents[agentName]; ok {
				data.AgentIcon = launcher.ResolveAgentIcon(agentName, &agentDef)
			} else {
				data.AgentIcon = launcher.ResolveAgentIcon(agentName, nil)
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
			}

			// Resolve capabilities & merge into sandbox policy (non-interactive preview)
			projectRoot := aidectx.ProjectRoot(cwd)
			capNames := sandbox.MergeCapNames(resolved.Context.Capabilities, nil, nil)
			var excludedCaps []string
			if cfg.ProjectOverride != nil {
				excludedCaps = append(excludedCaps, cfg.ProjectOverride.DisabledCapabilities...)
			}
			capNames = capability.AutoInclude(capNames, excludedCaps, homeDir, os.Getenv)
			contextCapSet := make(map[string]bool, len(resolved.Context.Capabilities))
			for _, c := range resolved.Context.Capabilities {
				contextCapSet[c] = true
			}

			resolvedSandbox, sbDisabled, _ := sandbox.ResolveSandboxRef(resolved.Context.Sandbox, cfg.Sandboxes)
			var configWritableExtra, configReadableExtra, configDeniedExtra []string
			if resolvedSandbox != nil {
				configWritableExtra = append([]string{}, resolvedSandbox.WritableExtra...)
				configReadableExtra = append([]string{}, resolvedSandbox.ReadableExtra...)
				configDeniedExtra = append([]string{}, resolvedSandbox.DeniedExtra...)
			}

			var yamlPins map[string][]string
			if cfg.ProjectOverride != nil && len(cfg.ProjectOverride.CapabilityVariants) > 0 {
				yamlPins = make(map[string][]string, len(cfg.ProjectOverride.CapabilityVariants))
				for k, v := range cfg.ProjectOverride.CapabilityVariants {
					yamlPins[k] = append([]string(nil), v...)
				}
			}
			variantOpts := sandbox.VariantSelectionOptions{
				ProjectRoot: projectRoot,
				FS:          os.DirFS(projectRoot),
				YAMLPins:    yamlPins,
				Interactive: false,
			}
			resolvedCapSet, capOverrides, capProvenance, _ := sandbox.ResolveCapabilitiesWithVariants(capNames, cfg, variantOpts)
			sandbox.ApplyOverrides(&resolvedSandbox, capOverrides)

			// Build unified env items
			var resolvedValues map[string]string
			if resolve && len(resolved.Context.Env) > 0 {
				resolvedValues = make(map[string]string, len(resolved.Context.Env))
				for k, v := range resolved.Context.Env {
					_, secretKey := display.ClassifyEnvSource(v)
					resolvedValues[k] = display.ResolveEnvDisplay(v, "", secretKey, secretMap)
				}
			}
			var neverAllowEnv []string
			var resolvedCaps []capability.ResolvedCapability
			if resolvedCapSet != nil {
				neverAllowEnv = resolvedCapSet.NeverAllowEnv
				resolvedCaps = resolvedCapSet.Capabilities
			}
			data.EnvItems = launcher.BuildEnvItems(resolved.Context.Env, resolvedCaps, neverAllowEnv, resolvedValues)

			// Populate capability display data
			if resolvedCapSet != nil && len(resolvedCapSet.Capabilities) > 0 {
				for _, rc := range resolvedCapSet.Capabilities {
					source := "--with"
					if contextCapSet[rc.Name] {
						source = "context config"
					}
					prov := capProvenance[rc.Name]
					data.Capabilities = append(data.Capabilities, ui.CapabilityDisplay{
						Name:            rc.Name,
						ReadablePaths:   rc.Readable,
						WritablePaths:   rc.Writable,
						EnvVars:         rc.EnvAllow,
						Source:          source,
						Variants:        prov.Variants,
						ProvenanceTag:   ui.ProvenanceTag(prov.Reason),
						FreshGrant:      prov.Reason == "consent:granted",
						EvidenceSummary: prov.EvidenceSummary,
					})
				}
				data.NeverAllow = cfg.NeverAllow
				data.CompWarnings = capability.CompositionWarnings(resolvedCapSet.Capabilities)
			}

			// Project detection: show suggested capabilities not yet enabled
			if suggestions := capability.DetectProject(os.DirFS(projectRoot)); len(suggestions) > 0 {
				enabled := make(map[string]bool, len(data.Capabilities))
				for _, c := range data.Capabilities {
					enabled[c.Name] = true
				}
				registry := capability.MergedRegistry(nil)
				for _, name := range suggestions {
					if enabled[name] {
						continue
					}
					if capDef, ok := registry[name]; ok {
						data.SuggestedCaps = append(data.SuggestedCaps, ui.CapabilityDisplay{
							Name:          name,
							ReadablePaths: capDef.Readable,
							WritablePaths: capDef.Writable,
							Suggested:     true,
							Source:        "detected",
						})
					}
				}
			}

			// Build sandbox info
			if sbDisabled {
				data.Sandbox = &ui.SandboxInfo{Disabled: true}
			} else {
				tempDir := os.TempDir()
				policy, pathWarnings, policyErr := sandbox.PolicyFromConfig(resolvedSandbox, sandbox.Paths{ProjectRoot: projectRoot, RuntimeDir: "/tmp/aide-preview", HomeDir: homeDir, TempDir: tempDir})
				if policyErr == nil && policy != nil {
					tier := sandbox.PlatformIsolationTier(*policy)
					data.IsolationTier = &tier
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
						si.Hints = append(si.Hints, g.Hints...)
						if len(g.Rules) > 0 {
							display := ui.GuardDisplay{
								Name:      g.Name,
								Protected: g.Protected,
								Allowed:   g.Allowed,
								Readable:  g.Readable,
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

			// Populate extra paths that are from config (not capabilities)
			data.ExtraWritable = slices.DeleteFunc(configWritableExtra, func(s string) bool {
				return slices.Contains(capOverrides.WritableExtra, s)
			})
			data.ExtraReadable = slices.DeleteFunc(configReadableExtra, func(s string) bool {
				return slices.Contains(capOverrides.ReadableExtra, s)
			})
			data.ExtraDenied = slices.DeleteFunc(configDeniedExtra, func(s string) bool {
				return slices.Contains(capOverrides.DeniedExtra, s)
			})

			// Provisioning drift hint: compare config hash against
			// last-sync state. Errors here are best-effort — never
			// fail launch over a drift check.
			if homeDir != "" {
				cfgPath := config.FilePath()
				statePath := provision.DefaultStatePath(homeDir)
				// Resolve agentDir for hook template expansion in DriftStatus.
				driftAgentDir := ""
				if prov, ok := provision.ProvisionerFor(resolved.Context.Agent); ok {
					pCtx := provision.Context{HomeDir: homeDir, Env: resolved.Context.Env}
					driftAgentDir = provision.ResolveAgentDir(prov, pCtx)
				}
				if d, err := provision.DriftStatus(cfg, cfgPath, statePath, resolved.Name, driftAgentDir, homeDir); err == nil {
					if msg := provision.DriftMessage(d, resolved.Name); msg != "" {
						data.Warnings = append(data.Warnings, msg)
					}
				}
			}

			// aide which always renders regardless of show_info
			format, ferr := output.FromCmd(cmd)
			if ferr != nil {
				return ferr
			}
			style := effectiveBannerStyle(prefs.InfoStyle, isInteractiveTerminal(os.Stdout), os.Getenv("AIDE_INFO_STYLE"))
			return output.Emit(out, format, data, func(w io.Writer) error {
				if err := ui.RenderBanner(w, style, data); err != nil {
					return fmt.Errorf("rendering banner: %w", err)
				}
				return nil
			})
		},
	}
	sf.register(cmd, agentFlagUsage)
	return cmd
}

const agentFlagUsage = "Report as if launched with --agent (agent and profile directory)"

func useCmd() *cobra.Command {
	var matchPattern string
	var contextName string
	var secretFlag string
	var sandboxProfile string

	cmd := &cobra.Command{
		Use:    "use [agent-name]",
		Short:  "Bind current directory to an agent or profile",
		Hidden: true,
		Long: `Bind current directory (or a glob pattern) to an agent or profile in global config.

Examples:
  aide use claude                       # Bind CWD to claude
  aide use claude --match "~/work/*"    # Bind a glob pattern
  aide use claude --secret personal     # Also set secret
  aide use claude --sandbox strict      # Use a named sandbox profile`,
		SilenceUsage: true,
		Args:         cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.ErrOrStderr(), "aide use is deprecated; use 'aide profile bind' to attach a folder to a profile, or 'aide project set --agent <name>' to set this repository's agent")
			agentName := ""
			if len(args) > 0 {
				agentName = args[0]
			}

			if agentName == "" && contextName == "" {
				return fmt.Errorf("either an agent name or --profile is required")
			}

			env, _ := cmdEnv(cmd)
			cwd := env.CWD()
			if cwd == "" {
				return fmt.Errorf("getting working directory")
			}

			matchPath := cwd
			if matchPattern != "" {
				matchPath = matchPattern
			}

			cfg := env.Config()
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
					return fmt.Errorf("profile %q not found in config", contextName)
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

				if err := saveConfigWithMigrationPrompt(cmd, cfg); err != nil {
					return fmt.Errorf("writing config: %w", err)
				}

				fmt.Fprintf(out, "Added match rule to profile %q:\n", contextName)
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

			if err := saveConfigWithMigrationPrompt(cmd, cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}

			if exists {
				fmt.Fprintf(out, "Updated profile %q:\n", ctxName)
			} else {
				fmt.Fprintf(out, "Created profile %q:\n", ctxName)
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
	cmd.Flags().StringVar(&contextName, "context", "", "Add match rule to an existing profile")
	addProfileFlag(cmd, &contextName)
	cmd.Flags().StringVar(&secretFlag, "secret", "", "Secret name (e.g. work)")
	cmd.Flags().StringVar(&sandboxProfile, "sandbox", "", "Sandbox profile name (e.g. strict, none, default)")
	return cmd
}

func setupCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "setup",
		Short:        "Deprecated: use 'aide profile bind' or 'aide profile create'",
		Hidden:       true,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintln(cmd.ErrOrStderr(), "aide setup is deprecated; use 'aide profile bind' to attach a folder to an existing profile or 'aide profile create' to create a new one")
			env, err := cmdEnv(cmd)
			if err != nil {
				return err
			}
			var target *cobra.Command
			if len(env.Config().Contexts) > 0 {
				target = contextBindCmd()
			} else {
				target = contextCreateCmd()
			}
			target.SetIn(cmd.InOrStdin())
			target.SetOut(cmd.OutOrStdout())
			target.SetErr(cmd.ErrOrStderr())
			target.SetArgs([]string{})
			return target.Execute()
		},
	}
}

type statusCapEntry struct {
	Name     string   `json:"name"`
	Extends  []string `json:"extends,omitempty"`
	Readable []string `json:"readable,omitempty"`
	Writable []string `json:"writable,omitempty"`
	Deny     []string `json:"deny,omitempty"`
	EnvAllow []string `json:"env_allow,omitempty"`
}

type statusResult struct {
	Context             string           `json:"context"`
	Agent               string           `json:"agent"`
	AgentPath           string           `json:"agent_path,omitempty"`
	Matched             string           `json:"matched,omitempty"`
	Secret              string           `json:"secret,omitempty"`
	SecretKeyCount      int              `json:"secret_key_count,omitempty"`
	Capabilities        []statusCapEntry `json:"capabilities,omitempty"`
	NeverAllow          []string         `json:"never_allow,omitempty"`
	CredentialWarnings  []string         `json:"credential_warnings,omitempty"`
	CompositionWarnings []string         `json:"composition_warnings,omitempty"`
	Network             string           `json:"network"`
	SandboxTier         string           `json:"sandbox_tier"`
	SandboxGuardCount   int              `json:"sandbox_guard_count"`
	AutoApprove         bool             `json:"auto_approve"`
	// Credentials names the bindings a launch injects (never values).
	Credentials     []ui.CredentialInfo `json:"credentials,omitempty"`
	AuthMode        string              `json:"auth_mode,omitempty"`
	CredentialError string              `json:"credential_error,omitempty"`
	// Selection sources and the selected credential names (never
	// values); see ui.BannerData.
	ProfileSource       string   `json:"profile_source,omitempty"`
	HarnessSource       string   `json:"harness_source,omitempty"`
	CredentialSource    string   `json:"credential_source,omitempty"`
	CredentialSelection []string `json:"credential_selection,omitempty"`
	Notices             []string `json:"notices,omitempty"`
	// ProjectTrust lists each untrusted or denied .aide.yaml (it
	// contributes nothing); InheritedGrants the security-sensitive
	// grants a linked worktree takes from its main checkout's file.
	ProjectTrust    []ui.TrustInfo      `json:"project_trust,omitempty"`
	InheritedGrants []ui.InheritedGrant `json:"inherited_grants,omitempty"`
}

func statusCmd() *cobra.Command {
	var sf selectionFlags
	cmd := &cobra.Command{
		Use:          "status",
		Short:        "Show detailed view of current profile and capabilities",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			env, err := cmdEnv(cmd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			cwd := env.CWD()
			cfg := env.Config()

			homeDir, _ := os.UserHomeDir()
			preview, err := previewSelection(cmd, cfg, cwd, homeDir, &sf)
			if err != nil {
				return err
			}
			resolved, inputs, sel, notices := preview.RC, preview.Inputs, preview.Sel, preview.Notices

			var credData ui.BannerData
			credCtx, _, credPlan, credErr := launcher.PlanProfileCredentials(cfg, resolved, inputs, &sel)
			resolved.Context = credCtx
			launcher.SetBannerCredentials(&credData, credPlan)
			launcher.SetBannerSelection(&credData, sel)
			credError := ""
			if credErr != nil {
				credError = credErr.Error()
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
			registry := env.Registry()

			var disabledCaps []string
			if cfg.ProjectOverride != nil {
				disabledCaps = cfg.ProjectOverride.DisabledCapabilities
			}
			capNames := capability.AutoInclude(resolved.Context.Capabilities, disabledCaps, homeDir, os.Getenv)
			var capSet *capability.Set
			if len(capNames) > 0 {
				capSet, err = capability.ResolveAll(capNames, registry, cfg.NeverAllow, cfg.NeverAllowEnv)
				if err != nil {
					return fmt.Errorf("resolving capabilities: %w", err)
				}
				sandbox.AddSkillsharePaths(capSet.Capabilities, cfg)
			}

			// Resolve sandbox policy for rule count
			sandboxPolicy, sandboxDisabled, _ := sandbox.ResolveSandboxRef(
				resolved.Context.Sandbox, cfg.Sandboxes,
			)
			var guardCount int
			if !sandboxDisabled {
				homeDir, _ := os.UserHomeDir()
				tempDir := os.TempDir()
				pol, _, _ := sandbox.PolicyFromConfig(sandboxPolicy, sandbox.Paths{ProjectRoot: cwd, HomeDir: homeDir, TempDir: tempDir})
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

			var capEntries []statusCapEntry
			if capSet != nil {
				capEntries = make([]statusCapEntry, 0, len(capSet.Capabilities))
				for _, cap := range capSet.Capabilities {
					entry := statusCapEntry{
						Name:     cap.Name,
						Readable: cap.Readable,
						Writable: cap.Writable,
						Deny:     cap.Deny,
						EnvAllow: cap.EnvAllow,
					}
					if len(cap.Sources) > 1 {
						entry.Extends = cap.Sources[1:]
					}
					capEntries = append(capEntries, entry)
				}
			}

			neverAllow := cfg.NeverAllow
			if capSet != nil {
				neverAllow = capSet.NeverAllow
			}

			var credWarnings, compWarnings []string
			if capSet != nil && len(capSet.Capabilities) > 0 {
				var allEnvAllow []string
				for _, cap := range capSet.Capabilities {
					allEnvAllow = append(allEnvAllow, cap.EnvAllow...)
				}
				credWarnings = capability.CredentialWarnings(allEnvAllow)
				compWarnings = capability.CompositionWarnings(capSet.Capabilities)
			}

			var sandboxTierLine string
			if sandboxDisabled {
				sandboxTierLine = "disabled"
			} else {
				homeDir, _ := os.UserHomeDir()
				tempDir := os.TempDir()
				pol, _, polErr := sandbox.PolicyFromConfig(sandboxPolicy, sandbox.Paths{ProjectRoot: cwd, HomeDir: homeDir, TempDir: tempDir})
				switch {
				case polErr != nil:
					sandboxTierLine = fmt.Sprintf("policy load failed: %v", polErr)
				case pol == nil:
					sandboxTierLine = "policy unavailable"
				default:
					tier := sandbox.PlatformIsolationTier(*pol)
					sandboxTierLine = formatIsolationTierStatus(tier)
				}
			}

			result := statusResult{
				Context:             resolved.Name,
				Agent:               agentName,
				AgentPath:           agentPath,
				Matched:             resolved.MatchReason,
				Secret:              secretName,
				SecretKeyCount:      secretKeyCount,
				Capabilities:        capEntries,
				NeverAllow:          neverAllow,
				CredentialWarnings:  credWarnings,
				CompositionWarnings: compWarnings,
				Network:             networkMode,
				SandboxTier:         sandboxTierLine,
				SandboxGuardCount:   guardCount,
				AutoApprove:         autoApprove,
				Credentials:         credData.Credentials,
				AuthMode:            credData.AuthMode,
				CredentialError:     credError,
				ProfileSource:       credData.ProfileSource,
				HarnessSource:       credData.HarnessSource,
				CredentialSource:    credData.CredentialSource,
				CredentialSelection: credData.CredentialSelection,
				Notices:             notices,
				ProjectTrust:        preview.Gate.Files,
				InheritedGrants:     preview.Gate.InheritedGrants,
			}

			format, ferr := output.FromCmd(cmd)
			if ferr != nil {
				return ferr
			}
			return output.Emit(out, format, result, func(w io.Writer) error {
				line := strings.Repeat("\u2500", 40)
				fmt.Fprintln(w, line)

				fmt.Fprintf(w, "Profile:      %s\n", result.Context)
				fmt.Fprintf(w, "Agent:        %s \u2192 %s\n", result.Agent, result.AgentPath)
				fmt.Fprintf(w, "Matched:      %s\n", result.Matched)
				if line := statusSelectionLine(result); line != "" {
					fmt.Fprintf(w, "Selected by:  %s\n", line)
				}
				for _, n := range result.Notices {
					fmt.Fprintf(w, "Note:         %s\n", n)
				}

				if result.Secret != "" {
					if result.SecretKeyCount > 0 {
						fmt.Fprintf(w, "Secret:       %s (%d keys)\n", result.Secret, result.SecretKeyCount)
					} else {
						fmt.Fprintf(w, "Secret:       %s\n", result.Secret)
					}
				}

				if line := statusCredentialLine(result); line != "" {
					fmt.Fprintf(w, "Credentials:  %s\n", line)
				}

				if len(result.Capabilities) > 0 {
					fmt.Fprintln(w)
					fmt.Fprintln(w, "Capabilities:")
					for _, cap := range result.Capabilities {
						label := cap.Name
						if len(cap.Extends) > 0 {
							label += " (extends " + strings.Join(cap.Extends, ", ") + ")"
						}
						fmt.Fprintf(w, "  %s\n", label)
						if len(cap.Readable) > 0 {
							fmt.Fprintf(w, "    readable:  %s\n", strings.Join(cap.Readable, ", "))
						}
						if len(cap.Writable) > 0 {
							fmt.Fprintf(w, "    writable:  %s\n", strings.Join(cap.Writable, ", "))
						}
						if len(cap.Deny) > 0 {
							fmt.Fprintf(w, "    deny:      %s\n", strings.Join(cap.Deny, ", "))
						}
						if len(cap.EnvAllow) > 0 {
							fmt.Fprintf(w, "    env:       %s\n", strings.Join(cap.EnvAllow, ", "))
						}
						fmt.Fprintf(w, "    source:    profile config\n")
						fmt.Fprintln(w)
					}
				}

				if len(result.NeverAllow) > 0 {
					fmt.Fprintln(w, "Never-allow:")
					for _, path := range result.NeverAllow {
						fmt.Fprintf(w, "  %s\n", path)
					}
				}

				if len(result.CredentialWarnings) > 0 {
					fmt.Fprintln(w)
					fmt.Fprintln(w, "Credentials exposed:")
					for _, warn := range result.CredentialWarnings {
						fmt.Fprintf(w, "  \u26a0 %s\n", warn)
					}
				}
				if len(result.CompositionWarnings) > 0 {
					fmt.Fprintln(w)
					for _, warn := range result.CompositionWarnings {
						fmt.Fprintf(w, "\u26a0 %s\n", warn)
					}
				}

				writeStatusProjectTrust(w, result)

				fmt.Fprintln(w)
				fmt.Fprintf(w, "Network: %s\n", result.Network)
				fmt.Fprintf(w, "Sandbox: %s (%d guards)\n", result.SandboxTier, result.SandboxGuardCount)
				if result.AutoApprove {
					fmt.Fprintln(w, "Auto-approve: yes")
				} else {
					fmt.Fprintln(w, "Auto-approve: no")
				}
				fmt.Fprintln(w, line)
				return nil
			})
		},
	}
	sf.register(cmd, agentFlagUsage)
	return cmd
}

// writeStatusProjectTrust prints each untrusted or denied .aide.yaml
// and the grants inherited from a main checkout, labeled by file.
func writeStatusProjectTrust(w io.Writer, r statusResult) {
	for _, f := range r.ProjectTrust {
		origin := ""
		if f.Source == config.LayerInherited {
			origin = " (inherited from main checkout)"
		}
		fmt.Fprintf(w, "\nProject file: %s %s%s, not applied\n", f.Path, f.Status, origin)
	}
	if len(r.InheritedGrants) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Inherited from main checkout:")
		for _, g := range r.InheritedGrants {
			fmt.Fprintf(w, "  %-12s %s  \u2190 %s\n", g.Kind, g.Value, g.Source)
		}
	}
}

// statusSelectionLine says where the profile, harness and credential
// selection came from.
func statusSelectionLine(r statusResult) string {
	var parts []string
	for _, p := range []struct{ what, source string }{
		{"profile", r.ProfileSource},
		{"harness", r.HarnessSource},
		{"credentials", r.CredentialSource},
	} {
		if p.source != "" {
			parts = append(parts, p.what+" from "+p.source)
		}
	}
	return strings.Join(parts, ", ")
}

func formatIsolationTierStatus(tier sandbox.IsolationTier) string {
	switch tier.Tier {
	case sandbox.TierPrimary:
		switch tier.Backend {
		case sandbox.BackendLandlock:
			return fmt.Sprintf("primary (Landlock ABI %d)", tier.KernelABI)
		case sandbox.BackendSeatbelt:
			return "primary (Seatbelt)"
		default:
			return fmt.Sprintf("primary (%s)", tier.Backend)
		}
	case sandbox.TierDegraded:
		return fmt.Sprintf("degraded (%s)", tier.Backend)
	case sandbox.TierUnavailable:
		return "unavailable"
	default:
		return tier.Tier
	}
}

// statusCredentialLine renders the credential bindings for aide status,
// by name and env var name only.
func statusCredentialLine(r statusResult) string {
	if r.CredentialError != "" {
		return "error: " + r.CredentialError
	}
	return ui.CredentialSummary(r.Credentials, r.AuthMode)
}
