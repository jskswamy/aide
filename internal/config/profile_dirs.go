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

// DefaultProfileName is the reserved name of the built-in profile that always
// uses each harness's native configuration directory (~/.claude, ~/.codex,
// ~/.pi/agent, ~/.omp/agent, ~/.config/opencode).
const DefaultProfileName = "default"

// IsDefaultProfile reports whether name is the reserved built-in "default" profile.
func IsDefaultProfile(name string) bool {
	return name == DefaultProfileName
}

// dirUse is one profile's native config dir for one harness.
type dirUse struct {
	profile string
	dir     string
	global  bool // the profile selects no dir: the harness default
}

// NativeConfigDir is the config dir harness h (canonical name) uses
// under this profile: ConfigDirs[h] when set, else the single
// ProfileDir pin (all harnesses), else ~/.<h>-<Profile>, or the
// harness's default dir when the profile does not select an isolated
// config dir. Paths are tilde-expanded against home.
func (c Context) NativeConfigDir(h, home string) string {
	if dir := c.ConfigDirs[h]; dir != "" {
		return homepath.Expand(dir, home)
	}
	if !c.SelectsConfigDir() {
		if d, ok := harness.Lookup(h); ok {
			return d.DefaultDir(home)
		}
		return ""
	}
	return harness.ProfileConfigDir(h, c.Profile, c.ProfileDir, home)
}

// SelectsConfigDir reports whether the profile asks for its own native
// config dirs rather than the harnesses' default (shared) state.
func (c Context) SelectsConfigDir() bool {
	return c.Profile != "" || c.ProfileDir != "" || len(c.ConfigDirs) > 0
}

var harnessSandboxEnv = func(d harness.Descriptor, home string) map[string]string {
	return d.SandboxEnv(home, nil)
}

// LaunchConfigDir is the native config dir a launch of harness h uses
// under this profile: the profile's own dir when it selects one, else
// the harness's dir variable from the profile's env:, then from the
// harness's sandbox module when sandboxing is enabled, else its
// default dir. "" for a harness without a known config dir.
func (c Context) LaunchConfigDir(h, home string, _ func(string) string) string {
	if c.SelectsConfigDir() {
		return c.NativeConfigDir(h, home)
	}
	d, ok := harness.Lookup(h)
	if !ok || d.ProfileEnvKey == "" || d.DefaultConfigDir == "" {
		return ""
	}
	if v := c.Env[d.ProfileEnvKey]; v != "" {
		return homepath.Expand(v, home)
	}
	if c.Sandbox == nil || (!c.Sandbox.Disabled && c.Sandbox.ProfileName != "none") {
		if v := harnessSandboxEnv(d, home)[d.ProfileEnvKey]; v != "" {
			return homepath.Expand(v, home)
		}
	}
	return d.DefaultDir(home)
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

// validateProfileDirs fails when any profile uses a harness's global
// default config dir or when two profiles would run any profile-capable
// harness from the same config dir. Every harness is checked, not just
// each profile's agent:, because --agent can launch any profile under
// any harness.
func (c *Config) validateProfileDirs(home string) error {
	names := sortedKeys(c.Contexts)
	for _, d := range harness.All() {
		if !d.SupportsProfiles() {
			continue
		}
		h := d.Name
		var seen []dirUse
		for _, name := range names {
			ctx := c.Contexts[name]
			if d.DefaultConfigDir != "" && (pinsGlobalDir(ctx, d, home) || envPinsGlobalDir(ctx, d, home)) {
				return fmt.Errorf("%w: profile %q cannot use %s's global default config dir %s; use an isolated per-profile directory (e.g. ~/.%s-%s) instead",
					ErrProfileDirAlias, name, h, homepath.Tilde(d.DefaultDir(home), home), h, configDirName(name))
			}
			dir, global := ctx.NativeConfigDir(h, home), false
			switch {
			case ctx.SelectsConfigDir():
				if !filepath.IsAbs(dir) {
					continue // provisioning rejects it; never resolve against the cwd
				}
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

// ApplyDefaultProfileDir sets ctx.Profile to name's default config_dir_name
// when ctx does not already name one, its harness supports profile dirs, and
// its env does not explicitly set the harness's profile env var.
func (c *Config) ApplyDefaultProfileDir(name string, ctx *Context) {
	var harnessOf func(string) string
	if c != nil {
		harnessOf = c.HarnessOf
	}
	applyDefaultProfileDir(name, ctx, harnessOf)
}

func (c *Config) applyDefaultProfileDirs() {
	for name, ctx := range c.Contexts {
		applyDefaultProfileDir(name, &ctx, c.HarnessOf)
		c.Contexts[name] = ctx
	}
}

func applyDefaultProfileDir(name string, ctx *Context, harnessOf func(string) string) {
	if IsDefaultProfile(name) {
		if stripDefaultConfigDirOverrides(ctx) {
			ctx.IgnoredDefaultConfigDir = true
		}
		return
	}
	if name == "" || ctx.Profile != "" {
		return
	}
	h := ctx.Agent
	if harnessOf != nil {
		h = harnessOf(ctx.Agent)
	}
	d, ok := harness.Lookup(h)
	if ctx.Agent != "" && (!ok || !d.SupportsProfiles()) {
		return
	}
	if !ctx.SelectsConfigDir() && hasExplicitProfileEnv(*ctx) {
		return
	}
	ctx.Profile = configDirName(name)
}

func stripDefaultConfigDirOverrides(ctx *Context) bool {
	if ctx == nil {
		return false
	}
	stripped := ctx.Profile != "" || ctx.ProfileDir != "" || len(ctx.ConfigDirs) > 0
	ctx.Profile = ""
	ctx.ProfileDir = ""
	ctx.ConfigDirs = nil
	for _, d := range harness.All() {
		for _, key := range d.ConfigEnvKeys {
			if _, ok := ctx.Env[key]; ok {
				delete(ctx.Env, key)
				stripped = true
			}
		}
	}
	if len(ctx.Env) == 0 {
		ctx.Env = nil
	}
	return stripped
}

func hasExplicitProfileEnv(ctx Context) bool {
	for _, d := range harness.All() {
		if d.ProfileEnvKey != "" {
			if _, hasEnv := ctx.Env[d.ProfileEnvKey]; hasEnv {
				return true
			}
		}
	}
	return false
}

func envPinsGlobalDir(ctx Context, d harness.Descriptor, home string) bool {
	if ctx.SelectsConfigDir() || d.ProfileEnvKey == "" || home == "" {
		return false
	}
	v, ok := ctx.Env[d.ProfileEnvKey]
	return ok && sameDirPath(homepath.Expand(v, home), d.DefaultDir(home))
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
	// A templated value ({{ .ProjectRoot }}) resolves per launch, so
	// there is no one dir to compare at load.
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
