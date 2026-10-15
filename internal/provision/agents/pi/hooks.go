package pi

import (
	"path/filepath"

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

// piMatcherMap maps aide's normalized matchers to Pi's built-in tool
// names, as carried in the event's toolName (types.d.ts in pi 1.1.0).
var piMatcherMap = map[string]string{
	"shell": "bash",
}

func extensionsDir(ctx provision.Context) string {
	return filepath.Join(agentDir(ctx), "extensions")
}

// piHookCodec reads and writes Pi's aide-*.ts extension files. The
// extension shape follows Pi's docs/extensions.md: a default-exported
// function receiving the ExtensionAPI. Node built-ins such as
// node:child_process are available to extensions.
var piHookCodec = &provision.CommentHookCodec{
	Label:    "pi hooks",
	Artifact: provision.PiHookArtifact,
	Events:   piEventMap,
	Matchers: piMatcherMap,
	Body: func(nativeEvent, toolGuard, command string) string {
		handler, guard := "async () => {\n", ""
		if toolGuard != "" {
			handler = "async (event) => {\n"
			guard = "    if (event.toolName !== " + toolGuard + ") return;\n"
		}
		return "import { execSync } from \"node:child_process\";\n" +
			"import type { ExtensionAPI } from \"@earendil-works/pi-coding-agent\";\n\n" +
			"export default function (pi: ExtensionAPI) {\n" +
			"  pi.on(\"" + nativeEvent + "\", " + handler +
			guard +
			"    execSync(\"" + command + "\", { stdio: \"inherit\" });\n" +
			"  });\n" +
			"}\n"
	},
}

// ReadHooks returns aide-managed hooks by listing aide-*.ts extensions.
func (d *Driver) ReadHooks(ctx provision.Context) ([]provision.Hook, error) {
	return provision.ReadHooks(extensionsDir(ctx), piHookCodec)
}

// WriteHooks removes all aide-*.ts extensions and writes new ones for
// desired. prevManaged is unused for file-based formats; the aide-
// naming prefix is the ownership signal, same as gemini/hermes/opencode.
func (d *Driver) WriteHooks(ctx provision.Context, _ []provision.Hook, desired []provision.Hook) error {
	return provision.WriteHooks(extensionsDir(ctx), desired, piHookCodec)
}
