package opencode

import (
	"path/filepath"
	"strings"

	"github.com/jskswamy/aide/internal/provision"
)

// openCodeEventMap maps aide's normalized hook events to OpenCode's
// native plugin hooks, verified against opencode 1.19.0.
// tool.execute.before/after are the direct PreToolUse/PostToolUse
// equivalents. session_start maps to the session.created bus event,
// which reaches plugins through the generic "event" hook, so the
// generated plugin filters for it and skips subagent child sessions.
// session_end maps to the plugin "dispose" hook, which runs when
// opencode exits normally (not when it is killed). WriteHooks skips
// unmapped events silently.
var openCodeEventMap = map[string]string{
	"pre_tool":      "tool.execute.before",
	"post_tool":     "tool.execute.after",
	"session_start": openCodeSessionCreated,
	"session_end":   "dispose",
}

// openCodeSessionCreated is the bus event type for a new session. It is
// not a plugin hook name; the generated plugin listens on "event".
const openCodeSessionCreated = "session.created"

// openCodeMatcherMap maps aide's normalized matchers to OpenCode's
// built-in tool ids, as passed in the hook's input.tool (verified
// against opencode 1.19.0).
var openCodeMatcherMap = map[string]string{
	"shell": "bash",
}

func openCodeHooksDir(ctx provision.Context) string {
	return filepath.Join(agentDir(ctx), "plugin")
}

// openCodeHookCodec reads and writes OpenCode's aide-*.js plugin files.
// Each exports the OpenCode plugin shape, shelling out to the hook
// command through Bun's `$`.
var openCodeHookCodec = &provision.CommentHookCodec{
	Label:    "opencode hooks",
	Artifact: provision.OpenCodeHookArtifact,
	Events:   openCodeEventMap,
	Matchers: openCodeMatcherMap,
	Body: func(nativeEvent, toolGuard, command string) string {
		hook, handler, guard := nativeEvent, "async () => {\n", ""
		switch {
		case nativeEvent == openCodeSessionCreated:
			hook, handler = "event", "async ({ event }) => {\n"
			guard = "    if (event.type !== \"" + openCodeSessionCreated + "\" || event.properties.info?.parentID) return\n"
		case toolGuard != "" && strings.HasPrefix(nativeEvent, "tool."):
			handler = "async (input) => {\n"
			guard = "    if (input.tool !== " + toolGuard + ") return\n"
		}
		return "export const AideHook = async ({ $ }) => ({\n" +
			"  \"" + hook + "\": " + handler +
			guard +
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
