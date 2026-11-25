package doctor

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// SchemaVersion is bumped when a JSON field is renamed or removed.
const SchemaVersion = 1

// Report is what aide doctor prints, as text or JSON.
type Report struct {
	SchemaVersion int        `json:"schema_version"`
	AideVersion   string     `json:"aide_version"`
	InsideSandbox bool       `json:"inside_sandbox"`
	Directory     *Directory `json:"directory"` // nil when the config did not load
	Counts        Counts     `json:"counts"`
	Findings      []Finding  `json:"findings"`
}

// Counts tallies findings by severity.
type Counts struct {
	Errors   int `json:"errors"`
	Warnings int `json:"warnings"`
	Tips     int `json:"tips"`
}

// Directory is what launch would use in the current directory.
type Directory struct {
	CWD           string          `json:"cwd"`
	Profile       string          `json:"profile"`
	MatchReason   string          `json:"match_reason"`
	Agent         string          `json:"agent"`
	SecretStore   string          `json:"secret_store,omitempty"`
	Capabilities  []DirCapability `json:"capabilities"`
	Guards        []string        `json:"guards"`
	Network       string          `json:"network"`
	ProjectConfig []ProjectFile   `json:"project_config"`
	// Detected are capabilities whose project markers are present.
	Detected []string `json:"detected_capabilities"`
}

// DirCapability is an active capability and the variants it uses.
type DirCapability struct {
	Name     string   `json:"name"`
	Variants []string `json:"variants"`
}

// ProjectFile is one .aide.yaml layer and its trust state.
type ProjectFile struct {
	Layer string `json:"layer"` // "local" or "inherited"
	Path  string `json:"path"`
	State string `json:"state"` // "trusted", "untrusted" or "denied"
}

var severityRank = map[Severity]int{SeverityError: 0, SeverityWarning: 1, SeverityTip: 2}

// NewReport counts and orders findings: this directory's profile first,
// then other profiles; within each, worst first.
func NewReport(version string, dir *Directory, findings []Finding) Report {
	r := Report{SchemaVersion: SchemaVersion, AideVersion: version, Directory: dir, Findings: append([]Finding{}, findings...)}
	for _, f := range r.Findings {
		switch f.Severity {
		case SeverityError:
			r.Counts.Errors++
		case SeverityWarning:
			r.Counts.Warnings++
		case SeverityTip:
			r.Counts.Tips++
		}
	}
	sort.SliceStable(r.Findings, func(i, j int) bool {
		a, b := r.Findings[i], r.Findings[j]
		if r.here(a) != r.here(b) {
			return r.here(a)
		}
		if severityRank[a.Severity] != severityRank[b.Severity] {
			return severityRank[a.Severity] < severityRank[b.Severity]
		}
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		return a.Subject < b.Subject
	})
	return r
}

// HasErrors reports whether aide doctor should exit non-zero, as
// validate does: errors only; warnings and tips never.
func (r Report) HasErrors() bool { return r.Counts.Errors > 0 }

// here reports whether f concerns the current directory: its profile
// or agent, or nothing more specific (such as a config that failed to
// load).
func (r Report) here(f Finding) bool {
	if r.Directory == nil || f.Profile == "" && f.ID != "config.agent-not-on-path" {
		return true
	}
	return f.Profile == r.Directory.Profile || f.ID == "config.agent-not-on-path" && f.Subject == r.Directory.Agent
}

var marks = map[Severity]string{SeverityError: "✗", SeverityWarning: "⚠", SeverityTip: "·"}

// RenderText writes the human report.
func (r Report) RenderText(w io.Writer) error {
	var b strings.Builder
	fmt.Fprintf(&b, "%s, %s, %s\n", plural(r.Counts.Errors, "error"), plural(r.Counts.Warnings, "warning"), plural(r.Counts.Tips, "tip"))
	if r.InsideSandbox {
		b.WriteString("Run inside an aide sandbox: fixes are not applied here; run aide doctor in a separate terminal.\n")
	}
	if d := r.Directory; d != nil {
		b.WriteString("\n")
		row := func(label, value string) {
			if value != "" {
				fmt.Fprintf(&b, "%-9s %s\n", label, value)
			}
		}
		row("Profile", d.Profile+paren(d.MatchReason))
		row("Agent", d.Agent)
		row("Secrets", d.SecretStore)
		var caps []string
		for _, c := range d.Capabilities {
			caps = append(caps, c.Name+paren(strings.Join(c.Variants, ", ")))
		}
		row("Caps", strings.Join(caps, ", "))
		row("Sandbox", plural(len(d.Guards), "guard")+", network "+d.Network)
		for _, p := range d.ProjectConfig {
			row("Project", fmt.Sprintf("%s (%s) %s", p.Path, p.Layer, p.State))
		}
		row("Detected", strings.Join(d.Detected, ", "))
		b.WriteString("  details: aide status, aide sandbox show\n")
	}
	section := ""
	for _, f := range r.Findings {
		name := "Other profiles"
		if r.here(f) {
			name = "This directory"
		}
		if name != section {
			section = name
			fmt.Fprintf(&b, "\n%s\n", name)
		}
		fmt.Fprintf(&b, "  %s %s\n", marks[f.Severity], f.Message)
		if len(f.Fix.Argv) > 0 {
			fmt.Fprintf(&b, "      fix: %s\n", strings.Join(f.Fix.Argv, " "))
		}
		for _, s := range f.Fix.Steps {
			fmt.Fprintf(&b, "      → %s\n", s)
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func paren(s string) string {
	if s == "" {
		return ""
	}
	return " (" + s + ")"
}
