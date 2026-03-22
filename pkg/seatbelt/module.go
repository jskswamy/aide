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

// NetworkMode controls the level of network access.
type NetworkMode int

const (
	// NetworkOpen allows all network traffic (inbound + outbound).
	NetworkOpen NetworkMode = iota
	// NetworkOutbound allows outbound connections only.
	NetworkOutbound
	// NetworkNone denies all network traffic (default-deny covers it).
	NetworkNone
)

// Context provides runtime information to modules.
type Context struct {
	HomeDir     string
	ProjectRoot string
	TempDir     string
	RuntimeDir  string
	Env         []string    // for env var overrides (AWS_CONFIG_FILE, KUBECONFIG, etc.)
	GOOS        string      // for OS-specific paths ("darwin", "linux")

	// Fields consumed by specific always-guards
	Network     NetworkMode // consumed by network guard
	AllowPorts  []int       // consumed by network guard
	DenyPorts   []int       // consumed by network guard
	ExtraDenied []string    // consumed by filesystem guard (user-configured denied: paths)
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
