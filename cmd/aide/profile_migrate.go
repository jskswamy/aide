package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/fsutil"
	"github.com/jskswamy/aide/internal/provision"
)

func profileMigrateCmd() *cobra.Command {
	var (
		write bool
		adopt []string
	)
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Convert config.yaml from contexts: to profiles:",
		Long: `Convert your aide config from the legacy contexts: schema to profiles:.

Without --write, prints the migrated config and what changes; nothing
is written. With --write, the original is kept as config.yaml.bak and
the new file is written atomically.

  contexts: -> profiles:, default_context: -> default_profile:
  profile: -> config_dir_name:, profile_dir: -> config_dir:
  env: provider keys templated from the profile's secret: store
    -> credentials: bindings selected by default_credentials:

Comments, unrelated keys and native config dirs are kept, so no login
moves. Profiles that run a harness from its global default dir (such
as ~/.claude) share that login; they are listed and left unchanged
unless named with --adopt, which keeps the global login for that one
profile by pinning config_dirs to the default dir. No login dir is
copied. .aide.yaml files are never changed, and launching aide never
rewrites the config.`,
		Example: `  aide profile migrate                     # preview
  aide profile migrate --write             # apply
  aide profile migrate --write --adopt personal`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("resolving home: %w", err)
			}
			return runProfileMigrate(cmd.OutOrStdout(), config.FilePath(), provision.DefaultStatePath(home), home, adopt, write)
		},
	}
	cmd.Flags().BoolVar(&write, "write", false, "Write the migrated config (default: preview only)")
	cmd.Flags().StringArrayVar(&adopt, "adopt", nil,
		"Keep this profile on its harness's global login by pinning config_dirs to the default dir (repeatable)")
	_ = cmd.RegisterFlagCompletionFunc("adopt", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return profileNamesForCompletion(), cobra.ShellCompDirectiveNoFileComp
	})
	return cmd
}

func runProfileMigrate(out io.Writer, cfgPath, statePath, home string, adopt []string, write bool) error {
	data, err := os.ReadFile(cfgPath) // #nosec G304 -- the user's own aide config
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("no config at %s; nothing to migrate", cfgPath)
	}
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}
	m, err := config.MigrateProfiles(data, config.MigrateOptions{Home: home, Adopt: adopt})
	if err != nil {
		return fmt.Errorf("%s: %w", cfgPath, err)
	}
	if !m.Changed {
		fmt.Fprintf(out, "Nothing to migrate: %s already uses profiles:.\n", cfgPath)
		printGlobalLogins(out, m.GlobalLogins)
		return nil
	}
	if !write {
		fmt.Fprintf(out, "Preview of %s after migration (nothing written; re-run with --write to apply):\n\n", cfgPath)
		fmt.Fprintf(out, "%s\n", strings.TrimRight(string(m.Output), "\n"))
		fmt.Fprintln(out)
		printMigrationSummary(out, m)
		return nil
	}

	backup, err := backupPath(cfgPath)
	if err != nil {
		return err
	}
	if err := fsutil.AtomicWrite(backup, data); err != nil {
		return fmt.Errorf("writing backup %s: %w", backup, err)
	}
	oldHash, err := provision.ConfigHash(cfgPath)
	if err != nil {
		return err
	}
	if err := fsutil.AtomicWrite(cfgPath, m.Output); err != nil {
		return fmt.Errorf("writing %s: %w", cfgPath, err)
	}
	newHash, err := provision.ConfigHash(cfgPath)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Migrated %s (original kept as %s).\n", cfgPath, backup)
	printMigrationSummary(out, m)
	n, err := provision.RehashConfig(statePath, oldHash, newHash)
	if err != nil {
		return fmt.Errorf("config migrated, but updating provisioning state failed (aide will report drift until `aide sync`): %w", err)
	}
	if n > 0 {
		fmt.Fprintf(out, "Updated provisioning state for %d profile(s), so the rewrite is not reported as drift.\n", n)
	}
	return nil
}

// backupPath is <path>.bak, or a timestamped name when that exists,
// so an earlier backup is never overwritten.
func backupPath(path string) (string, error) {
	backup := path + ".bak"
	if _, err := os.Lstat(backup); errors.Is(err, os.ErrNotExist) {
		return backup, nil
	} else if err != nil {
		return "", fmt.Errorf("checking backup %s: %w", backup, err)
	}
	return path + ".bak-" + time.Now().UTC().Format("20060102T150405Z"), nil
}

func printMigrationSummary(out io.Writer, m *config.Migration) {
	if len(m.Renamed) > 0 {
		fmt.Fprintln(out, "Renamed:")
		for _, r := range m.Renamed {
			fmt.Fprintf(out, "  %s\n", r)
		}
	}
	if len(m.Credentials) > 0 {
		fmt.Fprintln(out, "Provider keys moved from env: to credentials: (values stay in the secret store):")
		for _, c := range m.Credentials {
			fmt.Fprintf(out, "  profile %s: %s -> credential %s (store %s, key %s)", c.Profile, c.EnvVar, c.Name, c.Store, c.Key)
			if len(c.Harnesses) > 0 {
				fmt.Fprintf(out, "; custom name, limited to harnesses: [%s]", strings.Join(c.Harnesses, ", "))
			}
			fmt.Fprintln(out)
		}
	}
	printGlobalLogins(out, m.GlobalLogins)
	fmt.Fprintln(out, "Comments, unrelated keys and native config dirs are kept (blank lines and indentation are normalized). .aide.yaml files are not changed.")
}

func printGlobalLogins(out io.Writer, logins []config.GlobalLogin) {
	if len(logins) == 0 {
		return
	}
	fmt.Fprintln(out, "Profiles on a harness's global login (its default config dir, shared by every profile listed for that dir):")
	var pending []string
	for _, g := range logins {
		state := "unchanged"
		if g.Adopted {
			state = fmt.Sprintf("adopted: config_dirs pins %s to %s", g.Harness, g.Dir)
		} else {
			pending = append(pending, g.Profile)
		}
		fmt.Fprintf(out, "  %s: %s %s (%s)\n", g.Profile, g.Harness, g.Dir, state)
	}
	if len(pending) > 0 {
		fmt.Fprintf(out, "To keep the global login for one profile, re-run with --adopt %s; give the others their own "+
			"config_dir_name: and log in there. No login dir is copied.\n", pending[0])
	}
}
