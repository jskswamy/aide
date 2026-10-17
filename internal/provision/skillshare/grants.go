package skillshare

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/fsutil"
	"github.com/jskswamy/aide/internal/harness"
	"github.com/jskswamy/aide/pkg/seatbelt/modules"
)

// ConfigDir is Skillshare's config dir: $XDG_CONFIG_HOME/skillshare,
// else ~/.config/skillshare. It holds config.yaml (unless
// $SKILLSHARE_CONFIG moves it) and the default skills/ and agents/.
func ConfigDir(home string, getenv func(string) string) string {
	return filepath.Join(xdgDir(getenv("XDG_CONFIG_HOME"), home, ".config"), "skillshare")
}

// stateDir is Skillshare's state dir: $XDG_STATE_HOME/skillshare, else
// ~/.local/state/skillshare.
func stateDir(home string, getenv func(string) string) string {
	return filepath.Join(xdgDir(getenv("XDG_STATE_HOME"), home, ".local/state"), "skillshare")
}

// xdgDir is an XDG base dir: v when it is absolute (the spec ignores
// relative values), else home/def.
func xdgDir(v, home, def string) string {
	if filepath.IsAbs(v) {
		return v
	}
	return filepath.Join(home, filepath.FromSlash(def))
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
	g := grantChecker{home: fsutil.ResolveNearest(home), protected: protectedDirs(cfg, home, getenv)}
	var out []string
	add := func(literal string, underHome bool) {
		for _, p := range g.grant(literal, underHome) {
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

// grantChecker decides which paths the capability may grant.
type grantChecker struct {
	home      string   // resolved
	protected []string // resolved
}

// grant returns literal's resolved path (and literal, when it
// differs) if it exists and may be granted, else nothing. underHome
// requires the resolved path to lie strictly inside $HOME.
func (g grantChecker) grant(literal string, underHome bool) []string {
	if !filepath.IsAbs(literal) || strings.Contains(literal, "{{") {
		return nil
	}
	literal = filepath.Clean(literal)
	resolved, err := filepath.EvalSymlinks(literal)
	if err != nil {
		return nil // missing, or a broken or looping link
	}
	if strings.Contains(resolved, "{{") || !g.allowed(resolved, underHome) {
		return nil
	}
	if resolved == literal {
		return []string{resolved}
	}
	return []string{resolved, literal}
}

func (g grantChecker) allowed(p string, underHome bool) bool {
	if p == string(filepath.Separator) || p == g.home {
		return false
	}
	if underHome && !fsutil.IsUnderDir(p, g.home) {
		return false
	}
	for _, d := range g.protected {
		if fsutil.IsUnderDir(p, d) || fsutil.IsUnderDir(d, p) {
			return false
		}
	}
	return true
}

// protectedDirs are the dirs no skillshare grant may overlap, resolved:
// the sensitive home dirs, every harness's default config dir, every
// profile's config dirs (and dirs its env: points a harness at), and
// aide's config dir.
func protectedDirs(cfg *config.Config, home string, getenv func(string) string) []string {
	var dirs []string
	add := func(d string) {
		if !filepath.IsAbs(d) {
			return
		}
		if r := fsutil.ResolveNearest(d); !slices.Contains(dirs, r) {
			dirs = append(dirs, r)
		}
	}
	for _, rel := range modules.SensitiveHomeDirs() {
		add(filepath.Join(home, filepath.FromSlash(rel)))
	}
	add(config.DirFrom(xdgDir(getenv("XDG_CONFIG_HOME"), home, ".config")))
	all := harness.All()
	for _, d := range all {
		add(d.DefaultDir(home))
	}
	if cfg == nil {
		return dirs
	}
	for _, name := range slices.Sorted(maps.Keys(cfg.Contexts)) {
		ctx := cfg.Contexts[name]
		for _, d := range all {
			if ctx.SelectsConfigDir() && d.SupportsProfiles() {
				add(ctx.NativeConfigDir(d.Name, home))
			}
			for _, key := range d.ConfigEnvKeys {
				if v := ctx.Env[key]; v != "" && !strings.Contains(v, "{{") {
					add(expand(v, home))
				}
			}
		}
	}
	return dirs
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
