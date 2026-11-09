package omp

import (
	"path/filepath"

	"github.com/jskswamy/aide/internal/provision"
)

// ompEventMap maps aide's normalized hook events to OMP's native
// extension events (tool_call, tool_result, session_start,
// session_shutdown). Generated session boundary handlers skip the
// "reload" reason so live extension reloads do not re-fire session
// hooks. WriteHooks skips unmapped events silently.
var ompEventMap = map[string]string{
	"pre_tool":      "tool_call",
	"post_tool":     "tool_result",
	"session_start": "session_start",
	"session_end":   "session_shutdown",
}

// ompMatcherMap maps aide's normalized matchers to OMP's built-in tool
// names carried in event.toolName.
var ompMatcherMap = map[string]string{
	"shell": "bash",
}

func extensionsDir(ctx provision.Context) string {
	return filepath.Join(agentDir(ctx), "extensions")
}

// ompHookCodec reads and writes OMP's aide-*.ts extension files under
// <agentDir>/extensions/.
var ompHookCodec = &provision.CommentHookCodec{
	Label:    "omp hooks",
	Artifact: provision.OMPHookArtifact,
	Events:   ompEventMap,
	Matchers: ompMatcherMap,
	Body: func(nativeEvent, toolGuard, command string) string {
		handler, guard := "async () => {\n", ""
		switch {
		case nativeEvent == "session_start" || nativeEvent == "session_shutdown":
			handler = "async (event) => {\n"
			guard = "    if (event.reason === \"reload\") return;\n"
		case toolGuard != "":
			handler = "async (event) => {\n"
			guard = "    if (event.toolName !== " + toolGuard + ") return;\n"
		}
		return "import { execSync } from \"node:child_process\";\n" +
			"import type { ExtensionAPI } from \"@oh-my-pi/pi-coding-agent\";\n\n" +
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
	return provision.ReadHooks(extensionsDir(ctx), ompHookCodec)
}

// WriteHooks removes all aide-*.ts extensions and writes new ones for desired.
func (d *Driver) WriteHooks(ctx provision.Context, _ []provision.Hook, desired []provision.Hook) error {
	return provision.WriteHooks(extensionsDir(ctx), desired, ompHookCodec)
}
