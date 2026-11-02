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

// saveConfigWithMigrationPrompt saves cfg using the modular split layout.
// When cfg was loaded from a legacy single-file config.yaml, an interactive
// terminal prompts the user to migrate first (saving config.yaml.bak);
// declining or running non-interactively returns config.ErrLegacyMonolithicConfig.
func saveConfigWithMigrationPrompt(cmd *cobra.Command, cfg *config.Config) error {
	if cfg == nil || !cfg.LegacyMonolithic {
		return config.WriteConfig(cfg)
	}
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

func saveConfigWithMigrationPromptIO(cfg *config.Config, cfgPath, home string, in io.Reader, errOut io.Writer, interactive bool) error {
	if cfg == nil || !cfg.LegacyMonolithic {
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
	backup, err := backupPath(cfgPath)
	if err != nil {
		return err
	}
	if err := fsutil.AtomicWrite(backup, origData); err != nil {
		return fmt.Errorf("writing backup %s: %w", backup, err)
	}
	oldHash, _ := provision.ConfigHash(cfgPath)

	// Migrate legacy credentials on cfg if it was using the contexts: schema
	if !cfg.ProfileSchema {
		for name, ctx := range cfg.Contexts {
			if translated, legacy, terr := config.TranslateLegacyCredentials(ctx, ctx.Secret); terr == nil && len(legacy) > 0 {
				cfg.Contexts[name] = translated
			}
		}
	}
	cfg.ProfileSchema = true
	cfg.LegacyMonolithic = false

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
