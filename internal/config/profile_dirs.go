package config

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/jskswamy/aide/internal/fsutil"
	"github.com/jskswamy/aide/internal/harness"
	"github.com/jskswamy/aide/internal/homepath"
)

// ErrProfileDirAlias reports that two profiles resolve to the same native harness
// config dir, so their logins would not be isolated.
var ErrProfileDirAlias = errors.New("profiles share a native config dir")

// resolveDir canonicalizes a dir that may not exist yet. A var so tests
// can prove existing dirs are compared by identity, not by path.
var resolveDir = fsutil.ResolveNearest

// dirUse is one profile's native config dir for one harness.
type dirUse struct {
	profile string
	path    string      // resolveDir of the dir
	info    os.FileInfo // nil when the dir does not exist (yet)
}

func newDirUse(profile, dir string) dirUse {
	u := dirUse{profile: profile, path: resolveDir(dir)}
	if info, err := os.Stat(dir); err == nil {
		u.info = info
	}
	return u
}

// caseInsensitiveDirs reports whether dir names that differ only by
// case name one dir, as on default macOS and Windows filesystems. A var
// so tests can exercise both. On a case-sensitive macOS volume this
// refuses a case-only difference that would have worked.
var caseInsensitiveDirs = runtime.GOOS == "darwin" || runtime.GOOS == "windows"

// sameDir compares existing dirs by identity (os.SameFile catches
// symlinks, case-insensitive filesystems and hard links) and dirs that
// do not exist yet by their nearest-ancestor-resolved path, case-folded
// where the filesystem ignores case.
func (u dirUse) sameDir(o dirUse) bool {
	switch {
	case u.info != nil && o.info != nil:
		return os.SameFile(u.info, o.info)
	case u.info == nil && o.info == nil:
		if caseInsensitiveDirs {
			return strings.EqualFold(u.path, o.path)
		}
		return u.path == o.path
	}
	return false
}

// NativeConfigDir is the config dir harness h (canonical name) uses
// under this profile: ConfigDirs[h] when set, else the single
// ProfileDir pin (all harnesses), else ~/.<h>-<Profile>. Paths are
// tilde-expanded against home. Meaningful only when SelectsConfigDir.
func (c Context) NativeConfigDir(h, home string) string {
	if dir := c.ConfigDirs[h]; dir != "" {
		return homepath.Expand(dir, home)
	}
	return harness.ProfileConfigDir(h, c.Profile, c.ProfileDir, home)
}

// SelectsConfigDir reports whether the profile asks for its own native
// config dirs rather than the harnesses' default (shared) state.
func (c Context) SelectsConfigDir() bool {
	return c.Profile != "" || c.ProfileDir != "" || len(c.ConfigDirs) > 0
}

// ErrInvalidConfigDirs reports a malformed config_dirs map.
var ErrInvalidConfigDirs = errors.New("invalid config_dirs")

// validateConfigDirs checks every config_dirs map: keys are canonical
// names of harnesses that support profiles, values are paths under
// home, and the profile names its dirs (config_dir_name/profile) so
// harnesses without an entry still get a derived dir of their own.
func (c *Config) validateConfigDirs(home string) error {
	section, nameKey := "profiles", "config_dir_name"
	if !c.ProfileSchema {
		section, nameKey = "contexts", "profile"
	}
	for _, name := range sortedKeys(c.Contexts) {
		ctx := c.Contexts[name]
		if len(ctx.ConfigDirs) == 0 {
			continue
		}
		if ctx.Profile == "" {
			return fmt.Errorf("%w: %s.%s: config_dirs needs %s: as well; harnesses without an entry derive their dir from it",
				ErrInvalidConfigDirs, section, name, nameKey)
		}
		for _, h := range slices.Sorted(maps.Keys(ctx.ConfigDirs)) {
			d, ok := harness.Lookup(h)
			if !ok || d.Name != h || !d.SupportsProfiles() {
				return fmt.Errorf("%w: %s.%s: config_dirs.%s: not a harness with profile support (use one of: %s)",
					ErrInvalidConfigDirs, section, name, h, strings.Join(profileHarnesses(), ", "))
			}
			raw := ctx.ConfigDirs[h]
			dir := homepath.Expand(raw, home)
			if raw == "" || !filepath.IsAbs(dir) || !fsutil.IsUnderDir(dir, home) || filepath.Clean(dir) == filepath.Clean(home) {
				return fmt.Errorf("%w: %s.%s: config_dirs.%s: %q must be a dir under $HOME",
					ErrInvalidConfigDirs, section, name, h, raw)
			}
		}
	}
	return nil
}

func profileHarnesses() []string {
	var out []string
	for _, d := range harness.All() {
		if d.SupportsProfiles() {
			out = append(out, d.Name)
		}
	}
	return out
}

// validateProfileDirs fails when two profiles would run any
// profile-capable harness from the same config dir. Every harness is
// checked, not just each profile's agent:, because --agent can launch
// any profile under any harness.
func (c *Config) validateProfileDirs(home string) error {
	names := sortedKeys(c.Contexts)
	for _, h := range profileHarnesses() {
		var seen []dirUse
		for _, name := range names {
			ctx := c.Contexts[name]
			if ctx.Profile == "" {
				continue
			}
			dir := ctx.NativeConfigDir(h, home)
			if !filepath.IsAbs(dir) {
				continue // provisioning rejects it; never resolve against the cwd
			}
			use := newDirUse(name, dir)
			for _, prev := range seen {
				if prev.sameDir(use) {
					return c.profileDirAliasError(h, prev.profile, name, dir, home)
				}
			}
			seen = append(seen, use)
		}
	}
	return nil
}

// profileDirAliasError explains why profiles a and b collide on
// harness h's dir and how to separate them without losing a login.
func (c *Config) profileDirAliasError(h, a, b, dir, home string) error {
	nameKey := "config_dir_name"
	if !c.ProfileSchema {
		nameKey = "profile"
	}
	ctxA, ctxB := c.Contexts[a], c.Contexts[b]
	ha, okA := profileHarness(ctxA.Agent)
	hb, okB := profileHarness(ctxB.Agent)
	var msg strings.Builder
	fmt.Fprintf(&msg, "profiles %q (agent %s) and %q (agent %s) both resolve %s's config dir to %s",
		a, ctxA.Agent, b, ctxB.Agent, h, tildePath(dir, home))
	switch {
	case ha != h || hb != h:
		fmt.Fprintf(&msg, "; any profile can launch any harness (--agent %s), so both would use the same %s login", h, h)
		if okA && okB && ha != hb && sharesDerivedDirs(ctxA, ctxB) {
			// Each owns the login of its own agent; b takes a name of its
			// own but keeps its login, and a moves off b's login dir.
			fmt.Fprintf(&msg, ". To keep both logins, merge the two profiles into one, or separate them with "+
				"%s: %s, config_dirs: {%s: %s} on %q and config_dirs: {%s: %s} on %q",
				nameKey, b, hb, tildePath(ctxB.NativeConfigDir(hb, home), home), b,
				hb, tildePath(harness.ProfileConfigDir(hb, a, "", home), home), a)
		} else {
			fmt.Fprintf(&msg, ". To keep both logins, merge the two profiles into one, or add a config_dirs entry "+
				"for %s to one of them so it no longer resolves to %s, leaving each existing login dir in place",
				h, tildePath(dir, home))
		}
	default:
		fmt.Fprintf(&msg, ", so they would share one %s login. Merge the two profiles into one, or give one of them "+
			"its own %s: or a config_dirs entry for %s (it will need a fresh %s login there)", h, nameKey, h, h)
	}
	return fmt.Errorf("%w: %s", ErrProfileDirAlias, msg.String())
}

// sharesDerivedDirs reports whether a and b collide only because they
// derive every dir from the same name, with no pins of their own.
func sharesDerivedDirs(a, b Context) bool {
	return a.Profile == b.Profile && a.ProfileDir == "" && b.ProfileDir == "" &&
		len(a.ConfigDirs) == 0 && len(b.ConfigDirs) == 0
}

// profileHarness is the canonical name of agent's harness when it
// supports profiles.
func profileHarness(agent string) (string, bool) {
	d, ok := harness.Lookup(agent)
	if !ok || !d.SupportsProfiles() {
		return agent, false
	}
	return d.Name, true
}

// tildePath shows path relative to home as ~/..., as users write it.
func tildePath(path, home string) string {
	if rel, err := filepath.Rel(home, path); err == nil && home != "" && rel != ".." && !strings.HasPrefix(rel, "../") {
		return "~/" + rel
	}
	return path
}

// userHome is the home dir used for profile dir checks at load.
func userHome() (string, bool) {
	home, err := os.UserHomeDir()
	return home, err == nil && home != ""
}
