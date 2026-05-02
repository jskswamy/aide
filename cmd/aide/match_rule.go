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
