package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jskswamy/aide/internal/config"
	aidectx "github.com/jskswamy/aide/internal/context"
	"github.com/jskswamy/aide/internal/launcher"
	"github.com/jskswamy/aide/internal/output"
	"github.com/jskswamy/aide/internal/prefs"
	"github.com/jskswamy/aide/internal/provision"
)

// projectCmd manages the user-local preferences saved for the current
// repository. It never writes .aide.yaml or config.yaml.
func projectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Save this repository's default harness and credentials (user-local)",
		Long: `Save the harness and credential bindings aide uses for this repository
by default, under the current profile. Preferences live in aide's state
directory, keyed by profile and the normalized origin remote, so every
worktree and clone of the repository shares them under the same
profile. They hold names only, never secret values, and never change
.aide.yaml or config.yaml.

One-time flags on aide itself (--agent, --credential, --auth) override
these for a single launch without changing them.`,
		SilenceUsage: true,
	}
	cmd.AddCommand(projectSetCmd(), projectClearCmd(), projectShowCmd())
	return cmd
}

// projectTarget is the profile and repository identity preferences are
// saved under.
type projectTarget struct {
	cfg  *config.Config
	home string
	rc   *aidectx.ResolvedContext
	key  prefs.Key
	path string
}

// resolveProjectTarget selects the profile (--profile, else matched,
// else default) and the normalized origin remote of cwd.
func resolveProjectTarget(cmd *cobra.Command, profile string) (*projectTarget, error) {
	env, err := cmdEnv(cmd)
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolving home directory: %w", err)
	}
	cwd, cfg := env.CWD(), env.Config()
	rc, err := aidectx.ResolveSelected(cfg, cwd, aidectx.GitRemotes(cwd), profile)
	if err != nil {
		return nil, fmt.Errorf("resolving profile: %w", err)
	}
	return &projectTarget{
		cfg:  cfg,
		home: home,
		rc:   rc,
		key:  prefs.Key{Profile: rc.Name, Remote: aidectx.CurrentRemote(cwd)},
		path: prefs.DefaultPath(home),
	}, nil
}

func (t *projectTarget) describe() string {
	return fmt.Sprintf("profile %q in %s", t.key.Profile, t.key.Remote)
}

func projectSetCmd() *cobra.Command {
	var profile, agent string
	var credentials []string
	var noCredentials bool
	cmd := &cobra.Command{
		Use:   "set",
		Short: "Save the default harness and/or credentials for this repository",
		Example: `  aide project set --agent pi
  aide project set --credential openrouter
  aide project set --agent claude --no-credentials   # native login`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			credsSet := cmd.Flags().Changed("credential")
			if agent == "" && !credsSet && !noCredentials {
				return errors.New("nothing to save: pass --agent, --credential or --no-credentials")
			}
			if credsSet && noCredentials {
				return errors.New("--credential and --no-credentials conflict: pick one")
			}
			t, err := resolveProjectTarget(cmd, profile)
			if err != nil {
				return err
			}
			if err := t.key.Validate(); err != nil {
				return fmt.Errorf("cannot save repository preferences: %w", err)
			}
			f, err := prefs.Load(t.path)
			if err != nil {
				return err
			}
			p, _ := f.Get(t.key)
			if agent != "" {
				p.Harness = agent
			}
			switch {
			case noCredentials:
				p.Credentials = prefs.Selection()
			case credsSet:
				p.Credentials = prefs.Selection(nonEmpty(credentials)...)
			}
			if err := t.check(p); err != nil {
				return err
			}
			f.Set(t.key, p)
			if err := prefs.Save(t.path, f); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Saved for %s: %s\n", t.describe(), describePreference(p))
			return nil
		},
	}
	cmd.Flags().StringVar(&profile, "profile", "", "Save under this profile instead of the one matched here")
	cmd.Flags().StringVar(&agent, "agent", "", "Default harness for this repository")
	cmd.Flags().StringArrayVar(&credentials, "credential", nil, "Credential binding to inject by default (repeatable); replaces the saved selection")
	cmd.Flags().BoolVar(&noCredentials, "no-credentials", false, "Inject no credentials by default (native login)")
	registerSelectionCompletions(cmd)
	return cmd
}

// check validates p the way a launch would use it: the harness must be
// launchable with the profile's config dir, and the credentials must
// be bindings of the profile that allow that harness.
func (t *projectTarget) check(p prefs.Preference) error {
	rc := *t.rc
	in := launcher.SelectionInputs{Agent: p.Harness}
	if p.Credentials != nil {
		in.Credentials, in.CredentialsSet = *p.Credentials, true
	}
	sel, _, err := launcher.SelectHarness(t.cfg, &rc, in, exec.LookPath)
	if err != nil {
		return err
	}
	if _, err := provision.InjectProfileEnv(rc.Context, rc.Context.Env, t.home); err != nil {
		return fmt.Errorf("profile %q: %w", rc.Name, err)
	}
	_, _, err = launcher.PreviewCredentials(t.cfg, &rc, in, &sel)
	return err
}

func projectClearCmd() *cobra.Command {
	var profile string
	var clearAgent, clearCredentials bool
	cmd := &cobra.Command{
		Use:   "clear",
		Short: "Clear saved repository preferences (all, or the fields named)",
		Long: `Clear what aide project set saved for this repository under the current
profile. With --agent or --credential only that field is cleared; with
neither, both are. Cleared fields fall back to .aide.yaml and the
profile defaults.`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			t, err := resolveProjectTarget(cmd, profile)
			if err != nil {
				return err
			}
			if err := t.key.Validate(); err != nil {
				return fmt.Errorf("cannot clear repository preferences: %w", err)
			}
			all := !clearAgent && !clearCredentials
			out := cmd.OutOrStdout()
			var cleared bool
			err = prefs.Update(t.path, func(f *prefs.File) error {
				p, ok := f.Get(t.key)
				if !ok {
					return nil
				}
				if all || clearAgent {
					cleared = cleared || p.Harness != ""
					p.Harness = ""
				}
				if all || clearCredentials {
					cleared = cleared || p.Credentials != nil
					p.Credentials = nil
				}
				f.Set(t.key, p)
				return nil
			})
			if err != nil {
				return err
			}
			if !cleared {
				fmt.Fprintf(out, "Nothing saved to clear for %s\n", t.describe())
				return nil
			}
			fmt.Fprintf(out, "Cleared for %s\n", t.describe())
			return nil
		},
	}
	cmd.Flags().StringVar(&profile, "profile", "", "Clear under this profile instead of the one matched here")
	cmd.Flags().BoolVar(&clearAgent, "agent", false, "Clear only the saved harness")
	cmd.Flags().BoolVar(&clearCredentials, "credential", false, "Clear only the saved credential selection")
	registerSelectionCompletions(cmd)
	return cmd
}

// projectShowResult is `aide project show --format json`. Credentials
// is null when unset and [] for an explicit empty selection.
type projectShowResult struct {
	Profile     string    `json:"profile"`
	Repository  string    `json:"repository"`
	Harness     string    `json:"harness,omitempty"`
	Credentials *[]string `json:"credentials"`
	File        string    `json:"file"`
}

func projectShowCmd() *cobra.Command {
	var profile string
	cmd := &cobra.Command{
		Use:          "show",
		Short:        "Show the preferences saved for this repository",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			format, err := output.FromCmd(cmd)
			if err != nil {
				return err
			}
			t, err := resolveProjectTarget(cmd, profile)
			if err != nil {
				return err
			}
			res := projectShowResult{Profile: t.key.Profile, Repository: t.key.Remote, File: t.path}
			identityErr := t.key.Validate()
			if identityErr == nil {
				f, err := prefs.Load(t.path)
				if err != nil {
					return err
				}
				p, _ := f.Get(t.key)
				res.Harness, res.Credentials = p.Harness, p.Credentials
			}
			return output.Emit(cmd.OutOrStdout(), format, res, func(w io.Writer) error {
				fmt.Fprintf(w, "Profile:      %s\n", res.Profile)
				if identityErr != nil {
					fmt.Fprintf(w, "Repository:   none (%v)\n", identityErr)
					return nil
				}
				fmt.Fprintf(w, "Repository:   %s\n", res.Repository)
				fmt.Fprintf(w, "Saved:        %s\n", describePreference(prefs.Preference{Harness: res.Harness, Credentials: res.Credentials}))
				fmt.Fprintf(w, "File:         %s\n", res.File)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&profile, "profile", "", "Show preferences under this profile instead of the one matched here")
	registerSelectionCompletions(cmd)
	return cmd
}

// describePreference renders p for humans.
func describePreference(p prefs.Preference) string {
	if p.IsZero() {
		return "nothing (profile and .aide.yaml defaults apply)"
	}
	harness := "harness not set"
	if p.Harness != "" {
		harness = "harness " + p.Harness
	}
	creds := "credentials not set"
	if p.Credentials != nil {
		if len(*p.Credentials) == 0 {
			creds = "no credentials"
		} else {
			creds = "credentials " + strings.Join(*p.Credentials, ", ")
		}
	}
	return harness + ", " + creds
}
