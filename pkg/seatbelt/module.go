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

// Guard is a Module with metadata for the guard system.
type Guard interface {
	Module
	// Type returns the guard type: "always", "default", or "opt-in".
	Type() string
	// Description returns a human-readable description shown in CLI output.
	Description() string
}

// Context provides runtime information to modules.
type Context struct {
	HomeDir     string
	ProjectRoot string
	TempDir     string
	RuntimeDir  string
	Env         []string    // for env var overrides (AWS_CONFIG_FILE, KUBECONFIG, etc.)
	GOOS        string      // for OS-specific paths ("darwin", "linux")

	// Fields consumed by specific always-guards
	Network     string   // consumed by network guard: "outbound", "none", "unrestricted", or ""
	AllowPorts  []int    // consumed by network guard
	DenyPorts   []int    // consumed by network guard
	ExtraDenied []string // consumed by filesystem guard (user-configured denied: paths)
}

// HomePath returns homeDir joined with a relative path.
func (c *Context) HomePath(rel string) string {
	return filepath.Join(c.HomeDir, rel)
}

// EnvLookup searches ctx.Env for a KEY=VALUE entry and returns the value.
// Returns ("", false) if not found. Guards use this instead of os.Getenv().
func (c *Context) EnvLookup(key string) (string, bool) {
	prefix := key + "="
	for _, e := range c.Env {
		if strings.HasPrefix(e, prefix) {
			return e[len(prefix):], true
		}
	}
	return "", false
}

// Validate checks that required Context fields are set.
// Returns a ValidationResult with errors for missing required fields.
func (c *Context) Validate() *ValidationResult {
	r := &ValidationResult{}
	if c.HomeDir == "" {
		r.AddError("context: HomeDir is required for guard path resolution")
	}
	if c.GOOS == "" {
		r.AddError("context: GOOS is required for OS-aware guards")
	}
	return r
}

// RuleIntent determines a rule's position in the rendered profile.
// The renderer stable-sorts rules by intent: lower values appear first.
// Seatbelt uses last-rule-wins, so higher intent values take precedence.
type RuleIntent int

// RuleIntent values determine rendering order. Seatbelt uses last-rule-wins,
// so higher values take precedence.
const (
	Setup    RuleIntent = 100 // infrastructure allows + refinement denies
	Restrict RuleIntent = 200 // block sensitive paths
	Grant    RuleIntent = 300 // re-allow within restricted paths
)

// Rule represents a Seatbelt rule or comment block.
type Rule struct {
	intent  RuleIntent
	comment string
	lines   string
}

// Allow creates an (allow <operation>) rule.
func Allow(operation string) Rule {
	return Rule{intent: Setup, lines: "(allow " + operation + ")"}
}

// Deny creates a (deny <operation>) rule.
func Deny(operation string) Rule {
	return Rule{intent: Setup, lines: "(deny " + operation + ")"}
}

// Comment creates a ;; comment line.
func Comment(text string) Rule {
	return Rule{intent: Setup, comment: text}
}

// Section creates a ;; --- section header --- comment.
func Section(name string) Rule {
	return Rule{intent: Setup, comment: "--- " + name + " ---"}
}

// Raw creates a rule from raw Seatbelt text (may be multi-line).
func Raw(text string) Rule {
	return Rule{intent: Setup, lines: text}
}

// SetupRule creates a rule with Setup intent (infrastructure allows + refinement denies).
func SetupRule(text string) Rule { return Rule{intent: Setup, lines: text} }

// RestrictRule creates a rule with Restrict intent (block sensitive paths).
func RestrictRule(text string) Rule { return Rule{intent: Restrict, lines: text} }

// GrantRule creates a rule with Grant intent (re-allow within restricted paths).
func GrantRule(text string) Rule { return Rule{intent: Grant, lines: text} }

// SectionSetup creates a section header comment with Setup intent.
func SectionSetup(name string) Rule { return Rule{intent: Setup, comment: "--- " + name + " ---"} }

// SectionRestrict creates a section header comment with Restrict intent.
func SectionRestrict(name string) Rule {
	return Rule{intent: Restrict, comment: "--- " + name + " ---"}
}

// SectionGrant creates a section header comment with Grant intent.
func SectionGrant(name string) Rule { return Rule{intent: Grant, comment: "--- " + name + " ---"} }

// Intent returns the rule's intent, which determines sort order in the rendered profile.
func (r Rule) Intent() RuleIntent {
	return r.intent
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
