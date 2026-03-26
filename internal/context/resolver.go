package context

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gobwas/glob"
	"github.com/jskswamy/aide/internal/config"
)

// ResolvedContext holds the result of context resolution.
type ResolvedContext struct {
	Name        string             // name of the matched context
	MatchReason string             // human-readable reason for the match
	Context     config.Context     // the resolved context (with project override merged if applicable)
	Preferences config.Preferences // resolved display/behavior preferences
}

// Specificity tiers. Within a tier, longer pattern string = higher specificity.
const (
	specificityDefault   = 0
	specificityRemote    = 100
	specificityPathGlob  = 200
	specificityPathExact = 300
)

// Resolve picks the best matching context from cfg for the given cwd and remoteURL.
//
// If cfg.IsMinimal(), it returns a normalized "default" context built from the
// flat config fields.
//
// For each context, each match rule is scored:
//   - exact path match: 300 + len(pattern)
//   - glob path match:  200 + len(pattern)
//   - remote match:     100 + len(pattern)
//
// The highest-scoring context wins. If nothing matches, falls back to
// cfg.DefaultContext. If that is also unset, returns an error.
//
// If cfg.ProjectOverride is set, it is merged on top of the matched context:
// env merges additively (override wins on conflict), agent/secret/mcp_servers/sandbox
// replace if set.
func Resolve(cfg *config.Config, cwd string, remoteURL string) (*ResolvedContext, error) {
	// Handle minimal config: build a synthetic default context
	if cfg.IsMinimal() {
		ctx := config.Context{
			Agent:       cfg.Agent,
			Env:         cfg.Env,
			Secret: cfg.Secret,
			MCPServers:  cfg.MCPServers,
			Sandbox:     config.SandboxPolicyToRef(cfg.Sandbox),
			Yolo:        cfg.Yolo,
		}
		rc := &ResolvedContext{
			Name:        "default",
			MatchReason: "minimal config (default)",
			Context:     ctx,
		}
		rc.Preferences = config.ResolvePreferences(cfg.Preferences, nil)
		applyProjectOverride(rc, cfg.ProjectOverride, cfg.Sandboxes)
		return rc, nil
	}

	// Score all contexts to find the best match
	var bestName string
	var bestRule *config.MatchRule
	var bestScore int

	for name, ctx := range cfg.Contexts {
		for i := range ctx.Match {
			rule := &ctx.Match[i]
			score := scoreRule(rule, cwd, remoteURL)
			if score > 0 && score > bestScore {
				bestName = name
				bestRule = rule
				bestScore = score
			}
		}
	}

	var rc *ResolvedContext

	if bestName != "" {
		ctx := cfg.Contexts[bestName]
		rc = &ResolvedContext{
			Name:        bestName,
			MatchReason: describeMatch(bestRule, bestScore),
			Context:     ctx,
		}
	} else if cfg.DefaultContext != "" {
		if ctx, ok := cfg.Contexts[cfg.DefaultContext]; ok {
			rc = &ResolvedContext{
				Name:        cfg.DefaultContext,
				MatchReason: fmt.Sprintf("default_context (%s)", cfg.DefaultContext),
				Context:     ctx,
			}
		}
	}

	if rc == nil {
		return nil, fmt.Errorf(
			"no context matched for cwd=%s remote=%s and no default_context configured",
			cwd, remoteURL,
		)
	}

	rc.Preferences = config.ResolvePreferences(cfg.Preferences, nil)
	applyProjectOverride(rc, cfg.ProjectOverride, cfg.Sandboxes)
	return rc, nil
}

// applyProjectOverride merges a ProjectOverride on top of the resolved context.
// env merges additively (override wins on conflict); other fields replace if set.
// sandboxes is the named profile map from Config.Sandboxes, used to expand profile references.
func applyProjectOverride(rc *ResolvedContext, po *config.ProjectOverride, sandboxes map[string]config.SandboxPolicy) {
	if po == nil {
		return
	}
	if po.Agent != "" {
		rc.Context.Agent = po.Agent
	}
	if po.Secret != "" {
		rc.Context.Secret = po.Secret
	}
	if len(po.MCPServers) > 0 {
		rc.Context.MCPServers = po.MCPServers
	}
	if po.Sandbox != nil {
		// Ensure we have an inline policy to merge into.
		// If context uses a profile reference, expand it first.
		if rc.Context.Sandbox == nil {
			rc.Context.Sandbox = &config.SandboxRef{Inline: &config.SandboxPolicy{}}
		}
		if rc.Context.Sandbox.ProfileName != "" && sandboxes != nil {
			if profile, ok := sandboxes[rc.Context.Sandbox.ProfileName]; ok {
				profileCopy := profile
				rc.Context.Sandbox = &config.SandboxRef{Inline: &profileCopy}
			}
		}
		if rc.Context.Sandbox.Inline == nil {
			rc.Context.Sandbox.Inline = &config.SandboxPolicy{}
		}
		inline := rc.Context.Sandbox.Inline

		// Additive fields (append + dedup)
		inline.DeniedExtra = dedupStrings(append(inline.DeniedExtra, po.Sandbox.DeniedExtra...))
		inline.ReadableExtra = dedupStrings(append(inline.ReadableExtra, po.Sandbox.ReadableExtra...))
		inline.WritableExtra = dedupStrings(append(inline.WritableExtra, po.Sandbox.WritableExtra...))
		inline.GuardsExtra = dedupStrings(append(inline.GuardsExtra, po.Sandbox.GuardsExtra...))
		inline.Unguard = dedupStrings(append(inline.Unguard, po.Sandbox.Unguard...))

		// Replace-if-set fields
		if len(po.Sandbox.Writable) > 0 {
			inline.Writable = po.Sandbox.Writable
		}
		if len(po.Sandbox.Readable) > 0 {
			inline.Readable = po.Sandbox.Readable
		}
		if len(po.Sandbox.Denied) > 0 {
			inline.Denied = po.Sandbox.Denied
		}
		if len(po.Sandbox.Guards) > 0 {
			inline.Guards = po.Sandbox.Guards
		}
		if po.Sandbox.Network != nil {
			inline.Network = po.Sandbox.Network
		}
		if po.Sandbox.AllowSubprocess != nil {
			inline.AllowSubprocess = po.Sandbox.AllowSubprocess
		}
		if po.Sandbox.CleanEnv != nil {
			inline.CleanEnv = po.Sandbox.CleanEnv
		}
	}
	if po.Yolo != nil {
		rc.Context.Yolo = po.Yolo
	}
	// Capabilities: additive merge, then subtract disabled
	if len(po.Capabilities) > 0 || len(po.DisabledCapabilities) > 0 {
		merged := dedupStrings(append(rc.Context.Capabilities, po.Capabilities...))
		rc.Context.Capabilities = subtractStrings(merged, po.DisabledCapabilities)
	}
	// Env: merge additively, override wins on conflict
	if len(po.Env) > 0 {
		merged := make(map[string]string, len(rc.Context.Env)+len(po.Env))
		for k, v := range rc.Context.Env {
			merged[k] = v
		}
		for k, v := range po.Env {
			merged[k] = v
		}
		rc.Context.Env = merged
	}
	if po.Preferences != nil {
		rc.Preferences = config.ResolvePreferences(&rc.Preferences, po.Preferences)
	}
	rc.MatchReason = fmt.Sprintf("project override on top of %s", rc.MatchReason)
}

// dedupStrings returns a new slice with duplicate strings removed, preserving order.
func dedupStrings(s []string) []string {
	seen := make(map[string]bool, len(s))
	var result []string
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	return result
}

// subtractStrings returns elements in a that are not in b.
func subtractStrings(a, b []string) []string {
	remove := make(map[string]bool, len(b))
	for _, v := range b {
		remove[v] = true
	}
	var result []string
	for _, v := range a {
		if !remove[v] {
			result = append(result, v)
		}
	}
	return result
}

// scoreRule returns a specificity score for a single match rule, or 0 if it
// does not match.
func scoreRule(rule *config.MatchRule, cwd string, remoteURL string) int {
	if rule.Path != "" {
		return scorePathRule(rule.Path, cwd)
	}
	if rule.Remote != "" {
		return scoreRemoteRule(rule.Remote, remoteURL)
	}
	return 0
}

// scorePathRule scores a path match rule against cwd.
// Expands ~ to home directory. Exact match gets specificityPathExact + len,
// glob match gets specificityPathGlob + len.
//
// Matching strategy (checked in order):
//  1. Direct: cwd itself matches the pattern (exact or glob)
//  2. Parent walk: walk up from cwd; if any parent matches, score it
//  3. Base match: if cwd equals the non-glob prefix of the pattern,
//     the user is at the root of the pattern's scope (e.g., cwd is
//     /work and pattern is /work/*)
func scorePathRule(pattern string, cwd string) int {
	expanded := expandTilde(pattern)

	// Try exact match first (works even if pattern has no glob chars)
	absPattern, _ := filepath.Abs(expanded)

	g, err := glob.Compile(expanded, filepath.Separator)
	if err != nil {
		if absPattern == cwd {
			return specificityPathExact + len(pattern)
		}
		return 0
	}

	// Walk cwd and its parents, return the score for the first match.
	dir := cwd
	for {
		if absPattern == dir {
			return specificityPathExact + len(pattern)
		}
		if g.Match(dir) {
			return specificityPathGlob + len(pattern)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	// Base match: if cwd is the literal prefix of the glob pattern,
	// the user is at the root directory that the pattern covers.
	// E.g., pattern=/work/* and cwd=/work → match.
	base := globBase(expanded)
	if base != "" {
		absBase, err := filepath.Abs(base)
		if err == nil && absBase == cwd {
			return specificityPathGlob + len(pattern)
		}
	}

	return 0
}

// globBase returns the longest non-glob prefix directory of a pattern.
// For "/home/user/work/*" it returns "/home/user/work".
// For "/home/user/work/**" it returns "/home/user/work".
// For a pattern with no glob characters it returns "".
func globBase(pattern string) string {
	if !strings.ContainsAny(pattern, "*?[{") {
		return ""
	}
	dir := pattern
	for strings.ContainsAny(filepath.Base(dir), "*?[{") {
		dir = filepath.Dir(dir)
	}
	return dir
}

// scoreRemoteRule scores a remote match rule against a normalized remote URL.
func scoreRemoteRule(pattern string, remoteURL string) int {
	if remoteURL == "" {
		return 0
	}

	// Exact match gets a bonus
	if pattern == remoteURL {
		return specificityRemote + len(pattern) + 50
	}

	// Glob match
	g, err := glob.Compile(pattern)
	if err != nil {
		return 0
	}
	if g.Match(remoteURL) {
		return specificityRemote + len(pattern)
	}
	return 0
}

// expandTilde replaces a leading ~ with the user's home directory.
func expandTilde(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[1:])
}

// describeMatch produces a human-readable description of why a rule matched.
func describeMatch(rule *config.MatchRule, score int) string {
	if rule == nil {
		return "default"
	}
	if rule.Path != "" {
		if score >= specificityPathExact {
			return fmt.Sprintf("exact path match: %s", rule.Path)
		}
		return fmt.Sprintf("path glob match: %s", rule.Path)
	}
	if rule.Remote != "" {
		return fmt.Sprintf("remote match: %s", rule.Remote)
	}
	return "unknown"
}
