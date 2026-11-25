package skillshare

import (
	"os"
	"path/filepath"
	"slices"

	"gopkg.in/yaml.v3"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/homepath"
	"github.com/jskswamy/aide/internal/pathguard"
)

// ConfigDir is Skillshare's config dir: $XDG_CONFIG_HOME/skillshare,
// else ~/.config/skillshare. It holds config.yaml (unless
// $SKILLSHARE_CONFIG moves it) and the default skills/ and agents/.
func ConfigDir(home string, getenv func(string) string) string {
	return filepath.Join(homepath.XDG(getenv("XDG_CONFIG_HOME"), home, ".config"), "skillshare")
}

// stateDir is Skillshare's state dir: $XDG_STATE_HOME/skillshare, else
// ~/.local/state/skillshare.
func stateDir(home string, getenv func(string) string) string {
	return filepath.Join(homepath.XDG(getenv("XDG_STATE_HOME"), home, ".local/state"), "skillshare")
}

// Installed reports whether Skillshare is set up for this user: its
// config dir exists. It decides whether the skillshare capability is
// auto-included.
func Installed(home string, getenv func(string) string) bool {
	info, err := os.Stat(ConfigDir(home, getenv))
	return err == nil && info.IsDir()
}

// ReadableDirs are the paths the skillshare capability grants read
// access to: skills/ and agents/ in ConfigDir, plugins/ in Skillshare's
// state dir (Claude's and Pi's settings reference it), and every path
// under sources: (or the older source:) in Skillshare's config. Each
// is listed by its symlink-resolved path, since Seatbelt and Landlock
// match the resolved path, and by its literal path too when that
// differs. Missing paths are left out. Nothing else of Skillshare's is
// granted: not config.yaml, the hooks/, mcp/ or logs/ state, backups
// or the cache.
//
// A sources: path is refused when it is relative, $HOME, "/", outside
// $HOME, or overlaps a sensitive dir (~/.ssh, ~/.aws, ...), a harness's
// default config dir, any profile's config dir in cfg, or aide's own
// config dir: granting it would expose logins and keys. The fixed dirs
// get the same checks, except that an $XDG_* dir may live outside $HOME.
func ReadableDirs(cfg *config.Config, home string, getenv func(string) string) []string {
	g := pathguard.NewChecker(cfg, home, getenv)
	var out []string
	add := func(literal string, underHome bool) {
		for _, p := range g.Grant(literal, underHome) {
			if !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
	}
	dir := ConfigDir(home, getenv)
	add(filepath.Join(dir, "skills"), false)
	add(filepath.Join(dir, "agents"), false)
	add(filepath.Join(stateDir(home, getenv), "plugins"), false)
	for _, src := range configSources(home, getenv) {
		add(src, true)
	}
	return out
}

// configSources are the paths Skillshare's config lists under sources:
// (any nesting) or the older top-level source:, tilde-expanded. A
// missing or unreadable config lists none.
func configSources(home string, getenv func(string) string) []string {
	data, err := os.ReadFile(ConfigPath(home, getenv)) // #nosec G304 -- the user's own Skillshare config
	if err != nil {
		return nil
	}
	var doc yaml.Node
	if yaml.Unmarshal(data, &doc) != nil || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil
	}
	root := doc.Content[0]
	var out []string
	for i := 0; i+1 < len(root.Content); i += 2 {
		if k := root.Content[i].Value; k == "sources" || k == "source" {
			for _, v := range scalars(root.Content[i+1]) {
				out = append(out, expand(v, home))
			}
		}
	}
	return out
}

// scalars are the scalar values in n: n itself, a sequence's items or
// a mapping's values, at any depth.
func scalars(n *yaml.Node) []string {
	switch n.Kind {
	case yaml.ScalarNode:
		if n.Tag == "!!null" || n.Value == "" {
			return nil
		}
		return []string{n.Value}
	case yaml.SequenceNode:
		var out []string
		for _, c := range n.Content {
			out = append(out, scalars(c)...)
		}
		return out
	case yaml.MappingNode:
		var out []string
		for i := 1; i < len(n.Content); i += 2 {
			out = append(out, scalars(n.Content[i])...)
		}
		return out
	case yaml.DocumentNode, yaml.AliasNode:
	}
	return nil
}
