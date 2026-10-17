package main

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jskswamy/aide/internal/config"
	aidectx "github.com/jskswamy/aide/internal/context"
	"github.com/jskswamy/aide/internal/launcher"
)

const (
	profileFlagUsage    = "Use this profile instead of the one matched for this directory"
	credentialFlagUsage = "Inject this credential binding (repeatable); replaces the saved and default selection for this run"
	authFlagUsage       = "Authentication: api (inject selected credentials) or login (native login, inject none)"
)

// selectionFlags are the one-time launch selections: --profile,
// --agent, --credential and --auth. They never change saved
// preferences.
type selectionFlags struct {
	profile     string
	agent       string
	credentials []string
	auth        string
	// credentialsSet reports --credential was given; filled by inputs.
	credentialsSet bool
}

// register adds the flags with shell completions. agentUsage
// describes --agent for cmd.
func (f *selectionFlags) register(cmd *cobra.Command, agentUsage string) {
	cmd.Flags().StringVar(&f.profile, "profile", "", profileFlagUsage)
	cmd.Flags().StringVar(&f.agent, "agent", "", agentUsage)
	cmd.Flags().StringArrayVar(&f.credentials, "credential", nil, credentialFlagUsage)
	cmd.Flags().StringVar(&f.auth, "auth", "", authFlagUsage)
	registerSelectionCompletions(cmd)
}

// inputs validates the flags and returns them as launcher inputs.
func (f *selectionFlags) inputs(cmd *cobra.Command) (launcher.SelectionInputs, error) {
	auth, err := parseAuthFlag(f.auth)
	if err != nil {
		return launcher.SelectionInputs{}, err
	}
	f.credentialsSet = cmd.Flags().Changed("credential")
	return launcher.SelectionInputs{
		Agent:          f.agent,
		Credentials:    nonEmpty(f.credentials),
		CredentialsSet: f.credentialsSet,
		Auth:           auth,
	}, nil
}

// parseAuthFlag validates --auth.
func parseAuthFlag(v string) (launcher.AuthMode, error) {
	switch launcher.AuthMode(v) {
	case "":
		return "", nil
	case launcher.AuthAPI, launcher.AuthLogin:
		return launcher.AuthMode(v), nil
	}
	return "", fmt.Errorf("--auth %q: use %s or %s", v, launcher.AuthAPI, launcher.AuthLogin)
}

// nonEmpty drops empty names, so --credential= is an explicitly empty
// selection.
func nonEmpty(names []string) []string {
	return slices.DeleteFunc(slices.Clone(names), func(s string) bool { return s == "" })
}

// registerSelectionCompletions completes profile, harness and
// credential names for whichever of those flags cmd has.
func registerSelectionCompletions(cmd *cobra.Command) {
	complete := func(names func(*cobra.Command) []string) cobra.CompletionFunc {
		return func(c *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
			return names(c), cobra.ShellCompDirectiveNoFileComp
		}
	}
	for flag, names := range map[string]func(*cobra.Command) []string{
		"profile":    func(*cobra.Command) []string { return profileNamesForCompletion() },
		"agent":      func(*cobra.Command) []string { return harnessNamesForCompletion() },
		"credential": credentialNamesForCompletion,
	} {
		if cmd.Flags().Lookup(flag) != nil {
			_ = cmd.RegisterFlagCompletionFunc(flag, complete(names))
		}
	}
	if cmd.Flags().Lookup("auth") != nil {
		_ = cmd.RegisterFlagCompletionFunc("auth", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
			return []string{string(launcher.AuthAPI), string(launcher.AuthLogin)}, cobra.ShellCompDirectiveNoFileComp
		})
	}
}

// completionConfig loads the config for completions, or nil.
func completionConfig() (*config.Config, string) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, ""
	}
	cfg, err := config.Load(config.Dir(), cwd)
	if err != nil {
		return nil, ""
	}
	return cfg, cwd
}

func profileNamesForCompletion() []string {
	cfg, _ := completionConfig()
	if cfg == nil {
		return nil
	}
	return slices.Sorted(maps.Keys(cfg.Contexts))
}

func harnessNamesForCompletion() []string {
	names := slices.Clone(launcher.KnownAgents)
	if cfg, _ := completionConfig(); cfg != nil {
		for name := range cfg.Agents {
			if !slices.Contains(names, name) {
				names = append(names, name)
			}
		}
	}
	slices.Sort(names)
	return names
}

// credentialNamesForCompletion lists the credential bindings of the
// profile --profile names, else the one matched for this directory.
func credentialNamesForCompletion(cmd *cobra.Command) []string {
	cfg, cwd := completionConfig()
	if cfg == nil {
		return nil
	}
	profile, _ := cmd.Flags().GetString("profile")
	rc, err := aidectx.ResolveSelected(cfg, cwd, aidectx.GitRemotes(cwd), profile)
	if err != nil {
		return nil
	}
	return slices.Sorted(maps.Keys(rc.Context.Credentials))
}

// previewSelection resolves what a launch from cwd would select with
// f (launcher.ResolveSelection; it mutates cfg). Its Notices hold
// everything a launch would print before starting, including the
// gate's warnings and why the launch would be refused. Credentials are
// left to launcher.PlanProfileCredentials.
func previewSelection(cmd *cobra.Command, cfg *config.Config, cwd, homeDir string, f *selectionFlags) (launcher.Resolution, error) {
	in, err := f.inputs(cmd)
	if err != nil {
		return launcher.Resolution{}, err
	}
	res, err := (&launcher.Launcher{Profile: f.profile}).ResolveSelection(cfg, cwd, homeDir, in)
	res.Notices = append(slices.Clone(res.Gate.Warnings), res.Notices...)
	if res.Refused != nil {
		res.Notices = append(res.Notices, "launch would fail: "+res.Refused.Error())
	}
	return res, err
}

// contextFlagDeprecation is printed to stderr when --context is used.
const contextFlagDeprecation = "--context is deprecated; use --profile"

// addProfileFlag gives a command that already takes --context a
// --profile flag selecting the same thing, and deprecates --context:
// it is hidden from help and using it prints a notice to stderr (never
// stdout, so --format json stays parseable). --profile fills target
// (the --context variable) before RunE, and the two must agree when
// both are given.
func addProfileFlag(cmd *cobra.Command, target *string) {
	var profile string
	usage := "Profile name"
	if f := cmd.Flags().Lookup("context"); f != nil {
		usage = strings.NewReplacer("contexts", "profiles", "context", "profile", "Context", "Profile").Replace(f.Usage)
		f.Hidden = true
	}
	cmd.Flags().StringVar(&profile, "profile", "", usage)
	registerSelectionCompletions(cmd)
	next := cmd.PreRunE
	cmd.PreRunE = func(c *cobra.Command, args []string) error {
		if c.Flags().Changed("context") {
			fmt.Fprintln(c.ErrOrStderr(), contextFlagDeprecation)
		}
		if profile != "" {
			if *target != "" && *target != profile {
				return fmt.Errorf("--profile %q and --context %q name different profiles; pass one", profile, *target)
			}
			*target = profile
		}
		if next != nil {
			return next(c, args)
		}
		return nil
	}
}

// passthroughSelectionError refuses selections that need a config when
// none exists (passthrough). --auth login is accepted: passthrough
// injects nothing, so the harness always uses its native login.
func passthroughSelectionError(f *selectionFlags, in launcher.SelectionInputs, configFile string) error {
	if f.profile != "" || in.CredentialsSet || in.Auth == launcher.AuthAPI {
		return fmt.Errorf("--profile, --credential and --auth api need profiles in %s; none exists yet (create one with: aide init). "+
			"Without a config aide injects no credentials and the harness uses its native login", configFile)
	}
	return nil
}
