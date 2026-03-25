package launcher

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/jskswamy/aide/internal/capability"
	"github.com/jskswamy/aide/internal/config"
	aidectx "github.com/jskswamy/aide/internal/context"
	"github.com/jskswamy/aide/internal/sandbox"
	"github.com/jskswamy/aide/internal/secrets"
	"github.com/jskswamy/aide/internal/ui"
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
	"gemini": "--yolo",
}

// Launcher orchestrates the full agent launch flow.
type Launcher struct {
	Execer    Execer
	ConfigDir string       // override for testing (default: config.Dir())
	LookPath  LookPathFunc // override for testing (default: exec.LookPath)
	Yolo      bool         // inject agent-specific skip-permissions flag
	NoYolo    bool         // override: disable yolo mode (overrides config and --yolo)
	Stderr    io.Writer    // override for testing (default: os.Stderr)
}

// stderr returns the effective stderr writer.
func (l *Launcher) stderr() io.Writer {
	if l.Stderr != nil {
		return l.Stderr
	}
	return os.Stderr
}

// configDir returns the effective config directory.
func (l *Launcher) configDir() string {
	if l.ConfigDir != "" {
		return l.ConfigDir
	}
	return config.Dir()
}

// Launch resolves context, decrypts secrets, resolves templates, creates
// a runtime directory, applies sandbox policy, and execs the agent binary.
func (l *Launcher) Launch(cwd string, agentOverride string, extraArgs []string, cleanEnv bool, resolve bool, withCaps []string, withoutCaps []string) error {
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

	// 5b. Resolve effective auto-approve from config layers + CLI flags.
	// Priority: --no-auto-approve/--no-yolo (highest) > --auto-approve/--yolo flag > project override > context > preferences
	var prefYolo *bool
	if cfg.Preferences != nil {
		prefYolo = cfg.Preferences.Yolo
	}
	effectiveYolo := l.resolveEffectiveYolo(prefYolo, rc.Context.Yolo, nil)
	if effectiveYolo {
		yoloArgs, err := YoloArgs(agentName)
		if err != nil {
			return err
		}
		extraArgs = append(yoloArgs, extraArgs...)
		// No separate warning here — auto-approve is shown in the banner
		// as the last line via renderAutoApprove().
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

	// 12. Resolve capabilities and merge into sandbox config.
	// Merge context capabilities + CLI --with, minus CLI --without.
	contextCapNames := rc.Context.Capabilities
	capNames := make([]string, len(contextCapNames))
	copy(capNames, contextCapNames)
	capNames = append(capNames, withCaps...)
	if len(withoutCaps) > 0 {
		blocked := make(map[string]bool, len(withoutCaps))
		for _, c := range withoutCaps {
			blocked[c] = true
		}
		var filtered []string
		for _, c := range capNames {
			if !blocked[c] {
				filtered = append(filtered, c)
			}
		}
		capNames = filtered
	}

	// Build capability source map: track whether each cap came from context or --with.
	contextCapSet := make(map[string]bool, len(contextCapNames))
	for _, c := range contextCapNames {
		contextCapSet[c] = true
	}

	// 13. Apply sandbox (DD-18: always applied unless explicitly disabled).
	// ResolveSandboxRef resolves named profiles; PolicyFromConfig handles nil → defaults.
	sandboxCfg, sbDisabled, sbErr := sandbox.ResolveSandboxRef(rc.Context.Sandbox, cfg.Sandboxes)
	if sbErr != nil {
		cleanup()
		return fmt.Errorf("resolving sandbox: %w", sbErr)
	}

	// Merge capability overrides into sandbox config before PolicyFromConfig.
	var resolvedCapSet *capability.Set
	var capOverrides capability.SandboxOverrides
	if len(capNames) > 0 {
		userDefined := capability.FromConfigDefs(cfg.Capabilities)
		registry := capability.MergedRegistry(userDefined)
		capSet, err := capability.ResolveAll(capNames, registry, cfg.NeverAllow, cfg.NeverAllowEnv)
		if err != nil {
			cleanup()
			return fmt.Errorf("resolving capabilities: %w", err)
		}
		resolvedCapSet = capSet
		capOverrides = capSet.ToSandboxOverrides()

		if sandboxCfg == nil {
			sandboxCfg = &config.SandboxPolicy{}
		}
		sandboxCfg.Unguard = append(sandboxCfg.Unguard, capOverrides.Unguard...)
		sandboxCfg.ReadableExtra = append(sandboxCfg.ReadableExtra, capOverrides.ReadableExtra...)
		sandboxCfg.WritableExtra = append(sandboxCfg.WritableExtra, capOverrides.WritableExtra...)
		sandboxCfg.DeniedExtra = append(sandboxCfg.DeniedExtra, capOverrides.DeniedExtra...)
	}

	homeDir, _ := os.UserHomeDir()
	var pathWarnings []string
	if !sbDisabled {
		tempDir := os.TempDir()
		policy, pw, err := sandbox.PolicyFromConfig(sandboxCfg, projectRoot, rtDir.Path(), homeDir, tempDir)
		pathWarnings = pw
		if err != nil {
			cleanup()
			return fmt.Errorf("building sandbox policy: %w", err)
		}
		if policy != nil {
			// 12b. Propagate merged env so modules can inspect env vars.
			policy.Env = env
			// 12c. Set agent module for sandbox profile
			policy.AgentModule = ResolveAgentModule(agentName)

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

	// 13. Render startup banner
	prefs := rc.Preferences
	if resolve {
		t := true
		prefs.ShowInfo = &t
		prefs.InfoDetail = "detailed"
	}
	if prefs.ShowInfo != nil && *prefs.ShowInfo {
		bannerData := l.buildBannerData(rc, agentName, binary, resolvedEnv, pathWarnings, sbDisabled, sandboxCfg, projectRoot, rtDir.Path(), homeDir, &prefs, resolvedCapSet, capOverrides, contextCapSet, withoutCaps, cfg)
		bannerData.Yolo = effectiveYolo
		bannerData.AutoApprove = effectiveYolo
		ui.RenderBanner(l.stderr(), prefs.InfoStyle, bannerData)
		fmt.Fprintln(l.stderr())
	}

	// 14. Exec the agent binary
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

// buildBannerData constructs a BannerData from the resolved context and launch state.
func (l *Launcher) buildBannerData(
	rc *aidectx.ResolvedContext,
	agentName, binary string,
	resolvedEnv map[string]string,
	pathWarnings []string,
	sbDisabled bool,
	sandboxCfg *config.SandboxPolicy,
	projectRoot, rtDirPath, homeDir string,
	prefs *config.Preferences,
	resolvedCapSet *capability.Set,
	capOverrides capability.SandboxOverrides,
	contextCapSet map[string]bool,
	withoutCaps []string,
	cfg *config.Config,
) *ui.BannerData {
	data := &ui.BannerData{
		ContextName: rc.Name,
		MatchReason: rc.MatchReason,
		AgentName:   agentName,
		AgentPath:   binary,
		SecretName:  rc.Context.Secret,
		Warnings:    pathWarnings,
	}

	// Build env annotations
	if len(rc.Context.Env) > 0 {
		data.Env = make(map[string]string, len(rc.Context.Env))
		for k, v := range rc.Context.Env {
			source, _ := classifyEnvSource(v)
			data.Env[k] = "← " + source
		}
	}

	// In detailed mode, add resolved env values
	if prefs.InfoDetail == "detailed" && len(resolvedEnv) > 0 {
		data.EnvResolved = make(map[string]string, len(resolvedEnv))
		for k, v := range resolvedEnv {
			data.EnvResolved[k] = redactValue(v)
		}
	}

	// Populate capability display data
	if resolvedCapSet != nil && len(resolvedCapSet.Capabilities) > 0 {
		for _, rc := range resolvedCapSet.Capabilities {
			paths := append([]string{}, rc.Readable...)
			paths = append(paths, rc.Writable...)
			source := "--with"
			if contextCapSet[rc.Name] {
				source = "context config"
			}
			data.Capabilities = append(data.Capabilities, ui.CapabilityDisplay{
				Name:    rc.Name,
				Paths:   paths,
				EnvVars: rc.EnvAllow,
				Source:  source,
			})
		}
		data.NeverAllow = cfg.NeverAllow
		data.CredWarnings = capability.CredentialWarnings(capOverrides.EnvAllow)
		data.CompWarnings = capability.CompositionWarnings(resolvedCapSet.Capabilities)
	}

	// Build disabled caps from --without
	for _, name := range withoutCaps {
		data.DisabledCaps = append(data.DisabledCaps, ui.CapabilityDisplay{
			Name:     name,
			Source:   "--without",
			Disabled: true,
		})
	}

	// Project detection: if no capabilities active, suggest based on project files
	if len(data.Capabilities) == 0 && len(data.DisabledCaps) == 0 {
		suggestions := capability.DetectProject(projectRoot)
		if len(suggestions) > 0 {
			data.Warnings = append(data.Warnings,
				fmt.Sprintf("Detected project tools. Suggested: aide --with %s", strings.Join(suggestions, " ")))
		}
	}

	// Build sandbox info
	if sbDisabled {
		data.Sandbox = &ui.SandboxInfo{Disabled: true}
	} else {
		tempDir := os.TempDir()
		policy, _, _ := sandbox.PolicyFromConfig(sandboxCfg, projectRoot, rtDirPath, homeDir, tempDir)
		if policy != nil {
			guardResults := sandbox.EvaluateGuards(policy)
			availableNames := sandbox.AvailableGuardNames(policy.Guards)
			si := &ui.SandboxInfo{
				Network: networkDisplayName(string(policy.Network)),
			}
			if len(policy.AllowPorts) > 0 {
				portStrs := make([]string, len(policy.AllowPorts))
				for i, p := range policy.AllowPorts {
					portStrs[i] = strconv.Itoa(p)
				}
				si.Ports = strings.Join(portStrs, ", ")
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
			data.Sandbox = si
		}
	}

	return data
}

// networkDisplayName converts raw network mode to user-friendly display.
func networkDisplayName(mode string) string {
	switch mode {
	case "outbound":
		return "outbound only"
	case "none":
		return "none"
	case "unrestricted":
		return "unrestricted"
	default:
		return mode
	}
}

// classifyEnvSource determines the source type of an env template value.
func classifyEnvSource(val string) (source string, secretKey string) {
	if strings.Contains(val, ".secrets.") || strings.Contains(val, "index .secrets") {
		// Extract the key name from template patterns like {{ .secrets.foo }} or {{ index .secrets "foo" }}
		if idx := strings.Index(val, ".secrets."); idx >= 0 {
			rest := val[idx+len(".secrets."):]
			end := strings.IndexAny(rest, " \t}\"")
			if end > 0 {
				return "secrets." + rest[:end], rest[:end]
			}
		}
		if idx := strings.Index(val, "index .secrets"); idx >= 0 {
			// Find the quoted key name
			rest := val[idx:]
			qStart := strings.Index(rest, "\"")
			if qStart >= 0 {
				rest2 := rest[qStart+1:]
				qEnd := strings.Index(rest2, "\"")
				if qEnd > 0 {
					return "secrets." + rest2[:qEnd], rest2[:qEnd]
				}
			}
		}
		return "secret", ""
	}
	if strings.Contains(val, ".project_root") {
		return "project_root", ""
	}
	if strings.Contains(val, ".runtime_dir") {
		return "runtime_dir", ""
	}
	if strings.Contains(val, "{{") {
		return "template", ""
	}
	return "literal", ""
}

// redactValue shows the first 8 chars of a value then ***.
func redactValue(s string) string {
	if len(s) <= 8 {
		return s + "***"
	}
	return s[:8] + "***"
}

// resolveEffectiveYolo computes the effective yolo state considering CLI flags
// and config layers. --no-yolo always wins. --yolo flag sets a floor.
// Config layers are resolved via config.ResolveYolo (preferences < context < project).
func (l *Launcher) resolveEffectiveYolo(preferences, context, project *bool) bool {
	if l.NoYolo {
		return false
	}
	if l.Yolo {
		return true
	}
	return config.ResolveYolo(preferences, context, project)
}

// yoloSource returns a human-readable string describing why yolo is active.
func yoloSource(cliFlag bool, preferences, context, project *bool) string {
	if cliFlag {
		return "--yolo flag"
	}
	if project != nil && *project {
		return ".aide.yaml"
	}
	if context != nil && *context {
		return "context config"
	}
	if preferences != nil && *preferences {
		return "preferences"
	}
	return "config"
}
