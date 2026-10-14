package opencode

import (
	"path/filepath"

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

// openCodeHookCodec reads and writes OpenCode's aide-*.js plugin files.
// Each exports the OpenCode plugin shape, shelling out to the hook
// command through Bun's `$`.
var openCodeHookCodec = &provision.CommentHookCodec{
	Label:    "opencode hooks",
	Artifact: provision.OpenCodeHookArtifact,
	Events:   openCodeEventMap,
	Body: func(nativeEvent, command string) string {
		return "export const AideHook = async ({ $ }) => ({\n" +
			"  \"" + nativeEvent + "\": async () => {\n" +
			"    await $`" + command + "`\n" +
			"  },\n" +
			"})\n"
	},
}

// ReadHooks returns aide-managed hooks by listing aide-*.js plugins.
func (d *Driver) ReadHooks(ctx provision.Context) ([]provision.Hook, error) {
	return provision.ReadHooks(openCodeHooksDir(ctx), openCodeHookCodec)
}

// WriteHooks removes all aide-*.js plugins and writes new ones for
// desired. prevManaged is unused for file-based formats; the aide-
// naming prefix is the ownership signal, same as gemini/hermes.
func (d *Driver) WriteHooks(ctx provision.Context, _ []provision.Hook, desired []provision.Hook) error {
	return provision.WriteHooks(openCodeHooksDir(ctx), desired, openCodeHookCodec)
}
