package launcher

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	aidectx "github.com/jskswamy/aide/internal/context"
	"github.com/jskswamy/aide/internal/sandbox"
	"github.com/jskswamy/aide/internal/ui"
)

// KnownAgents is the list of agent binaries aide can detect on PATH.
var KnownAgents = []string{
	"claude",
	"codex",
	"aider",
	"goose",
	"amp",
	"gemini",
	"copilot",
}

// LookPathFunc abstracts exec.LookPath for testability.
type LookPathFunc func(file string) (string, error)

// PassthroughResult describes what the passthrough scanner found.
type PassthroughResult struct {
	// Found agents with their resolved paths.
	Found map[string]string
}

// IsKnownAgent returns true if the agent name is in KnownAgents.
func IsKnownAgent(name string) bool {
	for _, a := range KnownAgents {
		if a == name {
			return true
		}
	}
	return false
}

// ScanAgents scans PATH for known agent binaries.
func ScanAgents(lookPath LookPathFunc) *PassthroughResult {
	found := make(map[string]string)
	for _, name := range KnownAgents {
		if path, err := lookPath(name); err == nil {
			found[name] = path
		}
	}
	return &PassthroughResult{Found: found}
}

// Passthrough handles the zero-config case: no config.yaml exists.
// If agentOverride is set, it launches that specific agent (must be known).
// Otherwise it scans PATH for known agents and auto-selects.
func (l *Launcher) Passthrough(cwd string, agentOverride string, extraArgs []string) error {
	lookPath := l.lookPath()

	// If user specified --agent, resolve and launch it directly.
	// Accept known agents and any binary resolvable on PATH.
	if agentOverride != "" {
		binary, err := lookPath(agentOverride)
		if err != nil {
			return fmt.Errorf(
				"agent %q not found on PATH.\n\n"+
					"Register it first: aide agents add %s --binary /path/to/binary\n"+
					"Known agents: %s",
				agentOverride, agentOverride, strings.Join(KnownAgents, ", "),
			)
		}
		return l.execAgent(cwd, agentOverride, binary, extraArgs)
	}

	// No --agent flag: scan PATH for known agents.
	result := ScanAgents(lookPath)

	switch len(result.Found) {
	case 0:
		return fmt.Errorf(
			"no config found and no known agents on PATH.\n\n"+
				"Install an agent or create a config file:\n"+
				"  aide init            Create ~/.config/aide/config.yaml\n\n"+
				"Supported agents: %s", strings.Join(KnownAgents, ", "),
		)

	case 1:
		var name, binary string
		for name, binary = range result.Found {
			break
		}
		_ = writeFirstRunHint(name)
		return l.execAgent(cwd, name, binary, extraArgs)

	default:
		var agents []string
		for name := range result.Found {
			agents = append(agents, name)
		}
		return fmt.Errorf(
			"multiple agents found on PATH: %s\n\n"+
				"Specify which agent to use:\n"+
				"  aide --agent %s     Run a specific agent\n"+
				"  aide init            Create a config to set a default",
			strings.Join(agents, ", "),
			agents[0],
		)
	}
}

// execAgent injects yolo flags if needed, applies the OS sandbox, and execs the agent.
func (l *Launcher) execAgent(cwd, name, binary string, extraArgs []string) error {
	if l.Yolo && !l.NoYolo {
		yoloArgs, err := YoloArgs(name)
		if err != nil {
			return err
		}
		extraArgs = append(yoloArgs, extraArgs...)
		fmt.Fprintf(l.stderr(), "\033[1;33mWARNING:\033[0m yolo mode enabled (source: --yolo flag)\n")
		fmt.Fprintln(l.stderr(), "  Agent permission checks are disabled.")
		fmt.Fprintln(l.stderr(), "  OS sandbox is active (use `aide sandbox show` to inspect).")
		fmt.Fprintln(l.stderr())
	}

	// Resolve project root from cwd (git root or cwd fallback).
	projectRoot := aidectx.ProjectRoot(cwd)

	// Create runtime directory for sandbox profile files.
	rtDir, err := NewRuntimeDir()
	if err != nil {
		return fmt.Errorf("creating runtime dir: %w", err)
	}
	// Best-effort cleanup of stale runtime dirs from previous runs.
	_ = CleanStale()

	tempDir := os.TempDir()

	policy := sandbox.DefaultPolicy(projectRoot, rtDir.Path(), tempDir, os.Environ())
	policy.AgentModule = ResolveAgentModule(name)

	cmd := &exec.Cmd{
		Path: binary,
		Args: append([]string{binary}, extraArgs...),
		Env:  os.Environ(),
	}

	sb := sandbox.NewSandbox()
	if err := sb.Apply(cmd, policy, rtDir.Path()); err != nil {
		return fmt.Errorf("applying sandbox: %w", err)
	}

	// Render minimal startup banner
	guardResults := sandbox.EvaluateGuards(&policy)
	availableNames := sandbox.AvailableGuardNames(policy.Guards)
	si := &ui.SandboxInfo{
		Network: networkDisplayName(string(policy.Network)),
	}
	for _, g := range guardResults {
		if len(g.Rules) > 0 {
			display := ui.GuardDisplay{
				Name:      g.Name,
				Protected: g.Protected,
				Allowed:   g.Allowed,
			}
			for _, o := range g.Overrides {
				display.Overrides = append(display.Overrides, ui.GuardOverride{
					EnvVar:      o.EnvVar,
					Value:       o.Value,
					DefaultPath: o.DefaultPath,
				})
			}
			si.Active = append(si.Active, display)
		} else if len(g.Skipped) > 0 {
			si.Skipped = append(si.Skipped, ui.GuardDisplay{
				Name:   g.Name,
				Reason: strings.Join(g.Skipped, "; "),
			})
		}
	}
	si.Available = availableNames
	bannerData := &ui.BannerData{
		AgentName: name,
		AgentPath: binary,
		Sandbox:   si,
		Yolo:      l.Yolo && !l.NoYolo,
	}
	if err := ui.RenderBanner(l.stderr(), "compact", bannerData); err != nil {
		fmt.Fprintf(l.stderr(), "warning: banner render failed: %v\n", err)
	}
	fmt.Fprintln(l.stderr())

	return l.Execer.Exec(cmd.Path, cmd.Args, cmd.Env)
}

// lookPath returns the LookPathFunc to use (real or injected for testing).
func (l *Launcher) lookPath() LookPathFunc {
	if l.LookPath != nil {
		return l.LookPath
	}
	return exec.LookPath
}

// firstRunHintDir returns the directory for the sentinel file.
func firstRunHintDir() string {
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		home, _ := os.UserHomeDir()
		configHome = filepath.Join(home, ".config")
	}
	return filepath.Join(configHome, "aide")
}

// writeFirstRunHint writes a sentinel file to suppress future hints.
func writeFirstRunHint(agentName string) error {
	dir := firstRunHintDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	sentinel := filepath.Join(dir, ".first-run-done")
	return os.WriteFile(sentinel, []byte(agentName+"\n"), 0o644)
}

// IsFirstRun returns true if the first-run sentinel file does not exist.
func IsFirstRun() bool {
	sentinel := filepath.Join(firstRunHintDir(), ".first-run-done")
	_, err := os.Stat(sentinel)
	return os.IsNotExist(err)
}
