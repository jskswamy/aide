package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/jskswamy/aide/internal/config"
	aidectx "github.com/jskswamy/aide/internal/context"
	"github.com/jskswamy/aide/internal/launcher"
	"github.com/jskswamy/aide/internal/output"
)

// contextDeprecation is printed to stderr by every aide context
// subcommand.
const contextDeprecation = "aide context is deprecated; use aide profile (same subcommands)"

// profileCmd manages profiles: the identities (match rules, secret
// store, native config dirs, credentials) aide launches under.
func profileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Manage aide profiles",
	}
	addProfileSubcommands(cmd)
	return cmd
}

// contextCmd is aide context, the deprecated alias of aide profile.
// The notice goes to stderr so --format json output stays parseable.
func contextCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "context",
		Short:  "Deprecated alias of aide profile",
		Hidden: true,
		PersistentPreRun: func(c *cobra.Command, _ []string) {
			fmt.Fprintln(c.ErrOrStderr(), contextDeprecation)
		},
	}
	addProfileSubcommands(cmd)
	return cmd
}

func addProfileSubcommands(cmd *cobra.Command) {
	cmd.AddCommand(contextListCmd())
	cmd.AddCommand(withProfileArgCompletion(contextBindCmd()))
	cmd.AddCommand(contextCreateCmd())
	cmd.AddCommand(withProfileArgCompletion(contextRenameCmd()))
	cmd.AddCommand(withProfileArgCompletion(contextRemoveCmd()))
	cmd.AddCommand(contextSetSecretCmd())
	cmd.AddCommand(contextRemoveSecretCmd())
	cmd.AddCommand(withProfileArgCompletion(contextSetDefaultCmd()))
	cmd.AddCommand(profileMigrateCmd())
}

// withProfileArgCompletion completes an existing profile name as the
// first argument.
func withProfileArgCompletion(cmd *cobra.Command) *cobra.Command {
	cmd.ValidArgsFunction = func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return profileNamesForCompletion(), cobra.ShellCompDirectiveNoFileComp
	}
	return cmd
}

type matchRuleEntry struct {
	Path   string `json:"path,omitempty"`
	Remote string `json:"remote,omitempty"`
}

type contextEntry struct {
	Name    string           `json:"name"`
	Default bool             `json:"default"`
	Agent   string           `json:"agent"`
	Secret  string           `json:"secret,omitempty"`
	Match   []matchRuleEntry `json:"match,omitempty"`
	EnvKeys []string         `json:"env_keys,omitempty"`
}

func contextListCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "list",
		Short:        "List all configured profiles",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			env, err := cmdEnv(cmd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			cfg := env.Config()
			out := cmd.OutOrStdout()

			names := make([]string, 0, len(cfg.Contexts))
			for name := range cfg.Contexts {
				names = append(names, name)
			}
			sort.Strings(names)

			entries := make([]contextEntry, 0, len(names))
			for _, name := range names {
				ctx := cfg.Contexts[name]
				e := contextEntry{
					Name:    name,
					Default: name == cfg.DefaultContext,
					Agent:   ctx.Agent,
					Secret:  ctx.Secret,
				}
				for _, rule := range ctx.Match {
					if rule.Path != "" || rule.Remote != "" {
						e.Match = append(e.Match, matchRuleEntry{Path: rule.Path, Remote: rule.Remote})
					}
				}
				if len(ctx.Env) > 0 {
					keys := make([]string, 0, len(ctx.Env))
					for k := range ctx.Env {
						keys = append(keys, k)
					}
					sort.Strings(keys)
					e.EnvKeys = keys
				}
				entries = append(entries, e)
			}

			format, ferr := output.FromCmd(cmd)
			if ferr != nil {
				return ferr
			}
			return output.Emit(out, format, entries, func(w io.Writer) error {
				if len(entries) == 0 {
					fmt.Fprintln(w, "No profiles configured.")
					return nil
				}
				for i, e := range entries {
					if e.Default {
						fmt.Fprintf(w, "%s (default)\n", e.Name)
					} else {
						fmt.Fprintln(w, e.Name)
					}
					fmt.Fprintf(w, "  Agent:    %s\n", e.Agent)
					if e.Secret != "" {
						fmt.Fprintf(w, "  Secret:   %s\n", e.Secret)
					}
					for _, rule := range e.Match {
						if rule.Path != "" {
							fmt.Fprintf(w, "  Match:    %s\n", rule.Path)
						}
						if rule.Remote != "" {
							fmt.Fprintf(w, "  Match:    %s (remote)\n", rule.Remote)
						}
					}
					if len(e.EnvKeys) > 0 {
						fmt.Fprintf(w, "  Env:      %s\n", strings.Join(e.EnvKeys, ", "))
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

func contextBindCmd() *cobra.Command {
	var (
		forcePath   bool
		forceRemote bool
	)

	cmd := &cobra.Command{
		Use:   "bind [name]",
		Short: "Attach this folder to an existing profile",
		Long: `Attach the current folder to an existing profile.

Examples:
  aide profile bind work               # auto-detect: git remote if repo, else folder path
  aide profile bind work --path        # force exact folder path match
  aide profile bind work --remote      # force git remote match (errors if not a git repo)
  aide profile bind                    # interactive picker over existing profiles`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if forcePath && forceRemote {
				return fmt.Errorf("--path and --remote are mutually exclusive")
			}

			out := cmd.OutOrStdout()
			reader := bufio.NewReader(os.Stdin)

			env, err := cmdEnv(cmd)
			if err != nil {
				return err
			}
			cwd := env.CWD()
			cfg := env.Config()

			var name string
			if len(args) == 1 {
				name = args[0]
			} else {
				picked, err := pickExistingContext(out, reader, cfg)
				if err != nil {
					return err
				}
				name = picked
			}

			ctx, ok := cfg.Contexts[name]
			if !ok {
				// TTY: offer to create. Non-TTY: hard error.
				if isStdinTTY() {
					fmt.Fprintf(out, "Profile %q doesn't exist. Create it now? [y/N]: ", name)
					ans, _ := reader.ReadString('\n')
					if strings.EqualFold(strings.TrimSpace(ans), "y") {
						return runCreateWizard(cmd, name, createOptions{here: tristateYes})
					}
				}
				return fmt.Errorf("profile %q not found.\nRun: aide profile create %s", name, name)
			}

			var rule config.MatchRule
			var desc string
			switch {
			case forceRemote:
				remote := aidectx.CurrentRemote(cwd)
				if remote == "" {
					return fmt.Errorf("--remote requires the current folder to be a git repo with an '%[1]s' remote that has a host (not a git repo, no %[1]s, or a local-path %[1]s)", aidectx.DefaultRemoteName)
				}
				rule = config.MatchRule{Remote: remote}
				desc = fmt.Sprintf("by remote %s", remote)
			case forcePath:
				rule = config.MatchRule{Path: cwd}
				desc = fmt.Sprintf("by path %s", cwd)
			default:
				rule, desc = autoDetectMatchRule(cwd)
			}

			// Dedup: same profile already covers this repo.
			for _, r := range ctx.Match {
				if rulesCollide(r, rule) {
					fmt.Fprintf(out, "This folder is already bound to profile %q.\n", name)
					return nil
				}
			}

			// Cross-profile collision: same repo bound to a different profile.
			if existing := findExistingBind(cfg, name, rule); existing != nil {
				if !isStdinTTY() {
					return fmt.Errorf("folder already bound to profile %q; re-run interactively to move it", existing.ctxName)
				}
				fmt.Fprintf(out, "This folder is already bound to profile %q.\n[a]bort or [m]ove to %q? [a]: ", existing.ctxName, name)
				ans, _ := reader.ReadString('\n')
				if strings.ToLower(strings.TrimSpace(ans)) != "m" {
					return fmt.Errorf("aborted")
				}
				old := cfg.Contexts[existing.ctxName]
				old.Match = removeMatchRule(old.Match, existing.rule)
				cfg.Contexts[existing.ctxName] = old
			}

			ctx.Match = append(ctx.Match, rule)
			cfg.Contexts[name] = ctx
			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}
			fmt.Fprintf(out, "Bound this folder to profile %q (matched %s)\n", name, desc)
			return nil
		},
	}

	cmd.Flags().BoolVar(&forcePath, "path", false, "Force exact folder path match")
	cmd.Flags().BoolVar(&forceRemote, "remote", false, "Force git remote match (errors if not a git repo)")
	return cmd
}

// pickExistingContext shows a numbered menu of existing profiles and
// returns the chosen name. Returns an error in non-TTY mode (the
// caller is expected to require a positional name in that case).
func pickExistingContext(out io.Writer, reader *bufio.Reader, cfg *config.Config) (string, error) {
	if !isStdinTTY() {
		return "", fmt.Errorf("a profile name is required in non-interactive mode")
	}
	if len(cfg.Contexts) == 0 {
		return "", fmt.Errorf("no profiles configured. Run: aide profile create <name>")
	}
	names := make([]string, 0, len(cfg.Contexts))
	for n := range cfg.Contexts {
		names = append(names, n)
	}
	sort.Strings(names)

	fmt.Fprintln(out, "Existing profiles:")
	for i, n := range names {
		fmt.Fprintf(out, "  [%d] %s\n", i+1, n)
	}
	fmt.Fprint(out, "Choose [1]: ")
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("reading selection: %w", err)
	}
	input = strings.TrimSpace(input)
	choice := 1
	if input != "" {
		n, err := strconv.Atoi(input)
		if err != nil || n < 1 || n > len(names) {
			return "", fmt.Errorf("invalid selection: %q", input)
		}
		choice = n
	}
	return names[choice-1], nil
}

// isStdinTTY reports whether stdin is connected to a terminal. Used by
// commands that need to choose between interactive prompting and a
// non-TTY hard error.
func isStdinTTY() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) // #nosec G115 -- stdin fd is always 0/1/2, never overflows int
}

type createTristate int

const (
	tristateUnset createTristate = iota
	tristateYes
	tristateNo
)

type createOptions struct {
	agent  string
	secret string
	here   createTristate
}

func contextCreateCmd() *cobra.Command {
	var (
		agent  string
		secret string
		here   bool
		noHere bool
	)

	cmd := &cobra.Command{
		Use:   "create [name]",
		Short: "Create a new profile",
		Long: `Create a new profile.

Examples:
  aide profile create                                              # interactive wizard
  aide profile create work                                         # name pre-filled
  aide profile create work --agent claude --secret-store firmus --here
  aide profile create work --agent claude --no-here                # skip cwd binding`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if here && noHere {
				return fmt.Errorf("--here and --no-here are mutually exclusive")
			}

			name := ""
			if len(args) == 1 {
				name = args[0]
			}

			opts := createOptions{
				agent:  agent,
				secret: secret,
				here:   tristateUnset,
			}
			if here {
				opts.here = tristateYes
			} else if noHere {
				opts.here = tristateNo
			}

			return runCreateWizard(cmd, name, opts)
		},
	}

	cmd.Flags().StringVar(&agent, "agent", "", "Agent name (skips agent prompt)")
	cmd.Flags().StringVar(&secret, "secret-store", "", "Secret store name (skips secret prompt)")
	cmd.Flags().BoolVar(&here, "here", false, "Bind this folder as a match rule")
	cmd.Flags().BoolVar(&noHere, "no-here", false, "Skip cwd binding")
	return cmd
}

// runCreateWizard creates a new profile, optionally binding cwd. The
// wizard fills in any missing fields by prompting in TTY mode, or by
// returning a helpful error in non-TTY mode.
func runCreateWizard(cmd *cobra.Command, prefilledName string, opts createOptions) error {
	out := cmd.OutOrStdout()
	reader := bufio.NewReader(os.Stdin)
	tty := isStdinTTY()

	env, err := cmdEnv(cmd)
	if err != nil {
		return err
	}
	cwd := env.CWD()
	cfg := env.Config()

	// 1. Name
	name := strings.TrimSpace(prefilledName)
	if name == "" {
		if !tty {
			return fmt.Errorf("a profile name is required in non-interactive mode")
		}
		fmt.Fprint(out, "Profile name: ")
		raw, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("reading profile name: %w", err)
		}
		name = strings.TrimSpace(raw)
		if name == "" {
			return fmt.Errorf("profile name cannot be empty")
		}
	}
	if _, exists := cfg.Contexts[name]; exists {
		return fmt.Errorf("profile %q already exists", name)
	}

	// 2. Agent
	agentName := opts.agent
	if agentName == "" {
		if detected := singleAgentOnPath(); detected != "" {
			agentName = detected
			fmt.Fprintf(out, "Using agent: %s (auto-detected)\n", agentName)
		} else if tty {
			fmt.Fprint(out, "Agent: ")
			raw, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("reading agent: %w", err)
			}
			agentName = strings.TrimSpace(raw)
		}
	}
	if agentName == "" {
		return fmt.Errorf("no agent provided and none could be auto-detected on PATH (pass --agent)")
	}
	if !launcher.IsKnownAgent(agentName) {
		return fmt.Errorf("unknown agent %q.\nKnown agents: %s",
			agentName, strings.Join(launcher.KnownAgents, ", "))
	}

	// 3. Secret store (optional)
	secretName := strings.TrimSpace(opts.secret)
	if secretName == "" && tty && opts.secret == "" {
		fmt.Fprint(out, "Secret store name (optional, press enter to skip): ")
		raw, _ := reader.ReadString('\n')
		secretName = strings.TrimSpace(raw)
	}

	// 4. Bind cwd?
	bindHere := false
	switch opts.here {
	case tristateYes:
		bindHere = true
	case tristateNo:
		bindHere = false
	case tristateUnset:
		if tty {
			fmt.Fprintf(out, "Bind this folder to %q now? [Y/n]: ", name)
			raw, _ := reader.ReadString('\n')
			ans := strings.ToLower(strings.TrimSpace(raw))
			bindHere = (ans == "" || ans == "y" || ans == "yes")
		}
	}

	// 5. Build and persist.
	if cfg.Agents == nil {
		cfg.Agents = make(map[string]config.AgentDef)
	}
	if cfg.Contexts == nil {
		cfg.Contexts = make(map[string]config.Context)
	}
	if _, ok := cfg.Agents[agentName]; !ok {
		cfg.Agents[agentName] = config.AgentDef{Binary: agentName}
	}

	newCtx := config.Context{Agent: agentName}
	if secretName != "" {
		newCtx.Secret = secretName
	}
	var bindDesc string
	if bindHere {
		rule, desc := autoDetectMatchRule(cwd)
		newCtx.Match = []config.MatchRule{rule}
		bindDesc = desc
	}
	isolateNewProfile(out, cfg, name, &newCtx)
	cfg.Contexts[name] = newCtx
	if cfg.DefaultContext == "" {
		cfg.DefaultContext = name
	}

	if err := config.WriteConfig(cfg); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	fmt.Fprintf(out, "Created profile %q (agent: %s).\n", name, agentName)
	if bindHere {
		fmt.Fprintf(out, "Bound this folder (matched %s).\n", bindDesc)
	}
	return nil
}

// singleAgentOnPath returns the single supported agent binary present
// on PATH. If zero or multiple are found, returns "".
func singleAgentOnPath() string {
	scan := launcher.ScanAgents(exec.LookPath)
	if len(scan.Found) == 1 {
		for name := range scan.Found {
			return name
		}
	}
	return ""
}

func contextRenameCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "rename <old-name> <new-name>",
		Short:        "Rename a profile",
		Args:         cobra.ExactArgs(2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			oldName := args[0]
			newName := args[1]

			env, err := cmdEnv(cmd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			cfg := env.Config()

			ctx, ok := cfg.Contexts[oldName]
			if !ok {
				return fmt.Errorf("profile %q not found", oldName)
			}
			if _, exists := cfg.Contexts[newName]; exists {
				return fmt.Errorf("profile %q already exists", newName)
			}

			cfg.Contexts[newName] = ctx
			delete(cfg.Contexts, oldName)

			if cfg.DefaultContext == oldName {
				cfg.DefaultContext = newName
			}

			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Renamed profile %q -> %q\n", oldName, newName)
			return nil
		},
	}
}

func contextRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "remove <name>",
		Short:        "Remove a profile from configuration",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			env, err := cmdEnv(cmd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			cfg := env.Config()
			ctx, ok := cfg.Contexts[name]
			if !ok {
				return fmt.Errorf("profile %q not found", name)
			}
			removedAgent := ctx.Agent
			delete(cfg.Contexts, name)
			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Removed profile %q\n", name)

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
					fmt.Fprintf(out, "\nAgent %q is no longer used by any profile.\n", removedAgent)
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
		Short:        "Set the secret on the current profile",
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
			fmt.Fprintf(cmd.OutOrStdout(), "Set secret %q on profile %q\n", secretName, ctxName)
			return nil
		},
	}

	cmd.Flags().StringVar(&contextName, "context", "", "Target profile (default: CWD-matched)")
	addProfileFlag(cmd, &contextName)
	return cmd
}

func contextRemoveSecretCmd() *cobra.Command {
	var contextName string

	cmd := &cobra.Command{
		Use:          "remove-secret",
		Short:        "Remove the secret from the current profile",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, ctxName, ctx, err := resolveContextForMutation(contextName)
			if err != nil {
				return err
			}

			oldSecret := ctx.Secret
			if oldSecret == "" {
				return fmt.Errorf("profile %q has no secret configured", ctxName)
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
			fmt.Fprintf(cmd.OutOrStdout(), "Removed secret %q from profile %q\n", oldSecret, ctxName)
			return nil
		},
	}

	cmd.Flags().StringVar(&contextName, "context", "", "Target profile (default: CWD-matched)")
	addProfileFlag(cmd, &contextName)
	return cmd
}

func contextSetDefaultCmd() *cobra.Command {
	var profile string
	cmd := &cobra.Command{
		Use:   "set-default [profile-name]",
		Short: "Set a profile as the default fallback",
		Long: `Set a profile as the default fallback when no match rules apply.

If no profile name is given, --profile names it, else the CWD-matched
profile is used.`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := cmdEnv(cmd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			cwd := env.CWD()
			cfg := env.Config()

			ctxName := profile
			if len(args) > 0 {
				if profile != "" && profile != args[0] {
					return fmt.Errorf("--profile %q and argument %q name different profiles; pass one", profile, args[0])
				}
				ctxName = args[0]
			}
			if ctxName == "" {
				rc, err := aidectx.Resolve(cfg, cwd, aidectx.GitRemotes(cwd))
				if err != nil {
					return fmt.Errorf("resolving profile: %w", err)
				}
				ctxName = rc.Name
			}

			if _, ok := cfg.Contexts[ctxName]; !ok {
				return fmt.Errorf("profile %q not found", ctxName)
			}

			cfg.DefaultContext = ctxName
			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Set default profile to %q\n", ctxName)
			return nil
		},
	}
	cmd.Flags().StringVar(&profile, "profile", "", "Profile to make the default (instead of the CWD-matched one)")
	registerSelectionCompletions(cmd)
	return cmd
}

// isolateNewProfile gives a profile being created its own config dirs
// when another profile already uses its harness's global login
// (config.IsolateNewProfile), and says so.
func isolateNewProfile(out io.Writer, cfg *config.Config, name string, ctx *config.Context) {
	others := cfg.IsolateNewProfile(name, ctx)
	if len(others) == 0 {
		return
	}
	key := "config_dir_name"
	if !cfg.ProfileSchema {
		key = "profile"
	}
	h := cfg.HarnessOf(ctx.Agent)
	fmt.Fprintf(out, "Profile %q gets its own config dirs (%s: %s), so it does not share %s's global login with %s. "+
		"Log in to %s again under this profile.\n", name, key, ctx.Profile, h, strings.Join(others, ", "), h)
}
