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
	Rules(ctx *Context) GuardResult
}

// Guard is a Module with metadata for the guard system.
type Guard interface {
	Module
	// Type returns the guard type: "always", "default", or "opt-in".
	Type() string
	// Description returns a human-readable description shown in CLI output.
	Description() string
}

// GuardResult holds rules and diagnostics from a guard evaluation.
type GuardResult struct {
	Name      string     // guard name, set by the profile builder from Module.Name()
	Rules     []Rule
	Protected []string   // paths being denied
	Allowed   []string   // paths explicitly allowed (exceptions)
	Skipped   []string   // "~/.config/op not found" etc.
	Overrides []Override // env var overrides detected
}

// Override records when an env var changed a guard's default path.
type Override struct {
	EnvVar      string // e.g. "KUBECONFIG"
	Value       string // e.g. "/custom/kubeconfig"
	DefaultPath string // e.g. "~/.kube/config"
}

// Context provides runtime information to modules.
type Context struct {
	HomeDir     string
	ProjectRoot string
	TempDir     string
	RuntimeDir  string
	Env         []string // for env var overrides (AWS_CONFIG_FILE, KUBECONFIG, etc.)
	GOOS        string   // for OS-specific paths ("darwin", "linux")

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
// The renderer stable-sorts rules by intent: allows first, then denies.
// Seatbelt uses deny-wins-over-allow semantics — deny rules always
// take precedence regardless of position. The sort order is for
// readability only.
type RuleIntent int

const (
	Allow RuleIntent = 100 // broad infrastructure + directory allows
	Deny  RuleIntent = 200 // narrow specific-file/path denials
)

// Rule represents a Seatbelt rule or comment block.
type Rule struct {
	intent  RuleIntent
	comment string
	lines   string
}

// AllowOp creates an (allow <operation>) rule.
func AllowOp(operation string) Rule {
	return Rule{intent: Allow, lines: "(allow " + operation + ")"}
}

// DenyOp creates a (deny <operation>) rule.
// DenyOp has Allow intent because deny-ops in the Setup layer (e.g. deny default)
// are infrastructure rules, not credential guards.
func DenyOp(operation string) Rule {
	return Rule{intent: Allow, lines: "(deny " + operation + ")"}
}

// Comment creates a ;; comment line.
func Comment(text string) Rule {
	return Rule{intent: Allow, comment: text}
}

// Section creates a ;; --- section header --- comment.
func Section(name string) Rule {
	return Rule{intent: Allow, comment: "--- " + name + " ---"}
}

// Raw creates a rule from raw Seatbelt text (may be multi-line).
func Raw(text string) Rule {
	return Rule{intent: Allow, lines: text}
}

// AllowRule creates a rule with Allow intent.
func AllowRule(text string) Rule { return Rule{intent: Allow, lines: text} }

// DenyRule creates a rule with Deny intent.
func DenyRule(text string) Rule { return Rule{intent: Deny, lines: text} }

// SectionAllow creates a section header comment with Allow intent.
func SectionAllow(name string) Rule { return Rule{intent: Allow, comment: "--- " + name + " ---"} }

// SectionDeny creates a section header comment with Deny intent.
func SectionDeny(name string) Rule { return Rule{intent: Deny, comment: "--- " + name + " ---"} }

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
