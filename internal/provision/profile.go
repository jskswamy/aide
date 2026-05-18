package provision

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/homepath"
)

// Profile-related sentinel errors. Callers branch via errors.Is() —
// wrappers preserve the sentinel so rewording the user-facing
// message doesn't break tests.
var (
	// ErrProfileNotSupported: agent driver has no ProfileEnvKey.
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
	drv, ok := ProvisionerFor(ctx.Agent)
	if !ok {
		return fmt.Errorf("no provisioner registered for agent %q", ctx.Agent)
	}
	type profileResolver interface {
		Profile(name, override, homeDir string) (string, string, error)
	}
	pr, ok := drv.(profileResolver)
	if !ok {
		return fmt.Errorf("%w: %q (driver lacks Profile method)", ErrProfileNotSupported, ctx.Agent)
	}
	envKey, absPath, err := pr.Profile(ctx.Profile, ctx.ProfileDir, homeDir)
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
// ValidateProfile or the driver's Profile method.
func InjectProfileEnv(ctx config.Context, env map[string]string, homeDir string) (map[string]string, error) {
	if ctx.Profile == "" && ctx.ProfileDir == "" {
		return env, nil
	}
	if err := ValidateProfile(ctx, homeDir, false); err != nil {
		return env, err
	}
	drv, ok := ProvisionerFor(ctx.Agent)
	if !ok {
		return env, fmt.Errorf("no provisioner registered for agent %q", ctx.Agent)
	}
	pr, ok := drv.(interface {
		Profile(name, override, homeDir string) (string, string, error)
	})
	if !ok {
		return env, fmt.Errorf("%w: %q (driver lacks Profile method)", ErrProfileNotSupported, ctx.Agent)
	}
	envKey, absPath, err := pr.Profile(ctx.Profile, ctx.ProfileDir, homeDir)
	if err != nil {
		return env, err
	}
	if env == nil {
		env = map[string]string{}
	}
	env[envKey] = absPath
	return env, nil
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
	if override != "" {
		return d.Caps.ProfileEnvKey, homepath.Expand(override, homeDir), nil
	}
	dirName := fmt.Sprintf(".%s-%s", d.Caps.AgentName, name)
	return d.Caps.ProfileEnvKey, filepath.Join(homeDir, dirName), nil
}
