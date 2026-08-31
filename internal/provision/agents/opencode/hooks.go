package opencode

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jskswamy/aide/internal/fsutil"
	"github.com/jskswamy/aide/internal/provision"
)

// openCodeEventMap maps aide's normalized hook events to OpenCode's
// native plugin hook names. Confirmed via OpenCode's plugin API docs:
// tool.execute.before/after are the direct PreToolUse/PostToolUse
// equivalents. session_start/session_end have no confirmed
// one-to-one native event (OpenCode's "event" hook fires on a wider
// range of payloads) — they're intentionally left unmapped for now;
// WriteHooks skips unmapped events silently rather than guessing at a
// filter condition that hasn't been verified.
var openCodeEventMap = map[string]string{
	"pre_tool":  "tool.execute.before",
	"post_tool": "tool.execute.after",
}

func openCodeHooksDir(ctx provision.Context) string {
	return filepath.Join(ctx.HomeDir, ".config", "opencode", "plugin")
}

// openCodeHookCodec implements provision.HookCodec for OpenCode's
// aide-*.js plugin files. The generated file's real behavior lives in
// a JS callback, which is impractical to parse back out — Decode
// instead reads two `// aide-*:` comment lines that carry the
// canonical (event, command) pair, the same "metadata survives the
// round-trip even though the executable body doesn't need to be
// re-parsed" approach gemini's hook codec takes with its `exec `
// line, just via comments instead of a directly-executable line.
type openCodeHookCodec struct{}

func (c *openCodeHookCodec) Match(name string) bool {
	return provision.OpenCodeHookArtifact.Owns(name)
}

func (c *openCodeHookCodec) Decode(path string) (provision.Hook, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return provision.Hook{}, err
	}
	var command, event string
	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case strings.HasPrefix(line, "// aide-event: "):
			native := strings.TrimPrefix(line, "// aide-event: ")
			event = provision.ReverseLookup(openCodeEventMap, native, native)
		case strings.HasPrefix(line, "// aide-command: "):
			command = strings.TrimPrefix(line, "// aide-command: ")
		}
	}
	return provision.Hook{Event: event, Command: command}, nil
}

func (c *openCodeHookCodec) Encode(dir string, h provision.Hook) error {
	nativeEvent, ok := openCodeEventMap[h.Event]
	if !ok {
		return nil // unsupported event — skip silently
	}
	if err := provision.ValidateHookCommand(h.Command); err != nil {
		return fmt.Errorf("opencode hooks: %w", err)
	}
	name := provision.OpenCodeHookArtifact.Name(h.Command)
	script := "// Managed by aide. Do not edit manually.\n" +
		"// aide-event: " + nativeEvent + "\n" +
		"// aide-command: " + h.Command + "\n" +
		"export const AideHook = async ({ $ }) => ({\n" +
		"  \"" + nativeEvent + "\": async () => {\n" +
		"    await $`" + h.Command + "`\n" +
		"  },\n" +
		"})\n"
	if err := fsutil.AtomicWrite(filepath.Join(dir, name), []byte(script)); err != nil {
		return fmt.Errorf("opencode hooks: write plugin: %w", err)
	}
	return nil
}

func (c *openCodeHookCodec) Remove(path string) error {
	return os.Remove(path)
}

// ReadHooks returns aide-managed hooks by listing aide-*.js plugins.
func (d *Driver) ReadHooks(ctx provision.Context) ([]provision.Hook, error) {
	return provision.ReadHooks(openCodeHooksDir(ctx), &openCodeHookCodec{})
}

// WriteHooks removes all aide-*.js plugins and writes new ones for
// desired. prevManaged is unused for file-based formats; the aide-
// naming prefix is the ownership signal, same as gemini/hermes.
func (d *Driver) WriteHooks(ctx provision.Context, _ []provision.Hook, desired []provision.Hook) error {
	return provision.WriteHooks(openCodeHooksDir(ctx), desired, &openCodeHookCodec{})
}
