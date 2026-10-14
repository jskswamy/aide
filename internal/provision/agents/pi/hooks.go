package pi

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jskswamy/aide/internal/fsutil"
	"github.com/jskswamy/aide/internal/provision"
)

// piEventMap maps aide's normalized hook events to Pi's native
// extension events, per Pi's bundled docs/extensions.md: "tool_call"
// fires before a tool executes (the PreToolUse equivalent) and
// "tool_result" after it (the PostToolUse equivalent). Session events
// are left unmapped: Pi's "session_shutdown" also fires on session
// switch, not only on exit, so there is no clean session_end
// equivalent. WriteHooks skips unmapped events silently rather than
// guessing.
var piEventMap = map[string]string{
	"pre_tool":  "tool_call",
	"post_tool": "tool_result",
}

func extensionsDir(ctx provision.Context) string {
	return filepath.Join(agentDir(ctx), "extensions")
}

// piHookCodec implements provision.HookCodec for Pi's aide-*.ts
// extension files. As with the OpenCode codec, the generated file's
// behavior lives in a callback that's impractical to parse back out, so
// Decode reads two `// aide-*:` comment lines carrying the canonical
// (event, command) pair.
type piHookCodec struct{}

func (c *piHookCodec) Match(name string) bool {
	return provision.PiHookArtifact.Owns(name)
}

func (c *piHookCodec) Decode(path string) (provision.Hook, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return provision.Hook{}, err
	}
	var command, event string
	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case strings.HasPrefix(line, "// aide-event: "):
			native := strings.TrimPrefix(line, "// aide-event: ")
			event = provision.ReverseLookup(piEventMap, native, native)
		case strings.HasPrefix(line, "// aide-command: "):
			command = strings.TrimPrefix(line, "// aide-command: ")
		}
	}
	return provision.Hook{Event: event, Command: command}, nil
}

func (c *piHookCodec) Encode(dir string, h provision.Hook) error {
	nativeEvent, ok := piEventMap[h.Event]
	if !ok {
		return nil // unsupported event — skip silently
	}
	if err := provision.ValidateHookCommand(h.Command); err != nil {
		return fmt.Errorf("pi hooks: %w", err)
	}
	name := provision.PiHookArtifact.Name(h.Command)
	// Extension shape per Pi's docs/extensions.md: a default-exported
	// function receiving the ExtensionAPI. Node built-ins such as
	// node:child_process are available to extensions.
	script := "// Managed by aide. Do not edit manually.\n" +
		"// aide-event: " + nativeEvent + "\n" +
		"// aide-command: " + h.Command + "\n" +
		"import { execSync } from \"node:child_process\";\n" +
		"import type { ExtensionAPI } from \"@earendil-works/pi-coding-agent\";\n\n" +
		"export default function (pi: ExtensionAPI) {\n" +
		"  pi.on(\"" + nativeEvent + "\", async () => {\n" +
		"    execSync(\"" + h.Command + "\", { stdio: \"inherit\" });\n" +
		"  });\n" +
		"}\n"
	if err := fsutil.AtomicWrite(filepath.Join(dir, name), []byte(script)); err != nil {
		return fmt.Errorf("pi hooks: write extension: %w", err)
	}
	return nil
}

func (c *piHookCodec) Remove(path string) error {
	return os.Remove(path)
}

// ReadHooks returns aide-managed hooks by listing aide-*.ts extensions.
func (d *Driver) ReadHooks(ctx provision.Context) ([]provision.Hook, error) {
	return provision.ReadHooks(extensionsDir(ctx), &piHookCodec{})
}

// WriteHooks removes all aide-*.ts extensions and writes new ones for
// desired. prevManaged is unused for file-based formats; the aide-
// naming prefix is the ownership signal, same as gemini/hermes/opencode.
func (d *Driver) WriteHooks(ctx provision.Context, _ []provision.Hook, desired []provision.Hook) error {
	return provision.WriteHooks(extensionsDir(ctx), desired, &piHookCodec{})
}
