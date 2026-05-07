// Package diag produces redacted post-mortem reports for failed child agent runs.
package diag

import "time"

// EnvKey records that an env var was injected, but never its value.
type EnvKey struct {
	Name   string
	Length int
}

// SandboxInfo captures the resolved sandbox at exec time.
type SandboxInfo struct {
	Disabled   bool
	Variants   []string
	GuardNames []string
	RenderedSB string // full .sb content; goes to file only, not summary
}

// Denial is one macOS sandbox-deny event captured under --diagnose-trace.
type Denial struct {
	Operation string
	Path      string
	PID       int
}

// Report is the typed redaction surface. No field may hold a secret value.
type Report struct {
	AideVersion   string
	AideCommit    string
	AideBuildDate string
	OS            string
	Arch          string
	Shell         string
	Locale        string

	CWD            string
	ResolvedConfig string
	AgentBinary    string
	Argv           []string

	EnvKeys           []EnvKey
	SecretSourcePaths []string // file paths only (sops files, age key files), never values
	AgeKeySource      string

	Sandbox SandboxInfo

	ExitCode        int
	Signal          string // "" if not signal-killed
	Runtime         time.Duration
	StderrTail      string // already redacted ($HOME → ~)
	StderrTruncated int    // bytes dropped, 0 if none

	Denials          []Denial // populated only by --diagnose-trace
	TraceUnavailable string   // reason if trace mode requested but failed
}

// Classification categorizes the failure for the TL;DR line.
func (r Report) Classification() string {
	switch {
	case r.ExitCode == 0:
		return "exited cleanly"
	case r.Signal != "":
		return "killed by " + r.Signal
	case r.Runtime < 500*time.Millisecond:
		return "fast-fail (<500ms)"
	default:
		return "crashed mid-run"
	}
}
