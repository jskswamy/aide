package diag

// This file holds the collector that turns launcher state into a
// redacted Report.

import (
	"runtime"
	"strconv"
	"strings"
	"time"
)

// PreInput is the data the launcher hands the collector before the child
// executes. Env values may contain secrets; collector strips them at the
// chokepoint so they never enter the Report.
type PreInput struct {
	AideVersion   string
	AideCommit    string
	AideBuildDate string
	Shell         string
	Locale        string

	CWD            string
	ResolvedConfig string
	AgentBinary    string
	Argv           []string

	Env               []string // raw env slice; values redacted at collection time
	SecretSourcePaths []string
	AgeKeySource      string

	Sandbox SandboxInfo
}

// Pre snapshots the launcher state into a Report (without exit fields).
// Strips env values and argv =VALUE pairs whose flag name implies
// secrecy. This is the single chokepoint where secret values would
// otherwise enter the report.
func Pre(in PreInput) Report {
	return Report{
		AideVersion:       in.AideVersion,
		AideCommit:        in.AideCommit,
		AideBuildDate:     in.AideBuildDate,
		OS:                runtime.GOOS,
		Arch:              runtime.GOARCH,
		Shell:             in.Shell,
		Locale:            in.Locale,
		CWD:               in.CWD,
		ResolvedConfig:    in.ResolvedConfig,
		AgentBinary:       in.AgentBinary,
		Argv:              redactArgv(in.Argv),
		EnvKeys:           collectEnvKeys(in.Env),
		SecretSourcePaths: append([]string(nil), in.SecretSourcePaths...),
		AgeKeySource:      in.AgeKeySource,
		Sandbox:           in.Sandbox,
	}
}

func collectEnvKeys(env []string) []EnvKey {
	out := make([]EnvKey, 0, len(env))
	for _, kv := range env {
		i := strings.IndexByte(kv, '=')
		if i < 0 {
			continue
		}
		out = append(out, EnvKey{Name: kv[:i], Length: len(kv) - i - 1})
	}
	return out
}

// sensitiveFlagSubstrings names flag-name fragments that imply a secret
// value follows. Match is case-insensitive.
var sensitiveFlagSubstrings = []string{
	"api-key", "apikey", "token", "secret",
	"password", "passwd", "auth-token",
	"authorization", "credential", "passphrase",
	"private-key",
}

// redactArgv replaces "--key=value" with "--key=<redacted:N>" when the
// flag name matches a sensitive substring. Also handles the
// space-separated form "--key value" by redacting the next argv element
// when the flag name is sensitive and the next token is not itself a
// flag. Conservative — defaults to passing the value through if no
// substring matches.
func redactArgv(argv []string) []string {
	if len(argv) == 0 {
		return nil
	}
	out := make([]string, len(argv))
	skipNext := false
	for i, a := range argv {
		if skipNext {
			skipNext = false
			continue
		}
		if !strings.HasPrefix(a, "-") {
			out[i] = a
			continue
		}
		eq := strings.IndexByte(a, '=')
		if eq > 0 {
			flag := strings.ReplaceAll(strings.ToLower(a[:eq]), "_", "-")
			val := a[eq+1:]
			if val != "" && flagIsSensitive(flag) {
				out[i] = a[:eq+1] + "<redacted:" + strconv.Itoa(len(val)) + ">"
			} else {
				out[i] = a
			}
			continue
		}
		// No "=" in this arg. Look ahead for a space-separated value.
		flag := strings.ReplaceAll(strings.ToLower(a), "_", "-")
		if flagIsSensitive(flag) && i+1 < len(argv) && !strings.HasPrefix(argv[i+1], "-") {
			out[i] = a
			out[i+1] = "<redacted:" + strconv.Itoa(len(argv[i+1])) + ">"
			skipNext = true
			continue
		}
		out[i] = a
	}
	return out
}

func flagIsSensitive(flag string) bool {
	for _, s := range sensitiveFlagSubstrings {
		if strings.Contains(flag, s) {
			return true
		}
	}
	return false
}

// PostInput is the data the launcher hands the collector after the child
// exits. The launcher is the only system component that has direct access
// to home-dir paths in stderr; Post rewrites them once at the boundary so
// the rest of the pipeline doesn't have to think about it.
type PostInput struct {
	ExitCode        int
	Signal          string
	Runtime         time.Duration
	StderrTail      string
	StderrTruncated int64
	HomeDir         string
}

// Post folds the run results into the snapshot. Rewrites $HOME → ~ in
// stderr so the file is paste-safe.
func Post(r Report, in PostInput) Report {
	r.ExitCode = in.ExitCode
	r.Signal = in.Signal
	r.Runtime = in.Runtime
	r.StderrTail = rewriteHome(in.StderrTail, in.HomeDir)
	r.StderrTruncated = int(in.StderrTruncated)
	return r
}

func rewriteHome(s, home string) string {
	if home == "" {
		return s
	}
	return strings.ReplaceAll(s, home, "~")
}
