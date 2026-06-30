package hermes

import (
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

// hermesHookCodec implements provision.HookCodec for hermes' aide_<hash>/ directory artifacts.
type hermesHookCodec struct{}

func (c *hermesHookCodec) Match(name string) bool {
	return provision.HermesHookArtifact.Owns(name)
}

func (c *hermesHookCodec) Decode(path string) (provision.Hook, error) {
	cmd, err := readCommandFromInitPy(path)
	if err != nil {
		return provision.Hook{}, err
	}
	return provision.Hook{Event: "pre_tool", Command: cmd}, nil
}

func (c *hermesHookCodec) Encode(dir string, h provision.Hook) error {
	nativeEvent := hermesEventMap[h.Event]
	if nativeEvent == "" {
		return nil // unsupported event — skip silently
	}
	if err := provision.ValidateHookCommand(h.Command); err != nil {
		return fmt.Errorf("hermes hooks: %w", err)
	}

	hookDir := filepath.Join(dir, provision.HermesHookArtifact.Name(h.Command))
	if err := os.MkdirAll(hookDir, 0o750); err != nil {
		return fmt.Errorf("hermes hooks: mkdir: %w", err)
	}

	// Write __init__.py
	if err := writeInitPy(hookDir, h.Command); err != nil {
		return err
	}

	// Write plugin.yaml
	if err := writePluginYaml(hookDir, provision.HermesHookArtifact.Name(h.Command), nativeEvent); err != nil {
		return err
	}

	return nil
}

func (c *hermesHookCodec) Remove(path string) error {
	return os.RemoveAll(path)
}

// ReadHooks returns aide-managed hooks from ~/.hermes/plugins/aide_*/ directories.
func (d *Driver) ReadHooks(ctx provision.Context) ([]provision.Hook, error) {
	return readHermesHooks(hermesPluginsDir(ctx))
}

// readHermesHooks is a custom read path for hermes because its artifacts are directories,
// which ReadHooks doesn't enumerate by default (ReadDir returns all entries).
// This wrapper filters for directories before delegating to the codec.
func readHermesHooks(dir string) ([]provision.Hook, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("readdir: %w", err)
	}
	var out []provision.Hook
	codec := &hermesHookCodec{}
	for _, e := range entries {
		if !e.IsDir() || !codec.Match(e.Name()) {
			continue
		}
		hook, err := codec.Decode(filepath.Join(dir, e.Name()))
		if err != nil {
			continue // skip malformed artifacts
		}
		out = append(out, hook)
	}
	return out, nil
}

// WriteHooks reconciles desired hooks into ~/.hermes/plugins/aide_*/ directories.
// prevManaged is unused for file-based formats; aide_ naming is the ownership signal.
func (d *Driver) WriteHooks(ctx provision.Context, _ []provision.Hook, desired []provision.Hook) error {
	return writeHermesHooks(hermesPluginsDir(ctx), desired)
}

// writeHermesHooks is a custom write path for hermes because its artifacts are directories.
// Standard WriteHooks works, but this custom version is clearer for hermes-specific behavior.
func writeHermesHooks(dir string, desired []provision.Hook) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	// Remove all aide_* directories.
	if existing, err := os.ReadDir(dir); err == nil {
		codec := &hermesHookCodec{}
		for _, e := range existing {
			if e.IsDir() && codec.Match(e.Name()) {
				if err := codec.Remove(filepath.Join(dir, e.Name())); err != nil {
					return fmt.Errorf("remove: %w", err)
				}
			}
		}
	}

	// Write new directories for each desired hook.
	codec := &hermesHookCodec{}
	for _, h := range desired {
		if err := codec.Encode(dir, h); err != nil {
			return err
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
