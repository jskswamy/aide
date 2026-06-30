package copilot

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jskswamy/aide/internal/fsutil"
	"github.com/jskswamy/aide/internal/provision"
)

var copilotEventMap = map[string]string{
	"pre_tool": "PreToolUse",
}

func copilotHooksDir(ctx provision.Context) string {
	return filepath.Join(ctx.HomeDir, ".config", "copilot", "hooks")
}

type copilotHookFile struct {
	Hooks map[string][]copilotHookEntry `json:"hooks"`
}

type copilotHookEntry struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// ReadHooks returns aide-managed hooks from aide-*.json files.
func (d *Driver) ReadHooks(ctx provision.Context) ([]provision.Hook, error) {
	dir := copilotHooksDir(ctx)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("copilot hooks: readdir: %w", err)
	}
	var out []provision.Hook
	for _, e := range entries {
		if !provision.CopilotHookArtifact.Owns(e.Name()) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var hf copilotHookFile
		if err := json.Unmarshal(data, &hf); err != nil {
			continue
		}
		for nativeEvent, items := range hf.Hooks {
			normEvent := provision.ReverseLookup(copilotEventMap, nativeEvent, nativeEvent)
			for _, item := range items {
				out = append(out, provision.Hook{Event: normEvent, Command: item.Command})
			}
		}
	}
	return out, nil
}

// WriteHooks removes aide-*.json files and writes new ones for desired.
// prevManaged is unused for file-based formats; aide-* naming is the ownership signal.
func (d *Driver) WriteHooks(ctx provision.Context, _ []provision.Hook, desired []provision.Hook) error {
	dir := copilotHooksDir(ctx)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("copilot hooks: mkdir: %w", err)
	}
	if existing, err := os.ReadDir(dir); err == nil {
		for _, e := range existing {
			if provision.CopilotHookArtifact.Owns(e.Name()) {
				_ = os.Remove(filepath.Join(dir, e.Name()))
			}
		}
	}
	for _, h := range desired {
		nativeEvent := copilotEventMap[h.Event]
		if nativeEvent == "" {
			continue
		}
		hf := copilotHookFile{
			Hooks: map[string][]copilotHookEntry{
				nativeEvent: {{Type: "command", Command: h.Command}},
			},
		}
		data, err := json.MarshalIndent(hf, "", "  ")
		if err != nil {
			return fmt.Errorf("copilot hooks: marshal: %w", err)
		}
		name := provision.CopilotHookArtifact.Name(h.Command)
		if err := fsutil.AtomicWrite(filepath.Join(dir, name), data); err != nil {
			return fmt.Errorf("copilot hooks: write: %w", err)
		}
	}
	return nil
}
