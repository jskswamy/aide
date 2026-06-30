package gemini

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jskswamy/aide/internal/fsutil"
	"github.com/jskswamy/aide/internal/provision"
)

var geminiEventMap = map[string]string{
	"pre_tool": "BeforeTool",
}

func geminiHooksDir(ctx provision.Context) string {
	return filepath.Join(ctx.HomeDir, ".gemini", "hooks")
}

// geminiHookCodec implements provision.HookCodec for gemini's aide_*.sh scripts.
type geminiHookCodec struct{}

func (c *geminiHookCodec) Match(name string) bool {
	return provision.GeminiHookArtifact.Owns(name)
}

func (c *geminiHookCodec) Decode(path string) (provision.Hook, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return provision.Hook{}, err
	}
	cmd := extractCommandFromScript(string(data))
	return provision.Hook{Event: "pre_tool", Command: cmd}, nil
}

func (c *geminiHookCodec) Encode(dir string, h provision.Hook) error {
	if geminiEventMap[h.Event] == "" {
		return nil // unsupported event — skip silently
	}
	if err := provision.ValidateHookCommand(h.Command); err != nil {
		return fmt.Errorf("gemini hooks: %w", err)
	}
	name := provision.GeminiHookArtifact.Name(h.Command)
	script := "#!/bin/bash\nexec " + h.Command + "\n"
	if err := fsutil.AtomicWrite(filepath.Join(dir, name), []byte(script)); err != nil {
		return fmt.Errorf("gemini hooks: write script: %w", err)
	}
	if err := os.Chmod(filepath.Join(dir, name), 0o755); err != nil {
		return fmt.Errorf("gemini hooks: chmod script: %w", err)
	}
	return nil
}

func (c *geminiHookCodec) Remove(path string) error {
	return os.Remove(path)
}

// ReadHooks returns aide-managed hooks by listing aide_*.sh scripts.
func (d *Driver) ReadHooks(ctx provision.Context) ([]provision.Hook, error) {
	return provision.ReadHooks(geminiHooksDir(ctx), &geminiHookCodec{})
}

// WriteHooks removes all aide_*.sh scripts and writes new ones for desired.
// prevManaged is unused for file-based formats; aide_* naming is the ownership signal.
func (d *Driver) WriteHooks(ctx provision.Context, _ []provision.Hook, desired []provision.Hook) error {
	return provision.WriteHooks(geminiHooksDir(ctx), desired, &geminiHookCodec{})
}

func extractCommandFromScript(script string) string {
	for _, line := range strings.Split(script, "\n") {
		if strings.HasPrefix(line, "exec ") {
			return strings.TrimPrefix(line, "exec ")
		}
	}
	return ""
}
