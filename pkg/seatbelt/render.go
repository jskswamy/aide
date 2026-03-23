package seatbelt

import (
	"fmt"
	"strings"
)

// renderRules converts a slice of Rules to Seatbelt profile text.
func renderRules(rules []Rule) string {
	var b strings.Builder
	for _, r := range rules {
		if r.comment != "" {
			fmt.Fprintf(&b, ";; %s\n", r.comment)
		}
		if r.lines != "" {
			b.WriteString(r.lines)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// renderModule renders a module with a section header.
func renderModule(m Module, ctx *Context) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n;; === %s ===\n", m.Name())
	b.WriteString(renderRules(m.Rules(ctx)))
	return b.String()
}

// taggedRule pairs a rule with its source module name for rendering.
type taggedRule struct {
	module string
	rule   Rule
}

// renderTaggedRules renders a slice of taggedRules, grouping consecutive rules
// from the same module under a shared section header.
func renderTaggedRules(rules []taggedRule) string {
	var b strings.Builder
	currentModule := ""
	for _, tr := range rules {
		if tr.module != currentModule {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			fmt.Fprintf(&b, ";; === %s ===\n", tr.module)
			currentModule = tr.module
		}
		s := tr.rule.String()
		if s != "" {
			b.WriteString(s)
			if !strings.HasSuffix(s, "\n") {
				b.WriteByte('\n')
			}
		}
	}
	return b.String()
}
