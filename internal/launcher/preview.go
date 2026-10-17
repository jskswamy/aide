package launcher

import (
	"fmt"
	"os"

	"github.com/jskswamy/aide/internal/capability"
	"github.com/jskswamy/aide/internal/config"
	aidectx "github.com/jskswamy/aide/internal/context"
	"github.com/jskswamy/aide/internal/prefs"
	"github.com/jskswamy/aide/internal/sandbox"
	"github.com/jskswamy/aide/internal/ui"
)

// PreviewSessionEnv resolves the AIDE_* env vars a real `aide launch`
// would set for a context, without actually launching: no secret
// decryption, no interactive capability-variant prompts, no process exec.
//
// When contextName is empty, the context is resolved by matching cwd
// (mirroring Launch, including its trust gate on .aide.yaml); otherwise
// the named context is looked up directly with no project-override merge,
// matching the existing precedent in cmd/aide's resolveEffectiveCapabilities
// for inspecting a named context that may not be the one active at cwd.
//
// Capability names are resolved via the same context/--without/
// auto-include logic Launch uses, but not expanded through
// extends/combines/variants, so AIDE_CAPS may differ from a real launch's
// fully-resolved set in edge cases — good enough for a statusline preview,
// not a byte-exact simulation.
//
// A non-empty agent stands in for `--agent`, replacing the context's
// agent; otherwise a harness saved for the repository (aide project
// set) does, as at launch.
func PreviewSessionEnv(cfg *config.Config, cwd, contextName, homeDir, agent string) (map[string]string, error) {
	var ctx config.Context
	var name string
	var trustInfo *ui.TrustInfo
	var disabledCaps []string
	remotes := aidectx.GitRemotes(cwd)

	if contextName != "" {
		c, ok := cfg.Contexts[contextName]
		if !ok {
			return nil, fmt.Errorf("context %q not found", contextName)
		}
		ctx = c
		name = contextName
	} else {
		gate, err := GateProjectConfig(cfg)
		if err != nil {
			return nil, err
		}
		trustInfo = gate.Primary()
		rc, err := aidectx.Resolve(cfg, cwd, remotes)
		if err != nil {
			return nil, fmt.Errorf("resolving profile: %w", err)
		}
		ctx = rc.Context
		name = rc.Name
		if cfg.ProjectOverride != nil {
			disabledCaps = cfg.ProjectOverride.DisabledCapabilities
		}
	}
	// A harness saved for this repository and profile is what a launch
	// runs. Unreadable preferences are ignored here: a statusline stays
	// quiet, and the launch itself reports them.
	if saved, err := SavedPreference(prefs.DefaultPath(homeDir), name, remotes); err == nil && saved.Harness != "" {
		ctx.Agent = saved.Harness
	}
	if agent != "" {
		ctx.Agent = agent
	}

	sandboxCfg, sbDisabled, err := sandbox.ResolveSandboxRef(ctx.Sandbox, cfg.Sandboxes)
	if err != nil {
		return nil, fmt.Errorf("resolving sandbox: %w", err)
	}

	capNames := sandbox.MergeCapNames(ctx.Capabilities, nil, nil)
	capNames = capability.AutoInclude(capNames, disabledCaps, homeDir, os.Getenv)
	caps := &capability.Set{Capabilities: make([]capability.ResolvedCapability, len(capNames))}
	for i, n := range capNames {
		caps.Capabilities[i] = capability.ResolvedCapability{Name: n}
	}

	var prefYolo *bool
	if cfg.Preferences != nil {
		prefYolo = cfg.Preferences.Yolo
	}
	autoApprove := config.ResolveYolo(prefYolo, ctx.Yolo, nil)

	envSlice := injectAideSessionEnv(nil, sbDisabled, sandboxCfg, autoApprove, caps, name, ctx.Agent, trustInfo)
	return envSliceToMap(envSlice), nil
}

func envSliceToMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, kv := range env {
		for i := 0; i < len(kv); i++ {
			if kv[i] == '=' {
				m[kv[:i]] = kv[i+1:]
				break
			}
		}
	}
	return m
}
