package launcher

import (
	"path/filepath"
	"strings"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/harness"
	"github.com/jskswamy/aide/internal/homepath"
	"github.com/jskswamy/aide/internal/piinstall"
	"github.com/jskswamy/aide/internal/sandbox"
	"github.com/jskswamy/aide/pkg/seatbelt/modules"
)

// PrepareAgentSandbox adds managed-Pi grants only after every validation passes.
// cfg is nil for zero-config passthrough; policy.Env is the effective child env.
func PrepareAgentSandbox(policy *sandbox.Policy, cfg *config.Config, harnessName, executable string) error {
	if harnessName != "pi" || policy == nil {
		return nil
	}
	home := policy.HomeDir
	var private []string
	add := func(path string) {
		if path != "" && filepath.Clean(path) != filepath.Join(home, ".pi", "agent") {
			private = append(private, homepath.Expand(path, home))
		}
	}
	for _, rel := range modules.SensitiveHomeDirs() {
		add(filepath.Join(home, rel))
	}
	add(filepath.Join(home, ".config", "aide"))
	for _, name := range []string{"auth.json", "settings.json", "models.json", "sessions"} {
		add(filepath.Join(home, ".pi", "agent", name))
	}
	for _, d := range harness.All() {
		add(d.DefaultDir(home))
		for _, entry := range policy.Env {
			if value, ok := strings.CutPrefix(entry, d.ProfileEnvKey+"="); ok {
				add(value)
			}
		}
		if cfg == nil || !d.SupportsProfiles() {
			continue
		}
		for _, ctx := range cfg.Contexts {
			if ctx.SelectsConfigDir() {
				add(ctx.NativeConfigDir(d.Name, home))
			}
			if value := ctx.Env[d.ProfileEnvKey]; value != "" {
				add(value)
			}
		}
	}
	installation, err := piinstall.Discover(executable, home, policy.Env, private)
	if err != nil || installation == nil {
		return err
	}
	backend := sandbox.PlatformIsolationTier(*policy).Backend
	validation := *installation
	if backend == sandbox.BackendLandlock {
		parents, err := installation.ExecutableParents(executable, home, private)
		if err != nil {
			return err
		}
		validation.Readable = append(append([]string{}, installation.Readable...), parents...)
	}
	if err := sandbox.ValidatePiManagedInstall(*policy, validation, backend); err != nil {
		return err
	}
	policy.AgentModule = modules.PiAgentWithInstallation(installation.Root, installation.Readable)
	return nil
}
