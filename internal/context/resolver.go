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
	Name        string         // name of the matched context
	MatchReason string         // human-readable reason for the match
	Context     config.Context // the resolved context (with project override merged if applicable)
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
// env merges additively (override wins on conflict), agent/secrets_file/mcp_servers/sandbox
// replace if set.
func Resolve(cfg *config.Config, cwd string, remoteURL string) (*ResolvedContext, error) {
	// Handle minimal config: build a synthetic default context
	if cfg.IsMinimal() {
		ctx := config.Context{
			Agent:       cfg.Agent,
			Env:         cfg.Env,
			SecretsFile: cfg.SecretsFile,
			MCPServers:  cfg.MCPServers,
			Sandbox:     cfg.Sandbox,
		}
		rc := &ResolvedContext{
			Name:        "default",
			MatchReason: "minimal config (default)",
			Context:     ctx,
		}
		applyProjectOverride(rc, cfg.ProjectOverride)
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

	applyProjectOverride(rc, cfg.ProjectOverride)
	return rc, nil
}

// applyProjectOverride merges a ProjectOverride on top of the resolved context.
// env merges additively (override wins on conflict); other fields replace if set.
func applyProjectOverride(rc *ResolvedContext, po *config.ProjectOverride) {
	if po == nil {
		return
	}
	if po.Agent != "" {
		rc.Context.Agent = po.Agent
	}
	if po.SecretsFile != "" {
		rc.Context.SecretsFile = po.SecretsFile
	}
	if len(po.MCPServers) > 0 {
		rc.Context.MCPServers = po.MCPServers
	}
	if po.Sandbox != nil {
		rc.Context.Sandbox = po.Sandbox
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
	rc.MatchReason = fmt.Sprintf("project override on top of %s", rc.MatchReason)
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
func scorePathRule(pattern string, cwd string) int {
	expanded := expandTilde(pattern)

	// Try exact match
	absPattern, err := filepath.Abs(expanded)
	if err == nil && absPattern == cwd {
		return specificityPathExact + len(pattern)
	}

	// Try glob match
	g, err := glob.Compile(expanded, filepath.Separator)
	if err != nil {
		return 0
	}
	if g.Match(cwd) {
		return specificityPathGlob + len(pattern)
	}
	return 0
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
