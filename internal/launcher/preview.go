package launcher

import (
	"fmt"
	"os"

	"github.com/jskswamy/aide/internal/capability"
	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/sandbox"
)

// PreviewSessionEnv resolves the AIDE_* env vars a real `aide launch`
// would set for a context, without actually launching: no secret
// decryption, no interactive capability-variant prompts, no process exec.
//
// The selection is Launch's (ResolveSelection, which gates cfg's
// .aide.yaml): contextName stands in for --profile and agent for
// --agent; otherwise the context matched from cwd and a harness saved
// for the repository apply, as at launch. A selection step a launch
// would stop on is tolerated once the profile is resolved (a
// statusline stays quiet; the launch reports it): the preview shows
// the harness selected before that step, never a saved or --agent
// harness the launch rejects.
//
// Capability names are resolved via the same context/--without/
// auto-include logic Launch uses, but not expanded through
// extends/combines/variants, so AIDE_CAPS may differ from a real launch's
// fully-resolved set in edge cases — good enough for a statusline preview,
// not a byte-exact simulation.
func PreviewSessionEnv(cfg *config.Config, cwd, contextName, homeDir, agent string) (map[string]string, error) {
	res, err := (&Launcher{Profile: contextName}).ResolveSelection(cfg, cwd, homeDir, SelectionInputs{Agent: agent})
	if res.RC == nil {
		return nil, err
	}
	ctx, name := res.RC.Context, res.RC.Name
	var disabledCaps []string
	if cfg.ProjectOverride != nil {
		disabledCaps = cfg.ProjectOverride.DisabledCapabilities
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

	envSlice := injectAideSessionEnv(nil, sbDisabled, sandboxCfg, autoApprove, caps, name, ctx.Agent, res.Gate.Primary())
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
