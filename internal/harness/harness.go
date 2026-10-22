// Package harness is the single table of coding-agent harnesses aide
// knows about. Launcher discovery, sandbox module selection, yolo
// flags, display icons and provisioning profile support are all
// derived from it, so adding a harness means adding one entry here.
//
// pkg/seatbelt is public and must not import internal/, so the sandbox
// module is carried as a factory into pkg/seatbelt/modules rather than
// the modules reading this table.
package harness

import (
	"fmt"
	"path/filepath"
	"slices"
	"sort"

	"github.com/jskswamy/aide/internal/homepath"
	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/modules"
)

// Descriptor describes one harness.
type Descriptor struct {
	// Name is the canonical harness name: the binary aide discovers on
	// PATH and the value of a context's agent: field.
	Name string
	// Aliases are other names that resolve to this harness for
	// provisioning and display (e.g. the "cursor" provision driver for
	// "cursor-agent"). They are never used for PATH discovery or
	// sandbox selection.
	Aliases []string
	// Binary is the executable name looked up on PATH.
	Binary string
	// ProfileEnvKey is the env var aide sets to give a profile its own
	// config dir. Empty means profiles are not supported.
	ProfileEnvKey string
	// DefaultConfigDir is the home-relative, slash-separated config dir
	// the harness uses when ProfileEnvKey is unset. Empty when unknown.
	DefaultConfigDir string
	// ConfigEnvKeys are every env var that redirects the harness's
	// config or state location, including ProfileEnvKey.
	ConfigEnvKeys []string
	// Sandbox builds the harness's seatbelt module. Nil means aide has
	// no launcher or sandbox support for the harness.
	Sandbox func() seatbelt.Module
	// YoloSupported reports whether --yolo works for the harness.
	YoloSupported bool
	// YoloFlag is the skip-permissions flag. Empty with YoloSupported
	// means the harness never prompts, so nothing needs adding.
	YoloFlag string
	// Icon is the default display icon.
	Icon string
}

// Names returns the canonical name followed by the aliases.
func (d Descriptor) Names() []string { return append([]string{d.Name}, d.Aliases...) }

// SupportsProfiles reports whether the harness can run under a profile.
func (d Descriptor) SupportsProfiles() bool { return d.ProfileEnvKey != "" }

// DefaultDir is the harness's default config dir under home, or ""
// when it is not known.
func (d Descriptor) DefaultDir(home string) string {
	if d.DefaultConfigDir == "" {
		return ""
	}
	return filepath.Join(home, filepath.FromSlash(d.DefaultConfigDir))
}

// Launchable reports whether aide can discover, sandbox and launch the
// harness.
func (d Descriptor) Launchable() bool { return d.Sandbox != nil }

// YoloArgs returns the args that skip permission prompts, and false
// when the harness has no yolo mode.
func (d Descriptor) YoloArgs() ([]string, bool) {
	if !d.YoloSupported {
		return nil, false
	}
	if d.YoloFlag == "" {
		return nil, true
	}
	return []string{d.YoloFlag}, true
}

// opencodeSubcommands is the set of OpenCode v2 subcommands.
// Only "run" supports the --auto flag.
var opencodeSubcommands = map[string]bool{
	"acp": true, "api": true, "attach": true, "auth": true,
	"completion": true, "db": true, "debug": true, "export": true,
	"github": true, "import": true, "mcp": true, "mini": true,
	"models": true, "pair": true, "plugin": true, "pr": true,
	"reload": true, "run": true, "serve": true, "service": true,
	"session": true, "stats": true, "uninstall": true, "update": true,
	"upgrade": true, "web": true,
}

// ApplyYolo returns args with the harness's yolo flag applied.
// When yolo is unsupported or needs no flag (e.g. Pi), args is returned unchanged.
// If args already contains the yolo flag, args is returned unchanged.
// For OpenCode, subcommands are respected: '--auto' is placed after 'run',
// omitted for subcommands that do not accept it, and prepended for root invocations.
func (d Descriptor) ApplyYolo(args []string) []string {
	if !d.YoloSupported || d.YoloFlag == "" {
		return args
	}
	if slices.Contains(args, d.YoloFlag) {
		return args
	}

	if d.Name == "opencode" {
		if len(args) == 0 {
			return []string{d.YoloFlag}
		}
		if args[0] == "run" {
			out := make([]string, 0, len(args)+1)
			out = append(out, "run", d.YoloFlag)
			out = append(out, args[1:]...)
			return out
		}
		if opencodeSubcommands[args[0]] {
			// Subcommand does not accept --auto; do not inject it.
			return args
		}
	}

	out := make([]string, 0, len(args)+1)
	out = append(out, d.YoloFlag)
	out = append(out, args...)
	return out
}

// opencodeStandaloneSubcommands are the subcommands that start a session
// and accept --standalone.
var opencodeStandaloneSubcommands = map[string]bool{"run": true, "mini": true}

// ApplyLaunchArgs returns args with the flags every launch of the
// harness needs, independent of yolo. OpenCode v2 otherwise attaches
// to a per-user background service that keeps the env of whichever
// launch started it, so the profile's env and sandbox are ignored;
// '--standalone' gives the session a private server. It goes after
// 'run'/'mini', is omitted for other subcommands, and is prepended
// for root invocations. An explicit --standalone or --server wins.
func (d Descriptor) ApplyLaunchArgs(args []string) []string {
	if d.Name != "opencode" || slices.Contains(args, "--standalone") || slices.Contains(args, "--server") {
		return args
	}
	if len(args) > 0 && opencodeStandaloneSubcommands[args[0]] {
		return append([]string{args[0], "--standalone"}, args[1:]...)
	}
	if len(args) > 0 && opencodeSubcommands[args[0]] {
		return args
	}
	return append([]string{"--standalone"}, args...)
}

// descriptors is the harness table, kept in name order.
var descriptors = []Descriptor{
	{
		Name:          "agy",
		Aliases:       []string{"antigravity"},
		Binary:        "agy",
		Sandbox:       modules.AntigravityAgent,
		YoloSupported: true,
		YoloFlag:      "--dangerously-skip-permissions",
		Icon:          "🛸",
	},
	{
		Name:    "aider",
		Binary:  "aider",
		Sandbox: modules.AiderAgent,
	},
	{
		Name:          "amp",
		Binary:        "amp",
		ConfigEnvKeys: []string{"AMP_HOME"},
		Sandbox:       modules.AmpAgent,
	},
	{
		Name:             "claude",
		Binary:           "claude",
		ProfileEnvKey:    "CLAUDE_CONFIG_DIR",
		DefaultConfigDir: ".claude",
		ConfigEnvKeys:    []string{"CLAUDE_CONFIG_DIR"},
		Sandbox:          modules.ClaudeAgent,
		YoloSupported:    true,
		YoloFlag:         "--dangerously-skip-permissions",
		Icon:             "🤖",
	},
	{
		Name:             "codex",
		Binary:           "codex",
		ProfileEnvKey:    "CODEX_HOME",
		DefaultConfigDir: ".codex",
		ConfigEnvKeys:    []string{"CODEX_HOME"},
		Sandbox:          modules.CodexAgent,
		YoloSupported:    true,
		YoloFlag:         "--approve-for-me",
		Icon:             "📝",
	},
	{
		Name:             "copilot",
		Binary:           "copilot",
		ProfileEnvKey:    "COPILOT_HOME",
		DefaultConfigDir: ".copilot",
		ConfigEnvKeys:    []string{"COPILOT_HOME"},
		Sandbox:          modules.CopilotAgent,
		YoloSupported:    true,
		YoloFlag:         "--yolo",
		Icon:             "✈️",
	},
	{
		Name:          "cursor-agent",
		Aliases:       []string{"cursor"},
		Binary:        "cursor-agent",
		ConfigEnvKeys: []string{"CURSOR_CONFIG_DIR"},
		Sandbox:       modules.CursorAgent,
		Icon:          "🖱",
	},
	{
		// GEMINI_HOME is what aide has always used; upstream Gemini CLI
		// reads GEMINI_CLI_HOME. Left as is: Gemini is out of scope.
		Name:             "gemini",
		Binary:           "gemini",
		ProfileEnvKey:    "GEMINI_HOME",
		DefaultConfigDir: ".gemini",
		ConfigEnvKeys:    []string{"GEMINI_HOME"},
		Sandbox:          modules.GeminiAgent,
		YoloSupported:    true,
		YoloFlag:         "--yolo",
		Icon:             "✨",
	},
	{
		Name:          "goose",
		Binary:        "goose",
		ConfigEnvKeys: []string{"GOOSE_PATH_ROOT"},
		Sandbox:       modules.GooseAgent,
	},
	{
		// Hermes has a hook provisioning driver but no sandbox module, so
		// it is not launchable and not discovered on PATH.
		Name:   "hermes",
		Binary: "hermes",
	},
	{
		Name:             "opencode",
		Binary:           "opencode",
		ProfileEnvKey:    "OPENCODE_CONFIG_DIR",
		DefaultConfigDir: ".config/opencode",
		ConfigEnvKeys:    []string{"OPENCODE_CONFIG_DIR", "OPENCODE_CONFIG"},
		Sandbox:          modules.OpenCodeAgent,
		YoloSupported:    true,
		YoloFlag:         "--auto",
		Icon:             "🔷",
	},
	{
		// Pi's --approve trusts project-local files, a separate decision
		// aide leaves to the user; it is not a permissions bypass.
		// PI_CODING_AGENT_SESSION_DIR moves sessions out of the config
		// dir (`pi --help`), so a shell export would share them across
		// profiles.
		Name:             "pi",
		Binary:           "pi",
		ProfileEnvKey:    "PI_CODING_AGENT_DIR",
		DefaultConfigDir: ".pi/agent",
		ConfigEnvKeys:    []string{"PI_CODING_AGENT_DIR", "PI_CODING_AGENT_SESSION_DIR"},
		Sandbox:          modules.PiAgent,
		YoloSupported:    true,
		Icon:             "🥧",
	},
}

func init() {
	if !sort.SliceIsSorted(descriptors, func(i, j int) bool {
		return descriptors[i].Name < descriptors[j].Name
	}) {
		panic("harness: descriptors must be sorted by name")
	}
}

// All returns every descriptor in name order. The result is a copy.
func All() []Descriptor {
	out := make([]Descriptor, len(descriptors))
	for i, d := range descriptors {
		out[i] = d.clone()
	}
	return out
}

// clone returns d with its slices copied, so callers cannot mutate the
// table.
func (d Descriptor) clone() Descriptor {
	d.Aliases = slices.Clone(d.Aliases)
	d.ConfigEnvKeys = slices.Clone(d.ConfigEnvKeys)
	return d
}

// Lookup returns the descriptor whose name or alias is name.
func Lookup(name string) (Descriptor, bool) {
	for _, d := range descriptors {
		if slices.Contains(d.Names(), name) {
			return d.clone(), true
		}
	}
	return Descriptor{}, false
}

// ProfileConfigDir is the native config dir harness uses under profile
// name: override tilde-expanded against home when set, otherwise
// ~/.<harness>-<name>.
func ProfileConfigDir(harness, name, override, home string) string {
	if override != "" {
		return homepath.Expand(override, home)
	}
	return filepath.Join(home, fmt.Sprintf(".%s-%s", harness, name))
}
