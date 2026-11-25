package main

import (
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

	"github.com/jskswamy/aide/internal/capability"
	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/consent"
	aidectx "github.com/jskswamy/aide/internal/context"
	"github.com/jskswamy/aide/internal/doctor"
	"github.com/jskswamy/aide/internal/harness"
	"github.com/jskswamy/aide/internal/homepath"
	"github.com/jskswamy/aide/internal/launcher"
	"github.com/jskswamy/aide/internal/output"
	"github.com/jskswamy/aide/internal/sandbox"
	"github.com/jskswamy/aide/internal/secrets"
)

func doctorCmd() *cobra.Command {
	var sf selectionFlags
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
			if err := output.Emit(cmd.OutOrStdout(), format, r, r.RenderText); err != nil {
				return err
			}
			if r.HasErrors() {
				return fmt.Errorf("doctor found %d error(s)", r.Counts.Errors)
			}
			return nil
		},
	}
	sf.register(cmd, agentFlagUsage)
	return cmd
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

var secretRef = regexp.MustCompile(`\.secrets\.([A-Za-z0-9_]+)`)

// doctorDirectory resolves what launch would use here, without asking
// anything, writing anything or decrypting secrets, and reports what
// launch would warn about or fail on.
func doctorDirectory(cmd *cobra.Command, cfg *config.Config, cwd string, sf *selectionFlags) (*doctor.Directory, []doctor.Finding, error) {
	home, _ := os.UserHomeDir()
	res, err := previewSelection(cmd, cfg, cwd, home, sf)
	if err != nil {
		return nil, nil, err
	}
	rc := res.RC
	var here []doctor.Finding
	add := func(id, subject string, sev doctor.Severity, msg string, steps ...string) {
		here = append(here, doctor.Finding{ID: id, Subject: subject, Profile: rc.Name, Severity: sev, Message: msg, Fix: doctor.Fix{Steps: steps}})
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
			add("project.untrusted", f.Path, doctor.SeverityWarning, fmt.Sprintf("%s is not trusted, so it does not apply", f.Path), "Review "+f.Path, "Run: aide trust")
		}
	}

	credCtx, _, credErr := launcher.PlanProfileCredentials(cfg, rc, res.Inputs, &res.Sel)
	rc.Context = credCtx
	if credErr != nil {
		add("credential.plan-failed", rc.Name, doctor.SeverityError, fmt.Sprintf("credentials cannot be planned: %v; launch would fail", credErr))
	}
	harnessName := cfg.HarnessOf(rc.Context.Agent)
	if d, ok := harness.Lookup(harnessName); ok && d.Reach != harness.ReachMulti && res.Sel.Gateway.Source == launcher.SourceUnset {
		add("credential.choice-pending", rc.Name, doctor.SeverityWarning,
			fmt.Sprintf("%s has gateways marked but none chosen; a launch without a terminal fails", harnessName),
			fmt.Sprintf("Run: aide gateway use <name|none> --agent %s", harnessName))
	}

	agent := rc.Context.Agent
	// An agent missing from the agents map is config.unknown-agent.
	if binary, err := launcher.ResolveAgentBinary(cfg, agent); err == nil && !filepath.IsAbs(binary) {
		if _, err := exec.LookPath(binary); err != nil {
			add("agent.not-found", agent, doctor.SeverityError, fmt.Sprintf("agent %q binary %q not found on PATH; launch would fail", agent, binary),
				fmt.Sprintf("Install %s, or set agents.%s.binary", binary, agent))
		}
	}

	if store := rc.Context.Secret; store != "" {
		if keys, known := secrets.KeyNames(config.ResolveSecretPath(store)); known {
			for name, val := range rc.Context.Env {
				for _, m := range secretRef.FindAllStringSubmatch(val, -1) {
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
		Variants: sandbox.VariantSelectionOptions{Consent: consent.DefaultStore()},
	})
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
	if !sb.Disabled {
		paths := sandbox.Paths{ProjectRoot: projectRoot, RuntimeDir: "/tmp/aide-preview", HomeDir: home, TempDir: os.TempDir()}
		pol, pathWarnings, err := sandbox.PolicyFromConfig(sb.Config, paths)
		switch {
		case err != nil:
			add("sandbox.policy-failed", rc.Name, doctor.SeverityError, fmt.Sprintf("building the sandbox policy fails: %v; launch would fail", err))
		case pol != nil:
			dir.Guards, dir.Network = pol.Guards, string(pol.Network)
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
	here = append(here, redundantAllows(cfg, rc.Name, home)...)
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
	for _, name := range capability.DetectProject(projectFS) {
		if slices.Contains(active, name) || slices.Contains(disabled, name) {
			continue
		}
		dir.Detected = append(dir.Detected, name)
		c := capability.Builtins()[name]
		here = append(here, doctor.SuggestedCapability(name, matchedMarker(projectFS, c.Markers), append(slices.Clone(c.Readable), c.Writable...), rc.Name, local))
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
func redundantAllows(cfg *config.Config, profile, home string) []doctor.Finding {
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
		if l.Rejected || l.Override == nil {
			continue
		}
		s := doctor.Scope{Profile: profile, Inherited: l.Source == config.LayerInherited, Source: homepath.Tilde(l.Path, home)}
		out = append(out, doctor.RedundantAllows(l.Override.Sandbox, home, s)...)
	}
	return out
}
