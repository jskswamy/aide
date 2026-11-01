package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/fsutil"
	"github.com/jskswamy/aide/internal/provision/skillshare"
)

func skillshareCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skillshare",
		Short: "Point Skillshare at the config dirs aide launches profiles with",
	}
	cmd.AddCommand(skillshareTargetsCmd())
	return cmd
}

func skillshareTargetsCmd() *cobra.Command {
	var write bool
	cmd := &cobra.Command{
		Use:   "targets",
		Short: "Write Skillshare account targets for your profiles' config dirs",
		Long: `Generate Skillshare account targets (agent: + config_dir:) for every
profile and harness (claude, codex, opencode, pi) that launches from a
config dir of its own, so ` + "`skillshare sync`" + ` installs skills and other
resources into exactly the dirs aide launches with.

Without --write, prints the targets and what would change; nothing is
written. With --write, merges them into Skillshare's config
($SKILLSHARE_CONFIG, else $XDG_CONFIG_HOME/skillshare/config.yaml,
else ~/.config/skillshare/config.yaml) atomically, keeping the
original as config.yaml.bak. Everything else in the file is kept.

Targets aide writes are named aide-<profile>-<harness>; aide updates
them and removes the ones whose profile or dir is gone, and never
touches other targets. aide does not run Skillshare: run
` + "`skillshare sync`" + ` afterwards.

Not written: profiles without a config dir of their own (they use the
harness's default dir, which Skillshare's plain targets cover), a
harness's default dir, a dir two targets would share, a dir one of
your own targets already uses, and harnesses Skillshare has no account
targets for. Only names and dirs are written: never a credential.`,
		Example: `  aide skillshare targets           # preview
  aide skillshare targets --write   # apply, then run: skillshare sync`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			env, err := cmdEnv(cmd)
			if err != nil {
				return err
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("resolving home: %w", err)
			}
			return runSkillshareTargets(cmd.OutOrStdout(), env.Config(), home, skillshare.ConfigPath(home, os.Getenv), write)
		},
	}
	cmd.Flags().BoolVar(&write, "write", false, "Merge the targets into Skillshare's config (default: preview only)")
	return cmd
}

func runSkillshareTargets(out io.Writer, cfg *config.Config, home, path string, write bool) error {
	data, err := os.ReadFile(path) // #nosec G304 -- the user's own Skillshare config
	missing := errors.Is(err, os.ErrNotExist)
	if err != nil && !missing {
		return fmt.Errorf("reading Skillshare config: %w", err)
	}
	if missing && write {
		return fmt.Errorf("no Skillshare config at %s; run `skillshare init` first, then re-run with --write", path)
	}
	plan := skillshare.PlanTargets(cfg, home)
	res, err := skillshare.Merge(data, plan, home)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	if err := printSkillshareTargets(out, path, res); err != nil {
		return err
	}
	printSkips(out, "Not written (no dir of its own for Skillshare):", plan.Skipped)
	printSkips(out, "Not written (name or dir already used by your own Skillshare targets):", res.Conflicts)
	printSkips(out, "Not written (harness without aide-written Skillshare account targets):", plan.Unsupported)
	if len(plan.Skipped)+len(res.Conflicts)+len(plan.Unsupported) > 0 {
		fmt.Fprintln(out, "These are Skillshare limits; they do not change how aide isolates a profile's login.")
	}
	fmt.Fprintln(out)

	switch {
	case missing:
		fmt.Fprintf(out, "No Skillshare config at %s yet: run `skillshare init`, then `aide skillshare targets --write`.\n", path)
		return nil
	case !res.Changed:
		fmt.Fprintf(out, "%s is up to date.\n", path)
	case !write:
		fmt.Fprintln(out, "Nothing written; re-run with --write to apply.")
		return nil
	default:
		backup, err := backupPath(path)
		if err != nil {
			return err
		}
		if err := fsutil.AtomicWrite(backup, data); err != nil {
			return fmt.Errorf("writing backup %s: %w", backup, err)
		}
		if err := fsutil.AtomicWrite(path, res.Output); err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
		}
		fmt.Fprintf(out, "Updated %s (original kept as %s).\n", path, backup)
	}
	if len(res.Written) > 0 {
		fmt.Fprintln(out, "Run `skillshare sync` to install your resources into these dirs.")
	}
	return nil
}

func printSkillshareTargets(out io.Writer, path string, res *skillshare.Result) error {
	fmt.Fprintf(out, "Skillshare targets for aide profiles (%s):\n\n", path)
	if len(res.Written) == 0 {
		fmt.Fprintln(out, "  (none)")
	} else {
		block, err := skillshare.RenderTargets(res.Written)
		if err != nil {
			return err
		}
		for _, line := range strings.Split(strings.TrimRight(string(block), "\n"), "\n") {
			fmt.Fprintf(out, "  %s\n", line)
		}
	}
	fmt.Fprintln(out)
	for _, c := range []struct {
		mark  string
		names []string
	}{{"+", res.Added}, {"~", res.Updated}, {"-", res.Removed}} {
		for _, n := range c.names {
			fmt.Fprintf(out, "  %s %s\n", c.mark, n)
		}
	}
	for _, r := range res.References {
		fmt.Fprintf(out, "  ! removed target %s is still referenced; remove it there\n", r)
	}
	return nil
}

func printSkips(out io.Writer, heading string, skips []skillshare.Skip) {
	if len(skips) == 0 {
		return
	}
	fmt.Fprintf(out, "\n%s\n", heading)
	for _, s := range skips {
		fmt.Fprintf(out, "  %s\n", s)
	}
}
