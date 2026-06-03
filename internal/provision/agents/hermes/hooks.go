package hermes

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

var hermesEventMap = map[string]string{
	"pre_tool": "pre_tool_call",
}

func hermesPluginsDir(ctx provision.Context) string {
	return filepath.Join(ctx.HomeDir, ".hermes", "plugins")
}

func hookDirName(command string) string {
	sum := sha256.Sum256([]byte(command))
	return fmt.Sprintf("aide_%x", sum[:8])
}

// ReadHooks returns aide-managed hooks from ~/.hermes/plugins/aide_*/ directories.
func (d *Driver) ReadHooks(ctx provision.Context) ([]provision.Hook, error) {
	pluginsDir := hermesPluginsDir(ctx)
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("hermes hooks: readdir %s: %w", pluginsDir, err)
	}
	var out []provision.Hook
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "aide_") {
			continue
		}
		hookDir := filepath.Join(pluginsDir, e.Name())
		cmd, err := readCommandFromInitPy(hookDir)
		if err != nil {
			continue
		}
		out = append(out, provision.Hook{
			Event:   "pre_tool",
			Matcher: "",
			Command: cmd,
		})
	}
	return out, nil
}

// WriteHooks reconciles desired hooks into ~/.hermes/plugins/aide_*/ directories.
// prevManaged is unused for file-based formats; aide_ naming is the ownership signal.
func (d *Driver) WriteHooks(ctx provision.Context, _ []provision.Hook, desired []provision.Hook) error {
	pluginsDir := hermesPluginsDir(ctx)

	// Remove all aide_* directories.
	if err := removeAidePlugins(pluginsDir); err != nil {
		return err
	}

	// Create new directories for each desired hook that maps to a supported event.
	for _, h := range desired {
		nativeEvent := hermesEventMap[h.Event]
		if nativeEvent == "" {
			continue // unsupported event — skip silently
		}
		if err := provision.ValidateHookCommand(h.Command); err != nil {
			return fmt.Errorf("hermes hooks: %w", err)
		}

		hookDir := filepath.Join(pluginsDir, hookDirName(h.Command))
		if err := os.MkdirAll(hookDir, 0o750); err != nil {
			return fmt.Errorf("hermes hooks: mkdir: %w", err)
		}

		// Write __init__.py
		if err := writeInitPy(hookDir, h.Command); err != nil {
			return err
		}

		// Write plugin.yaml
		if err := writePluginYaml(hookDir, hookDirName(h.Command), nativeEvent); err != nil {
			return err
		}
	}

	return nil
}

// removeAidePlugins removes all aide_* plugin directories from pluginsDir.
func removeAidePlugins(pluginsDir string) error {
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("hermes hooks: readdir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "aide_") {
			if err := os.RemoveAll(filepath.Join(pluginsDir, e.Name())); err != nil {
				return fmt.Errorf("hermes hooks: remove: %w", err)
			}
		}
	}
	return nil
}

// readCommandFromInitPy reads the subprocess.run args from __init__.py
// and reconstructs the command string.
func readCommandFromInitPy(hookDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(hookDir, "__init__.py"))
	if err != nil {
		return "", err
	}
	content := string(data)
	// Extract the list from subprocess.run([...], check=True)
	start := strings.Index(content, "subprocess.run([")
	if start == -1 {
		return "", fmt.Errorf("hermes hooks: malformed __init__.py")
	}
	start += len("subprocess.run([")
	end := strings.Index(content[start:], "]")
	if end == -1 {
		return "", fmt.Errorf("hermes hooks: malformed __init__.py")
	}
	argsStr := content[start : start+end]
	return extractCommandFromArgs(argsStr)
}

// extractCommandFromArgs reconstructs a command from a Python list string
// like '"rtk", "hook", "hermes"' into "rtk hook hermes".
func extractCommandFromArgs(argsStr string) (string, error) {
	var args []string
	parts := strings.Split(argsStr, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "\"") && strings.HasSuffix(part, "\"") {
			args = append(args, part[1:len(part)-1])
		}
	}
	if len(args) == 0 {
		return "", fmt.Errorf("hermes hooks: no arguments found")
	}
	return strings.Join(args, " "), nil
}

// writeInitPy writes the __init__.py file with the given command.
func writeInitPy(hookDir, command string) error {
	// Parse command into list of arguments
	args := strings.Fields(command)
	var argsStr strings.Builder
	for i, arg := range args {
		if i > 0 {
			argsStr.WriteString(", ")
		}
		escaped := strings.ReplaceAll(arg, "\\", "\\\\")
		escaped = strings.ReplaceAll(escaped, "\"", "\\\"")
		fmt.Fprintf(&argsStr, "\"%s\"", escaped)
	}

	script := fmt.Sprintf("#!/usr/bin/env python3\nimport subprocess\nsubprocess.run([%s], check=True)\n", argsStr.String())
	path := filepath.Join(hookDir, "__init__.py")
	if err := fsutil.AtomicWrite(path, []byte(script)); err != nil {
		return fmt.Errorf("hermes hooks: write __init__.py: %w", err)
	}
	// Make it executable
	if err := os.Chmod(path, 0o755); err != nil {
		return fmt.Errorf("hermes hooks: chmod __init__.py: %w", err)
	}
	return nil
}

// writePluginYaml writes the plugin.yaml file.
func writePluginYaml(hookDir, dirName, nativeEvent string) error {
	yaml := fmt.Sprintf("name: %s\nhooks:\n  - %s\n", dirName, nativeEvent)
	path := filepath.Join(hookDir, "plugin.yaml")
	return fsutil.AtomicWrite(path, []byte(yaml))
}
