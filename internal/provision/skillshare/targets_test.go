package skillshare

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/harness"
	"github.com/jskswamy/aide/internal/homepath"
	"github.com/jskswamy/aide/internal/provision"
)

func testHome(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "home")
}

func targetNames(ts []Target) string {
	var names []string
	for _, tg := range ts {
		names = append(names, tg.Name)
	}
	return strings.Join(names, ",")
}

// launchDir is the dir aide launches harness h with under ctx: the
// value InjectProfileEnv sets for the harness's profile variable (the
// launcher's own call).
func launchDir(t *testing.T, h string, ctx config.Context, home string) string {
	t.Helper()
	env, err := provision.InjectProfileEnv(h, ctx, nil, home)
	if err != nil {
		t.Fatalf("InjectProfileEnv(%s): %v", h, err)
	}
	d, _ := harness.Lookup(h)
	return env[d.ProfileEnvKey]
}

func TestPlanTargets_ConfigDirsMatchLaunchDirs(t *testing.T) {
	home := testHome(t)
	cfg := &config.Config{ProfileSchema: true, Contexts: map[string]config.Context{
		"work": {Agent: "claude", Profile: "work"},
		"oss": {Agent: "codex", Profile: "oss", ConfigDirs: map[string]string{
			"codex":  "~/state/codex-oss",
			"claude": filepath.Join(home, "state", "claude-oss"),
		}},
	}}

	plan := PlanTargets(cfg, home)

	want := "aide-oss-claude,aide-oss-codex,aide-oss-opencode,aide-oss-pi,aide-work-claude,aide-work-codex,aide-work-opencode,aide-work-pi"
	if got := targetNames(plan.Targets); got != want {
		t.Fatalf("targets = %s, want %s", got, want)
	}
	for _, tg := range plan.Targets {
		dir := launchDir(t, tg.Agent, cfg.Contexts[tg.Profile], home)
		if got := homepath.Expand(tg.ConfigDir, home); got != dir {
			t.Errorf("%s: config_dir %s expands to %s, launch uses %s", tg.Name, tg.ConfigDir, got, dir)
		}
		if !strings.HasPrefix(tg.ConfigDir, "~/") {
			t.Errorf("%s: config_dir %q is not written relative to ~", tg.Name, tg.ConfigDir)
		}
	}
	if len(plan.Skipped)+len(plan.Unsupported) != 0 {
		t.Errorf("unexpected skips: %+v %+v", plan.Skipped, plan.Unsupported)
	}
}

func TestPlanTargets_DefaultDirsAreSkipped(t *testing.T) {
	home := testHome(t)
	cfg := &config.Config{ProfileSchema: true, Contexts: map[string]config.Context{
		"personal": {Agent: "claude"},
		"adopted":  {Agent: "claude", Profile: "adopted", ConfigDirs: map[string]string{"claude": "~/.claude", "pi": "~/.pi/agent"}},
	}}

	plan := PlanTargets(cfg, home)

	if got := targetNames(plan.Targets); got != "aide-adopted-codex,aide-adopted-opencode" {
		t.Errorf("targets = %s, want aide-adopted-codex,aide-adopted-opencode", got)
	}
	reasons := skipText(plan.Skipped)
	for _, want := range []string{
		"personal: no config_dir_name, config_dir or config_dirs; claude, codex, opencode, pi run from their default dirs",
		"adopted/claude: ~/.claude is claude's default dir",
		"adopted/pi: ~/.pi/agent is pi's default dir",
	} {
		if !strings.Contains(reasons, want) {
			t.Errorf("skips lack %q:\n%s", want, reasons)
		}
	}
}

func TestPlanTargets_SharedDirIsSkippedForEveryUser(t *testing.T) {
	home := testHome(t)
	cfg := &config.Config{ProfileSchema: true, Contexts: map[string]config.Context{
		"one": {Agent: "claude", Profile: "one", ProfileDir: "~/one", ConfigDirs: map[string]string{"pi": "~/one-pi"}},
	}}

	plan := PlanTargets(cfg, home)

	if got := targetNames(plan.Targets); got != "aide-one-pi" {
		t.Errorf("targets = %s, want aide-one-pi", got)
	}
	reasons := skipText(plan.Skipped)
	for _, want := range []string{"one/claude: ~/one is also", "one/codex: ~/one is also", "one/opencode: ~/one is also"} {
		if !strings.Contains(reasons, want) {
			t.Errorf("skips lack %q:\n%s", want, reasons)
		}
	}
}

func TestPlanTargets_UnsupportedHarnessReportedSeparately(t *testing.T) {
	home := testHome(t)
	cfg := &config.Config{ProfileSchema: true, Contexts: map[string]config.Context{
		"cp": {Agent: "copilot", Profile: "cp"},
	}}

	plan := PlanTargets(cfg, home)

	if got := targetNames(plan.Targets); got != "aide-cp-claude,aide-cp-codex,aide-cp-opencode,aide-cp-pi" {
		t.Errorf("targets = %s", got)
	}
	if len(plan.Skipped) != 0 {
		t.Errorf("unsupported harness reported as a directory skip: %+v", plan.Skipped)
	}
	if got := skipText(plan.Unsupported); !strings.Contains(got, "cp/copilot: aide writes Skillshare account targets for claude, codex, opencode and pi only") {
		t.Errorf("unsupported = %s", got)
	}
}

func skipText(skips []Skip) string {
	var b strings.Builder
	for _, s := range skips {
		b.WriteString(s.String())
		b.WriteByte('\n')
	}
	return b.String()
}

func TestPlanTargets_OmpReportedUnsupported(t *testing.T) {
	cfg := &config.Config{ProfileSchema: true, Contexts: map[string]config.Context{
		"o": {Agent: "omp", Profile: "o"},
	}}

	plan := PlanTargets(cfg, testHome(t))

	if got := skipText(plan.Unsupported); !strings.Contains(got, "o/omp: aide writes Skillshare account targets for claude, codex, opencode and pi only") {
		t.Errorf("unsupported = %q", got)
	}
}

func TestPlanTargets_EnvPinnedDirIsNotCalledDefault(t *testing.T) {
	cfg := &config.Config{ProfileSchema: true, Contexts: map[string]config.Context{
		"legacy": {Agent: "claude", Env: map[string]string{"CLAUDE_CONFIG_DIR": "~/.claude-legacy"}},
	}}

	plan := PlanTargets(cfg, testHome(t))

	got := skipText(plan.Skipped)
	for _, want := range []string{
		"legacy/claude: its dir is set through env.CLAUDE_CONFIG_DIR",
		"legacy: no config_dir_name, config_dir or config_dirs; codex, opencode, pi run from their default dirs",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("skips lack %q:\n%s", want, got)
		}
	}
}
