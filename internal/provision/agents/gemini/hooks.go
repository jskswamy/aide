package gemini

import (
	"crypto/sha256"
	"errors"
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

func hookScriptName(command string) string {
	sum := sha256.Sum256([]byte(command))
	return fmt.Sprintf("aide_%x.sh", sum[:8])
}

// ReadHooks returns aide-managed hooks by listing aide_*.sh scripts.
func (d *Driver) ReadHooks(ctx provision.Context) ([]provision.Hook, error) {
	dir := geminiHooksDir(ctx)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("gemini hooks: readdir %s: %w", dir, err)
	}
	var out []provision.Hook
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "aide_") || !strings.HasSuffix(e.Name(), ".sh") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		cmd := extractCommandFromScript(string(data))
		out = append(out, provision.Hook{Event: "pre_tool", Command: cmd})
	}
	return out, nil
}

// WriteHooks removes all aide_*.sh scripts and writes new ones for desired.
// prevManaged is unused for file-based formats; aide_* naming is the ownership signal.
func (d *Driver) WriteHooks(ctx provision.Context, _ []provision.Hook, desired []provision.Hook) error {
	dir := geminiHooksDir(ctx)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("gemini hooks: mkdir %s: %w", dir, err)
	}

	// Remove existing aide scripts.
	if existing, err := os.ReadDir(dir); err == nil {
		for _, e := range existing {
			if strings.HasPrefix(e.Name(), "aide_") && strings.HasSuffix(e.Name(), ".sh") {
				_ = os.Remove(filepath.Join(dir, e.Name()))
			}
		}
	}

	// Write new scripts.
	for _, h := range desired {
		if geminiEventMap[h.Event] == "" {
			continue // unsupported event — skip silently
		}
		if err := provision.ValidateHookCommand(h.Command); err != nil {
			return fmt.Errorf("gemini hooks: %w", err)
		}
		name := hookScriptName(h.Command)
		script := "#!/bin/bash\nexec " + h.Command + "\n"
		if err := fsutil.AtomicWrite(filepath.Join(dir, name), []byte(script)); err != nil {
			return fmt.Errorf("gemini hooks: write script: %w", err)
		}
		if err := os.Chmod(filepath.Join(dir, name), 0o755); err != nil {
			return fmt.Errorf("gemini hooks: chmod script: %w", err)
		}
	}
	return nil
}

func extractCommandFromScript(script string) string {
	for _, line := range strings.Split(script, "\n") {
		if strings.HasPrefix(line, "exec ") {
			return strings.TrimPrefix(line, "exec ")
		}
	}
	return ""
}
