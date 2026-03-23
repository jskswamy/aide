package modules

import (
	"fmt"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

// resolveConfigDirs returns directories for an agent given an env var
// override key and a list of default candidates. When the env var is
// set, only that path is returned (explicit override). Otherwise,
// candidates that exist or are under homeDir are returned.
//
// Empty env var semantics: ctx.EnvLookup returns ("", true) for KEY=,
// but we treat empty as unset (fall through to defaults). This matches
// the previous resolver behavior where KEY= was treated as unset.
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

// configDirRules generates file-read* file-write* Grant rules for
// agent config directories. Each dir gets a subpath rule.
func configDirRules(sectionName string, dirs []string) []seatbelt.Rule {
	if len(dirs) == 0 {
		return nil
	}
	rules := []seatbelt.Rule{
		seatbelt.SectionGrant(sectionName + " config"),
	}
	for _, dir := range dirs {
		rules = append(rules, seatbelt.GrantRule(fmt.Sprintf(
			`(allow file-read* file-write* (subpath %q))`, dir,
		)))
	}
	return rules
}
