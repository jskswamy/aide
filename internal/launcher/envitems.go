package launcher

import (
	"sort"

	"github.com/jskswamy/aide/internal/capability"
	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/display"
	"github.com/jskswamy/aide/internal/ui"
)

// BuildEnvItems constructs the unified env section for banner rendering.
// It merges context env vars and capability env_allow into one sorted list.
//
// contextEnv: the context's Env map after template resolution.
// resolvedCaps: capability.ResolvedCapability slice from ResolveAll.
// neverAllowEnv: vars to mark as blocked (from Set.NeverAllowEnv).
// resolvedValues: fully resolved env values for detailed mode; nil in normal mode.
func BuildEnvItems(
	contextEnv map[string]string,
	resolvedCaps []capability.ResolvedCapability,
	neverAllowEnv []string,
	resolvedValues map[string]string,
) []ui.EnvItem {
	neverSet := make(map[string]bool, len(neverAllowEnv))
	for _, v := range neverAllowEnv {
		neverSet[v] = true
	}

	// Build credential set from all capability env_allow vars.
	var allCapEnv []string
	for _, cap := range resolvedCaps {
		allCapEnv = append(allCapEnv, cap.EnvAllow...)
	}
	credSet := make(map[string]bool)
	for _, v := range capability.CredentialWarnings(allCapEnv) {
		credSet[v] = true
	}

	// Build cap source map: envVar -> first capName that grants it.
	capSource := make(map[string]string)
	for _, cap := range resolvedCaps {
		for _, env := range cap.EnvAllow {
			if _, exists := capSource[env]; !exists {
				capSource[env] = cap.Name
			}
		}
	}

	var items []ui.EnvItem
	seen := make(map[string]bool)

	// 1. Context env vars (sorted for determinism).
	ctxKeys := make([]string, 0, len(contextEnv))
	for k := range contextEnv {
		ctxKeys = append(ctxKeys, k)
	}
	sort.Strings(ctxKeys)

	for _, k := range ctxKeys {
		v := contextEnv[k]
		source, _ := display.ClassifyEnvSource(v)
		item := ui.EnvItem{
			Key:         k,
			Badge:       display.BadgeForSource(source),
			Annotation:  display.EnvAnnotation(v),
			CredWarning: credSet[k],
		}
		if neverSet[k] {
			item.Blocked = true
			item.Badge = "⊘"
			item.Annotation = "never-allow"
		}
		if resolvedValues != nil {
			if rv, ok := resolvedValues[k]; ok {
				item.ResolvedValue = display.RedactValue(rv)
			}
		}
		items = append(items, item)
		seen[k] = true
	}

	// 2. Capability env_allow vars not already in context env (sorted).
	capKeys := make([]string, 0)
	for k := range capSource {
		if !seen[k] {
			capKeys = append(capKeys, k)
		}
	}
	sort.Strings(capKeys)

	for _, k := range capKeys {
		item := ui.EnvItem{
			Key:         k,
			Badge:       "🔧",
			Annotation:  "← " + capSource[k],
			CredWarning: credSet[k],
		}
		if neverSet[k] {
			item.Blocked = true
			item.Badge = "⊘"
			item.Annotation = "never-allow"
		}
		items = append(items, item)
	}

	// 3. Never-allow vars not covered by context or any capability.
	for _, k := range neverAllowEnv {
		if !seen[k] && capSource[k] == "" {
			items = append(items, ui.EnvItem{
				Key:        k,
				Badge:      "⊘",
				Annotation: "never-allow",
				Blocked:    true,
			})
		}
	}

	return items
}

// ResolveAgentIcon returns the display icon for agentName.
// It checks agentDef.Icon first, then falls back to display.DefaultAgentIcons.
func ResolveAgentIcon(agentName string, agentDef *config.AgentDef) string {
	if agentDef != nil && agentDef.Icon != "" {
		return agentDef.Icon
	}
	return display.DefaultAgentIcons[agentName]
}
