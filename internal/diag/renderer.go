package diag

import (
	"fmt"
	"strings"
)

// Markdown renders the full report. Goes to the file; suitable for pasting
// into a GitHub issue.
func Markdown(r Report) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# aide diagnose report\n\n")
	fmt.Fprintf(&b, "> ⚠️  Review this file before sharing. It may contain paths, hostnames, or argv values that you consider sensitive. Secret values are redacted (only env-var names and lengths are recorded), but please skim every section before posting.\n\n")
	fmt.Fprintf(&b, "## TL;DR\n\nexit=%d runtime=%s — %s\n\n", r.ExitCode, r.Runtime, r.Classification())

	fmt.Fprintf(&b, "## Environment\n\n")
	fmt.Fprintf(&b, "- aide: %s (commit %s, built %s)\n", r.AideVersion, r.AideCommit, r.AideBuildDate)
	fmt.Fprintf(&b, "- os: %s/%s\n", r.OS, r.Arch)
	fmt.Fprintf(&b, "- shell: %s\n- locale: %s\n\n", r.Shell, r.Locale)

	fmt.Fprintf(&b, "## Invocation\n\n")
	fmt.Fprintf(&b, "- cwd: `%s`\n- config: `%s`\n- agent binary: `%s`\n- argv: `%s`\n\n",
		r.CWD, r.ResolvedConfig, r.AgentBinary, strings.Join(r.Argv, " "))

	fmt.Fprintf(&b, "## Secrets wiring\n\n")
	for _, k := range r.EnvKeys {
		fmt.Fprintf(&b, "- env `%s` (len=%d)\n", k.Name, k.Length)
	}
	for _, p := range r.SecretSourcePaths {
		fmt.Fprintf(&b, "- secret source: `%s`\n", p)
	}
	if r.AgeKeySource != "" {
		fmt.Fprintf(&b, "- age key source: %s\n", r.AgeKeySource)
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "## Sandbox\n\n")
	fmt.Fprintf(&b, "- variants: %s\n- guards: %s\n\n",
		strings.Join(r.Sandbox.Variants, ", "), strings.Join(r.Sandbox.GuardNames, ", "))
	if r.Sandbox.RenderedSB != "" {
		fmt.Fprintf(&b, "<details><summary>rendered .sb</summary>\n\n```scheme\n%s```\n\n</details>\n\n", r.Sandbox.RenderedSB)
	}

	if r.StderrTruncated > 0 {
		fmt.Fprintf(&b, "## Child output (last %d bytes, %d dropped)\n\n```\n%s```\n\n", len(r.StderrTail), r.StderrTruncated, r.StderrTail)
	} else {
		fmt.Fprintf(&b, "## Child output (last %d bytes)\n\n```\n%s```\n\n", len(r.StderrTail), r.StderrTail)
	}

	if len(r.Denials) > 0 {
		fmt.Fprintf(&b, "## Sandbox denials\n\n| op | path | pid |\n|---|---|---|\n")
		for _, d := range r.Denials {
			fmt.Fprintf(&b, "| %s | %s | %d |\n", d.Operation, d.Path, d.PID)
		}
		b.WriteString("\n")
	} else if r.TraceUnavailable != "" {
		fmt.Fprintf(&b, "## Sandbox denials\n\n_unavailable: %s_\n\n", r.TraceUnavailable)
	}

	if len(r.Argv) > 0 {
		fmt.Fprintf(&b, "## Reproduction\n\n```\ncd %s && aide --diagnose -- %s\n```\n",
			r.CWD, strings.Join(r.Argv, " "))
	} else {
		fmt.Fprintf(&b, "## Reproduction\n\n```\ncd %s && aide --diagnose\n```\n", r.CWD)
	}

	return b.String()
}

// Summary renders the compact terminal post-mortem. Excludes rendered .sb.
func Summary(r Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "── aide diagnose ──\n")
	fmt.Fprintf(&b, "exit=%d runtime=%s — %s\n", r.ExitCode, r.Runtime, r.Classification())
	if r.StderrTail != "" {
		fmt.Fprintf(&b, "child stderr (last lines):\n%s", r.StderrTail)
	}
	fmt.Fprintf(&b, "sandbox: %s\n", strings.Join(r.Sandbox.Variants, ", "))
	return b.String()
}
