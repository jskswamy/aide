package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/fsutil"
	"github.com/jskswamy/aide/internal/gateway"
	"github.com/jskswamy/aide/internal/gatewaymigrate"
	"github.com/jskswamy/aide/internal/prefs"
	"github.com/jskswamy/aide/internal/provision"
)

// maybeNudgeConfigMigration prints a brief one-line reminder to errOut at most
// once every 7 days when cfg was loaded from a legacy single-file config.yaml
// and errOut is an interactive terminal.
func maybeNudgeConfigMigration(cfg *config.Config, errOut io.Writer, isTTY bool, homeDir string, now time.Time) {
	config.MaybeNudgeMigration(cfg, errOut, isTTY, homeDir, now)
}

func shouldNudgeForCmd(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	switch cmd.Name() {
	case "migrate", "prompt", "statusline":
		return false
	}
	return true
}

// saveConfigWithMigrationPrompt saves cfg using the modular split layout,
// first upgrading legacy gateways (see gatewaymigrate.Apply).
// When cfg was loaded from a legacy single-file config.yaml, an interactive
// terminal prompts the user to migrate first (saving config.yaml.bak);
// declining or running non-interactively returns config.ErrLegacyMonolithicConfig.
func saveConfigWithMigrationPrompt(cmd *cobra.Command, cfg *config.Config) error {
	home, _ := os.UserHomeDir()
	in := io.Reader(os.Stdin)
	errOut := io.Writer(os.Stderr)
	interactive := isInteractiveTerminal(os.Stdin) && isInteractiveTerminal(os.Stderr)
	if cmd != nil {
		in = cmd.InOrStdin()
		errOut = cmd.ErrOrStderr()
	}
	return saveConfigWithMigrationPromptIO(cfg, config.FilePath(), home, in, errOut, interactive)
}

// migrateGatewaysBeforeSave upgrades cfg's legacy gateways, with backups,
// so the write that follows does not hit config.ErrNeedsGatewayMigration.
func migrateGatewaysBeforeSave(cfg *config.Config, cfgPath, home string, errOut io.Writer) error {
	if cfg == nil || !cfg.NeedsGatewayMigration {
		return nil
	}
	res, err := gatewaymigrate.Migrate(cfg, gateway.CacheModels(home), filepath.Dir(cfgPath), filepath.Dir(prefs.DefaultPath(home)))
	fmt.Fprint(errOut, res.Summary)
	return err
}

func saveConfigWithMigrationPromptIO(cfg *config.Config, cfgPath, home string, in io.Reader, errOut io.Writer, interactive bool) error {
	if cfg == nil || !cfg.LegacyMonolithic {
		if err := migrateGatewaysBeforeSave(cfg, cfgPath, home, errOut); err != nil {
			return err
		}
		return config.WriteConfigTo(cfg, cfgPath)
	}
	if !interactive {
		return config.ErrLegacyMonolithicConfig
	}
	fmt.Fprint(errOut, "Config uses the legacy single-file layout and must be migrated before saving changes. Migrate now? (original kept as config.yaml.bak) [y/N]: ")
	reader := bufio.NewReader(in)
	line, _ := reader.ReadString('\n')
	ans := strings.ToLower(strings.TrimSpace(line))
	if ans != "y" && ans != "yes" {
		return config.ErrLegacyMonolithicConfig
	}

	origData, err := os.ReadFile(cfgPath)
	if err != nil {
		return fmt.Errorf("reading %s for backup: %w", cfgPath, err)
	}
	cfg.ProfileSchema = true
	cfg.LegacyMonolithic = false

	if err := config.ValidateForWrite(cfg, home); err != nil {
		cfg.LegacyMonolithic = true
		return err
	}

	backup, err := backupPath(cfgPath)
	if err != nil {
		cfg.LegacyMonolithic = true
		return err
	}
	if err := fsutil.AtomicWrite(backup, origData); err != nil {
		cfg.LegacyMonolithic = true
		return fmt.Errorf("writing backup %s: %w", backup, err)
	}
	oldHash, _ := provision.ConfigHash(cfgPath)

	if m, merr := config.MigrateProfiles(origData, config.MigrateOptions{Home: home}); merr == nil && m.SplitErr == nil && m.SplitLayout {
		if err := config.ApplyMigrationSplit(filepath.Dir(cfgPath), m); err != nil {
			cfg.LegacyMonolithic = true
			return fmt.Errorf("writing the split config files: %w", err)
		}
	}

	if err := migrateGatewaysBeforeSave(cfg, cfgPath, home, errOut); err != nil {
		cfg.LegacyMonolithic = true
		return err
	}
	if err := config.WriteConfigTo(cfg, cfgPath); err != nil {
		cfg.LegacyMonolithic = true
		return err
	}
	if home != "" {
		if newHash, herr := provision.ConfigHash(cfgPath); herr == nil && oldHash != "" && newHash != "" {
			_, _ = provision.RehashConfig(provision.DefaultStatePath(home), oldHash, newHash)
		}
	}
	fmt.Fprintf(errOut, "Migrated %s to modular config layout (original kept as %s).\n", filepath.Base(cfgPath), backup)
	return nil
}

// MigratedConfig returns the loaded config with its legacy gateways
// already migrated (with backups), so a command's edit runs on explicit
// gateway_choice: and converted gateways. A read-only config dir leaves
// the in-memory translation, which saving then refuses.
func (e *Env) MigratedConfig() (*config.Config, error) {
	errOut := io.Writer(os.Stderr)
	if e.cmd != nil {
		errOut = e.cmd.ErrOrStderr()
	}
	home, _ := os.UserHomeDir()
	if err := migrateGatewaysBeforeSave(e.cfg, config.FilePath(), home, errOut); err != nil {
		return nil, err
	}
	return e.cfg, nil
}
