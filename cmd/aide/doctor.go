package main

import (
	"bufio"
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/jskswamy/aide/internal/capability"
	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/consent"
	aidectx "github.com/jskswamy/aide/internal/context"
	"github.com/jskswamy/aide/internal/doctor"
	"github.com/jskswamy/aide/internal/fsutil"
	"github.com/jskswamy/aide/internal/harness"
	"github.com/jskswamy/aide/internal/homepath"
	"github.com/jskswamy/aide/internal/launcher"
	"github.com/jskswamy/aide/internal/output"
	"github.com/jskswamy/aide/internal/sandbox"
	"github.com/jskswamy/aide/internal/secrets"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

// doctorFixHooks are what --fix uses to ask and to run a fix; tests
// replace them.
var doctorFixHooks = struct {
	interactive func() bool
	ask         func(doctor.Finding) bool
	run         func(argv []string) (string, int, error)
}{
	interactive: func() bool { return isInteractiveTerminal(os.Stdin) && isInteractiveTerminal(os.Stderr) },
	ask:         askFix,
	run:         runFixCommand,
}

func doctorCmd() *cobra.Command {
	var sf selectionFlags
	var fix bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Report what is wrong with aide's setup here, and how to fix it",
		Long: `Report the profile, sandbox and trust state launch would use in this
directory, then every problem found in it and in the rest of the config,
each with a fix command or steps. It changes nothing and never decrypts
secrets. Exits non-zero only when it finds an error.

To explain a run that already failed, use aide --diagnose.`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			format, err := output.FromCmd(cmd)
			if err != nil {
				return err
			}
			r := runDoctor(cmd, &sf)
			var refused error
			if fix {
				if refused = fixRefusal(r); refused == nil {
					r.FixResults = doctor.ApplyFixes(r, doctorFixHooks.interactive(), doctorFixHooks.ask, doctorFixHooks.run,
						func() doctor.Report { return runDoctor(cmd, &sf) })
					results := r.FixResults
					r = runDoctor(cmd, &sf)
					r.FixResults = results
				}
			}
			if err := output.Emit(cmd.OutOrStdout(), format, r, r.RenderText); err != nil {
				return err
			}
			if refused != nil {
				return refused
			}
			if r.HasErrors() {
				return fmt.Errorf("doctor found %d error(s)", r.Counts.Errors)
			}
			return nil
		},
	}
	sf.register(cmd, agentFlagUsage)
	cmd.Flags().BoolVar(&fix, "fix", false, "run each finding's fix command, asking before each one")
	return cmd
}

// fixRefusal says why --fix must not run at all.
func fixRefusal(r doctor.Report) error {
	switch {
	case r.InsideSandbox:
		return errors.New("--fix refused: running inside an aide sandbox, which cannot write the config; run aide doctor --fix in a separate terminal")
	case r.Directory == nil || r.Directory.Profile == "":
		return errors.New("--fix refused: the config does not load")
	case r.LegacyLayout:
		return errors.New("--fix refused: the config uses the legacy single-file layout; run aide config migrate first")
	case r.GatewayMigration:
		return errors.New("--fix refused: a gateway migration is pending; run aide once to migrate first")
	}
	return nil
}

// askFix shows one fix and asks for a yes on the terminal.
func askFix(f doctor.Finding) bool {
	fmt.Fprintf(os.Stderr, "\n%s\n  run: %s\n", f.Message, doctor.ShellJoin(f.Fix.Argv))
	if f.Source != "" {
		fmt.Fprintf(os.Stderr, "  changes: %s (%s)\n", f.Source, f.Layer)
	}
	for _, s := range f.Fix.SideEffects {
		fmt.Fprintf(os.Stderr, "  also: %s\n", s)
	}
	if f.WidensAccess {
		fmt.Fprintln(os.Stderr, "  this widens what the agent can reach")
	}
	fmt.Fprint(os.Stderr, "Apply? [y/N] ")
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.EqualFold(strings.TrimSpace(line), "y")
}

// runFixCommand runs an aide command with this binary, stdin closed and
// output captured.
func runFixCommand(argv []string) (string, int, error) {
	self, err := os.Executable()
	if err != nil {
		return "", -1, err
	}
	c := exec.Command(self, argv[1:]...)
	c.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool { return strings.HasPrefix(kv, "AIDE_FORMAT=") })
	out, err := c.CombinedOutput()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return string(out), exitErr.ExitCode(), nil
	}
	return string(out), 0, err
}

// runDoctor builds the report. It never fails: problems, including a
// config that does not load, become findings.
func runDoctor(cmd *cobra.Command, sf *selectionFlags) doctor.Report {
	inside := os.Getenv("AIDE_SANDBOX") == "on"
	env, loadErr := cmdEnv(cmd)
	if loadErr != nil {
		// Without a config, only what the project itself shows is known.
		dir := &doctor.Directory{CWD: env.CWD(), Capabilities: []doctor.DirCapability{}, Guards: []string{}, ProjectConfig: []doctor.ProjectFile{}}
		dir.Detected = append([]string{}, capability.DetectProject(os.DirFS(aidectx.ProjectRoot(env.CWD())))...)
		r := doctor.NewReport(version, dir, []doctor.Finding{loadFailure(loadErr, inside)})
		r.InsideSandbox = inside
		return r
	}
	cfg, cwd := env.Config(), env.CWD()
	dir, here, err := doctorDirectory(cmd, cfg, cwd, sf)
	findings := append(ownedByHere(doctor.ConfigChecks(cfg, exec.LookPath), dir, here), here...)
	if err != nil {
		findings = append(findings, doctor.Finding{
			ID: "selection.failed", Severity: doctor.SeverityError,
			Message: fmt.Sprintf("no profile can be selected here: %v", err),
			Fix:     doctor.Fix{Steps: []string{"Run aide which to see how profiles match this directory"}},
		})
	}
	r := doctor.NewReport(version, dir, findings)
	r.InsideSandbox = inside
	r.LegacyLayout, r.GatewayMigration = cfg.LegacyMonolithic, cfg.NeedsGatewayMigration
	return r
}

// loadFailure reports a config that did not load. Inside the sandbox a
// permission error means the sandbox hides the config, not that it is
// broken.
func loadFailure(err error, inside bool) doctor.Finding {
	if inside && errors.Is(err, fs.ErrPermission) {
		return doctor.Finding{
			ID: "doctor.config-unreadable", Severity: doctor.SeverityWarning,
			Message: fmt.Sprintf("the sandbox does not let aide doctor read the config: %v", err),
			Fix:     doctor.Fix{Steps: []string{"Run aide doctor in a separate terminal, outside the agent session"}},
		}
	}
	return doctor.Finding{
		ID: "config.load-failed", Severity: doctor.SeverityError,
		Message: fmt.Sprintf("the config does not load: %v", err),
		Fix:     doctor.Fix{Steps: []string{"Fix the file and line named above, then run aide validate"}},
	}
}

// ownedByHere drops config-wide findings that a current-directory check
// reports more precisely: the current agent's binary and the current
// profile's sandbox reference.
func ownedByHere(config []doctor.Finding, dir *doctor.Directory, here []doctor.Finding) []doctor.Finding {
	if dir == nil {
		return config
	}
	owned := map[string]bool{}
	for _, f := range here {
		owned[f.ID] = true
	}
	return slices.DeleteFunc(config, func(f doctor.Finding) bool {
		return f.ID == "config.agent-not-on-path" && f.Subject == dir.Agent && owned["agent.not-found"] ||
			f.ID == "config.sandbox-ref-invalid" && f.Subject == dir.Profile && owned["sandbox.resolve-failed"]
	})
}

var (
	templateAction = regexp.MustCompile(`\{\{[^}]*\}\}`)
	secretRef      = regexp.MustCompile(`\.secrets\.([A-Za-z0-9_]+)`)
)

// doctorDirectory resolves what launch would use here, without asking
// anything, writing anything or decrypting secrets, and reports what
// launch would warn about or fail on.
func doctorDirectory(cmd *cobra.Command, cfg *config.Config, cwd string, sf *selectionFlags) (*doctor.Directory, []doctor.Finding, error) {
	home, _ := os.UserHomeDir()
	// Selection merges project and capability paths into the profile's
	// sandbox block; the redundant-rule check needs each file's own.
	pristine, _ := config.Load(config.Dir(), cwd)
	res, err := previewSelection(cmd, cfg, cwd, home, sf)
	if err != nil {
		return nil, nil, err
	}
	rc := res.RC
	var here []doctor.Finding
	add := func(id, subject string, sev doctor.Severity, msg string, steps ...string) {
		here = append(here, doctor.Finding{ID: id, Subject: subject, Profile: rc.Name, Severity: sev, Message: msg, Fix: doctor.Fix{Steps: steps}})
	}

	if tied := aidectx.TiedMatches(cfg, cwd, aidectx.GitRemotes(cwd)); rc.Source == aidectx.SourceMatch && len(tied) > 1 {
		add("profile.match-tie", strings.Join(tied, ","), doctor.SeverityWarning,
			fmt.Sprintf("profiles %s match this directory equally well; %s wins only because its name sorts first", strings.Join(tied, ", "), tied[0]),
			"Make one profile's match rule more specific: aide profile bind <profile> --scope repo binds it to this repository", "See the rules with: aide profile list")
	}
	if rc.Context.Sandbox == nil && (cfg.ProjectOverride == nil || cfg.ProjectOverride.Sandbox == nil) {
		add("sandbox.default-policy", rc.Name, doctor.SeverityTip,
			"the profile uses aide's default sandbox; that may be what you want",
			"Review it with: aide sandbox show", "Tune it with: aide sandbox allow, aide sandbox deny or aide sandbox guard")
	}
	for _, n := range res.Notices {
		add("selection.notice", rc.Name, doctor.SeverityWarning, n)
	}
	if cfg.ProjectConfigErr != nil {
		add("project.parse-failed", ".aide.yaml", doctor.SeverityError, fmt.Sprintf("the project config does not load: %v", cfg.ProjectConfigErr), "Fix .aide.yaml at the line named above")
	}
	for _, f := range res.Gate.Files {
		if f.Status == "denied" {
			add("project.denied", f.Path, doctor.SeverityTip, fmt.Sprintf("%s is denied, so it does not apply", f.Path), "To be asked again, run: aide untrust "+f.Path)
		} else {
			add("project.untrusted", f.Path, doctor.SeverityWarning, fmt.Sprintf("%s is not trusted, so it does not apply", f.Path), "Review "+f.Path, "Run: aide trust "+doctor.ShellQuote(layerPath(cfg, f.Source, f.Path)))
		}
	}

	credCtx, credPlan, credErr := launcher.PlanProfileCredentials(cfg, rc, res.Inputs, &res.Sel)
	rc.Context = credCtx
	if credErr != nil {
		add("credential.plan-failed", rc.Name, doctor.SeverityError, fmt.Sprintf("credentials cannot be planned: %v; launch would fail", credErr))
	}
	for _, b := range credPlan.Bindings {
		// Launch refuses a selected keyless gateway off loopback.
		if gw, ok := rc.Context.Gateways[b.Name]; ok {
			if err := config.ValidateGatewayTarget(b.Name, gw); err != nil {
				add("gateway.target-refused", b.Name, doctor.SeverityError, fmt.Sprintf("%v; launch would fail", err))
			}
		}
		if b.Store == "" || b.Optional {
			continue
		}
		path := config.ResolveSecretPath(b.Store)
		if _, err := os.Stat(path); err != nil {
			add("credential.store-missing", b.Name, doctor.SeverityError,
				fmt.Sprintf("credential %q reads secret store %q, which does not exist; launch would fail", b.Name, b.Store),
				fmt.Sprintf("Create it with: aide secrets create %s", b.Store))
		} else if keys, known := secrets.KeyNames(path); known && !slices.Contains(keys, b.Key) {
			add("credential.key-missing", b.Name, doctor.SeverityError,
				fmt.Sprintf("credential %q needs key %q, which store %q does not have; launch would fail", b.Name, b.Key, b.Store),
				fmt.Sprintf("Add %s to the store with: aide secrets edit %s", b.Key, b.Store))
		}
	}
	harnessName := cfg.HarnessOf(rc.Context.Agent)
	if d, ok := harness.Lookup(harnessName); ok && d.Reach != harness.ReachMulti && res.Sel.Gateway.Source == launcher.SourceUnset {
		add("credential.choice-pending", rc.Name, doctor.SeverityWarning,
			fmt.Sprintf("%s has gateways marked but none chosen; a launch without a terminal fails", harnessName),
			fmt.Sprintf("Run: aide gateway use <name|none> --agent %s", harnessName))
	}

	agent := rc.Context.Agent
	// An agent missing from the agents map is config.unknown-agent.
	if binary, err := launcher.ResolveAgentBinary(cfg, agent); err == nil {
		if _, err := exec.LookPath(binary); err != nil {
			add("agent.not-found", agent, doctor.SeverityError, fmt.Sprintf("agent %q binary %q not found on PATH; launch would fail", agent, binary),
				fmt.Sprintf("Install %s, or set agents.%s.binary", binary, agent))
		}
	}

	if store := rc.Context.Secret; store != "" {
		// The recipient list is sops metadata, readable without decrypting.
		if keys, err := secrets.ListRecipients(config.ResolveSecretPath(store)); err == nil && len(keys) == 1 {
			add("secret.single-recipient", store, doctor.SeverityTip,
				fmt.Sprintf("secret store %q is encrypted for one key only; losing it loses the secrets", store),
				fmt.Sprintf("Add a second key, such as a teammate's or a backup: aide secrets rotate %s --add-key <age-public-key>", store))
		}
		if keys, known := secrets.KeyNames(config.ResolveSecretPath(store)); known {
			for name, val := range rc.Context.Env {
				for _, m := range secretRef.FindAllStringSubmatch(strings.Join(templateAction.FindAllString(val, -1), " "), -1) {
					if !slices.Contains(keys, m[1]) {
						add("secret.key-missing", rc.Name+"."+name, doctor.SeverityError,
							fmt.Sprintf("env var %s uses secret key %q, which store %q does not have; launch would fail", name, m[1], store),
							fmt.Sprintf("Add %s to the store with: aide secrets edit %s", m[1], store))
					}
				}
			}
		}
	}

	projectRoot := aidectx.ProjectRoot(cwd)
	dir := &doctor.Directory{
		CWD: cwd, Profile: rc.Name, MatchReason: rc.MatchReason, Agent: rc.Context.Agent,
		SecretStore: rc.Context.Secret, ProjectConfig: projectFiles(cfg, res.Gate, home),
		Capabilities: []doctor.DirCapability{}, Guards: []string{}, Detected: []string{},
	}
	sb, sbErr := launcher.ResolveSandbox(cfg, rc, launcher.SandboxInputs{
		ProjectRoot: projectRoot, HomeDir: home,
		Getenv:   launcher.PreviewGetenv(rc.Context.Env, home),
		Variants: sandbox.VariantSelectionOptions{Consent: consent.DefaultStore(), EnvLookup: launcher.PreviewGetenv(rc.Context.Env, home)},
	})
	if slices.Contains(rc.Context.Capabilities, guards.RetiredGuardGitRemote) {
		add("capability.retired", "capability:git-remote", doctor.SeverityTip,
			"the git-remote capability is retired and grants nothing; git remotes follow the sandbox's network rules",
			"Remove it: aide cap disable git-remote (add --global --profile "+rc.Name+" if the profile lists it)")
	}
	if sb.Config != nil && (slices.Contains(sb.Config.Guards, guards.RetiredGuardGitRemote) || slices.Contains(sb.Config.GuardsExtra, guards.RetiredGuardGitRemote)) {
		// aide sandbox unguard edits only the profile's or project's own
		// block; a named sandbox has to be edited directly.
		step := "Remove it: aide sandbox unguard git-remote (add --global --profile " + rc.Name + " if the profile lists it)"
		if ref := rc.Context.Sandbox; ref != nil && ref.ProfileName != "" {
			step = "Remove git-remote from guards or guards_extra in the named sandbox " + ref.ProfileName + " (sandboxes: in your aide config)"
		}
		add("capability.retired", "guard:git-remote", doctor.SeverityTip, "the git-remote guard is retired and grants nothing", step)
	}
	if sbErr != nil {
		add("sandbox.resolve-failed", rc.Name, doctor.SeverityError, fmt.Sprintf("%v; launch would fail", sbErr))
	}
	var active []string
	if sb.Caps != nil {
		var envAllow []string
		for _, c := range sb.Caps.Capabilities {
			prov := sb.Provenance[c.Name]
			dir.Capabilities = append(dir.Capabilities, doctor.DirCapability{Name: c.Name, Variants: append([]string{}, prov.Variants...)})
			active = append(active, c.Name)
			envAllow = append(envAllow, c.EnvAllow...)
			if prov.Reason == "default:non-interactive" {
				add("capability.consent-pending", c.Name, doctor.SeverityTip,
					fmt.Sprintf("%s found %s but uses its default variant until you choose", c.Name, prov.EvidenceSummary),
					"Run aide here once to choose, or see: aide cap variants")
			}
		}
		for _, w := range capability.CredentialWarnings(envAllow) {
			add("credential.warning", w, doctor.SeverityWarning, w)
		}
		for _, w := range capability.CompositionWarnings(sb.Caps.Capabilities) {
			add("capability.composition", w, doctor.SeverityWarning, w)
		}
	}
	dir.Network = "sandbox disabled"
	if sbErr != nil {
		dir.Network = "unknown: the sandbox does not resolve"
	} else if !sb.Disabled {
		paths := sandbox.Paths{ProjectRoot: projectRoot, RuntimeDir: "/tmp/aide-preview", HomeDir: home, TempDir: os.TempDir()}
		pol, pathWarnings, err := sandbox.PolicyFromConfig(sb.Config, paths)
		switch {
		case err != nil:
			add("sandbox.policy-failed", rc.Name, doctor.SeverityError, fmt.Sprintf("building the sandbox policy fails: %v; launch would fail", err))
		case pol != nil:
			dir.Guards, dir.Network = pol.Guards, string(pol.Network)
			here = append(here, sandboxTips(rc.Name, pol)...)
			if sb.Caps != nil {
				here = append(here, deniedCapabilityPaths(rc.Name, sb.Caps.Capabilities, pol.ExtraDenied, home)...)
			}
		}
		// Granted paths that do not exist are common and harmless; one
		// tip covers them all.
		var missing int
		for _, w := range pathWarnings {
			if strings.HasPrefix(w, "skipped: ") {
				missing++
				continue
			}
			add("sandbox.path-warning", w, doctor.SeverityTip, w)
		}
		if missing > 0 {
			add("sandbox.paths-missing", rc.Name, doctor.SeverityTip,
				fmt.Sprintf("%d granted sandbox paths do not exist and are skipped", missing),
				"See which with: aide sandbox show")
		}
	}
	if pristine != nil {
		here = append(here, redundantAllows(pristine, cfg, rc.Name, home)...)
	}
	var disabled []string
	if cfg.ProjectOverride != nil {
		disabled = cfg.ProjectOverride.DisabledCapabilities
	}
	var local *doctor.ProjectFile
	for i := range dir.ProjectConfig {
		if dir.ProjectConfig[i].Layer == config.LayerLocal {
			local = &dir.ProjectConfig[i]
		}
	}
	projectFS := os.DirFS(projectRoot)
	detected := capability.DetectProject(projectFS)
	// gpu has no markers on purpose (importing torch must never turn it
	// on), so only doctor looks for GPU libraries, and only to suggest.
	if capability.AnyMarkerMatches(projectFS, gpuLibraryMarkers) {
		detected = append(detected, "gpu")
	}
	markersOf := func(name string) []capability.Marker {
		if name == "gpu" {
			return gpuLibraryMarkers
		}
		return capability.Builtins()[name].Markers
	}
	for _, name := range detected {
		if slices.Contains(active, name) || slices.Contains(disabled, name) {
			continue
		}
		dir.Detected = append(dir.Detected, name)
		c := capability.Builtins()[name]
		here = append(here, doctor.SuggestedCapability(name, matchedMarker(projectFS, markersOf(name)), append(slices.Clone(c.Readable), c.Writable...), rc.Name, local))
	}
	return dir, here, nil
}

// projectFiles lists each .aide.yaml layer and whether it applies.
func projectFiles(cfg *config.Config, gate launcher.ProjectGate, home string) []doctor.ProjectFile {
	files := []doctor.ProjectFile{}
	for _, l := range cfg.ProjectLayers {
		state := "trusted"
		if l.Rejected {
			state = "untrusted"
			for _, f := range gate.Files {
				if f.Source == l.Source && f.Status != "" {
					state = f.Status
				}
			}
		}
		files = append(files, doctor.ProjectFile{Layer: l.Source, Path: homepath.Tilde(l.Path, home), State: state})
	}
	return files
}

// matchedMarker names the first marker present in the project.
func matchedMarker(fsys fs.FS, markers []capability.Marker) string {
	for _, m := range markers {
		if capability.AnyMarkerMatches(fsys, []capability.Marker{m}) {
			return cmp.Or(m.File, m.DirExists, m.GlobPath, m.Contains.File, "a project marker")
		}
	}
	return "a project marker"
}

// redundantAllows checks the sandbox blocks that apply to the current
// profile: its own (or the named sandbox it uses) and each trusted
// .aide.yaml.
func redundantAllows(cfg, applied *config.Config, profile, home string) []doctor.Finding {
	var out []doctor.Finding
	if ref := cfg.Contexts[profile].Sandbox; ref != nil {
		if ref.ProfileName != "" {
			if sp, ok := cfg.Sandboxes[ref.ProfileName]; ok {
				out = append(out, doctor.RedundantAllows(&sp, home, doctor.Scope{Profile: profile, Named: ref.ProfileName})...)
			}
		} else {
			out = append(out, doctor.RedundantAllows(ref.Inline, home, doctor.Scope{Profile: profile, Global: true})...)
		}
	}
	for _, l := range cfg.ProjectLayers {
		// Trust is decided by selection, on the applied config.
		if !slices.ContainsFunc(applied.ProjectLayers, func(a config.ProjectLayer) bool { return a.Path == l.Path && !a.Rejected }) {
			continue
		}
		// The loader rewrites relative paths; --remove matches the file's
		// own text, so read the rules as written.
		var raw config.ProjectOverride
		if err := yaml.Unmarshal(l.Contents, &raw); err != nil {
			continue
		}
		// Relative rules resolve against the checkout root, as the loader does.
		base, _ := config.CheckoutRoot(filepath.Dir(l.Path))
		s := doctor.Scope{Profile: profile, Inherited: l.Source == config.LayerInherited, Source: homepath.Tilde(l.Path, home), Base: base}
		out = append(out, doctor.RedundantAllows(raw.Sandbox, home, s)...)
	}
	return out
}

// sandboxTips suggests tightening a policy: unrestricted network and
// opt-in guards that are available but off.
func sandboxTips(profile string, pol *sandbox.Policy) []doctor.Finding {
	var out []doctor.Finding
	if pol.Network == "unrestricted" {
		out = append(out, doctor.Finding{
			ID: "sandbox.network-unrestricted", Subject: profile, Profile: profile, Severity: doctor.SeverityTip,
			Message: "the agent's network is unrestricted, inbound and outbound",
			Fix: doctor.Fix{Steps: []string{
				"If it only needs web access: aide sandbox network outbound, then aide sandbox ports 443",
				"If the network capability opened it, disable that instead: aide cap disable network",
			}},
		})
	}
	var off []string
	for _, g := range guards.AllGuards() {
		if g.Type() == "opt-in" && !slices.Contains(pol.Guards, g.Name()) {
			off = append(off, g.Name())
		}
	}
	if len(off) > 0 {
		out = append(out, doctor.Finding{
			ID: "sandbox.opt-in-guards", Subject: profile, Profile: profile, Severity: doctor.SeverityTip,
			Message: "opt-in guards available but off: " + strings.Join(off, ", "),
			Fix:     doctor.Fix{Steps: []string{"See what each protects with: aide sandbox guards", "Turn one on with: aide sandbox guard <name>"}},
		})
	}
	return out
}

// gpuLibraryMarkers are signs a Python project uses the GPU.
var gpuLibraryMarkers = func() []capability.Marker {
	var ms []capability.Marker
	for _, lib := range []string{"torch", "jax", "tensorflow", "moderngl", "cupy"} {
		ms = append(ms,
			capability.Marker{Contains: capability.ContainsSpec{File: "requirements.txt", Pattern: lib}},
			capability.Marker{Contains: capability.ContainsSpec{File: "pyproject.toml", Pattern: lib}},
			capability.Marker{GlobContains: capability.GlobContainsSpec{Glob: "*.py", Pattern: "import " + lib}},
		)
	}
	return ms
}()

// deniedCapabilityPaths reports capability paths that a denied_extra
// rule blocks, so the capability does not do what it says.
func deniedCapabilityPaths(profile string, caps []capability.ResolvedCapability, denied []string, home string) []doctor.Finding {
	var out []doctor.Finding
	for _, c := range caps {
		for _, p := range append(slices.Clone(c.Readable), c.Writable...) {
			path := homepath.Expand(p, home)
			for _, d := range denied {
				if fsutil.IsUnderDir(path, homepath.Expand(d, home)) {
					out = append(out, doctor.Finding{
						ID: "capability.path-denied", Subject: c.Name + ":" + p, Profile: profile, Severity: doctor.SeverityWarning,
						Message: fmt.Sprintf("the %s capability grants %s, but denied_extra %s blocks it", c.Name, p, d),
						Fix:     doctor.Fix{Steps: []string{fmt.Sprintf("Drop %s from denied_extra, or disable %s: aide cap disable %s", d, c.Name, c.Name)}},
					})
				}
			}
		}
	}
	return out
}

// layerPath is the real path of the .aide.yaml layer a trust entry
// names; the entry itself holds a ~-shortened path for display.
func layerPath(cfg *config.Config, source, display string) string {
	for _, l := range cfg.ProjectLayers {
		if l.Source == source {
			return l.Path
		}
	}
	return display
}
