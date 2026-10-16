package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jskswamy/aide/internal/fsutil"
	"github.com/jskswamy/aide/internal/harness"
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

// sameDir compares existing dirs by identity (os.SameFile catches
// symlinks, case-insensitive filesystems and hard links) and dirs that
// do not exist yet by their nearest-ancestor-resolved path.
func (u dirUse) sameDir(o dirUse) bool {
	switch {
	case u.info != nil && o.info != nil:
		return os.SameFile(u.info, o.info)
	case u.info == nil && o.info == nil:
		return u.path == o.path
	}
	return false
}

// validateProfileDirs fails when two profiles would run any
// profile-capable harness from the same config dir. Every harness is
// checked, not just each profile's agent:, because --agent can launch
// any profile under any harness.
func (c *Config) validateProfileDirs(home string) error {
	names := sortedKeys(c.Contexts)
	for _, h := range harness.All() {
		if !h.SupportsProfiles() {
			continue
		}
		var seen []dirUse
		for _, name := range names {
			ctx := c.Contexts[name]
			if ctx.Profile == "" {
				continue
			}
			dir := harness.ProfileConfigDir(h.Name, ctx.Profile, ctx.ProfileDir, home)
			if !filepath.IsAbs(dir) {
				continue // provisioning rejects it; never resolve against the cwd
			}
			use := newDirUse(name, dir)
			for _, prev := range seen {
				if prev.sameDir(use) {
					return c.profileDirAliasError(h.Name, prev.profile, name, dir, home)
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
	nameKey, dirKey := "config_dir_name", "config_dir"
	if !c.ProfileSchema {
		nameKey, dirKey = "profile", "profile_dir"
	}
	ctxA, ctxB := c.Contexts[a], c.Contexts[b]
	var msg strings.Builder
	fmt.Fprintf(&msg, "profiles %q (agent %s) and %q (agent %s) both resolve %s's config dir to %s",
		a, ctxA.Agent, b, ctxB.Agent, h, tildePath(dir, home))
	if harnessName(ctxA.Agent) != h || harnessName(ctxB.Agent) != h {
		fmt.Fprintf(&msg, "; any profile can launch any harness (--agent %s), so both would use the same %s login", h, h)
	} else {
		fmt.Fprintf(&msg, ", so they would share one %s login", h)
	}
	curA, okA := currentProfileDir(ctxA, home)
	curB, okB := currentProfileDir(ctxB, home)
	if okA && okB && !newDirUse(a, curA).sameDir(newDirUse(b, curB)) {
		fmt.Fprintf(&msg, ". To keep both logins, merge the two profiles into one, or pin each to the dir it uses today: "+
			"%s: %s on %q and %s: %s on %q",
			dirKey, tildePath(curA, home), a, dirKey, tildePath(curB, home), b)
	} else {
		fmt.Fprintf(&msg, ". Merge the two profiles into one, or give one of them its own %s/%s "+
			"(its harness will need a fresh login there)", nameKey, dirKey)
	}
	return fmt.Errorf("%w: %s", ErrProfileDirAlias, msg.String())
}

// harnessName is the canonical harness name for agent, or agent itself
// when it is not a known harness.
func harnessName(agent string) string {
	if d, ok := harness.Lookup(agent); ok {
		return d.Name
	}
	return agent
}

// currentProfileDir is the dir ctx uses with its own agent:, the one
// holding its login today.
func currentProfileDir(ctx Context, home string) (string, bool) {
	d, ok := harness.Lookup(ctx.Agent)
	if !ok || !d.SupportsProfiles() {
		return "", false
	}
	return harness.ProfileConfigDir(d.Name, ctx.Profile, ctx.ProfileDir, home), true
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
