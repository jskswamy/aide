package seatbelt

import (
	"fmt"
	"strings"
)

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
