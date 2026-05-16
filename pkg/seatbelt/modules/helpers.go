package modules

import (
	"fmt"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

// resolveConfigDirs returns config directories for an agent.
// When envKey is set to a non-empty value, only that path is returned.
// Otherwise, candidates that exist on disk or are under homeDir are returned.
func resolveConfigDirs(ctx *seatbelt.Context, envKey string, candidates []string) []string {
	if envKey != "" {
		if dir, ok := ctx.EnvLookup(envKey); ok && dir != "" {
			return []string{dir}
		}
	}
	var dirs []string
	for _, p := range candidates {
		if seatbelt.ExistsOrUnderHome(ctx.HomeDir, p) {
			dirs = append(dirs, p)
		}
	}
	return dirs
}

// configDirRules generates file-read* file-write* Allow rules for each dir.
func configDirRules(sectionName string, dirs []string) []seatbelt.Rule {
	if len(dirs) == 0 {
		return nil
	}
	rules := []seatbelt.Rule{seatbelt.SectionAllow(sectionName + " config")}
	for _, dir := range dirs {
		rules = append(rules, seatbelt.AllowRule(fmt.Sprintf(
			`(allow file-read* file-write* (subpath %q))`, dir,
		)))
	}
	return rules
}
