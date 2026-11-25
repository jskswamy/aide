package launcher

import (
	"bufio"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/jskswamy/aide/internal/capability"
	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/consent"
	aidectx "github.com/jskswamy/aide/internal/context"
	"github.com/jskswamy/aide/internal/diag"
	"github.com/jskswamy/aide/internal/display"
	"github.com/jskswamy/aide/internal/envscrub"
	"github.com/jskswamy/aide/internal/gateway"
	"github.com/jskswamy/aide/internal/gatewaymigrate"
	"github.com/jskswamy/aide/internal/harness"
	"github.com/jskswamy/aide/internal/homepath"
	"github.com/jskswamy/aide/internal/prefs"
	"github.com/jskswamy/aide/internal/sandbox"
	"github.com/jskswamy/aide/internal/secrets"
	"github.com/jskswamy/aide/internal/trust"
	"github.com/jskswamy/aide/internal/ui"
	"github.com/jskswamy/aide/pkg/seatbelt"
)

//go:generate mockgen -destination=mocks/mock_execer.go -package=mocks github.com/jskswamy/aide/internal/launcher Execer

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

// isStdoutTTY reports whether os.Stdout is a character device (TTY).
// Used by banner-style resolution so CI/redirected runs force compact
// mode regardless of the user's configured preference.
func isStdoutTTY() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// filterNeverAllowEnv removes any entry from env whose key is in the blocked set.
// This prevents never-allow variables from reaching the agent process.
func filterNeverAllowEnv(env []string, blocked []string) []string {
	if len(blocked) == 0 {
		return env
	}
	blockedSet := make(map[string]bool, len(blocked))
	for _, k := range blocked {
		blockedSet[k] = true
	}
	result := env[:0:0] // avoid aliasing
	for _, e := range env {
		k, _, _ := strings.Cut(e, "=")
		if !blockedSet[k] {
			result = append(result, e)
		}
	}
	return result
}

// injectAideSessionEnv appends AIDE_* environment variables reflecting the
// active aide session state. Inherited by the agent and all child processes
// (including aide statusline claude invoked by the agent's TUI).
func injectAideSessionEnv(
	env []string,
	sbDisabled bool,
	sandboxCfg *config.SandboxPolicy,
	autoApprove bool,
	caps *capability.Set,
	contextName string,
	agentName string,
	trustInfo *ui.TrustInfo,
	gateway string,
) []string {
	add := make(map[string]string, 10)

	if sbDisabled {
		add["AIDE_SANDBOX"] = "off"
	} else {
		add["AIDE_SANDBOX"] = "on"
	}

	networkMode := "outbound"
	if sandboxCfg != nil && sandboxCfg.Network != nil && sandboxCfg.Network.Mode == "unrestricted" {
		networkMode = "unrestricted"
	}
	add["AIDE_NETWORK_MODE"] = networkMode

	capList := ""
	if caps != nil {
		names := make([]string, 0, len(caps.Capabilities))
		for _, c := range caps.Capabilities {
			names = append(names, c.Name)
		}
		capList = strings.Join(names, ",")
	}
	add["AIDE_CAPS"] = capList

	if trustInfo == nil || trustInfo.Status == "" {
		add["AIDE_TRUST"] = "trusted"
	} else {
		add["AIDE_TRUST"] = "untrusted"
	}

	if autoApprove {
		add["AIDE_AUTO_APPROVE"] = "1"
	}

	// AIDE_PROFILE is the current name; AIDE_CONTEXT stays for scripts
	// and statuslines written against older releases. Same value.
	if contextName != "" {
		add["AIDE_PROFILE"] = contextName
		add["AIDE_CONTEXT"] = contextName
	}

	if agentName != "" {
		add["AIDE_AGENT"] = agentName
	}

	add["AIDE_GATEWAY"] = gateway

	return mergeEnv(env, add)
}

// Launcher orchestrates the full agent launch flow.
type Launcher struct {
	Execer              Execer
	ConfigDir           string       // override for testing (default: config.Dir())
	LookPath            LookPathFunc // override for testing (default: exec.LookPath)
	Yolo                bool         // inject agent-specific skip-permissions flag
	NoYolo              bool         // override: disable yolo mode (overrides config and --yolo)
	Stderr              io.Writer    // override for testing (default: os.Stderr)
	Stdin               io.Reader    // override for testing (default: os.Stdin)
	StdinIsTTY          func() bool  // override for testing (default: checks os.Stdin)
	IgnoreProjectConfig bool         // skip .aide.yaml entirely
	UnrestrictedNetwork bool         // force unrestricted network, clear port rules (-N flag)
	// PolicyHook, when set, receives the final sandbox policy just
	// before it is applied. Tests use it to see what launch builds.
	PolicyHook func(sandbox.Policy)
	TrustStore *trust.Store // override for testing (default: trust.DefaultStore())
	// Auth selects API credential injection (default) or native login,
	// which injects no provider credentials.
	Auth AuthMode
	// Profile is --profile: select this profile instead of matching.
	Profile string
	// Credentials is --credential; CredentialsSet reports the flag was
	// given, so an empty selection is explicit. It replaces the saved
	// and profile-default selection for this launch only.
	Credentials    []string
	CredentialsSet bool
	// PrefsPath overrides the repository preferences file (default:
	// prefs.DefaultPath under HOME). Launches only read it.
	PrefsPath string

	// Variant selection inputs (optional; zero value disables variant flow).
	VariantOverrides   map[string][]string
	ConsentStore       *consent.Store
	Prompter           capability.Prompter
	AutoYes            bool
	Interactive        bool
	inReader           *bufio.Reader
	promptProjectTrust bool

	// Diagnose enables post-mortem report generation (forks instead of execve).
	Diagnose bool
	// DiagnoseTrace implies Diagnose; additionally captures macOS sandbox denials.
	DiagnoseTrace bool

	// Version, Commit, BuildDate carry the goreleaser-injected build
	// metadata. They are populated in cmd/aide/main.go and surfaced in
	// the --diagnose report's Environment section. Empty values are
	// rendered verbatim ("dev", "none", "unknown" by default).
	Version   string
	Commit    string
	BuildDate string
}

// stderr returns the effective stderr writer.
func (l *Launcher) stderr() io.Writer {
	if l.Stderr != nil {
		return l.Stderr
	}
	return os.Stderr
}

// stdinReader returns the shared buffered stdin reader for interactive prompts.
func (l *Launcher) stdinReader() *bufio.Reader {
	if l.inReader == nil {
		if l.Stdin != nil {
			l.inReader = bufio.NewReader(l.Stdin)
		} else {
			l.inReader = bufio.NewReader(os.Stdin)
		}
	}
	return l.inReader
}

// prefsPath returns the effective repository preferences file.
func (l *Launcher) prefsPath(homeDir string) string {
	if l.PrefsPath != "" {
		return l.PrefsPath
	}
	return prefs.DefaultPath(homeDir)
}

// configDir returns the effective config directory.
func (l *Launcher) configDir() string {
	if l.ConfigDir != "" {
		return l.ConfigDir
	}
	return config.Dir()
}

// migrateGateways upgrades the legacy gateways of cfg on disk, with
// backups, printing the summary. A failure never stops the launch:
// aide runs from the in-memory translation and says so.
func (l *Launcher) migrateGateways(cfg *config.Config) {
	home, err := os.UserHomeDir()
	if err != nil || cfg.LegacyMonolithic { // a single-file config is split first, by aide config migrate --write
		return
	}
	res, err := gatewaymigrate.Migrate(cfg, gateway.CacheModels(home), l.configDir(), filepath.Dir(l.prefsPath(home)))
	switch {
	case err != nil:
		fmt.Fprintf(l.stderr(), "aide: warning: could not migrate gateway config (%v); running from its in-memory translation. Retry with: aide config migrate --write\n", err)
	case res.Summary != "":
		fmt.Fprint(l.stderr(), res.Summary)
	}
}

// Launch resolves context, decrypts secrets, resolves templates, creates
// a runtime directory, applies sandbox policy, and execs the agent binary.
func (l *Launcher) Launch(cwd string, agentOverride string, extraArgs []string, cleanEnv bool, resolve bool, withCaps []string, withoutCaps []string) error {
	// 1. Load config
	cfg, err := config.Load(l.configDir(), cwd)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if cfg.NeedsGatewayMigration {
		l.migrateGateways(cfg)
	}
	if err := l.ensureDefaultProfileOnDisk(cfg, agentOverride); err != nil {
		return err
	}

	// Resolve the user's home directory once. Every downstream caller
	// (tilde expansion, sandbox policy, agent-env injection) reads this
	// value rather than calling os.UserHomeDir() independently. A missing
	// HOME is a hard configuration error — silently passing "" would cause
	// sandbox rules and CLAUDE_CONFIG_DIR to resolve to wrong paths.
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolving home directory: %w", err)
	}
	if f, ok := l.stderr().(*os.File); ok {
		config.MaybeNudgeMigration(cfg, l.stderr(), term.IsTerminal(int(f.Fd())), homeDir, time.Now())
	}

	// 2. Select: trust gate, profile, saved repository preference, final
	// harness and its profile env, as every preview does.
	l.promptProjectTrust = l.Interactive && !l.IgnoreProjectConfig
	res, err := l.ResolveSelection(cfg, cwd, homeDir, SelectionInputs{
		Agent:          agentOverride,
		Credentials:    l.Credentials,
		CredentialsSet: l.CredentialsSet,
		Auth:           l.Auth,
	})
	for _, w := range res.Gate.Warnings {
		fmt.Fprintf(l.stderr(), "aide: warning: %s\n", w)
	}
	for _, n := range res.Notices {
		fmt.Fprintf(l.stderr(), "aide: note: %s\n", n)
	}
	if err != nil {
		return err
	}
	if res.Refused != nil {
		return res.Refused
	}
	cfg, gate, rc, inputs, sel := res.Config, res.Gate, res.RC, res.Inputs, res.Sel
	projectRoot := aidectx.ProjectRoot(cwd)

	if notice := nativeAuthNotice(cfg.HarnessOf(rc.Context.Agent), rc.Context, homeDir); notice != "" {
		fmt.Fprintf(l.stderr(), "aide: note: %s\n", notice)
	}
	// Credential binding and Codex custom provider names, scrubbed from
	// the inherited env below.
	scrubNames := ScrubNames(cfg, homeDir)
	if notice := codexConfigNoticeFor(cfg.HarnessOf(rc.Context.Agent), rc.Context, homeDir, os.Getenv); notice != "" {
		fmt.Fprintf(l.stderr(), "aide: note: %s\n", notice)
	}

	// 5. Look up agent binary
	agentName := rc.Context.Agent
	binary, err := ResolveAgentBinary(cfg, agentName)
	if err != nil {
		return err
	}

	// 5a. Credentials: check the
	// selection against the final harness, and decrypt only the selected
	// bindings' stores, all before any side effect: a missing binding,
	// store or key stops the launch here.
	sel.prompt = l.choicePrompter(cwd, homeDir)
	profileCtx, credPlan, err := PlanProfileCredentials(cfg, rc, inputs, &sel)
	for _, n := range sel.ChoiceNotices {
		fmt.Fprintf(l.stderr(), "aide: note: %s\n", n)
	}
	if err != nil {
		return err
	}
	rc.Context = profileCtx
	for _, b := range credPlan.Bindings {
		if gw, ok := profileCtx.Gateways[b.Name]; ok {
			if err := config.ValidateGatewayTarget(b.Name, gw); err != nil {
				return err
			}
		}
	}
	if len(credPlan.Bindings) == 0 && len(credPlan.Skipped) > 0 {
		fmt.Fprintf(l.stderr(), "aide: note: %s has no credential for %s (%s: other harnesses only); %s uses its own login\n",
			profileLabel(cfg, rc.Name), cfg.HarnessOf(agentName), strings.Join(credPlan.Skipped, ", "), cfg.HarnessOf(agentName))
	}
	if len(credPlan.Ignored) > 0 {
		fmt.Fprintf(l.stderr(), "aide: note: native login: not injecting credentials %s\n", strings.Join(credPlan.Ignored, ", "))
	}
	loadStore := CachingStoreLoader(loadStoreFrom(filepath.Join(l.configDir(), "secrets")))
	if len(credPlan.Bindings) == 0 && credPlan.Auth != AuthLogin && !l.CredentialsSet {
		l.noteAmbientProviderKeys(cfg, rc, nil)
	}
	var withheldNotes []string
	credEnv, withheld, err := resolveCredentialEnv(credPlan, loadStore)
	if err != nil {
		return err
	}
	for _, name := range slices.Sorted(maps.Keys(withheld)) {
		fmt.Fprintf(l.stderr(), "aide: warning: gateway %q is withheld for this launch: %v\n", name, withheld[name])
		withheldNotes = append(withheldNotes, fmt.Sprintf("gateway %q is withheld for this launch: %v", name, withheld[name]))
	}
	credPlan.Bindings = slices.DeleteFunc(credPlan.Bindings, func(b PlannedCredential) bool { return withheld[b.Name] != nil })
	l.ensureGatewaySynced(rc, cfg.HarnessOf(agentName), homeDir, credPlan, credEnv)

	// 5b. Resolve effective auto-approve from config layers + CLI flags.
	// Priority: --no-auto-approve/--no-yolo (highest) > --auto-approve/--yolo flag > project override > context > preferences
	var prefYolo *bool
	if cfg.Preferences != nil {
		prefYolo = cfg.Preferences.Yolo
	}
	effectiveYolo := l.resolveEffectiveYolo(prefYolo, rc.Context.Yolo, nil)
	extraArgs, choiceEnv := l.applyGatewayChoice(rc.Name, cfg.HarnessOf(agentName), rc.Context, sel.Gateway, homeDir, extraArgs)
	extraArgs = ApplyLaunchArgs(cfg.HarnessOf(agentName), extraArgs)
	if effectiveYolo {
		var err error
		extraArgs, err = ApplyYoloArgs(agentName, extraArgs)
		if err != nil {
			return err
		}
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
		var err error
		secretsMap, err = loadStore(secretsPath)
		if err != nil {
			cleanup()
			return err
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

	// Tilde-expand leading "~/" in values so the child agent receives
	// absolute paths. Agents (e.g. claude reading CLAUDE_CONFIG_DIR) don't
	// expand "~" themselves, so an unexpanded value reads the wrong dir.
	for k, v := range resolvedEnv {
		resolvedEnv[k] = homepath.Expand(v, homeDir)
	}

	warnLegacyCredentialEnv(l.stderr(), profileLabel(cfg, rc.Name), rc.Context.Env, cfg.CredentialScrubNames())

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
	// The banner names the agent; binary may be rewritten to the sandbox
	// wrapper below.
	agentBinary := binary

	sessionGetenv := func(k string) string {
		if v, ok := resolvedEnv[k]; ok {
			return v
		}
		return os.Getenv(k)
	}

	// 12-13. Resolve capabilities and merge them into the sandbox config
	// (DD-18: the sandbox always applies unless explicitly disabled).
	if l.Prompter == nil && l.Interactive {
		l.Prompter = ui.NewTTYPrompter(l.stdinReader(), l.stderr())
	}
	sbRes, err := ResolveSandbox(cfg, rc, SandboxInputs{
		ProjectRoot: projectRoot, HomeDir: homeDir, Getenv: sessionGetenv,
		WithCaps: withCaps, WithoutCaps: withoutCaps,
		UnrestrictedNetwork: l.UnrestrictedNetwork,
		Variants: sandbox.VariantSelectionOptions{
			CLIOverrides: l.VariantOverrides,
			Consent:      l.ConsentStore,
			Prompter:     l.Prompter,
			Interactive:  l.Interactive,
			AutoYes:      l.AutoYes,
			EnvLookup:    sessionGetenv,
		},
	})
	if err != nil {
		cleanup()
		return err
	}
	sandboxCfg, sbDisabled := sbRes.Config, sbRes.Disabled
	resolvedCapSet, capOverrides, capProvenance := sbRes.Caps, sbRes.Overrides, sbRes.Provenance
	contextCapSet := sbRes.ContextCaps
	configWritableExtra, configReadableExtra, configDeniedExtra := sbRes.ConfigWritable, sbRes.ConfigReadable, sbRes.ConfigDenied

	// Build environment: the inherited env with credentials
	// scrubbed (independent of --clean-env), then the context/profile
	// env, which already carries the profile dir, on top. It is built
	// after capability resolution because a capability's env_allow
	// re-admits the ambient cloud variables it grants.
	baseEnv := os.Environ()
	if cleanEnv {
		baseEnv = filterEssentialEnv(baseEnv, capOverrides.EnvAllow)
	}
	permitted := slices.Clone(capOverrides.EnvAllow)
	for _, b := range credPlan.Bindings {
		permitted = append(permitted, b.Permit...)
	}
	baseEnv = envscrub.ScrubCredentials(baseEnv, launchScrubRegistry(rc.Context, permitted), scrubNames)
	env := mergeEnv(baseEnv, resolvedEnv)
	// Selected credential bindings go on top of the general env.
	env = mergeEnv(env, credEnv)

	// Filter never-allow env vars from the agent's environment.
	// Combine top-level config and capability-resolved never-allow vars.
	var blockedVars []string
	if len(cfg.NeverAllowEnv) > 0 {
		blockedVars = append(blockedVars, cfg.NeverAllowEnv...)
	}
	if resolvedCapSet != nil && len(resolvedCapSet.NeverAllowEnv) > 0 {
		blockedVars = append(blockedVars, resolvedCapSet.NeverAllowEnv...)
	}
	// A stripped keyless placeholder would let Claude Code send its
	// saved login to the gateway.
	for _, b := range credPlan.Bindings {
		for k, v := range b.Set {
			if v == config.KeylessPlaceholder && slices.Contains(blockedVars, k) {
				cleanup()
				return fmt.Errorf("never_allow_env strips %s, which keyless gateway %q needs so %s does not send its login; "+
					"remove it from never_allow_env or use a gateway with a token", k, b.Name, cfg.HarnessOf(agentName))
			}
		}
	}
	if len(blockedVars) > 0 {
		env = filterNeverAllowEnv(env, blockedVars)
	}
	env = l.mergeChoiceEnv(env, choiceEnv, sel.Gateway.Source == SourceFlag, blockedVars)

	// An OpenCode profile with a config dir keeps its accounts in its own
	// database there. The path is read from OPENCODE_CONFIG_DIR before the
	// sandbox and set after it.
	var openCodeDB string
	if cfg.HarnessOf(agentName) == "opencode" && rc.Context.SelectsConfigDir() && opensOpenCodeProfileDB(extraArgs) {
		if openCodeDB, err = openCodeProfileDB(env, blockedVars); err != nil {
			cleanup()
			return err
		}
	}
	var ompConfigRoot string
	if cfg.HarnessOf(agentName) == "omp" && rc.Context.SelectsConfigDir() {
		if ompConfigRoot, err = ompProfileConfigRoot(env, homeDir, blockedVars); err != nil {
			cleanup()
			return err
		}
	}

	var pathWarnings []string
	var diagSandbox diag.SandboxInfo
	// sandboxExtraFiles parks fds the sandbox layer needs the child to
	// inherit (Landlock policy memfd, bwrap seccomp memfd). SyscallExecer
	// ignores it (syscall.Exec preserves all non-CLOEXEC fds), but
	// runDiagnose uses cmd.Start which closes everything outside
	// stdin/stdout/stderr + ExtraFiles.
	var sandboxExtraFiles []*os.File
	if sbDisabled {
		diagSandbox.Disabled = true
	}
	if !sbDisabled {
		tempDir := os.TempDir()
		policy, pw, err := sandbox.PolicyFromConfig(sandboxCfg, sandbox.Paths{ProjectRoot: projectRoot, RuntimeDir: rtDir.Path(), HomeDir: homeDir, TempDir: tempDir})
		pathWarnings = pw
		if err != nil {
			cleanup()
			return fmt.Errorf("building sandbox policy: %w", err)
		}
		if policy != nil {
			// 12b. Propagate merged env and capability EnvAllow so modules and
			// CleanEnv filtering see the active session's allowed keys.
			policy.Env = env
			policy.EnvAllow = capOverrides.EnvAllow
			// 12c. Set agent module for sandbox profile
			policy.AgentModule = ResolveAgentModule(cfg.HarnessOf(agentName))
			// 12d. Apply agent-module env overrides (e.g. claude's
			// CLAUDE_CONFIG_DIR redirect to a sandbox-friendly location).
			// AgentEnv observes the un-injected env via ctx.EnvLookup, so a
			// user-set value is respected — module returns nil for that key
			// and aide leaves it alone.
			env = applyAgentEnv(env, policy)
			// 12e. Sync policy.Env with the post-injection env so the
			// re-exec child receives injected keys (e.g. CLAUDE_CONFIG_DIR)
			// in its serialised policy. Without this, capability guards in
			// the child evaluate env-var-dependent paths from the stale
			// pre-injection snapshot and may grant the wrong Landlock path.
			policy.Env = env
			if err := PrepareAgentSandbox(policy, cfg, cfg.HarnessOf(agentName), binary); err != nil {
				cleanup()
				return err
			}

			cmd := &exec.Cmd{
				Path: binary,
				Args: append([]string{binary}, extraArgs...),
				Env:  env,
			}
			if l.PolicyHook != nil {
				l.PolicyHook(*policy)
			}
			sb := sandbox.NewSandbox()
			if err := sb.Apply(cmd, *policy, rtDir.Path()); err != nil {
				cleanup()
				return fmt.Errorf("applying sandbox: %w", err)
			}
			binary = cmd.Path
			extraArgs = cmd.Args[1:]
			env = cmd.Env
			sandboxExtraFiles = cmd.ExtraFiles

			// Capture sandbox info for --diagnose. Best-effort: profile
			// generation is allowed to fail silently — the report still
			// gets variants/guard names without it.
			diagSandbox.GuardNames = append([]string(nil), policy.Guards...)
			if rendered, gerr := sb.GenerateProfile(*policy); gerr == nil {
				diagSandbox.RenderedSB = rendered
			}
		}
	}
	if openCodeDB != "" {
		env = mergeEnv(env, map[string]string{"OPENCODE_DB": openCodeDB})
	}
	if ompConfigRoot != "" {
		env = mergeEnv(env, map[string]string{"PI_CONFIG_DIR": ompConfigRoot})
	}
	if resolvedCapSet != nil {
		seen := make(map[string]bool)
		for _, rc := range resolvedCapSet.Capabilities {
			prov := capProvenance[rc.Name]
			for _, v := range prov.Variants {
				key := rc.Name + "=" + v
				if seen[key] {
					continue
				}
				seen[key] = true
				diagSandbox.Variants = append(diagSandbox.Variants, key)
			}
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
		bannerData := l.buildBannerData(rc, agentName, agentBinary, resolvedEnv, pathWarnings, sbDisabled, sandboxCfg, projectRoot, rtDir.Path(), homeDir, cwd, &prefs, resolvedCapSet, capOverrides, capProvenance, contextCapSet, withoutCaps, cfg, configWritableExtra, configReadableExtra, configDeniedExtra, gate)
		bannerData.Yolo = effectiveYolo
		SetBannerCredentials(bannerData, credPlan)
		SetBannerSelection(bannerData, sel)
		EnrichBannerHarnessesAndGateways(bannerData, cfg, rc, agentName, homeDir, l.lookPath(), sel.Choices)
		for i := range bannerData.GatewayChoices {
			if g := &bannerData.GatewayChoices[i]; g.Harness == bannerData.Harness {
				g.Warnings = append(g.Warnings, withheldNotes...)
			}
		}
		bannerData.AutoApprove = effectiveYolo
		style := ui.EffectiveBannerStyle(prefs.InfoStyle, isStdoutTTY(), os.Getenv("AIDE_INFO_STYLE"))
		if err := ui.RenderBanner(l.stderr(), style, bannerData); err != nil {
			fmt.Fprintf(l.stderr(), "warning: banner render failed: %v\n", err)
		}
		fmt.Fprintln(l.stderr())
	}

	// Inject AIDE_* session vars for statusline and subprocesses.
	env = injectAideSessionEnv(env, sbDisabled, sandboxCfg, effectiveYolo, resolvedCapSet, rc.Name, agentName, gate.Primary(), sel.Gateway.Segment())

	// 14. Exec the agent binary
	args := append([]string{binary}, extraArgs...)
	if l.Diagnose {
		dc := l.buildDiagContext(cfg, rc, credPlan, diagSandbox)
		return l.runDiagnose(binary, args, env, sandboxExtraFiles, dc)
	}
	return l.Execer.Exec(binary, args, env)
}

// buildDiagContext bundles inputs needed by --diagnose into a single
// struct. Reuses state already gathered by Launch (sandbox info, secret
// reference, resolved config path) rather than re-deriving anything.
//
// Best-effort: AgeKeySource is discovered fresh here because the
// secrets path in Launch only runs when a context references a secret.
// If discovery fails we leave the field empty rather than failing the
// whole launch — diagnose is supposed to be observational.
func (l *Launcher) buildDiagContext(cfg *config.Config, rc *aidectx.ResolvedContext, plan CredentialPlan, sb diag.SandboxInfo) diagContext {
	dc := diagContext{
		Version:   l.Version,
		Commit:    l.Commit,
		BuildDate: l.BuildDate,
		Sandbox:   sb,
	}
	var applied []string
	if cfg != nil {
		for _, layer := range cfg.ProjectLayers {
			if !layer.Rejected {
				applied = append(applied, layer.Path)
			}
		}
	}
	switch {
	case len(applied) > 0:
		dc.ResolvedConfig = strings.Join(applied, " + ")
	case cfg != nil && len(cfg.ProjectLayers) == 0 && cfg.ProjectConfigPath != "":
		dc.ResolvedConfig = cfg.ProjectConfigPath
	default:
		dc.ResolvedConfig = l.configDir() + " (no project config)"
	}
	if rc != nil && rc.Context.Secret != "" {
		dc.SecretSourcePaths = []string{config.ResolveSecretPath(rc.Context.Secret)}
	}
	for _, p := range plan.storePaths() {
		if !slices.Contains(dc.SecretSourcePaths, p) {
			dc.SecretSourcePaths = append(dc.SecretSourcePaths, p)
		}
	}
	if id, err := secrets.DiscoverAgeKey(); err == nil && id != nil {
		dc.AgeKeySource = ageKeySourceLabel(id)
	}
	return dc
}

// ageKeySourceLabel returns a short human-readable label for an age
// identity source. Mirrors the labels used by `aide secrets status` so
// the diagnose report stays consistent with the rest of the CLI.
func ageKeySourceLabel(id *secrets.AgeIdentity) string {
	switch id.Source {
	case secrets.SourceYubiKey:
		return "yubikey"
	case secrets.SourceEnvKey:
		return "env:SOPS_AGE_KEY"
	case secrets.SourceEnvKeyFile:
		return "env:SOPS_AGE_KEY_FILE=" + id.KeyData
	case secrets.SourceDefaultFile:
		return "file:" + id.KeyData
	default:
		return ""
	}
}

// ResolveAgentBinary selects the configured binary before PATH resolution.
func ResolveAgentBinary(cfg *config.Config, agentName string) (string, error) {
	if agentName == "" {
		return "", fmt.Errorf("no agent specified in profile")
	}

	// Look up in agents map
	if agent, ok := cfg.Agents[agentName]; ok {
		return agent.Binary, nil
	}

	if d, ok := harness.Lookup(agentName); ok {
		return d.Binary, nil
	}

	// If there are agents defined but this one isn't found, that's an error
	if len(cfg.Agents) > 0 {
		return "", fmt.Errorf("agent %q not found in agents map", agentName)
	}

	// No agents map at all (minimal config without normalization) - use agent name as binary
	return agentName, nil
}

// ApplyYoloArgs returns extraArgs with the yolo flag applied for the
// given agent. Returns an error if the agent does not support yolo mode.
// For OpenCode, subcommands are respected: '--auto' is placed after 'run',
// omitted on subcommands that reject it, or placed at the front for
// interactive/root invocations.
func ApplyYoloArgs(agentName string, extraArgs []string) ([]string, error) {
	if d, ok := launchableHarness(agentName); ok {
		if _, ok := d.YoloArgs(); ok {
			return d.ApplyYolo(extraArgs), nil
		}
	}
	var supported []string
	for _, d := range harness.All() {
		if _, ok := d.YoloArgs(); ok && d.Launchable() {
			supported = append(supported, d.Name)
		}
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
				"profile %q references secrets in env vars but has no secret configured.\n\n"+
					"Fix with: aide profile set-secret <name> --profile %s",
				contextName, contextName,
			)
		}
		return fmt.Errorf(
			"profile %q: secret key not found in %s.\n\n"+
				"Available keys: aide secrets keys %s\n"+
				"Re-wire:        aide env set <KEY> --secret-key <KEY_NAME> --global",
			contextName, secret, secret,
		)
	}

	if strings.Contains(msg, "nil pointer") || strings.Contains(msg, "can't evaluate field") {
		return fmt.Errorf(
			"profile %q references secrets but has no secret configured.\n\n"+
				"Fix with: aide profile set-secret <name> --profile %s",
			contextName, contextName,
		)
	}

	return fmt.Errorf("profile %q: %w", contextName, err)
}

// launchScrubRegistry is the credential registry for a configured
// launch. permitted (capability env_allow) re-admits ambient cloud
// variables only. Harness config-dir redirects are always scrubbed so
// custom profiles use their isolated directory and "default" uses the
// native harness directory without ambient shell interference.
func launchScrubRegistry(_ config.Context, permitted []string) envscrub.Registry {
	return envscrub.Credentials().Permit(permitted...).With(envscrub.ConfigDirRedirects()...)
}

// warnLegacyCredentialEnv flags context env: entries whose names are
// scrubbed credentials (cloud and scrub_env: names; provider secrets
// were already translated or refused, and provider selectors are
// ordinary config). They still reach the agent; only names are
// printed, never values.
func warnLegacyCredentialEnv(w io.Writer, label string, env map[string]string, custom []string) {
	names := slices.DeleteFunc(envscrub.Credentials().With(custom...).MatchingNames(env), envscrub.IsProviderSelectorName)
	if len(names) == 0 {
		return
	}
	fmt.Fprintf(w, "warning: %s passes credential variables through env: (%s); "+
		"credentials in env: are deprecated and will move to credentials: bindings\n",
		label, strings.Join(names, ", "))
}

// profileLabel names a resolved profile for messages: `profile "x"`,
// or `profile (context) "x"` when the config uses legacy contexts:.
func profileLabel(cfg *config.Config, name string) string {
	if cfg.ProfileSchema {
		return fmt.Sprintf("profile %q", name)
	}
	return fmt.Sprintf("profile (context) %q", name)
}

// filterEssentialEnv keeps only essential environment variables plus
// non-credential keys from extraAllowed (e.g. capability EnvAllow).
// Credential names from envscrub.Credentials() are always stripped under
// clean_env, matching sandbox.filterEnv so --no-sandbox --clean-env cannot
// leak ambient credentials from other active capabilities.
func filterEssentialEnv(env, extraAllowed []string) []string {
	essential := map[string]bool{
		"PATH": true, "HOME": true, "USER": true,
		"SHELL": true, "TERM": true, "LANG": true,
		"TMPDIR": true, "XDG_RUNTIME_DIR": true, "XDG_CONFIG_HOME": true,
	}
	for _, k := range extraAllowed {
		essential[k] = true
	}
	credentials := envscrub.Credentials()
	var filtered []string
	for _, e := range env {
		k, _, _ := strings.Cut(e, "=")
		if essential[k] && !credentials.Matches(k) {
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

// applyAgentEnv merges env-var overrides declared by the policy's
// AgentModule (via the optional seatbelt.EnvProvider interface). Modules
// use this to inject sandbox-friendly values (e.g. CLAUDE_CONFIG_DIR
// pointing at an aide-managed dir) while respecting user-set values —
// each implementation checks ctx.EnvLookup first and returns nil for
// keys the user already set.
func applyAgentEnv(env []string, policy *sandbox.Policy) []string {
	if policy == nil || policy.AgentModule == nil {
		return env
	}
	provider, ok := policy.AgentModule.(seatbelt.EnvProvider)
	if !ok {
		return env
	}
	overrides := provider.AgentEnv(policy.ToSeatbeltContext())
	if len(overrides) == 0 {
		return env
	}
	return mergeEnv(env, overrides)
}

// buildBannerData constructs a BannerData from the resolved context and launch state.
func (l *Launcher) buildBannerData(
	rc *aidectx.ResolvedContext,
	agentName, binary string,
	resolvedEnv map[string]string,
	pathWarnings []string,
	sbDisabled bool,
	sandboxCfg *config.SandboxPolicy,
	projectRoot, rtDirPath, homeDir, cwd string,
	prefs *config.Preferences,
	resolvedCapSet *capability.Set,
	capOverrides capability.SandboxOverrides,
	capProvenance map[string]capability.Provenance,
	contextCapSet map[string]bool,
	withoutCaps []string,
	cfg *config.Config,
	configWritableExtra, configReadableExtra, configDeniedExtra []string,
	gate ProjectGate,
) *ui.BannerData {
	data := &ui.BannerData{
		ContextName: rc.Name,
		MatchReason: rc.MatchReason,
		AgentName:   agentName,
		AgentPath:   binary,
		SecretName:  rc.Context.Secret,
		Warnings:    pathWarnings,
	}
	gate.SetBanner(data)

	// Populate icons
	data.ContextIcon = rc.Context.Icon
	if agentDef, ok := cfg.Agents[agentName]; ok {
		data.AgentIcon = ResolveAgentIcon(agentName, &agentDef)
	} else {
		data.AgentIcon = ResolveAgentIcon(agentName, nil)
	}

	// Build unified env items: context env + capability env_allow + never-allow markers.
	var detailedEnv map[string]string
	if prefs.InfoDetail == "detailed" {
		detailedEnv = resolvedEnv
	}
	var neverAllowEnv []string
	if resolvedCapSet != nil {
		neverAllowEnv = resolvedCapSet.NeverAllowEnv
	}
	var resolvedCaps []capability.ResolvedCapability
	if resolvedCapSet != nil {
		resolvedCaps = resolvedCapSet.Capabilities
	}
	data.EnvItems = BuildEnvItems(BannerContextEnv(rc.Context), resolvedCaps, neverAllowEnv, detailedEnv)

	// Populate capability display data
	if resolvedCapSet != nil && len(resolvedCapSet.Capabilities) > 0 {
		effectiveStyle := ui.EffectiveBannerStyle(prefs.InfoStyle, isStdoutTTY(), os.Getenv("AIDE_INFO_STYLE"))
		for _, rc := range resolvedCapSet.Capabilities {
			source := "--with"
			if contextCapSet[rc.Name] {
				source = "context config"
			}
			prov := capProvenance[rc.Name]
			disp := ui.CapabilityDisplay{
				Name:            rc.Name,
				ReadablePaths:   rc.Readable,
				WritablePaths:   rc.Writable,
				EnvVars:         rc.EnvAllow,
				Source:          source,
				Variants:        prov.Variants,
				ProvenanceTag:   ui.ProvenanceTag(prov.Reason),
				FreshGrant:      prov.Reason == "consent:granted",
				EvidenceSummary: prov.EvidenceSummary,
			}
			// Tier 3: ConfirmedAt — only look up when boxed style is in effect.
			if effectiveStyle == "boxed" && len(prov.Variants) > 0 && l.ConsentStore != nil {
				grants, gerr := l.ConsentStore.List(cwd)
				if gerr == nil {
					for _, g := range grants {
						if g.Capability == rc.Name {
							disp.ConfirmedAt = g.ConfirmedAt
							break
						}
					}
				}
			}
			data.Capabilities = append(data.Capabilities, disp)
		}
		data.NeverAllow = cfg.NeverAllow
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

	// Project detection: always detect and show missing capabilities
	if suggestions := capability.DetectProject(os.DirFS(projectRoot)); len(suggestions) > 0 {
		// Build set of already-enabled capability names
		enabled := make(map[string]bool, len(data.Capabilities))
		for _, c := range data.Capabilities {
			enabled[c.Name] = true
		}
		// Show only capabilities that are detected but not yet enabled
		registry := capability.MergedRegistry(nil)
		for _, name := range suggestions {
			if enabled[name] {
				continue
			}
			if capDef, ok := registry[name]; ok {
				data.SuggestedCaps = append(data.SuggestedCaps, ui.CapabilityDisplay{
					Name:          name,
					ReadablePaths: capDef.Readable,
					WritablePaths: capDef.Writable,
					Suggested:     true,
					Source:        "detected",
				})
			}
		}
	}

	// Build sandbox info
	if sbDisabled {
		data.Sandbox = &ui.SandboxInfo{Disabled: true}
	} else {
		tempDir := os.TempDir()
		policy, _, _ := sandbox.PolicyFromConfig(sandboxCfg, sandbox.Paths{ProjectRoot: projectRoot, RuntimeDir: rtDirPath, HomeDir: homeDir, TempDir: tempDir})
		if policy != nil {
			tier := sandbox.PlatformIsolationTier(*policy)
			data.IsolationTier = &tier
			guardResults := sandbox.EvaluateGuards(policy)
			availableNames := sandbox.AvailableGuardNames(policy.Guards)
			si := &ui.SandboxInfo{
				Network: display.NetworkDisplayName(string(policy.Network)),
			}
			if len(policy.AllowPorts) > 0 {
				portStrs := make([]string, len(policy.AllowPorts))
				for i, p := range policy.AllowPorts {
					portStrs[i] = strconv.Itoa(p)
				}
				si.Ports = strings.Join(portStrs, ", ")
			}
			for _, g := range guardResults {
				if len(g.Hints) > 0 {
					si.Hints = append(si.Hints, g.Hints...)
				}
				if len(g.Rules) > 0 {
					display := ui.GuardDisplay{
						Name:      g.Name,
						Protected: g.Protected,
						Allowed:   g.Allowed,
						Readable:  g.Readable,
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

	// Populate extra paths that are from config (not capabilities)
	data.ExtraWritable = stringSetDiff(configWritableExtra, capOverrides.WritableExtra)
	data.ExtraReadable = stringSetDiff(configReadableExtra, capOverrides.ReadableExtra)
	data.ExtraDenied = stringSetDiff(configDeniedExtra, capOverrides.DeniedExtra)

	return data
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

// stringSetDiff returns elements in a that are not in b.
func stringSetDiff(a, b []string) []string {
	if len(a) == 0 {
		return nil
	}
	bSet := make(map[string]bool, len(b))
	for _, s := range b {
		bSet[s] = true
	}
	var diff []string
	for _, s := range a {
		if !bSet[s] {
			diff = append(diff, s)
		}
	}
	return diff
}

// trustStore returns the effective trust store.
func (l *Launcher) trustStore() *trust.Store {
	if l.TrustStore != nil {
		return l.TrustStore
	}
	return trust.DefaultStore()
}

// ensureDefaultProfileOnDisk materializes config.yaml and profiles/default.yaml
// when "default" is missing on disk (SynthesizedDefault or absent) and cfg is
// not a legacy single-file config.
func (l *Launcher) ensureDefaultProfileOnDisk(cfg *config.Config, agentOverride string) error {
	if cfg.LegacyMonolithic || cfg.NeedsGatewayMigration { // a failed gateway migration must not stop the launch
		return nil
	}
	if _, hasDefault := cfg.Contexts[config.DefaultProfileName]; hasDefault && !cfg.SynthesizedDefault {
		return nil
	}

	lookPath := l.lookPath()
	scan := ScanAgents(lookPath)
	foundNames := slices.Sorted(maps.Keys(scan.Found))
	if len(foundNames) > 0 && cfg.Agents == nil {
		cfg.Agents = make(map[string]config.AgentDef, len(foundNames))
	}
	for _, name := range foundNames {
		if _, exists := cfg.Agents[name]; !exists {
			cfg.Agents[name] = config.AgentDef{Binary: name}
		}
	}

	var defaultAgent string
	switch len(foundNames) {
	case 0:
		if agentOverride != "" {
			if err := ValidateAgentOverride(cfg, agentOverride, lookPath); err != nil {
				return err
			}
			defaultAgent = agentOverride
			if cfg.Agents == nil {
				cfg.Agents = make(map[string]config.AgentDef)
			}
			if _, exists := cfg.Agents[defaultAgent]; !exists {
				cfg.Agents[defaultAgent] = config.AgentDef{Binary: defaultAgent}
			}
		} else if existing, ok := cfg.Contexts[config.DefaultProfileName]; ok && existing.Agent != "" {
			defaultAgent = existing.Agent
		} else if len(cfg.Agents) > 0 {
			defaultAgent = config.DefaultAgentCandidate(slices.Collect(maps.Keys(cfg.Agents)))
		} else {
			return fmt.Errorf(
				"no known agents found on PATH (supported: %s)",
				strings.Join(KnownAgents, ", "),
			)
		}
	case 1:
		defaultAgent = foundNames[0]
	default:
		candidate := config.DefaultAgentCandidate(foundNames)
		isTTY := isStdinTTY
		if l.StdinIsTTY != nil {
			isTTY = l.StdinIsTTY
		}
		if l.Interactive && isTTY() && agentOverride == "" {
			fmt.Fprintf(l.stderr(), "Detected agents on PATH: %s\n", strings.Join(foundNames, ", "))
			fmt.Fprintf(l.stderr(), "Default agent for profile %q [%s]: ", config.DefaultProfileName, candidate)
			raw, _ := l.stdinReader().ReadString('\n')
			picked := strings.TrimSpace(raw)
			if picked == "" {
				picked = candidate
			} else if err := ValidateAgentOverride(cfg, picked, lookPath); err != nil {
				return err
			}
			defaultAgent = picked
		} else {
			defaultAgent = candidate
		}
	}

	if cfg.Contexts == nil {
		cfg.Contexts = make(map[string]config.Context)
	}
	defCtx := cfg.Contexts[config.DefaultProfileName]
	defCtx.Agent = defaultAgent
	cfg.Contexts[config.DefaultProfileName] = defCtx
	if cfg.DefaultContext == "" {
		cfg.DefaultContext = config.DefaultProfileName
	}
	cfg.SynthesizedDefault = false
	cfg.SynthesizedDefaultContext = false

	cfgPath := filepath.Join(l.configDir(), "config.yaml")
	if err := config.WriteConfigTo(cfg, cfgPath); err != nil {
		return fmt.Errorf("writing default profile: %w", err)
	}
	return nil
}

// isStdinTTY reports whether os.Stdin is connected to a terminal.
func isStdinTTY() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) // #nosec G115 -- stdin fd is always 0/1/2, never overflows int
}

// yamlVariantPins extracts capability_variants from a ProjectOverride
// into the shape VariantSelectionOptions expects. Returns nil when the
// override is nil or has no pins.
func yamlVariantPins(po *config.ProjectOverride) map[string][]string {
	if po == nil || len(po.CapabilityVariants) == 0 {
		return nil
	}
	out := make(map[string][]string, len(po.CapabilityVariants))
	for k, v := range po.CapabilityVariants {
		out[k] = append([]string(nil), v...)
	}
	return out
}
