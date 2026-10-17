package context

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/gobwas/glob"
	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/homepath"
	"github.com/jskswamy/aide/internal/sliceutil"
)

// ResolvedContext holds the result of context resolution.
type ResolvedContext struct {
	Name        string             // name of the matched context
	MatchReason string             // human-readable reason for the match
	Context     config.Context     // the resolved context (with project override merged if applicable)
	Preferences config.Preferences // resolved display/behavior preferences
	// Source is how the profile was selected: SourceFlag, SourceMatch,
	// SourceDefault or SourcePrompt.
	Source string
}

// Profile selection sources, reported as profile_source.
const (
	SourceFlag    = "flag"    // explicit --profile
	SourceMatch   = "match"   // a path/remote match rule
	SourceDefault = "default" // default_profile, or a minimal config
	SourcePrompt  = "prompt"  // picked at the empty-state prompt
)

// Specificity tiers. Within a tier, longer pattern string = higher specificity.
//
// An exact remote URL sits between path-glob and path-exact: it is a unique
// repo identity (stronger than any directory glob, which is a catch-all) but
// weaker than an exact directory binding (which pins a specific checkout).
const (
	specificityDefault     = 0
	specificityRemote      = 100
	specificityPathGlob    = 200
	specificityRemoteExact = 250
	specificityPathExact   = 300
)

// Resolve picks the best matching context from cfg for the given cwd.
// remotes supplies git remote URLs: each remote rule is scored against the
// remote its remote_name selects (DefaultRemoteName when unset). A nil
// remotes means the directory has no remotes.
//
// If cfg.IsMinimal(), it returns a normalized "default" context built from the
// flat config fields.
//
// For each context, each match rule is scored:
//   - exact path match:   300 + len(pattern)
//   - exact remote match: 250 + len(pattern)
//   - glob path match:    200 + len(pattern)
//   - glob remote match:  100 + len(pattern)
//
// The highest-scoring context wins; on equal scores the lexically first
// context name wins. If nothing matches, falls back to cfg.DefaultContext.
// If that is also unset, returns an error.
//
// If cfg.ProjectOverride is set, it is merged on top of the matched context:
// env merges additively (override wins on conflict), agent/secret/mcp_servers/sandbox
// replace if set.
func Resolve(cfg *config.Config, cwd string, remotes RemoteLookup) (*ResolvedContext, error) {
	if cfg.IsMinimal() {
		return finishResolved(cfg, minimalContext(cfg)), nil
	}

	rc := bestMatch(cfg, cwd, remotes)
	if rc == nil {
		rc = defaultContext(cfg)
	}
	if rc == nil {
		return nil, fmt.Errorf(
			"no profile matched for cwd=%s remote=%s and no default profile configured",
			cwd, ParseRemoteHost(lookupRemote(remotes, DefaultRemoteName)),
		)
	}
	return finishResolved(cfg, rc), nil
}

// ResolveSelected selects the profile by precedence: the explicit name
// (--profile) when set, otherwise Resolve's matcher and default.
func ResolveSelected(cfg *config.Config, cwd string, remotes RemoteLookup, name string) (*ResolvedContext, error) {
	if name != "" {
		return ResolveByName(cfg, name)
	}
	return Resolve(cfg, cwd, remotes)
}

// ResolveByName selects the named profile regardless of match rules,
// then applies preferences and the project override as Resolve does.
// A minimal config has only the profile "default".
func ResolveByName(cfg *config.Config, name string) (*ResolvedContext, error) {
	if cfg.IsMinimal() {
		if name != "default" {
			return nil, fmt.Errorf("profile %q not found: this config defines no profiles (only %q)", name, "default")
		}
		rc := minimalContext(cfg)
		rc.Source = SourceFlag
		rc.MatchReason = "--profile default"
		return finishResolved(cfg, rc), nil
	}
	ctx, ok := cfg.Contexts[name]
	if !ok {
		defined := strings.Join(slices.Sorted(maps.Keys(cfg.Contexts)), ", ")
		if defined == "" {
			defined = "none"
		}
		return nil, fmt.Errorf("profile %q not found (defined: %s)", name, defined)
	}
	return finishResolved(cfg, &ResolvedContext{
		Name:        name,
		MatchReason: "--profile " + name,
		Context:     ctx,
		Source:      SourceFlag,
	}), nil
}

// minimalContext builds the synthetic "default" context of a minimal
// (flat) config.
func minimalContext(cfg *config.Config) *ResolvedContext {
	// Materialise the legacy list-of-names from a polymorphic
	// MCPServerMap whose entries are zero-valued (the unmarshaller
	// uses that shape to represent `mcp_servers: [a, b]`).
	var mcpList []string
	for k, v := range cfg.MCPServers {
		if v.Command == "" && v.URL == "" {
			mcpList = append(mcpList, k)
		}
	}
	return &ResolvedContext{
		Name:        "default",
		MatchReason: "minimal config (default)",
		Source:      SourceDefault,
		Context: config.Context{
			Agent:      cfg.Agent,
			Env:        cfg.Env,
			Secret:     cfg.Secret,
			MCPServers: mcpList,
			Sandbox:    config.SandboxPolicyToRef(cfg.Sandbox),
			Yolo:       cfg.Yolo,
		},
	}
}

// bestMatch returns the highest-scoring context, or nil if no rule
// matches. Contexts are visited in name order and only a strictly higher
// score replaces the leader, so ties go to the lexically first name.
func bestMatch(cfg *config.Config, cwd string, remotes RemoteLookup) *ResolvedContext {
	var bestName string
	var bestRule *config.MatchRule
	var bestScore int

	for _, name := range slices.Sorted(maps.Keys(cfg.Contexts)) {
		ctx := cfg.Contexts[name]
		for i := range ctx.Match {
			rule := &ctx.Match[i]
			score := scoreRule(rule, cwd, remotes)
			if score > 0 && score > bestScore {
				bestName = name
				bestRule = rule
				bestScore = score
			}
		}
	}
	if bestName == "" {
		return nil
	}
	return &ResolvedContext{
		Name:        bestName,
		MatchReason: describeMatch(bestRule, bestScore),
		Context:     cfg.Contexts[bestName],
		Source:      SourceMatch,
	}
}

// defaultContext returns cfg.DefaultContext when it names an existing
// context, or nil.
func defaultContext(cfg *config.Config) *ResolvedContext {
	if cfg.DefaultContext == "" {
		return nil
	}
	ctx, ok := cfg.Contexts[cfg.DefaultContext]
	if !ok {
		return nil
	}
	return &ResolvedContext{
		Name:        cfg.DefaultContext,
		MatchReason: fmt.Sprintf("default_context (%s)", cfg.DefaultContext),
		Context:     ctx,
		Source:      SourceDefault,
	}
}

// finishResolved applies preferences and the project override to a
// selected context. Every way of selecting a context ends here.
func finishResolved(cfg *config.Config, rc *ResolvedContext) *ResolvedContext {
	rc.Preferences = config.ResolvePreferences(cfg.Preferences, nil)
	applyProjectOverride(rc, cfg.ProjectOverride, cfg.Sandboxes)
	return rc
}

// lookupRemote returns the URL of the named remote (DefaultRemoteName when
// name is empty), or "" when remotes is nil.
func lookupRemote(remotes RemoteLookup, name string) string {
	if remotes == nil {
		return ""
	}
	if name == "" {
		name = DefaultRemoteName
	}
	return remotes(name)
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
		inline.DeniedExtra = sliceutil.Dedup(append(inline.DeniedExtra, po.Sandbox.DeniedExtra...))
		inline.ReadableExtra = sliceutil.Dedup(append(inline.ReadableExtra, po.Sandbox.ReadableExtra...))
		inline.WritableExtra = sliceutil.Dedup(append(inline.WritableExtra, po.Sandbox.WritableExtra...))
		inline.GuardsExtra = sliceutil.Dedup(append(inline.GuardsExtra, po.Sandbox.GuardsExtra...))
		inline.Unguard = sliceutil.Dedup(append(inline.Unguard, po.Sandbox.Unguard...))

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
		merged := sliceutil.Dedup(append(rc.Context.Capabilities, po.Capabilities...))
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
func scoreRule(rule *config.MatchRule, cwd string, remotes RemoteLookup) int {
	if rule.Path != "" {
		return scorePathRule(rule.Path, cwd)
	}
	if rule.Remote != "" {
		return scoreRemoteRule(rule.Remote, lookupRemote(remotes, rule.RemoteName))
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
	expanded := homepath.Expand(pattern, "")

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

// scoreRemoteRule scores a remote match rule against a remote URL.
//
// Both pattern and remoteURL are canonicalized to "host/org/repo" form before
// comparison so that the three interchangeable git URL forms — ssh scheme
// (ssh://git@host/org/repo.git), scp-style (git@host:org/repo.git), and
// https (https://host/org/repo.git) — all match a single pattern.
//
// Glob patterns (containing *, ?, [, {) are left as-is since they are already
// expressed in canonical form and url.Parse would mangle them.
func scoreRemoteRule(pattern string, remoteURL string) int {
	if remoteURL == "" {
		return 0
	}

	canonRemote := canonicalizeRemote(remoteURL)
	canonPattern := canonicalizeRemotePattern(pattern)

	// Exact match: promoted to its own tier above path-glob, since a unique
	// repo URL is a stronger identity signal than any directory catch-all.
	if canonPattern == canonRemote {
		return specificityRemoteExact + len(pattern)
	}

	// Glob match — match canonical remote first; fall back to raw remote so
	// pathological patterns written in URL form (e.g. ssh://...*) still work.
	g, err := glob.Compile(canonPattern)
	if err != nil {
		return 0
	}
	if g.Match(canonRemote) || g.Match(remoteURL) {
		return specificityRemote + len(pattern)
	}
	return 0
}

// canonicalizeRemote normalizes a git remote URL to "host/org/repo" form via
// ParseRemoteHost. Returns the input unchanged if it is empty or cannot be
// parsed into a host/path.
func canonicalizeRemote(s string) string {
	if s == "" {
		return s
	}
	if c := ParseRemoteHost(s); c != "" {
		return c
	}
	return s
}

// canonicalizeRemotePattern canonicalizes a remote rule pattern. URL-shaped
// patterns (with a scheme, or scp-style user@host:path) are normalized via
// ParseRemoteHost; everything else (globs, already-canonical host/org/repo,
// bare host names) is returned unchanged.
func canonicalizeRemotePattern(s string) string {
	if s == "" {
		return s
	}
	if strings.ContainsAny(s, "*?[{") {
		return canonicalizeRemoteGlob(s)
	}
	if c := ParseRemoteHost(s); c != "" {
		return c
	}
	return s
}

// canonicalizeRemoteGlob lowercases the host segment of a glob written in
// "host/org/..." form and trims a trailing slash, matching what
// ParseRemoteHost does to the remote it is compared against. Character
// classes in the host are lowercased too, which is harmless because hosts
// are compared lowercased. Globs written in URL form are left alone; they
// match the raw remote instead.
func canonicalizeRemoteGlob(s string) string {
	if strings.Contains(s, "://") || strings.Contains(s, "@") {
		return s
	}
	s = strings.TrimRight(s, "/")
	host, rest, found := strings.Cut(s, "/")
	if !found {
		return strings.ToLower(s)
	}
	return strings.ToLower(host) + "/" + rest
}

// displayRemoteRule renders a remote rule for the banner, `aide which` and
// JSON match_reason. Rules saved by older releases may be raw URLs with
// embedded tokens: non-glob rules show their normalized form, and globs
// show with userinfo removed.
func displayRemoteRule(pattern string) string {
	if !strings.ContainsAny(pattern, "*?[{") {
		if n := ParseRemoteHost(pattern); n != "" {
			return n
		}
	}
	return stripRemoteUserinfo(pattern)
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
		if score >= specificityRemoteExact {
			return fmt.Sprintf("exact remote match: %s", displayRemoteRule(rule.Remote))
		}
		return fmt.Sprintf("remote match: %s", displayRemoteRule(rule.Remote))
	}
	return "unknown"
}
