package launcher

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jskswamy/aide/internal/config"
	aidectx "github.com/jskswamy/aide/internal/context"
	"github.com/jskswamy/aide/internal/envscrub"
)

// detectAmbientProviderKeys inspects environ for non-empty provider secret
// variables that are not already configured in ctx.Env, permitted by
// capabilities, or covered by ctx.Credentials / ctx.Gateways. When
// requireHarnessMatch is true, built-in provider credentials that do not
// target activeHarness by default are skipped.
func detectAmbientProviderKeys(
	environ []string,
	ctx config.Context,
	permitted []string,
	activeHarness string,
	requireHarnessMatch bool,
) []string {
	excluded := make(map[string]bool)
	for k := range ctx.Env {
		excluded[k] = true
	}
	for _, p := range permitted {
		excluded[p] = true
	}
	for name, b := range ctx.Credentials {
		for _, e := range b.EnvNames(name) {
			excluded[e] = true
		}
	}
	for name, gw := range ctx.Gateways {
		excluded[gw.EnvVarName()] = true
		for _, h := range gw.EffectiveHarnesses() {
			excluded[gw.TokenEnv(name, h)] = true
		}
	}

	var keys []string
	for _, entry := range environ {
		k, v, ok := strings.Cut(entry, "=")
		if !ok || v == "" {
			continue
		}
		if !envscrub.IsProviderSecretName(k) || isExcludedAmbientKey(k, excluded, permitted) {
			continue
		}
		if requireHarnessMatch && activeHarness != "" {
			bindingName := config.LegacyBindingName(k)
			if config.IsBuiltinCredential(bindingName) && !(config.CredentialBinding{}).DefaultAllowsHarness(bindingName, activeHarness) {
				continue
			}
		}
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return slices.Compact(keys)
}

func isExcludedAmbientKey(k string, excluded map[string]bool, permitted []string) bool {
	if excluded[k] {
		return true
	}
	for _, p := range permitted {
		if prefix, ok := strings.CutSuffix(p, "*"); ok && prefix != "" && strings.HasPrefix(k, prefix) {
			return true
		}
	}
	return false
}

// loadStoreFrom returns a StoreLoader that checks secretsDir first for
// relative store names (or stores resolved against default SecretsDir)
// before falling back to loadSecretStore.
func loadStoreFrom(secretsDir string) StoreLoader {
	return func(path string) (map[string]string, error) {
		if secretsDir != "" {
			base := filepath.Base(path)
			if !strings.HasSuffix(base, ".enc.yaml") {
				base += ".enc.yaml"
			}
			candidate := filepath.Join(secretsDir, base)
			if _, err := os.Stat(candidate); err == nil {
				return loadSecretStore(candidate)
			}
		}
		return loadSecretStore(config.ResolveSecretPath(path))
	}
}

// noteAmbientProviderKeys tells the user about ambient provider API keys
// the launch scrubs from the environment when the profile has no active
// credential binding, and how to configure a gateway instead. It never asks.
func (l *Launcher) noteAmbientProviderKeys(cfg *config.Config, rc *aidectx.ResolvedContext, permitted []string) {
	keys := detectAmbientProviderKeys(os.Environ(), rc.Context, permitted, cfg.HarnessOf(rc.Context.Agent), true)
	if len(keys) == 0 {
		return
	}
	fmt.Fprintf(l.stderr(), "aide: note: scrubbed ambient %s from the environment (%s has no credential binding); "+
		"configure a gateway with aide gateway add <name> --url ... or pass --gateway none to silence\n",
		strings.Join(keys, ", "), profileLabel(cfg, rc.Name))
}
