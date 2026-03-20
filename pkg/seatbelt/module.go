package seatbelt

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Module contributes Seatbelt rules to a profile.
type Module interface {
	// Name returns a human-readable name for section comments.
	Name() string
	// Rules returns the Seatbelt rules this module contributes.
	Rules(ctx *Context) []Rule
}

// Context provides runtime information to modules.
type Context struct {
	HomeDir     string
	ProjectRoot string
	TempDir     string
	RuntimeDir  string
}

// HomePath returns homeDir joined with a relative path.
func (c *Context) HomePath(rel string) string {
	return filepath.Join(c.HomeDir, rel)
}

// Rule represents a Seatbelt rule or comment block.
type Rule struct {
	comment string
	lines   string
}

// Allow creates an (allow <operation>) rule.
func Allow(operation string) Rule {
	return Rule{lines: "(allow " + operation + ")"}
}

// Deny creates a (deny <operation>) rule.
func Deny(operation string) Rule {
	return Rule{lines: "(deny " + operation + ")"}
}

// Comment creates a ;; comment line.
func Comment(text string) Rule {
	return Rule{comment: text}
}

// Section creates a ;; --- section header --- comment.
func Section(name string) Rule {
	return Rule{comment: "--- " + name + " ---"}
}

// Raw creates a rule from raw Seatbelt text (may be multi-line).
func Raw(text string) Rule {
	return Rule{lines: text}
}

// String returns the rendered text of a single rule.
func (r Rule) String() string {
	var b strings.Builder
	if r.comment != "" {
		fmt.Fprintf(&b, ";; %s\n", r.comment)
	}
	if r.lines != "" {
		b.WriteString(r.lines)
	}
	return b.String()
}
