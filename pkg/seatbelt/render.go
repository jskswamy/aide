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
