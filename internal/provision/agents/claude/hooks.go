package claude

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jskswamy/aide/internal/fsutil"
	"github.com/jskswamy/aide/internal/provision"
)

var claudeEventMap = map[string]string{
	"pre_tool":      "PreToolUse",
	"post_tool":     "PostToolUse",
	"session_start": "SessionStart",
	"session_end":   "SessionEnd",
	"notification":  "Notification",
	"stop":          "Stop",
}

var claudeMatcherMap = map[string]string{
	"shell": "Bash",
}

func settingsPath(ctx provision.Context) string {
	dir := ctx.Env["CLAUDE_CONFIG_DIR"]
	if dir == "" {
		dir = filepath.Join(ctx.HomeDir, ".claude")
	}
	return filepath.Join(dir, "settings.json")
}

// ReadHooks returns all hooks currently in settings.json, including
// user-added entries. Ownership tracking is via managed.json, not markers.
func (d *Driver) ReadHooks(ctx provision.Context) ([]provision.Hook, error) {
	raw, err := readSettings(ctx)
	if err != nil {
		return nil, err
	}
	hooksRaw, _ := raw["hooks"].(map[string]interface{})
	var out []provision.Hook
	for nativeEvent, entries := range hooksRaw {
		norm := reverseClaudeEvent(nativeEvent)
		entryList, _ := entries.([]interface{})
		for _, e := range entryList {
			entry, _ := e.(map[string]interface{})
			hookList, _ := entry["hooks"].([]interface{})
			normMatcher := reverseClaudeMatcher(strVal(entry, "matcher"))
			for _, hi := range hookList {
				item, _ := hi.(map[string]interface{})
				cmd := strVal(item, "command")
				if cmd == "" {
					continue
				}
				out = append(out, provision.Hook{
					Event:   norm,
					Matcher: normMatcher,
					Command: cmd,
				})
			}
		}
	}
	return out, nil
}

// WriteHooks atomically reconciles hooks in settings.json.
// prevManaged entries are removed; desired entries are added.
// Hooks not in prevManaged (user-added) are left untouched.
func (d *Driver) WriteHooks(ctx provision.Context, prevManaged []provision.Hook, desired []provision.Hook) error {
	path := settingsPath(ctx)
	raw, err := readSettings(ctx)
	if err != nil {
		return err
	}

	if raw["hooks"] == nil {
		raw["hooks"] = map[string]interface{}{}
	}
	hooksObj, ok := raw["hooks"].(map[string]interface{})
	if !ok {
		hooksObj = map[string]interface{}{}
		raw["hooks"] = hooksObj
	}

	// Build a removal set keyed by (nativeEvent, nativeMatcher, command).
	type hookID struct{ event, matcher, command string }
	removeSet := map[hookID]bool{}
	for _, h := range prevManaged {
		nativeEvent := claudeEventMap[h.Event]
		if nativeEvent == "" {
			nativeEvent = h.Event
		}
		removeSet[hookID{nativeEvent, claudeMatcherMap[h.Matcher], h.Command}] = true
	}

	// Remove prevManaged entries from every bucket.
	for event, entries := range hooksObj {
		entryList, _ := entries.([]interface{})
		var keptEntries []interface{}
		for _, e := range entryList {
			entry, _ := e.(map[string]interface{})
			hookList, _ := entry["hooks"].([]interface{})
			matcher := strVal(entry, "matcher")
			var keptItems []interface{}
			for _, hi := range hookList {
				item, _ := hi.(map[string]interface{})
				cmd := strVal(item, "command")
				if !removeSet[hookID{event, matcher, cmd}] {
					keptItems = append(keptItems, hi)
				}
			}
			if len(keptItems) > 0 {
				entry["hooks"] = keptItems
				keptEntries = append(keptEntries, entry)
			}
		}
		if len(keptEntries) == 0 {
			delete(hooksObj, event)
		} else {
			hooksObj[event] = keptEntries
		}
	}

	// Add desired hooks, grouped by (nativeEvent, nativeMatcher).
	type bucketKey struct{ event, matcher string }
	added := map[bucketKey]bool{}
	for _, h := range desired {
		nativeEvent := claudeEventMap[h.Event]
		if nativeEvent == "" {
			nativeEvent = h.Event
		}
		nativeMatcher := claudeMatcherMap[h.Matcher]
		bk := bucketKey{nativeEvent, nativeMatcher}
		if !added[bk] {
			added[bk] = true
			entry := map[string]interface{}{
				"hooks": []interface{}{},
			}
			if nativeMatcher != "" {
				entry["matcher"] = nativeMatcher
			}
			existing, _ := hooksObj[nativeEvent].([]interface{})
			existing = append(existing, entry)
			hooksObj[nativeEvent] = existing
		}
		buckets, _ := hooksObj[nativeEvent].([]interface{})
		lastBucket, _ := buckets[len(buckets)-1].(map[string]interface{})
		items, _ := lastBucket["hooks"].([]interface{})
		lastBucket["hooks"] = append(items, map[string]interface{}{
			"type":    "command",
			"command": h.Command,
		})
	}

	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("claude hooks: marshal: %w", err)
	}
	return fsutil.AtomicWrite(path, data)
}

func readSettings(ctx provision.Context) (map[string]interface{}, error) {
	path := settingsPath(ctx)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]interface{}{}, nil
		}
		return nil, fmt.Errorf("claude hooks: read %s: %w", path, err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("claude hooks: parse %s: %w", path, err)
	}
	if m == nil {
		m = map[string]interface{}{}
	}
	return m, nil
}

func reverseClaudeEvent(native string) string {
	for k, v := range claudeEventMap {
		if v == native {
			return k
		}
	}
	return native
}

func reverseClaudeMatcher(native string) string {
	for k, v := range claudeMatcherMap {
		if v == native {
			return k
		}
	}
	return native
}

func strVal(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}
