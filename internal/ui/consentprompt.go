// Package ui's consent prompt renderer and TTY prompter.
//
// RenderPrompt builds the user-facing text for a variant consent
// request using a single template across first-time and
// evidence-changed states. TTYPrompter reads the user's answer from an
// io.Reader so tests can inject canned input without touching a real
// terminal. It implements capability.Prompter.

package ui

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/jskswamy/aide/internal/capability"
)

// RenderPrompt returns the multi-line prompt text for a variant
// consent request. It never reads from stdin; callers pair this with
// a separate input function (see TTYPrompter below).
func RenderPrompt(in capability.PromptInput) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[aide] %s — detection needs your confirmation\n\n", in.Capability)
	if len(in.PreviousVariants) > 0 {
		fmt.Fprintf(&b, "  Previously: %s\n", strings.Join(in.PreviousVariants, " + "))
	}
	names := make([]string, len(in.DetectedVariants))
	markers := make([]string, 0)
	paths := make([]string, 0)
	envs := make([]string, 0)
	for i, v := range in.DetectedVariants {
		names[i] = v.Name
		paths = append(paths, v.Readable...)
		paths = append(paths, v.Writable...)
		envs = append(envs, v.EnvAllow...)
		for _, m := range v.Markers {
			markers = append(markers, m.MatchSummary())
		}
	}
	fmt.Fprintf(&b, "  Detected:   %s\n", strings.Join(names, " + "))
	if len(markers) > 0 {
		fmt.Fprintf(&b, "              (markers: %s)\n", strings.Join(markers, ", "))
	}
	b.WriteString("\n")
	if len(paths) > 0 {
		fmt.Fprintf(&b, "  Grants:  %s\n", strings.Join(paths, ", "))
	}
	if len(envs) > 0 {
		fmt.Fprintf(&b, "  Env:     %s\n", strings.Join(envs, ", "))
	}
	b.WriteString("\n")
	b.WriteString("  [Y]es, grant all    [N]o, use default    [D]etails    [S]kip this launch    [C]ustomize\n")
	return b.String()
}

// NewTTYPrompter returns a capability.Prompter that reads the user's
// answer from in and writes prompts to out.
func NewTTYPrompter(in io.Reader, out io.Writer) capability.Prompter {
	return &ttyPrompter{in: bufio.NewReader(in), out: out}
}

type ttyPrompter struct {
	in  *bufio.Reader
	out io.Writer
}

func (p *ttyPrompter) PromptVariantConsent(in capability.PromptInput) capability.PromptResult {
	_, _ = fmt.Fprint(p.out, RenderPrompt(in))
	for {
		_, _ = fmt.Fprint(p.out, "> ")
		line, err := p.in.ReadString('\n')
		if err != nil && strings.TrimSpace(line) == "" {
			return capability.PromptResult{Decision: capability.PromptNo}
		}
		switch strings.ToUpper(strings.TrimSpace(line)) {
		case "Y", "YES":
			names := make([]string, len(in.DetectedVariants))
			for i, v := range in.DetectedVariants {
				names[i] = v.Name
			}
			return capability.PromptResult{Decision: capability.PromptYes, Variants: names}
		case "N", "NO":
			return capability.PromptResult{Decision: capability.PromptNo}
		case "S", "SKIP":
			return capability.PromptResult{Decision: capability.PromptSkip}
		case "D", "DETAILS":
			p.renderDetails(in)
			continue
		case "C", "CUSTOMIZE":
			return p.customizeLoop(in)
		default:
			_, _ = fmt.Fprintln(p.out, "please answer Y, N, D, S, or C")
		}
	}
}

func (p *ttyPrompter) renderDetails(in capability.PromptInput) {
	for _, v := range in.DetectedVariants {
		_, _ = fmt.Fprintf(p.out, "\n  %s (%s)\n", v.Name, v.Description)
		for _, m := range v.Markers {
			_, _ = fmt.Fprintf(p.out, "    marker: %s\n", m.MatchSummary())
		}
		for _, r := range v.Readable {
			_, _ = fmt.Fprintf(p.out, "    readable: %s\n", r)
		}
		for _, w := range v.Writable {
			_, _ = fmt.Fprintf(p.out, "    writable: %s\n", w)
		}
		for _, e := range v.EnvAllow {
			_, _ = fmt.Fprintf(p.out, "    env: %s\n", e)
		}
	}
	_, _ = fmt.Fprintln(p.out)
}

func (p *ttyPrompter) customizeLoop(in capability.PromptInput) capability.PromptResult {
	chosen := make([]string, 0, len(in.DetectedVariants))
	for _, v := range in.DetectedVariants {
		_, _ = fmt.Fprintf(p.out, "  grant %s? [y/N] ", v.Name)
		line, _ := p.in.ReadString('\n')
		trimmed := strings.TrimSpace(line)
		if strings.EqualFold(trimmed, "y") || strings.EqualFold(trimmed, "yes") {
			chosen = append(chosen, v.Name)
		}
	}
	if len(chosen) == 0 {
		return capability.PromptResult{Decision: capability.PromptNo}
	}
	return capability.PromptResult{Decision: capability.PromptYes, Variants: chosen}
}
