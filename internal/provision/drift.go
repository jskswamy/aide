package provision

import "github.com/jskswamy/aide/internal/config"

// DriftKind reports the launch-time drift state for a context.
type DriftKind int

const (
	// DriftNone indicates the context is in sync with config.yaml.
	// Banner stays silent.
	DriftNone DriftKind = iota
	// DriftConfigChanged indicates the context's config-hash differs
	// from the last sync OR the desired set has items the managed
	// state doesn't record (shortfall). Banner shows a one-line hint.
	DriftConfigChanged
	// DriftNeverSynced indicates state has no entry for this context.
	// First-run case for a new context (other contexts may have been
	// synced).
	DriftNeverSynced
)

// DriftStatus reports per-context drift for contextName. The check is
// purely launch-cheap: no agent shell-out. Two signals are combined:
//
//  1. Per-context ConfigHash compared to current config.yaml hash —
//     catches "user edited config.yaml since this context last synced".
//  2. Desired-set vs managed-set shortfall — catches "this context
//     declares items that state.Contexts[ctx] has not yet recorded as
//     managed", which happens when a new declaration was added and
//     `aide sync` has not yet been run for this context.
//
// Both signals share the same DriftConfigChanged kind because the
// user-facing remediation is identical: run `aide sync`.
func DriftStatus(cfg *config.Config, cfgPath, statePath, contextName string) (DriftKind, error) {
	st, err := LoadState(statePath)
	if err != nil {
		return DriftNone, err
	}
	cs := st.Contexts[contextName]
	if cs == nil {
		return DriftNeverSynced, nil
	}

	if cfg == nil {
		return DriftNone, nil
	}
	cur, err := ConfigHash(cfgPath)
	if err != nil {
		return DriftNone, err
	}
	if cur != "" && cs.ConfigHash != "" && cur != cs.ConfigHash {
		return DriftConfigChanged, nil
	}

	// Shortfall: declared items not yet recorded as managed. Cheap
	// in-process check — no agent poll.
	desired, err := ResolveDesired(cfg, contextName)
	if err != nil {
		// Unknown context or malformed config — leave drift silent;
		// the rest of `aide which` will surface the real error.
		return DriftNone, nil
	}
	if hasShortfall(desired, cs) {
		return DriftConfigChanged, nil
	}
	return DriftNone, nil
}

func hasShortfall(desired Desired, cs *ContextState) bool {
	for k := range desired.Marketplaces {
		if _, ok := cs.Marketplaces[k]; !ok {
			return true
		}
	}
	for k := range desired.Plugins {
		if _, ok := cs.Plugins[k]; !ok {
			return true
		}
	}
	for k := range desired.MCPServers {
		if _, ok := cs.MCPServers[k]; !ok {
			return true
		}
	}
	return false
}

// DriftMessage returns a single human-readable line for the banner,
// or "" when there's nothing to say.
func DriftMessage(d DriftKind, contextName string) string {
	switch d {
	case DriftConfigChanged:
		return "⚠ context \"" + contextName + "\": config changed since last sync — run `aide sync`"
	case DriftNeverSynced:
		return "⚠ context \"" + contextName + "\": never synced — run `aide sync` to install declared plugins/MCP servers"
	default:
		return ""
	}
}
