package launcher

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// KnownAgents is the list of agent binaries aide can detect on PATH.
var KnownAgents = []string{
	"claude",
	"codex",
	"aider",
	"goose",
	"amp",
}

// LookPathFunc abstracts exec.LookPath for testability.
type LookPathFunc func(file string) (string, error)

// PassthroughResult describes what the passthrough scanner found.
type PassthroughResult struct {
	// Found agents with their resolved paths.
	Found map[string]string
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
// It scans PATH for known agents and either execs the single found agent,
// returns a helpful error listing multiple agents, or suggests installation.
func (l *Launcher) Passthrough(cwd string, extraArgs []string) error {
	lookPath := l.lookPath()

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
		// Single agent found — exec it directly.
		var name, binary string
		for name, binary = range result.Found {
			break
		}

		_ = writeFirstRunHint(name)

		// Inject yolo flag if requested.
		if l.Yolo {
			yoloArgs, err := YoloArgs(name)
			if err != nil {
				return err
			}
			extraArgs = append(yoloArgs, extraArgs...)
		}

		args := append([]string{binary}, extraArgs...)
		return l.Execer.Exec(binary, args, os.Environ())

	default:
		// Multiple agents found — ask user to be specific.
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
