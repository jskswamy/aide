package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"slices"

	"github.com/spf13/cobra"

	"github.com/jskswamy/aide/internal/capability"
	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/consent"
	aidectx "github.com/jskswamy/aide/internal/context"
	"github.com/jskswamy/aide/internal/doctor"
	"github.com/jskswamy/aide/internal/homepath"
	"github.com/jskswamy/aide/internal/launcher"
	"github.com/jskswamy/aide/internal/output"
	"github.com/jskswamy/aide/internal/sandbox"
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
	findings := doctor.ConfigChecks(cfg, exec.LookPath)
	dir, err := doctorDirectory(cmd, cfg, cwd, sf)
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

// doctorDirectory resolves what launch would use here, without asking
// anything, writing anything or decrypting secrets.
func doctorDirectory(cmd *cobra.Command, cfg *config.Config, cwd string, sf *selectionFlags) (*doctor.Directory, error) {
	home, _ := os.UserHomeDir()
	res, err := previewSelection(cmd, cfg, cwd, home, sf)
	if err != nil {
		return nil, err
	}
	rc := res.RC
	credCtx, _, _ := launcher.PlanProfileCredentials(cfg, rc, res.Inputs, &res.Sel)
	rc.Context = credCtx

	projectRoot := aidectx.ProjectRoot(cwd)
	dir := &doctor.Directory{
		CWD: cwd, Profile: rc.Name, MatchReason: rc.MatchReason, Agent: rc.Context.Agent,
		SecretStore: rc.Context.Secret, ProjectConfig: projectFiles(cfg, res.Gate, home),
		Capabilities: []doctor.DirCapability{}, Guards: []string{}, Detected: []string{},
	}
	sb, _ := launcher.ResolveSandbox(cfg, rc, launcher.SandboxInputs{
		ProjectRoot: projectRoot, HomeDir: home,
		Variants: sandbox.VariantSelectionOptions{Consent: consent.DefaultStore()},
	})
	var active []string
	if sb.Caps != nil {
		for _, c := range sb.Caps.Capabilities {
			dir.Capabilities = append(dir.Capabilities, doctor.DirCapability{Name: c.Name, Variants: append([]string{}, sb.Provenance[c.Name].Variants...)})
			active = append(active, c.Name)
		}
	}
	dir.Network = "sandbox disabled"
	if !sb.Disabled {
		paths := sandbox.Paths{ProjectRoot: projectRoot, RuntimeDir: "/tmp/aide-preview", HomeDir: home, TempDir: os.TempDir()}
		if pol, _, err := sandbox.PolicyFromConfig(sb.Config, paths); err == nil && pol != nil {
			dir.Guards, dir.Network = pol.Guards, string(pol.Network)
		}
	}
	for _, name := range capability.DetectProject(os.DirFS(projectRoot)) {
		if !slices.Contains(active, name) {
			dir.Detected = append(dir.Detected, name)
		}
	}
	return dir, nil
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
