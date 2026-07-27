// cmd/aide/match_rule.go
package main

import (
	"fmt"

	"github.com/jskswamy/aide/internal/config"
	aidectx "github.com/jskswamy/aide/internal/context"
)

// autoDetectMatchRule returns the match rule that best identifies the
// given folder. If the folder is inside a git repo with an "origin"
// remote, match by remote URL (durable across worktrees and fresh
// checkouts). Otherwise match by exact folder path.
//
// The second return value is a human-readable description suitable for
// inclusion in user-facing output, e.g. "by remote git@…/foo.git" or
// "by path /Users/x/work/foo".
func autoDetectMatchRule(cwd string) (config.MatchRule, string) {
	if remote := aidectx.DetectRemote(cwd, "origin"); remote != "" {
		return config.MatchRule{Remote: remote}, fmt.Sprintf("by remote %s", remote)
	}
	return config.MatchRule{Path: cwd}, fmt.Sprintf("by path %s", cwd)
}

// rulesCollide reports whether two match rules refer to the same repo or path.
// Remote URLs are normalized via ParseRemoteHost so http/ssh/git variants match.
func rulesCollide(a, b config.MatchRule) bool {
	if a.Remote != "" && b.Remote != "" {
		return aidectx.ParseRemoteHost(a.Remote) == aidectx.ParseRemoteHost(b.Remote)
	}
	return a.Path != "" && a.Path == b.Path
}

type existingBind struct {
	ctxName string
	rule    config.MatchRule
}

// findExistingBind returns the first context (other than skipCtx) whose match
// rules collide with newRule, or nil if no collision is found.
func findExistingBind(cfg *config.Config, skipCtx string, newRule config.MatchRule) *existingBind {
	for ctxName, ctx := range cfg.Contexts {
		if ctxName == skipCtx {
			continue
		}
		for _, r := range ctx.Match {
			if rulesCollide(r, newRule) {
				return &existingBind{ctxName, r}
			}
		}
	}
	return nil
}

// removeMatchRule returns rules with all entries that collide with target removed.
func removeMatchRule(rules []config.MatchRule, target config.MatchRule) []config.MatchRule {
	out := rules[:0:0]
	for _, r := range rules {
		if !rulesCollide(r, target) {
			out = append(out, r)
		}
	}
	return out
}
