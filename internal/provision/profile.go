package provision

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/harness"
	"github.com/jskswamy/aide/internal/homepath"
)

// Profile-related sentinel errors. Callers branch via errors.Is() —
// wrappers preserve the sentinel so rewording the user-facing
// message doesn't break tests.
var (
	// ErrProfileNotSupported: the harness has no ProfileEnvKey.
	ErrProfileNotSupported = errors.New("profile not supported by agent")

	// ErrProfileNameInvalid: profile value fails charset/length rules
	// or profile_dir was supplied without profile.
	ErrProfileNameInvalid = errors.New("invalid profile name")

	// ErrProfileDirOutsideHome: profile_dir resolves outside $HOME
	// after tilde-expansion.
	ErrProfileDirOutsideHome = errors.New("profile_dir must be under $HOME")

	// ErrProfileConflict: both profile and the agent's env var are
	// declared in env:.
	ErrProfileConflict = errors.New("profile and explicit env var both declared")

	// ErrProfileNotProjectScoped: profile / profile_dir declared in
	// .aide.yaml (project override). profile is a user-side decision.
	ErrProfileNotProjectScoped = errors.New("profile not allowed in project override")
)

// validProfileName matches the allowed charset/length for a profile
// name: [a-zA-Z0-9_-]+ up to 64 characters.
var validProfileName = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// ValidateProfile runs all profile-related checks for a single
// resolved context. fromProjectOverride is true when the context's
// profile/profile_dir came from a .aide.yaml (project override) —
// such cases are rejected unconditionally because profile is a
// user-side decision.
func ValidateProfile(ctx config.Context, homeDir string, fromProjectOverride bool) error {
	if fromProjectOverride && (ctx.Profile != "" || ctx.ProfileDir != "") {
		return fmt.Errorf("%w: .aide.yaml may not set profile/profile_dir", ErrProfileNotProjectScoped)
	}
	if ctx.Profile == "" {
		if ctx.ProfileDir != "" {
			return fmt.Errorf("%w: profile_dir requires profile to be set", ErrProfileNameInvalid)
		}
		return nil
	}
	if !validProfileName.MatchString(ctx.Profile) {
		return fmt.Errorf("%w: %q (allowed: [a-zA-Z0-9_-]+, max 64 chars)",
			ErrProfileNameInvalid, ctx.Profile)
	}
	envKey, absPath, err := harnessProfile(ctx.Agent, ctx.Profile, ctx.ProfileDir, homeDir)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(absPath, homeDir+string(filepath.Separator)) {
		return fmt.Errorf("%w: %q resolves to %q",
			ErrProfileDirOutsideHome, ctx.ProfileDir, absPath)
	}
	if _, declared := ctx.Env[envKey]; declared {
		return fmt.Errorf("%w: both `profile: %s` and env.%s declared (pick one)",
			ErrProfileConflict, ctx.Profile, envKey)
	}
	return nil
}

// InjectProfileEnv validates and injects the per-profile env var
// into env when ctx.Profile is set. Returns env unchanged when
// ctx.Profile is empty. Caller is responsible for passing a writable
// (or nil) env map — when nil, a new map is returned only if profile
// is set, otherwise nil is returned. Errors propagate from
// ValidateProfile or the harness table.
func InjectProfileEnv(ctx config.Context, env map[string]string, homeDir string) (map[string]string, error) {
	if ctx.Profile == "" && ctx.ProfileDir == "" {
		return env, nil
	}
	if err := ValidateProfile(ctx, homeDir, false); err != nil {
		return env, err
	}
	envKey, absPath, err := harnessProfile(ctx.Agent, ctx.Profile, ctx.ProfileDir, homeDir)
	if err != nil {
		return env, err
	}
	if env == nil {
		env = map[string]string{}
	}
	env[envKey] = absPath
	return env, nil
}

// WithAgent returns ctx with its agent replaced, as `--agent` does.
// When ctx selects a profile, the profile owns the config-dir choice,
// so the previous agent's dir key is dropped from a copy of Env rather
// than leaking into the new agent's environment.
func WithAgent(ctx config.Context, agent string) config.Context {
	prev := ctx.Agent
	ctx.Agent = agent
	if ctx.Profile == "" || prev == agent {
		return ctx
	}
	prevKey := ProfileEnvKeyFor(prev)
	if _, ok := ctx.Env[prevKey]; !ok || prevKey == ProfileEnvKeyFor(agent) {
		return ctx
	}
	env := make(map[string]string, len(ctx.Env))
	for k, v := range ctx.Env {
		if k != prevKey {
			env[k] = v
		}
	}
	ctx.Env = env
	return ctx
}

// profileUnsupportedError is the user-facing error for a profile
// requested on a harness without profile support. It unwraps to
// ErrProfileNotSupported.
type profileUnsupportedError struct{ harness string }

func (e *profileUnsupportedError) Error() string {
	return fmt.Sprintf("Profiles are not supported for harness %q", e.harness)
}

func (e *profileUnsupportedError) Unwrap() error { return ErrProfileNotSupported }

// harnessProfile resolves the env var and absolute config-dir path
// for profile name on agent from the harness table. Agents with no
// descriptor or no profile support get a profileUnsupportedError.
func harnessProfile(agent, name, override, homeDir string) (envKey, absPath string, err error) {
	d, ok := harness.Lookup(agent)
	if !ok || !d.SupportsProfiles() {
		return "", "", &profileUnsupportedError{harness: agent}
	}
	return d.ProfileEnvKey, profileDir(d.Name, name, override, homeDir), nil
}

// profileDir is override tilde-expanded, or ~/.<agent>-<name>.
func profileDir(agent, name, override, homeDir string) string {
	if override != "" {
		return homepath.Expand(override, homeDir)
	}
	return filepath.Join(homeDir, fmt.Sprintf(".%s-%s", agent, name))
}

// Profile resolves the env var and absolute config-dir path for a
// profile name. override is non-empty when the user supplied
// profile_dir. Returns ErrProfileNotSupported (wrapped) when the
// driver does not advertise a ProfileEnvKey.
func (d DriverBase) Profile(name, override, homeDir string) (envKey, absPath string, err error) {
	if d.Caps.ProfileEnvKey == "" {
		return "", "", fmt.Errorf(
			"%w: %q (env var does not isolate the full config tree)",
			ErrProfileNotSupported, d.Caps.AgentName,
		)
	}
	return d.Caps.ProfileEnvKey, profileDir(d.Caps.AgentName, name, override, homeDir), nil
}

// ProfileEnvKeyFor returns the env var that relocates agent's config
// dir (e.g. CLAUDE_CONFIG_DIR), or "" when the harness has no profile
// support or is unknown.
func ProfileEnvKeyFor(agent string) string {
	d, _ := harness.Lookup(agent)
	return d.ProfileEnvKey
}
