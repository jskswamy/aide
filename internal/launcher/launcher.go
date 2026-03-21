package launcher

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/jskswamy/aide/internal/config"
	aidectx "github.com/jskswamy/aide/internal/context"
	"github.com/jskswamy/aide/internal/sandbox"
	"github.com/jskswamy/aide/internal/secrets"
)

// Execer abstracts process execution for testability.
type Execer interface {
	Exec(binary string, args []string, env []string) error
}

// SyscallExecer replaces the current process with the given binary via syscall.Exec.
type SyscallExecer struct{}

// Exec calls syscall.Exec, replacing the current process.
func (s *SyscallExecer) Exec(binary string, args []string, env []string) error {
	return syscall.Exec(binary, args, env)
}

// agentYoloFlags maps agent names to their "skip all permissions" flags.
var agentYoloFlags = map[string]string{
	"claude": "--dangerously-skip-permissions",
	"codex":  "--full-auto",
}

// Launcher orchestrates the full agent launch flow.
type Launcher struct {
	Execer    Execer
	ConfigDir string       // override for testing (default: config.ConfigDir())
	LookPath  LookPathFunc // override for testing (default: exec.LookPath)
	Yolo      bool         // inject agent-specific skip-permissions flag
}

// configDir returns the effective config directory.
func (l *Launcher) configDir() string {
	if l.ConfigDir != "" {
		return l.ConfigDir
	}
	return config.ConfigDir()
}

// Launch resolves context, decrypts secrets, resolves templates, creates
// a runtime directory, applies sandbox policy, and execs the agent binary.
func (l *Launcher) Launch(cwd string, agentOverride string, extraArgs []string, cleanEnv bool) error {
	// 1. Load config
	cfg, err := config.Load(l.configDir(), cwd)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// 2. Detect git remote + project root
	remoteURL := aidectx.DetectRemote(cwd, "origin")
	projectRoot := aidectx.ProjectRoot(cwd)

	// 3. Resolve context
	rc, err := aidectx.Resolve(cfg, cwd, remoteURL)
	if err != nil {
		return fmt.Errorf("resolving context: %w", err)
	}

	// 4. If agentOverride is set, validate and override context's agent.
	// Accept: known agents, agents defined in config, or names resolvable on PATH.
	if agentOverride != "" {
		_, inAgentsMap := cfg.Agents[agentOverride]
		if !IsKnownAgent(agentOverride) && !inAgentsMap {
			lookPath := l.lookPath()
			if _, err := lookPath(agentOverride); err != nil {
				return fmt.Errorf(
					"unknown agent %q (not in known agents, config, or PATH).\n\n"+
						"Register it first: aide agents add %s --binary /path/to/binary\n"+
						"Known agents: %s",
					agentOverride, agentOverride, strings.Join(KnownAgents, ", "),
				)
			}
		}
		rc.Context.Agent = agentOverride
	}

	// 5. Look up agent binary
	agentName := rc.Context.Agent
	binary, err := resolveAgentBinary(cfg, agentName)
	if err != nil {
		return err
	}

	// 5b. Inject yolo flag if requested
	if l.Yolo {
		yoloArgs, err := YoloArgs(agentName)
		if err != nil {
			return err
		}
		extraArgs = append(yoloArgs, extraArgs...)
	}

	// 6. Create runtime dir, register signal handlers
	rtDir, err := NewRuntimeDir()
	if err != nil {
		return fmt.Errorf("creating runtime dir: %w", err)
	}
	cancelSignals := rtDir.RegisterSignalHandlers()
	defer cancelSignals()

	// Helper to clean up on error
	cleanup := func() {
		_ = rtDir.Cleanup()
	}

	// 7. Clean stale dirs (best-effort)
	_ = CleanStale()

	// 8. Decrypt secrets if context has Secret
	var secretsMap map[string]string
	if rc.Context.Secret != "" {
		secretsPath := config.ResolveSecretPath(rc.Context.Secret)
		identity, err := secrets.DiscoverAgeKey()
		if err != nil {
			cleanup()
			return fmt.Errorf("discovering age key: %w", err)
		}
		secretsMap, err = secrets.DecryptSecretsFile(secretsPath, identity)
		if err != nil {
			cleanup()
			return fmt.Errorf("decrypting secrets: %w", err)
		}
	}

	// 9. Build TemplateData, resolve templates
	templateData := &config.TemplateData{
		Secrets:     secretsMap,
		ProjectRoot: projectRoot,
		RuntimeDir:  rtDir.Path(),
	}

	resolvedEnv, err := config.ResolveTemplates(rc.Context.Env, templateData)
	if err != nil {
		cleanup()
		return wrapTemplateError(err, rc.Name, rc.Context.Secret)
	}

	// 10. Build environment
	var baseEnv []string
	if cleanEnv {
		baseEnv = filterEssentialEnv(os.Environ())
	} else {
		baseEnv = os.Environ()
	}

	// Merge resolved env on top of base
	env := mergeEnv(baseEnv, resolvedEnv)

	// 11. Resolve binary to absolute path (syscall.Exec requires it).
	// Must happen before sandbox wrapping since sandbox rewrites cmd.Path.
	if !filepath.IsAbs(binary) {
		lookPath := l.lookPath()
		resolved, err := lookPath(binary)
		if err != nil {
			cleanup()
			return fmt.Errorf("agent %q not found on PATH: %w", binary, err)
		}
		binary = resolved
	}

	// 12. Apply sandbox (DD-18: always applied unless explicitly disabled).
	// ResolveSandboxRef resolves named profiles; PolicyFromConfig handles nil → defaults.
	sandboxCfg, sbDisabled, sbErr := sandbox.ResolveSandboxRef(rc.Context.Sandbox, cfg.Sandboxes)
	if sbErr != nil {
		cleanup()
		return fmt.Errorf("resolving sandbox: %w", sbErr)
	}
	if !sbDisabled {
		homeDir, _ := os.UserHomeDir()
		tempDir := os.TempDir()
		policy, err := sandbox.PolicyFromConfig(sandboxCfg, projectRoot, rtDir.Path(), homeDir, tempDir)
		if err != nil {
			cleanup()
			return fmt.Errorf("building sandbox policy: %w", err)
		}
		if policy != nil {
			cmd := &exec.Cmd{
				Path: binary,
				Args: append([]string{binary}, extraArgs...),
				Env:  env,
			}
			sb := sandbox.NewSandbox()
			if err := sb.Apply(cmd, *policy, rtDir.Path()); err != nil {
				cleanup()
				return fmt.Errorf("applying sandbox: %w", err)
			}
			binary = cmd.Path
			extraArgs = cmd.Args[1:]
			env = cmd.Env
		}
	}

	// 13. Exec the agent binary
	args := append([]string{binary}, extraArgs...)
	return l.Execer.Exec(binary, args, env)
}

// resolveAgentBinary determines the binary path from config and agent name.
func resolveAgentBinary(cfg *config.Config, agentName string) (string, error) {
	if agentName == "" {
		return "", fmt.Errorf("no agent specified in context")
	}

	// Look up in agents map
	if agent, ok := cfg.Agents[agentName]; ok {
		return agent.Binary, nil
	}

	// If there are agents defined but this one isn't found, that's an error
	if len(cfg.Agents) > 0 {
		return "", fmt.Errorf("agent %q not found in agents map", agentName)
	}

	// No agents map at all (minimal config without normalization) - use agent name as binary
	return agentName, nil
}

// YoloArgs returns the skip-permissions args for the given agent.
// Returns an error if the agent does not support yolo mode.
func YoloArgs(agentName string) ([]string, error) {
	// Normalize: strip path prefix to match by binary basename.
	base := filepath.Base(agentName)
	if flag, ok := agentYoloFlags[base]; ok {
		return []string{flag}, nil
	}
	supported := make([]string, 0, len(agentYoloFlags))
	for k := range agentYoloFlags {
		supported = append(supported, k)
	}
	return nil, fmt.Errorf(
		"--yolo not supported for agent %q. Supported agents: %s",
		agentName, strings.Join(supported, ", "),
	)
}

// wrapTemplateError converts raw Go template errors into actionable messages.
func wrapTemplateError(err error, contextName string, secret string) error {
	msg := err.Error()

	if strings.Contains(msg, "map has no entry for key") {
		if secret == "" {
			return fmt.Errorf(
				"context %q references secrets in env vars but has no secret configured.\n\n"+
					"Fix with: aide env set <KEY> --from-secret",
				contextName,
			)
		}
		return fmt.Errorf(
			"context %q: secret key not found in %s.\n\n"+
				"Available keys: aide secrets keys %s\n"+
				"Re-wire:        aide env set <KEY> --from-secret",
			contextName, secret, secret,
		)
	}

	if strings.Contains(msg, "nil pointer") || strings.Contains(msg, "can't evaluate field") {
		return fmt.Errorf(
			"context %q references secrets but has no secret configured.\n\n"+
				"Fix with: aide env set <KEY> --from-secret",
			contextName,
		)
	}

	return fmt.Errorf("context %q: %w", contextName, err)
}

// filterEssentialEnv keeps only essential environment variables.
func filterEssentialEnv(env []string) []string {
	essential := map[string]bool{
		"PATH": true, "HOME": true, "USER": true,
		"SHELL": true, "TERM": true, "LANG": true,
		"TMPDIR": true, "XDG_RUNTIME_DIR": true, "XDG_CONFIG_HOME": true,
	}
	var filtered []string
	for _, e := range env {
		k, _, _ := strings.Cut(e, "=")
		if essential[k] {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

// mergeEnv adds resolved env vars on top of base env, replacing any
// existing entries with the same key.
func mergeEnv(base []string, resolved map[string]string) []string {
	// Build a map of resolved keys for quick lookup
	resolvedKeys := make(map[string]bool, len(resolved))
	for k := range resolved {
		resolvedKeys[k] = true
	}

	// Filter out base entries that will be overridden
	var result []string
	for _, e := range base {
		k, _, _ := strings.Cut(e, "=")
		if !resolvedKeys[k] {
			result = append(result, e)
		}
	}

	// Append resolved entries
	for k, v := range resolved {
		result = append(result, k+"="+v)
	}

	return result
}
