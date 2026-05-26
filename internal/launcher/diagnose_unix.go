//go:build !windows

package launcher

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jskswamy/aide/internal/diag"
)

// diagExecerFactory builds the runner. Tests override.
var diagExecerFactory = func(lineLimit int, byteLimit int64) DiagnoseRunner {
	return &DiagnoseExecer{StderrLineLimit: lineLimit, StderrByteLimit: byteLimit}
}

// runDiagnose executes the child via fork+exec, gathers a RunResult,
// renders a post-mortem report, persists it, and prints a terminal
// summary plus the file path. Returns an exitError carrying the child's
// exit code so main can propagate it.
//
// extraFiles carries fds set by the sandbox layer (policy memfd for
// Landlock, seccomp memfd for bwrap) that must be inherited by the child.
func (l *Launcher) runDiagnose(binary string, args, env []string, extraFiles []*os.File, dc diagContext) error {
	lineLimit := envIntDefault("AIDE_DIAGNOSE_STDERR_LINES", 200)
	byteLimit := int64(envIntDefault("AIDE_DIAGNOSE_STDERR_BYTES", 65536))
	runner := diagExecerFactory(lineLimit, byteLimit)

	pre := l.buildDiagPreInput(binary, args, env, dc)
	report := diag.Pre(pre)

	res, runErr := runner.Run(binary, args, env, extraFiles)
	if runErr != nil {
		return runErr
	}

	home, _ := os.UserHomeDir()
	report = diag.Post(report, diag.PostInput{
		ExitCode:        res.ExitCode,
		Signal:          res.Signal,
		Runtime:         res.Runtime,
		StderrTail:      res.StderrTail,
		StderrTruncated: res.StderrTruncatedBytes,
		HomeDir:         home,
	})

	if l.DiagnoseTrace {
		reason, denials, _ := diag.CollectDenials(res.Pid, 30*time.Second)
		report.Denials = denials
		report.TraceUnavailable = reason
	}

	body := diag.Markdown(report)
	w := &diag.Writer{CacheDir: diagCacheDir()}
	path, werr := w.Write(body, l.stderr())

	fmt.Fprint(l.stderr(), diag.Summary(report))
	if werr == nil {
		fmt.Fprintf(l.stderr(), "\nfull report: %s\n", path)
		fmt.Fprintln(l.stderr(), "please review the file before sharing — secret values are redacted, but paths and argv are not.")
	}

	if res.ExitCode != 0 {
		return &exitError{code: res.ExitCode}
	}
	return nil
}

func diagCacheDir() string {
	if base := os.Getenv("XDG_CACHE_HOME"); base != "" {
		return filepath.Join(base, "aide", "diagnose")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "aide", "diagnose")
}

// buildDiagPreInput constructs the PreInput passed to diag.Pre. Pulls
// runtime values (cwd, shell, locale) from the process environment and
// folds in the launcher-derived diagContext so the Sandbox / Secrets
// wiring / Environment sections of the rendered report are populated.
func (l *Launcher) buildDiagPreInput(binary string, args, env []string, dc diagContext) diag.PreInput {
	cwd, _ := os.Getwd()
	return diag.PreInput{
		AideVersion:       dc.Version,
		AideCommit:        dc.Commit,
		AideBuildDate:     dc.BuildDate,
		Shell:             os.Getenv("SHELL"),
		Locale:            os.Getenv("LANG"),
		CWD:               cwd,
		ResolvedConfig:    dc.ResolvedConfig,
		AgentBinary:       binary,
		Argv:              args,
		Env:               env,
		SecretSourcePaths: dc.SecretSourcePaths,
		AgeKeySource:      dc.AgeKeySource,
		Sandbox:           dc.Sandbox,
	}
}
