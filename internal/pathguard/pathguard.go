// Package pathguard validates candidate filesystem paths for dynamic
// capability resolvers against sensitive, harness, aide, and system
// directories, and validates Seatbelt path strings against S-expression
// injection.
package pathguard

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/fsutil"
	"github.com/jskswamy/aide/internal/harness"
	"github.com/jskswamy/aide/internal/homepath"
	"github.com/jskswamy/aide/pkg/seatbelt/modules"
)

// systemRoots are host/system directories that writable env-derived grants
// must never target or overlap.
var systemRoots = []string{
	"/etc",
	"/var/run",
	"/private/var/run",
	"/usr",
	"/System",
	"/Library",
	"/tmp",
	"/private/tmp",
}

// Checker validates candidate filesystem paths before a dynamic capability
// resolver grants read, write, or Seatbelt socket access to them.
type Checker struct {
	home          string   // resolved $HOME
	protected     []string // resolved protected dirs
	writableRoots []string // resolved strict parent roots for env-derived writable dirs
	systemRoots   []string // resolved system roots
}

// NewChecker builds a Checker for home using cfg and getenv to discover
// protected directories (sensitive home dirs, harness default/profile config
// dirs, aide's config dir) and permitted per-user writable roots.
func NewChecker(cfg *config.Config, home string, getenv func(string) string) Checker {
	resolvedHome := fsutil.ResolveNearest(home)
	c := Checker{
		home:          resolvedHome,
		protected:     protectedDirs(cfg, home, getenv),
		writableRoots: writableRoots(resolvedHome),
	}
	for _, sr := range systemRoots {
		r := fsutil.ResolveNearest(sr)
		if !slices.Contains(c.systemRoots, r) {
			c.systemRoots = append(c.systemRoots, r)
		}
		if !slices.Contains(c.systemRoots, sr) {
			c.systemRoots = append(c.systemRoots, sr)
		}
	}
	return c
}

// Grant returns literal's resolved path (and literal, when it differs) if it
// exists and may be granted, else nothing. underHome requires the resolved
// path to lie strictly inside $HOME.
func (c Checker) Grant(literal string, underHome bool) []string {
	resolved, clean, ok := c.resolve(literal)
	if !ok || !c.allowed(resolved, underHome) {
		return nil
	}
	if resolved == clean {
		return []string{resolved}
	}
	return []string{resolved, clean}
}

// GrantDir is like Grant but additionally requires the resolved target to be
// an existing directory. When requireWritableRoot is true, the resolved path
// must lie strictly inside one of the per-user writable roots ($HOME,
// /run/user/<uid>, or the current user's os.TempDir() tree) and must not fall
// under any system root.
func (c Checker) GrantDir(literal string, requireWritableRoot bool) []string {
	resolved, clean, ok := c.resolve(literal)
	if !ok {
		return nil
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return nil
	}
	if !c.allowed(resolved, false) {
		return nil
	}
	underHome := resolved != c.home && fsutil.IsUnderDir(resolved, c.home)
	if !underHome && c.isSystemRoot(resolved) {
		return nil
	}
	if requireWritableRoot && !c.allowedWritableRoot(resolved) {
		return nil
	}
	if resolved == clean {
		return []string{resolved}
	}
	return []string{resolved, clean}
}

// SafeSeatbeltPath reports whether p is safe to interpolate inside a quoted
// Seatbelt S-expression string literal (e.g. (subpath "<p>")). It rejects
// empty strings, quotes, backslashes, parentheses, newlines, NUL, and any
// ASCII control characters.
func SafeSeatbeltPath(p string) bool {
	if p == "" {
		return false
	}
	for i := 0; i < len(p); i++ {
		b := p[i]
		if b < 0x20 || b == 0x7f || b == '"' || b == '\\' || b == '(' || b == ')' {
			return false
		}
	}
	return true
}

func (c Checker) resolve(literal string) (resolved, clean string, ok bool) {
	if !filepath.IsAbs(literal) || strings.Contains(literal, "{{") {
		return "", "", false
	}
	clean = filepath.Clean(literal)
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return "", "", false // missing, or a broken or looping link
	}
	if strings.Contains(resolved, "{{") {
		return "", "", false
	}
	return resolved, clean, true
}

func (c Checker) allowed(p string, underHome bool) bool {
	if p == string(filepath.Separator) || p == c.home {
		return false
	}
	if underHome && !fsutil.IsUnderDir(p, c.home) {
		return false
	}
	for _, d := range c.protected {
		if fsutil.IsUnderDir(p, d) || fsutil.IsUnderDir(d, p) {
			return false
		}
	}
	return true
}

func (c Checker) isSystemRoot(p string) bool {
	for _, sr := range c.systemRoots {
		if fsutil.IsUnderDir(p, sr) || fsutil.IsUnderDir(sr, p) {
			return true
		}
	}
	return false
}

func (c Checker) allowedWritableRoot(p string) bool {
	if p != c.home && fsutil.IsUnderDir(p, c.home) {
		return true
	}
	if c.isSystemRoot(p) {
		return false
	}
	for _, root := range c.writableRoots {
		if p != root && fsutil.IsUnderDir(p, root) {
			return true
		}
	}
	return false
}

func writableRoots(resolvedHome string) []string {
	var roots []string
	add := func(d string) {
		if d == "" || !filepath.IsAbs(d) {
			return
		}
		r := fsutil.ResolveNearest(d)
		if r == string(filepath.Separator) || r == "/tmp" || r == "/private/tmp" {
			return
		}
		if !slices.Contains(roots, r) {
			roots = append(roots, r)
		}
	}
	add(resolvedHome)
	add(fmt.Sprintf("/run/user/%d", os.Getuid()))
	add(os.TempDir())
	return roots
}

// protectedDirs are the dirs no dynamic capability grant may overlap,
// resolved: the sensitive home dirs, every harness's default config dir,
// every profile's config dirs (and dirs its env: points a harness at), and
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
	add(config.DirFrom(homepath.XDG(getenv("XDG_CONFIG_HOME"), home, ".config")))
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
					add(homepath.Expand(v, home))
				}
			}
		}
	}
	return dirs
}
