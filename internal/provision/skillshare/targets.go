// Package skillshare generates Skillshare account targets for aide
// profiles. Skillshare (https://skillshare.runkids.cc) installs skills,
// agents, MCP servers and other resources into harness config dirs; an
// account target names one dir with `agent:` and `config_dir:`. aide
// writes one target per profile and harness whose launch dir is its
// own, so `skillshare sync` fills exactly the dirs aide launches with.
// The integration is configuration-only: aide never runs Skillshare.
package skillshare

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/fsutil"
	"github.com/jskswamy/aide/internal/harness"
	"github.com/jskswamy/aide/internal/homepath"
	"github.com/jskswamy/aide/internal/provision"
)

// Agents are the harnesses aide writes account targets for: those
// with aide profile support that Skillshare also has account targets
// for (it supports claude, codex, pi and omp).
var Agents = []string{"claude", "codex", "pi"}

// accountAgents is Skillshare's whole agent: enum.
var accountAgents = []string{"claude", "codex", "pi", "omp"}

// NamePrefix starts the name of every target aide writes. A target
// whose name has it and that declares an account agent is aide's: aide
// updates it and removes it when its profile or dir goes away.
const NamePrefix = "aide-"

// TargetName is the name of the target for profile under agent.
func TargetName(profile, agent string) string { return NamePrefix + profile + "-" + agent }

// Target is one account target aide writes.
type Target struct {
	Name    string
	Profile string
	Agent   string
	// ConfigDir is the dir the launch uses, written relative to ~ when
	// under home (Skillshare accepts absolute or ~ paths).
	ConfigDir string
}

// Skip is a profile/harness pair aide writes no target for, and why.
type Skip struct {
	Profile string
	Agent   string // "" when the reason covers every harness
	Reason  string
}

func (s Skip) String() string {
	if s.Agent == "" {
		return s.Profile + ": " + s.Reason
	}
	return s.Profile + "/" + s.Agent + ": " + s.Reason
}

// Plan is the set of targets aide's profiles call for.
type Plan struct {
	Targets []Target
	// Skipped are pairs Skillshare's config_dir rules exclude: the
	// harness's default dir, or a dir several pairs share.
	Skipped []Skip
	// Unsupported are profiles whose harness has no Skillshare account
	// targets. This is about Skillshare, not about login isolation.
	Unsupported []Skip
}

// PlanTargets returns the targets for every profile in cfg: one per
// profile and harness in Agents whose launch dir is the profile's own.
// Dirs come from provision.InjectProfileEnv, the launcher's resolver.
func PlanTargets(cfg *config.Config, home string) Plan {
	var p Plan
	var candidates []Target
	for _, name := range slices.Sorted(maps.Keys(cfg.Contexts)) {
		ctx := cfg.Contexts[name]
		if h := cfg.HarnessOf(ctx.Agent); h != "" && !slices.Contains(Agents, h) {
			p.Unsupported = append(p.Unsupported, Skip{Profile: name, Agent: h,
				Reason: "aide writes Skillshare account targets for claude, codex and pi only; this profile's claude, codex and pi dirs are still listed"})
		}
		if !ctx.SelectsConfigDir() {
			p.Skipped = append(p.Skipped, noConfigDirSkips(name, ctx)...)
			continue
		}
		for _, agent := range Agents {
			t, skip := planTarget(name, agent, ctx, home)
			if skip != nil {
				p.Skipped = append(p.Skipped, *skip)
				continue
			}
			candidates = append(candidates, t)
		}
	}
	targets, shared := splitShared(candidates, home)
	p.Targets = targets
	p.Skipped = append(p.Skipped, shared...)
	return p
}

// noConfigDirSkips explains a profile with no config dir selector:
// harnesses whose dir variable it sets in env: (not followed here) and
// the rest, which run from their default dirs.
func noConfigDirSkips(profile string, ctx config.Context) []Skip {
	var skips []Skip
	var defaults []string
	for _, agent := range Agents {
		d, _ := harness.Lookup(agent)
		if _, ok := ctx.Env[d.ProfileEnvKey]; ok {
			skips = append(skips, Skip{Profile: profile, Agent: agent, Reason: fmt.Sprintf(
				"its dir is set through env.%s, which aide skillshare targets does not follow; use config_dirs: {%s: <dir>} instead",
				d.ProfileEnvKey, agent)})
			continue
		}
		defaults = append(defaults, agent)
	}
	if len(defaults) > 0 {
		skips = append(skips, Skip{Profile: profile, Reason: fmt.Sprintf(
			"no config_dir_name, config_dir or config_dirs; %s run from their default dirs, which Skillshare's plain targets cover",
			strings.Join(defaults, ", "))})
	}
	return skips
}

// planTarget resolves profile's dir for agent the way a launch does.
func planTarget(profile, agent string, ctx config.Context, home string) (Target, *Skip) {
	d, _ := harness.Lookup(agent)
	env, err := provision.InjectProfileEnv(agent, ctx, nil, home)
	if err != nil {
		return Target{}, &Skip{Profile: profile, Agent: agent, Reason: err.Error()}
	}
	dir := env[d.ProfileEnvKey]
	shown := homepath.Tilde(dir, home)
	if fsutil.SameDir(dir, filepath.Join(home, filepath.FromSlash(d.DefaultConfigDir))) {
		return Target{}, &Skip{Profile: profile, Agent: agent,
			Reason: fmt.Sprintf("%s is %s's default dir, which Skillshare's plain %s target covers", shown, agent, agent)}
	}
	return Target{Name: TargetName(profile, agent), Profile: profile, Agent: agent, ConfigDir: shown}, nil
}

// splitShared drops every candidate whose dir another candidate also
// uses: Skillshare requires one config_dir per target.
func splitShared(candidates []Target, home string) ([]Target, []Skip) {
	var kept []Target
	var skipped []Skip
	for i, t := range candidates {
		var others []string
		for j, o := range candidates {
			if i != j && fsutil.SameDir(expand(t.ConfigDir, home), expand(o.ConfigDir, home)) {
				others = append(others, o.Profile+"/"+o.Agent)
			}
		}
		if len(others) == 0 {
			kept = append(kept, t)
			continue
		}
		skipped = append(skipped, Skip{Profile: t.Profile, Agent: t.Agent,
			Reason: fmt.Sprintf("%s is also %s's dir; Skillshare needs a dir per target, so give each harness its own config_dirs entry",
				t.ConfigDir, strings.Join(others, ", "))})
	}
	return kept, skipped
}
