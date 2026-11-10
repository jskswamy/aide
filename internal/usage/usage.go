// Package usage scopes usage-report tools (backends) to aide
// profiles. One data-driven Backend entry carries everything a
// backend needs: the harness-to-env-var mapping, suffix rules,
// single-root and --all exclusion data, a scrub list, and a setup
// hook. Adding a second backend is a one-entry change here.
//
// ProfileEnv computes a profile's backend env from
// Context.NativeConfigDir so usage reports and aide launches always
// agree on which account the data belongs to; ScrubEnv strips every
// managed var so an outer session cannot leak across profiles.
package usage

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/jskswamy/aide/internal/config"
	aidectx "github.com/jskswamy/aide/internal/context"
	"github.com/jskswamy/aide/internal/harness"
)

// EnvMapping maps one aide harness to one backend env var.
type EnvMapping struct {
	// Var is the backend env var, e.g. "CLAUDE_CONFIG_DIR".
	Var string
	// Suffix is appended to the profile dir, e.g. "/sessions".
	Suffix string
	// NonIsolatedDir is the home-relative override when the profile
	// selects no config dir (opencode: ".local/share/opencode");
	// empty = use Context.NativeConfigDir as-is.
	NonIsolatedDir string
}

// SetupFunc writes the backend's own config (e.g. ccusage's pi named
// stores); in/out carry confirmation and progress; force bypasses the prompt.
type SetupFunc func(cfg *config.Config, home string, in io.Reader, out io.Writer, force, interactive bool) error

// ProfileConfigFunc isolates backend config for one profile and returns a cleanup function.
type ProfileConfigFunc func(args, scopeEnv []string) ([]string, func(), error)

// Backend is one usage-report tool's seam entry: its identity, its
// harness env mapping, and the rules --all and scrubbing follow.
type Backend struct {
	Name          string
	Binary        string
	InstallHint   string
	HarnessEnv    map[string]EnvMapping // aide harness name -> mapping
	SingleRoot    []string              // env vars accepting one dir only
	AllExclude    []string              // env vars --all must never set
	ScrubExtra    []string              // extra vars to scrub for this backend
	Setup         SetupFunc
	ProfileConfig ProfileConfigFunc
}

// ccusageBackend is the v1 (and only) Backend entry. The pi suffix
// matches where pi writes sessions under PI_CODING_AGENT_DIR; the
// opencode override matches where the shared opencode.db lives, which
// is not aide's .config/opencode default.
var ccusageBackend = Backend{
	Name:        "ccusage",
	Binary:      "ccusage",
	InstallHint: "ccusage not found on PATH — install it: brew install ccusage · nixpkgs: pkgs.ccusage",
	HarnessEnv: map[string]EnvMapping{
		"claude":   {Var: "CLAUDE_CONFIG_DIR"},
		"codex":    {Var: "CODEX_HOME"},
		"copilot":  {Var: "COPILOT_HOME"},
		"pi":       {Var: "PI_AGENT_DIR", Suffix: "/sessions"},
		"opencode": {Var: "OPENCODE_DATA_DIR", NonIsolatedDir: ".local/share/opencode"},
		"gemini":   {Var: "GEMINI_DATA_DIR", Suffix: "/tmp"},
	},
	SingleRoot: []string{"COPILOT_HOME"},
	AllExclude: []string{"PI_AGENT_DIR"},
	ScrubExtra: []string{
		"PI_CODING_AGENT_DIR", "PI_CODING_AGENT_SESSION_DIR",
		"OPENCODE_CONFIG_DIR", "OPENCODE_CONFIG",
		"GEMINI_HOME", "XDG_DATA_HOME",
	},
	Setup:         SetupPiStores,
	ProfileConfig: IsolatePiStores,
}

// Backends returns every known backend. v1: exactly one entry.
func Backends() []Backend { return []Backend{ccusageBackend} }

// DefaultBackend returns the backend v1 always resolves to,
// Backends()[0].
func DefaultBackend() Backend { return Backends()[0] }

// ProfileEnv returns "VAR=value" assignments for one profile, sorted
// by var name. Each assignment is the profile's native config dir for
// a profile-capable harness the backend maps, or the mapping's
// NonIsolatedDir when the profile selects no config dir. Paths are
// set even when they do not exist: the no-wrong-data guarantee means
// the backend's built-in fallbacks never read another profile's dir.
func ProfileEnv(b Backend, ctx config.Context, home string) []string {
	vars := make([]string, 0, len(b.HarnessEnv))
	values := make(map[string]string, len(b.HarnessEnv))
	for _, d := range harness.All() {
		if !d.SupportsProfiles() {
			continue
		}
		m, ok := b.HarnessEnv[d.Name]
		if !ok {
			continue
		}
		dir := ctx.NativeConfigDir(d.Name, home)
		if !ctx.SelectsConfigDir() && m.NonIsolatedDir != "" {
			dir = filepath.Join(home, m.NonIsolatedDir)
		}
		vars = append(vars, m.Var)
		values[m.Var] = dir + m.Suffix
	}
	sort.Strings(vars)
	out := make([]string, len(vars))
	for i, v := range vars {
		out[i] = v + "=" + values[v]
	}
	return out
}

// ResolveProfile looks up a named profile without CWD matching.
func ResolveProfile(cfg *config.Config, name string) (config.Context, error) {
	ctx, ok := cfg.Contexts[name]
	if !ok {
		return config.Context{}, fmt.Errorf("unknown profile %q", name)
	}
	return ctx, nil
}

// AllEnv merges existing profile directories into sorted backend assignments.
// Single-root vars use the first distinct dir in profile-name order.
func AllEnv(b Backend, cfg *config.Config, home string, warn io.Writer) []string {
	profiles := make([]string, 0, len(cfg.Contexts))
	for name := range cfg.Contexts {
		profiles = append(profiles, name)
	}
	sort.Strings(profiles)

	excluded := make(map[string]bool, len(b.AllExclude))
	for _, v := range b.AllExclude {
		excluded[v] = true
	}
	single := make(map[string]bool, len(b.SingleRoot))
	for _, v := range b.SingleRoot {
		single[v] = true
	}
	values := make(map[string][]string)
	seen := make(map[string]map[string]bool)
	skipped := make(map[string][]string)
	for _, name := range profiles {
		for _, assignment := range ProfileEnv(b, cfg.Contexts[name], home) {
			v, dir, _ := strings.Cut(assignment, "=")
			if excluded[v] {
				continue
			}
			if info, err := os.Stat(dir); err != nil || !info.IsDir() {
				continue
			}
			if seen[v] == nil {
				seen[v] = make(map[string]bool)
			}
			if seen[v][dir] {
				continue
			}
			seen[v][dir] = true
			if single[v] && len(values[v]) > 0 {
				skipped[v] = append(skipped[v], name)
				continue
			}
			values[v] = append(values[v], dir)
		}
	}
	vars := make([]string, 0, len(values))
	for v := range values {
		vars = append(vars, v)
	}
	sort.Strings(vars)
	out := make([]string, 0, len(vars))
	for _, v := range vars {
		out = append(out, v+"="+strings.Join(values[v], ","))
		if warn != nil && len(skipped[v]) > 0 {
			fmt.Fprintf(warn, "warning: %s supports one directory; skipping profiles: %s\n", v, strings.Join(skipped[v], ", "))
		}
	}
	return out
}

// ScrubEnv returns env minus every var the backend manages: every
// EnvMapping.Var plus ScrubExtra. Input order is preserved.
func ScrubEnv(env []string, b Backend) []string {
	drop := make(map[string]bool, len(b.HarnessEnv)+len(b.ScrubExtra))
	for _, m := range b.HarnessEnv {
		drop[m.Var] = true
	}
	for _, v := range b.ScrubExtra {
		drop[v] = true
	}
	out := make([]string, 0, len(env))
	for _, e := range env {
		k, _, _ := strings.Cut(e, "=")
		if !drop[k] {
			out = append(out, e)
		}
	}
	return out
}

// ResolveCWDProfile returns the profile aide would launch under for
// cwd, by running aide's own context resolver, so a usage run and a
// launch always agree on which account the data belongs to.
func ResolveCWDProfile(cfg *config.Config, cwd string) (string, config.Context, error) {
	rc, err := aidectx.Resolve(cfg, cwd, aidectx.GitRemotes(cwd))
	if err != nil {
		return "", config.Context{}, err
	}
	return rc.Name, rc.Context, nil
}

// ExitError carries a child process exit code; main.go's
// interface{ ExitCode() int } convention turns it into the process
// exit code.
type ExitError struct{ Code int }

// Error reports the child's exit status.
func (e *ExitError) Error() string { return fmt.Sprintf("exit status %d", e.Code) }

// ExitCode returns the child's exit code.
func (e *ExitError) ExitCode() int { return e.Code }

// NotFoundError reports a missing backend binary; Error() is the
// backend's InstallHint, the message the user needs to install it.
type NotFoundError struct{ Backend Backend }

// Error returns the backend's install hint.
func (e *NotFoundError) Error() string { return e.Backend.InstallHint }

// ExitCode is always 1: the run never started.
func (e *NotFoundError) ExitCode() int { return 1 }

// Run execs b.Binary with args and env, stdio wired through, no
// shell. env is the child's whole environment (already scrubbed and
// extended by the caller). A missing binary is a *NotFoundError, a
// non-zero child exit is an *ExitError (nil error for 0); anything
// else is a wrapped Go error.
func Run(b Backend, args, env []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if _, err := exec.LookPath(b.Binary); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return &NotFoundError{Backend: b}
		}
		return fmt.Errorf("looking up %s: %w", b.Binary, err)
	}
	cmd := exec.Command(b.Binary, args...)
	cmd.Env = env
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			if ee.ExitCode() == -1 {
				if status, ok := ee.Sys().(syscall.WaitStatus); ok && status.Signaled() {
					return &ExitError{Code: 128 + int(status.Signal())}
				}
				return fmt.Errorf("running %s: %w", b.Binary, err)
			}
			return &ExitError{Code: ee.ExitCode()}
		}
		return fmt.Errorf("running %s: %w", b.Binary, err)
	}
	return nil
}
