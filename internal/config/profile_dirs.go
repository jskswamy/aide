package config

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/jskswamy/aide/internal/fsutil"
	"github.com/jskswamy/aide/internal/harness"
	"github.com/jskswamy/aide/internal/homepath"
)

// ErrProfileDirAlias reports that two profiles resolve to the same native harness
// config dir, so their logins would not be isolated.
var ErrProfileDirAlias = errors.New("profiles share a native config dir")

// dirUse is one profile's native config dir for one harness.
type dirUse struct {
	profile string
	dir     string
	global  bool // the profile selects no dir: the harness default
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
// any profile under any harness. A profile selecting no dir runs from
// the harness's default dir, or the dir its env: names; several
// profiles on the default dir load (GlobalLoginError refuses their
// launch), but a profile pinned to that dir would look isolated while
// sharing their login, so it fails.
func (c *Config) validateProfileDirs(home string) error {
	names := sortedKeys(c.Contexts)
	for _, d := range harness.All() {
		if !d.SupportsProfiles() {
			continue
		}
		h := d.Name
		globals := c.globalLoginProfiles(h, home)
		var seen []dirUse
		for _, name := range names {
			ctx := c.Contexts[name]
			dir, global := ctx.NativeConfigDir(h, home), false
			switch {
			case ctx.SelectsConfigDir():
				if !filepath.IsAbs(dir) {
					continue // provisioning rejects it; never resolve against the cwd
				}
			case slices.Contains(globals, name):
				dir, global = d.DefaultDir(home), true
			case envDir(ctx, d, home) != "":
				dir = envDir(ctx, d, home)
			default:
				continue
			}
			use := dirUse{profile: name, dir: dir, global: global}
			for _, prev := range seen {
				if fsutil.SameDir(prev.dir, use.dir) && (!prev.global || !use.global) {
					return c.profileDirAliasError(h, prev.profile, name, dir, home)
				}
			}
			seen = append(seen, use)
		}
	}
	return nil
}

// globalLoginProfiles lists, sorted, the profiles that run harness h
// from its default (global) config dir (onGlobalDir) and could select
// a dir of their own: a profile whose own agent has no profile support
// is left out, and reaches h only through --agent (see
// GlobalLoginError). nil for a harness without profile support.
func (c *Config) globalLoginProfiles(h, home string) []string {
	d, ok := harness.Lookup(h)
	if !ok || !d.SupportsProfiles() || d.DefaultConfigDir == "" {
		return nil
	}
	var out []string
	for _, name := range sortedKeys(c.Contexts) {
		ctx := c.Contexts[name]
		if c.canSelectDir(ctx) && onGlobalDir(ctx, d, home) {
			out = append(out, name)
		}
	}
	return out
}

// canSelectDir reports whether ctx's own agent supports a config dir
// of its own (or names none, so its harness is not known yet).
func (c *Config) canSelectDir(ctx Context) bool {
	own, ok := harness.Lookup(c.HarnessOf(ctx.Agent))
	return ctx.Agent == "" || ok && own.SupportsProfiles()
}

// onGlobalDir reports whether ctx runs harness d from its default dir:
// it selects no config dir, and sets d's dir variable in env: either
// not at all or to the default dir itself.
func onGlobalDir(ctx Context, d harness.Descriptor, home string) bool {
	if ctx.SelectsConfigDir() {
		return false
	}
	v, redirected := ctx.Env[d.ProfileEnvKey]
	return !redirected || home != "" && sameDirPath(homepath.Expand(v, home), d.DefaultDir(home))
}

// envDir is the dir ctx's env: sets d's dir variable to, or "" when
// ctx selects its own dirs (they win at launch), the variable is unset,
// templated or relative, or it names d's default dir (onGlobalDir).
func envDir(ctx Context, d harness.Descriptor, home string) string {
	v := ctx.Env[d.ProfileEnvKey]
	dir := homepath.Expand(v, home)
	if ctx.SelectsConfigDir() || v == "" || strings.Contains(v, "{{") || !filepath.IsAbs(dir) || onGlobalDir(ctx, d, home) {
		return ""
	}
	return dir
}

// pinsGlobalDir reports whether ctx selects its own dirs but pins
// harness d's to d's default dir.
func pinsGlobalDir(ctx Context, d harness.Descriptor, home string) bool {
	return ctx.SelectsConfigDir() && home != "" && sameDirPath(ctx.NativeConfigDir(d.Name, home), d.DefaultDir(home))
}

func sameDirPath(a, b string) bool {
	return filepath.IsAbs(a) && fsutil.SameDir(a, b)
}

// configDirName is name as a valid config_dir_name: value.
func configDirName(name string) string {
	s := invalidDirNameChars.ReplaceAllString(name, "-")
	if len(s) > 64 {
		s = s[:64]
	}
	return s
}

var invalidDirNameChars = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// IsolateNewProfile gives ctx, a profile about to be added as name,
// its own config dirs (config_dir_name:) when another profile already
// runs its harness from the global default dir, so the two never share
// one login. The dir name gets a numeric suffix when another profile
// already uses it. It returns those other profiles (nil when ctx is
// left on the global dir).
func (c *Config) IsolateNewProfile(name string, ctx *Context) []string {
	if ctx.SelectsConfigDir() {
		return nil
	}
	home, _ := userHome()
	others := slices.DeleteFunc(c.globalLoginProfiles(c.HarnessOf(ctx.Agent), home), func(n string) bool { return n == name })
	if len(others) == 0 {
		return nil
	}
	base := configDirName(name)
	ctx.Profile = base
	for n := 2; c.dirNameTaken(name, ctx.Profile); n++ {
		suffix := fmt.Sprintf("-%d", n)
		ctx.Profile = base[:min(len(base), 64-len(suffix))] + suffix
	}
	return others
}

// dirNameTaken reports whether a profile other than name uses dir
// name dirName.
func (c *Config) dirNameTaken(name, dirName string) bool {
	for n, ctx := range c.Contexts {
		if n != name && ctx.Profile == dirName {
			return true
		}
	}
	return false
}

// GlobalLoginError reports, wrapping ErrProfileDirAlias, that launching
// profile name under harness h (the launch's final harness) would run
// h from its default dir while another profile uses that dir too, so
// they would share one login. Another profile counts when it pins h's
// dir to the default, or runs h from it as a profile that could select
// a dir of its own; one that cannot (its own agent has no profile
// support) counts only against a launch under a harness other than
// name's own agent, so creating such a profile never blocks the
// others' own launches. Launches refuse it; the config still loads.
func (c *Config) GlobalLoginError(name, h, home string) error {
	d, ok := harness.Lookup(h)
	ctx, known := c.Contexts[name]
	if !ok || !d.SupportsProfiles() || d.DefaultConfigDir == "" || !known ||
		!onGlobalDir(ctx, d, home) && !pinsGlobalDir(ctx, d, home) {
		return nil
	}
	ownLaunch := c.HarnessOf(ctx.Agent) == d.Name || ctx.Agent == ""
	users := []string{name}
	var pinned []string
	for _, q := range sortedKeys(c.Contexts) {
		qc := c.Contexts[q]
		switch {
		case q == name:
			if pinsGlobalDir(qc, d, home) {
				pinned = append(pinned, q)
			}
		case pinsGlobalDir(qc, d, home):
			users, pinned = append(users, q), append(pinned, q)
		case onGlobalDir(qc, d, home) && (c.canSelectDir(qc) || !ownLaunch):
			users = append(users, q)
		}
	}
	if len(users) < 2 {
		return nil
	}
	slices.Sort(users)
	section, nameKey := "profiles", "config_dir_name"
	if !c.ProfileSchema {
		section, nameKey = "contexts", "profile"
	}
	keep := users[0]
	switch {
	case len(pinned) > 0:
		keep = pinned[0]
	case slices.Contains(users, c.DefaultContext):
		keep = c.DefaultContext
	}
	defaultDir := homepath.Tilde(d.DefaultDir(home), home)
	var fixes []string
	for _, u := range users {
		switch uc := c.Contexts[u]; {
		case u == keep:
		case slices.Contains(pinned, u):
			fixes = append(fixes, fmt.Sprintf("%s.%s: point its %s dir away from %s", section, u, d.Name, defaultDir))
		case !c.canSelectDir(uc):
			fixes = append(fixes, fmt.Sprintf("%s.%s: launch it without --agent %s, or add `env: {%s: ~/.%s-%s}` (its agent %s cannot select a config dir)",
				section, u, d.Name, d.ProfileEnvKey, d.Name, configDirName(u), uc.Agent))
		default:
			fixes = append(fixes, fmt.Sprintf("%s.%s: add `%s: %s`", section, u, nameKey, configDirName(u)))
		}
	}
	return fmt.Errorf("%w: profiles %q all run %s from its global login %s, so they would share one %s login. "+
		"To keep %q on that login, edit your aide config: %s. Each of those then needs a fresh %s login in its own dir",
		ErrProfileDirAlias, users, d.Name, defaultDir, d.Name, keep, strings.Join(fixes, "; "), d.Name)
}

// profileDirAliasError explains why profiles a and b collide on
// harness h's dir and how to separate them without losing a login.
func (c *Config) profileDirAliasError(h, a, b, dir, home string) error {
	nameKey := "config_dir_name"
	if !c.ProfileSchema {
		nameKey = "profile"
	}
	ctxA, ctxB := c.Contexts[a], c.Contexts[b]
	d, _ := harness.Lookup(h)
	for _, p := range []string{b, a} {
		if envDir(c.Contexts[p], d, home) != "" {
			other := a
			if p == a {
				other = b
			}
			return fmt.Errorf("%w: profile %q sets env: {%s: %s}, a dir profile %q also runs %s from, so the two would "+
				"share one %s login. Point %q's %s at a dir no other profile uses (it will need a fresh %s login there)",
				ErrProfileDirAlias, p, d.ProfileEnvKey, c.Contexts[p].Env[d.ProfileEnvKey], other, h, h, p, d.ProfileEnvKey, h)
		}
	}
	if !ctxA.SelectsConfigDir() || !ctxB.SelectsConfigDir() {
		pinned, global := a, b
		if !ctxA.SelectsConfigDir() {
			pinned, global = b, a
		}
		return fmt.Errorf("%w: profile %q pins %s's config dir to %s, its global login, which profile %q also uses because it "+
			"selects no config dir, so the two would share one %s login while %q looks isolated. Keep the global login for one "+
			"of them: give %q its own %s: (it will need a fresh %s login there), or remove the pin from %q",
			ErrProfileDirAlias, pinned, h, homepath.Tilde(dir, home), global, h, pinned, global, nameKey, h, pinned)
	}
	ha, okA := profileHarness(c.HarnessOf(ctxA.Agent))
	hb, okB := profileHarness(c.HarnessOf(ctxB.Agent))
	var msg strings.Builder
	fmt.Fprintf(&msg, "profiles %q (agent %s) and %q (agent %s) both resolve %s's config dir to %s",
		a, ctxA.Agent, b, ctxB.Agent, h, homepath.Tilde(dir, home))
	switch {
	case ha != h || hb != h:
		fmt.Fprintf(&msg, "; any profile can launch any harness (--agent %s), so both would use the same %s login", h, h)
		if okA && okB && ha != hb && sharesDerivedDirs(ctxA, ctxB) {
			// Each owns the login of its own agent; b takes a name of its
			// own but keeps its login, and a moves off b's login dir.
			fmt.Fprintf(&msg, ". To keep both logins, merge the two profiles into one, or separate them with "+
				"%s: %s, config_dirs: {%s: %s} on %q and config_dirs: {%s: %s} on %q",
				nameKey, b, hb, homepath.Tilde(ctxB.NativeConfigDir(hb, home), home), b,
				hb, homepath.Tilde(harness.ProfileConfigDir(hb, a, "", home), home), a)
		} else {
			fmt.Fprintf(&msg, ". To keep both logins, merge the two profiles into one, or add a config_dirs entry "+
				"for %s to one of them so it no longer resolves to %s, leaving each existing login dir in place",
				h, homepath.Tilde(dir, home))
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

// userHome is the home dir used for profile dir checks at load.
func userHome() (string, bool) {
	home, err := os.UserHomeDir()
	return home, err == nil && home != ""
}
