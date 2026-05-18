package provision

import (
	"strings"

	"github.com/jskswamy/aide/internal/config"
)

// ApplyOverride composes a ContextOverride against a top-level map.
// Composition order: only (replace) → exclude (subtract) → extra (add).
// Returns a new map; never mutates inputs.
//
// Path syntax for Only / Exclude:
//   - "repo" or "name" — matches a top-level key.
//   - "repo/plugin" — for marketplace entries (list-valued), removes a
//     specific plugin from the entry's list. Whole entry is dropped
//     only when no plugins remain.
func ApplyOverride[T any](top map[string]T, override *config.ContextOverride[T]) map[string]T {
	out := copyOverrideMap(top)
	if override == nil {
		return out
	}
	if len(override.Only) > 0 {
		// Two-pass: first collect per-key sub-plugin sets, then build
		// the filtered map. A nil entry in keepSubs[key] means "keep
		// the whole entry"; a non-nil slice means "filter to these
		// sub-plugins". Multiple paths against the same key accumulate.
		keepSubs := map[string][]string{}
		keepAll := map[string]bool{}
		for _, path := range override.Only {
			key, sub := splitOverridePath(path)
			if sub == "" {
				keepAll[key] = true
				continue
			}
			keepSubs[key] = append(keepSubs[key], sub)
		}
		filtered := map[string]T{}
		for key := range keepAll {
			if v, ok := top[key]; ok {
				filtered[key] = v
			}
		}
		for key, subs := range keepSubs {
			if keepAll[key] {
				// Whole-entry already pulled; sub-filtering would lose
				// other plugins under that repo. Whole-entry wins.
				continue
			}
			v, ok := top[key]
			if !ok {
				continue
			}
			if pluginVal, ok := any(v).(config.PluginEntry); ok {
				kept := keepSubPlugins(pluginVal, subs)
				if len(kept.Plugins) == 0 && kept.Shape() == config.PluginShapeMarketplace {
					continue // no surviving plugins → drop the entry
				}
				if asT, ok := any(kept).(T); ok {
					filtered[key] = asT
				}
				continue
			}
			// T isn't PluginEntry (e.g. MCPServer): subpath syntax has
			// no meaning. Fall back to whole-entry to preserve current
			// behavior for non-plugin maps.
			filtered[key] = v
		}
		out = filtered
	}
	for _, path := range override.Exclude {
		key, sub := splitOverridePath(path)
		if sub == "" {
			delete(out, key)
			continue
		}
		// Sub-path (repo/plugin) — only valid for PluginEntry values.
		// Use type-assertion via interface{} to keep this helper generic;
		// for T that doesn't support sub-removal, this is a no-op aside
		// from the explicit branch.
		if pluginVal, ok := any(out[key]).(config.PluginEntry); ok {
			pluginVal = removeSubPlugin(pluginVal, sub)
			if len(pluginVal.Plugins) == 0 && pluginVal.Shape() == config.PluginShapeMarketplace {
				delete(out, key)
			} else {
				if asT, ok := any(pluginVal).(T); ok {
					out[key] = asT
				}
			}
		}
	}
	for k, v := range override.Extra {
		out[k] = v
	}
	return out
}

func copyOverrideMap[T any](in map[string]T) map[string]T {
	out := make(map[string]T, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// splitOverridePath splits "repo/plugin" into ("repo", "plugin"); for
// "owner/repo" returns ("owner/repo", ""); for plain keys (no slash),
// returns (key, "").
func splitOverridePath(p string) (key, sub string) {
	parts := strings.SplitN(p, "/", 3)
	switch len(parts) {
	case 1:
		return parts[0], ""
	case 2:
		return p, ""
	case 3:
		return parts[0] + "/" + parts[1], parts[2]
	}
	return p, ""
}

// keepSubPlugins returns a marketplace-shape entry containing only
// the plugins whose names are in `keep`. Plugins not present in the
// original entry are silently dropped (validation upstream). The
// result preserves the original entry's order, filtered.
func keepSubPlugins(entry config.PluginEntry, keep []string) config.PluginEntry {
	if entry.Shape() != config.PluginShapeMarketplace {
		return entry
	}
	keepSet := make(map[string]struct{}, len(keep))
	for _, k := range keep {
		keepSet[k] = struct{}{}
	}
	out := make([]string, 0, len(keep))
	for _, p := range entry.Plugins {
		if _, ok := keepSet[p]; ok {
			out = append(out, p)
		}
	}
	return config.PluginEntryMarketplace(out)
}

func removeSubPlugin(entry config.PluginEntry, plugin string) config.PluginEntry {
	if entry.Shape() != config.PluginShapeMarketplace {
		return entry
	}
	out := make([]string, 0, len(entry.Plugins))
	for _, p := range entry.Plugins {
		if p != plugin {
			out = append(out, p)
		}
	}
	entry.Plugins = out
	return entry
}
