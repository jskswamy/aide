package cursor

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jskswamy/aide/internal/fsutil"
	"github.com/jskswamy/aide/internal/provision"
)

var cursorEventMap = map[string]string{
	"pre_tool": "preToolUse",
}
var cursorMatcherMap = map[string]string{
	"shell": "Shell",
}

var reverseEventMap = map[string]string{}
var reverseMatcherMap = map[string]string{}

func init() {
	for k, v := range cursorEventMap {
		reverseEventMap[v] = k
	}
	for k, v := range cursorMatcherMap {
		reverseMatcherMap[v] = k
	}
}

type cursorHooksFile struct {
	Version int                         `json:"version"`
	Hooks   map[string][]cursorHookItem `json:"hooks"`
}
type cursorHookItem struct {
	Command string `json:"command"`
	Matcher string `json:"matcher,omitempty"`
}

func cursorHooksPath(ctx provision.Context) string {
	return filepath.Join(ctx.HomeDir, ".cursor", "hooks.json")
}

// ReadHooks returns all hooks from ~/.cursor/hooks.json, including
// user-added entries. Ownership tracking is via managed.json, not markers.
func (d *Driver) ReadHooks(ctx provision.Context) ([]provision.Hook, error) {
	cf, err := readCursorHooks(ctx)
	if err != nil {
		return nil, err
	}
	var out []provision.Hook
	for nativeEvent, items := range cf.Hooks {
		normEvent := reverseMap(reverseEventMap, nativeEvent)
		for _, item := range items {
			out = append(out, provision.Hook{
				Event:   normEvent,
				Matcher: reverseMap(reverseMatcherMap, item.Matcher),
				Command: item.Command,
			})
		}
	}
	return out, nil
}

// WriteHooks reconciles hooks in ~/.cursor/hooks.json.
// prevManaged entries are removed; desired entries are added.
// Hooks not in prevManaged (user-added) are left untouched.
func (d *Driver) WriteHooks(ctx provision.Context, prevManaged []provision.Hook, desired []provision.Hook) error {
	cf, err := readCursorHooks(ctx)
	if err != nil {
		return err
	}
	if cf.Hooks == nil {
		cf.Hooks = map[string][]cursorHookItem{}
	}
	cf.Version = 1

	// Build a removal set keyed by (nativeEvent, nativeMatcher, command).
	type hookID struct{ event, matcher, command string }
	removeSet := map[hookID]bool{}
	for _, h := range prevManaged {
		nativeEvent := cursorEventMap[h.Event]
		if nativeEvent == "" {
			nativeEvent = h.Event
		}
		removeSet[hookID{nativeEvent, cursorMatcherMap[h.Matcher], h.Command}] = true
	}

	// Remove prevManaged entries.
	for event, items := range cf.Hooks {
		var kept []cursorHookItem
		for _, item := range items {
			if !removeSet[hookID{event, item.Matcher, item.Command}] {
				kept = append(kept, item)
			}
		}
		if len(kept) == 0 {
			delete(cf.Hooks, event)
		} else {
			cf.Hooks[event] = kept
		}
	}

	// Add desired hooks.
	for _, h := range desired {
		nativeEvent := cursorEventMap[h.Event]
		if nativeEvent == "" {
			continue
		}
		nativeMatcher := cursorMatcherMap[h.Matcher]
		cf.Hooks[nativeEvent] = append(cf.Hooks[nativeEvent], cursorHookItem{
			Command: h.Command,
			Matcher: nativeMatcher,
		})
	}

	path := cursorHooksPath(ctx)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("cursor hooks: mkdir: %w", err)
	}
	data, err := json.MarshalIndent(cf, "", "  ")
	if err != nil {
		return fmt.Errorf("cursor hooks: marshal: %w", err)
	}
	return fsutil.AtomicWrite(path, data)
}

func readCursorHooks(ctx provision.Context) (*cursorHooksFile, error) {
	data, err := os.ReadFile(cursorHooksPath(ctx))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &cursorHooksFile{Version: 1, Hooks: map[string][]cursorHookItem{}}, nil
		}
		return nil, fmt.Errorf("cursor hooks: read: %w", err)
	}
	var cf cursorHooksFile
	if err := json.Unmarshal(data, &cf); err != nil {
		return nil, fmt.Errorf("cursor hooks: parse: %w", err)
	}
	if cf.Hooks == nil {
		cf.Hooks = map[string][]cursorHookItem{}
	}
	return &cf, nil
}

func reverseMap(m map[string]string, v string) string {
	if k, ok := m[v]; ok {
		return k
	}
	return v
}
