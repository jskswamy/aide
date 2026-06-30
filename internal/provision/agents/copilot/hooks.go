package copilot

import (
	"encoding/json"
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

// copilotHookCodec implements provision.HookCodec for copilot's aide-*.json files.
type copilotHookCodec struct{}

func (c *copilotHookCodec) Match(name string) bool {
	return provision.CopilotHookArtifact.Owns(name)
}

func (c *copilotHookCodec) Decode(path string) (provision.Hook, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return provision.Hook{}, err
	}
	var hf copilotHookFile
	if err := json.Unmarshal(data, &hf); err != nil {
		return provision.Hook{}, err
	}
	// Return first hook found; copilot format stores one hook per file.
	for nativeEvent, items := range hf.Hooks {
		normEvent := provision.ReverseLookup(copilotEventMap, nativeEvent, nativeEvent)
		if len(items) > 0 {
			return provision.Hook{Event: normEvent, Command: items[0].Command}, nil
		}
	}
	return provision.Hook{}, fmt.Errorf("copilot hooks: no hooks in file")
}

func (c *copilotHookCodec) Encode(dir string, h provision.Hook) error {
	nativeEvent := copilotEventMap[h.Event]
	if nativeEvent == "" {
		return nil // unsupported event — skip silently
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
	return nil
}

func (c *copilotHookCodec) Remove(path string) error {
	return os.Remove(path)
}

// ReadHooks returns aide-managed hooks from aide-*.json files.
func (d *Driver) ReadHooks(ctx provision.Context) ([]provision.Hook, error) {
	return provision.ReadHooks(copilotHooksDir(ctx), &copilotHookCodec{})
}

// WriteHooks removes aide-*.json files and writes new ones for desired.
// prevManaged is unused for file-based formats; aide-* naming is the ownership signal.
func (d *Driver) WriteHooks(ctx provision.Context, _ []provision.Hook, desired []provision.Hook) error {
	return provision.WriteHooks(copilotHooksDir(ctx), desired, &copilotHookCodec{})
}
