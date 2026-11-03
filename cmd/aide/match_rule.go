// cmd/aide/match_rule.go
package main

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jskswamy/aide/internal/config"
	aidectx "github.com/jskswamy/aide/internal/context"
)

type bindScopeOption struct {
	Rule      config.MatchRule
	Label     string
	Desc      string
	Shorthand string
}

// buildBindScopeOptions returns the ordered candidate match scopes for cwd and its
// normalized origin remote (host[:port]/path, or "" if not a git repo with a host remote).
// Order: [1] exact repo ("repo"), [2..N] parent prefixes from deepest team to org ("org"),
// [last] exact folder path ("path"). Never generates bare "<host>/*".
func buildBindScopeOptions(cwd, remote string) []bindScopeOption {
	pathOpt := bindScopeOption{
		Rule:      config.MatchRule{Path: cwd},
		Label:     fmt.Sprintf("This folder path only (%s)", cwd),
		Desc:      fmt.Sprintf("by path %s", cwd),
		Shorthand: "path",
	}
	if remote == "" {
		return []bindScopeOption{pathOpt}
	}
	opts := []bindScopeOption{
		{
			Rule:      config.MatchRule{Remote: remote},
			Label:     fmt.Sprintf("This repository only (%s)", remote),
			Desc:      fmt.Sprintf("by remote %s", remote),
			Shorthand: "repo",
		},
	}
	segments := strings.Split(remote, "/")
	for i := len(segments) - 1; i >= 2; i-- {
		prefix := strings.Join(segments[:i], "/") + "/*"
		shorthand := strings.Join(segments[1:i], "/")
		if i == 2 {
			shorthand = "org"
		}
		opts = append(opts, bindScopeOption{
			Rule:      config.MatchRule{Remote: prefix},
			Label:     fmt.Sprintf("All repositories in %s", prefix),
			Desc:      fmt.Sprintf("by remote %s", prefix),
			Shorthand: shorthand,
		})
	}
	opts = append(opts, pathOpt)
	return opts
}

// resolveScopeFlag matches a --scope flag value against the available bindScopeOptions for cwd.
func resolveScopeFlag(flagVal, cwd, remote string, opts []bindScopeOption) (config.MatchRule, string, error) {
	val := strings.TrimSpace(flagVal)
	canonVal := aidectx.CanonicalizeRemotePattern(val)
	canonValGlob := strings.TrimSuffix(canonVal, "/") + "/*"

	for _, opt := range opts {
		if val == opt.Shorthand {
			return opt.Rule, opt.Desc, nil
		}
		if opt.Rule.Remote != "" {
			if canonVal == opt.Rule.Remote || canonValGlob == opt.Rule.Remote {
				return opt.Rule, opt.Desc, nil
			}
			// Also match "<org>" when shorthand is "org" (e.g., "--scope acme" for github.com/acme/*)
			segments := strings.Split(strings.TrimSuffix(opt.Rule.Remote, "/*"), "/")
			if len(segments) >= 2 && val == strings.Join(segments[1:], "/") {
				return opt.Rule, opt.Desc, nil
			}
		}
	}

	var valid []string
	for _, opt := range opts {
		if opt.Rule.Remote != "" && opt.Shorthand != "repo" {
			valid = append(valid, fmt.Sprintf("%s (%s)", opt.Shorthand, opt.Rule.Remote))
		} else {
			valid = append(valid, opt.Shorthand)
		}
	}
	if (val == "repo" || val == "org") && remote == "" {
		return config.MatchRule{}, "", fmt.Errorf("--scope %s requires a git repository with an origin remote (valid scopes for %s: %s)", val, cwd, strings.Join(valid, ", "))
	}
	return config.MatchRule{}, "", fmt.Errorf("invalid --scope %q for %s (valid scopes: %s)", flagVal, cwd, strings.Join(valid, ", "))
}

// autoDetectMatchRule returns the match rule that best identifies the
// given folder. If the folder is inside a git repo whose default remote
// has a host, match by the normalized remote (durable across worktrees and
// fresh checkouts, and free of any credentials in the URL). Otherwise
// match by exact folder path.
//
// The second return value is a human-readable description suitable for
// inclusion in user-facing output, e.g. "by remote github.com/org/foo" or
// "by path /Users/x/work/foo".
func autoDetectMatchRule(cwd string) (config.MatchRule, string) {
	if remote := aidectx.CurrentRemote(cwd); remote != "" {
		return config.MatchRule{Remote: remote}, fmt.Sprintf("by remote %s", remote)
	}
	return config.MatchRule{Path: cwd}, fmt.Sprintf("by path %s", cwd)
}

func effectiveRemoteName(r config.MatchRule) string {
	if r.RemoteName == "" {
		return aidectx.DefaultRemoteName
	}
	return r.RemoteName
}

// rulesCollide reports whether two match rules refer to the same repo or path.
// Remote URLs are normalized via CanonicalizeRemotePattern so http/ssh/git variants
// match; remotes without a host (local paths) are compared verbatim.
func rulesCollide(a, b config.MatchRule) bool {
	if a.Path != "" && b.Path != "" {
		return a.Path == b.Path
	}
	if a.Remote != "" && b.Remote != "" {
		return effectiveRemoteName(a) == effectiveRemoteName(b) &&
			remoteIdentity(a.Remote) == remoteIdentity(b.Remote)
	}
	return false
}

// remoteIdentity is the canonicalized remote pattern, or the rule text itself
// when it has no host to normalize.
func remoteIdentity(remote string) string {
	return aidectx.CanonicalizeRemotePattern(remote)
}

// remoteRuleCovers reports whether broad is a remote prefix glob ("<prefix>/*")
// that strictly covers narrow on the same effective remote_name.
func remoteRuleCovers(broad, narrow config.MatchRule) bool {
	if broad.Remote == "" || narrow.Remote == "" {
		return false
	}
	if effectiveRemoteName(broad) != effectiveRemoteName(narrow) {
		return false
	}
	canonBroad := remoteIdentity(broad.Remote)
	if !strings.HasSuffix(canonBroad, "/*") {
		return false
	}
	canonNarrow := remoteIdentity(narrow.Remote)
	if canonNarrow == canonBroad {
		return false
	}
	prefix := strings.TrimSuffix(canonBroad, "*") // ends with "/"
	return strings.HasPrefix(canonNarrow, prefix)
}

// pruneSubsumedRemoteRules partitions rules into those kept and those pruned because
// broad strictly covers them via remoteRuleCovers.
func pruneSubsumedRemoteRules(rules []config.MatchRule, broad config.MatchRule) (kept, pruned []config.MatchRule) {
	for _, r := range rules {
		if remoteRuleCovers(broad, r) {
			pruned = append(pruned, r)
			continue
		}
		kept = append(kept, r)
	}
	return kept, pruned
}

type existingBind struct {
	ctxName string
	rule    config.MatchRule
}

// findExistingBind returns the first context (other than skipCtx) whose match
// rules collide with newRule, or nil if no collision is found.
func findExistingBind(cfg *config.Config, skipCtx string, newRule config.MatchRule) *existingBind {
	names := make([]string, 0, len(cfg.Contexts))
	for ctxName := range cfg.Contexts {
		if ctxName != skipCtx {
			names = append(names, ctxName)
		}
	}
	sort.Strings(names)
	for _, ctxName := range names {
		ctx := cfg.Contexts[ctxName]
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

type promptIO struct {
	reader *bufio.Reader
	out    io.Writer
	tty    bool
}

var isInputTTY = func(_ *cobra.Command) bool {
	return isStdinTTY()
}

func newPromptIO(cmd *cobra.Command) *promptIO {
	return &promptIO{
		reader: bufio.NewReader(cmd.InOrStdin()),
		out:    cmd.OutOrStdout(),
		tty:    isInputTTY(cmd),
	}
}

func formatRuleShort(r config.MatchRule) string {
	if r.Remote != "" {
		return fmt.Sprintf("remote: %s", r.Remote)
	}
	return fmt.Sprintf("path: %s", r.Path)
}

func formatRuleDesc(r config.MatchRule) string {
	if r.Remote != "" {
		return fmt.Sprintf("by remote %s", r.Remote)
	}
	return fmt.Sprintf("by path %s", r.Path)
}

func selectBindScope(cwd, scopeFlag string, pio *promptIO) (config.MatchRule, string, error) {
	remote := aidectx.CurrentRemote(cwd)
	opts := buildBindScopeOptions(cwd, remote)
	if scopeFlag != "" {
		return resolveScopeFlag(scopeFlag, cwd, remote, opts)
	}
	if pio == nil || !pio.tty || len(opts) == 1 {
		return opts[0].Rule, opts[0].Desc, nil
	}

	fmt.Fprintln(pio.out, "Select match scope:")
	for i, opt := range opts {
		def := ""
		if i == 0 {
			def = " (default)"
		}
		fmt.Fprintf(pio.out, "  [%d] %s%s\n", i+1, opt.Label, def)
	}
	for {
		fmt.Fprint(pio.out, "Choose [1]: ")
		line, err := pio.reader.ReadString('\n')
		if err != nil && len(line) == 0 {
			return config.MatchRule{}, "", fmt.Errorf("aborted")
		}
		choice := strings.TrimSpace(line)
		if choice == "" {
			return opts[0].Rule, opts[0].Desc, nil
		}
		if strings.EqualFold(choice, "c") {
			return config.MatchRule{}, "", fmt.Errorf("aborted")
		}
		idx, convErr := strconv.Atoi(choice)
		if convErr == nil && idx >= 1 && idx <= len(opts) {
			return opts[idx-1].Rule, opts[idx-1].Desc, nil
		}
		fmt.Fprintf(pio.out, "Please enter a number between 1 and %d, or c to cancel.\n", len(opts))
	}
}

type ruleRef struct {
	profile string
	rule    config.MatchRule
	score   int
}

func scoreBeats(scoreA int, profileA string, scoreB int, profileB string) bool {
	if scoreA != scoreB {
		return scoreA > scoreB
	}
	return profileA < profileB
}

func findShadowingRules(cfg *config.Config, targetProfile string, targetScore int, cwd string, remotes aidectx.RemoteLookup) []ruleRef {
	var conflicts []ruleRef
	names := make([]string, 0, len(cfg.Contexts))
	for name := range cfg.Contexts {
		if name != targetProfile {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		ctx := cfg.Contexts[name]
		for _, r := range ctx.Match {
			rCopy := r
			s := aidectx.ScoreRule(&rCopy, cwd, remotes)
			if s > 0 && scoreBeats(s, name, targetScore, targetProfile) {
				conflicts = append(conflicts, ruleRef{profile: name, rule: r, score: s})
			}
		}
	}
	sort.SliceStable(conflicts, func(i, j int) bool {
		return scoreBeats(conflicts[i].score, conflicts[i].profile, conflicts[j].score, conflicts[j].profile)
	})
	return conflicts
}

func canonicalOriginRemote(raw string) string {
	if c := aidectx.ParseRemoteHost(raw); c != "" {
		return c
	}
	if raw != "" && !strings.HasPrefix(raw, "/") && !strings.HasPrefix(raw, ".") &&
		!strings.Contains(raw, "://") && !strings.Contains(raw, "@") && strings.Contains(raw, "/") {
		return raw
	}
	return ""
}

func findNarrowerOverlaps(cfg *config.Config, targetProfile string, broad config.MatchRule, skip []ruleRef) []ruleRef {
	if !strings.HasSuffix(remoteIdentity(broad.Remote), "/*") {
		return nil
	}
	var overlaps []ruleRef
	names := make([]string, 0, len(cfg.Contexts))
	for name := range cfg.Contexts {
		if name != targetProfile {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		ctx := cfg.Contexts[name]
		for _, r := range ctx.Match {
			if !remoteRuleCovers(broad, r) {
				continue
			}
			skipped := false
			for _, s := range skip {
				if s.profile == name && rulesCollide(s.rule, r) {
					skipped = true
					break
				}
			}
			if !skipped {
				overlaps = append(overlaps, ruleRef{profile: name, rule: r})
			}
		}
	}
	return overlaps
}

func promptMenuChoice(pio *promptIO, validChoices []string) (string, error) {
	for {
		fmt.Fprint(pio.out, "Choose [1]: ")
		line, err := pio.reader.ReadString('\n')
		if err != nil && len(line) == 0 {
			return "", fmt.Errorf("aborted")
		}
		choice := strings.TrimSpace(line)
		if choice == "" {
			return "1", nil
		}
		if strings.EqualFold(choice, "c") {
			return "", fmt.Errorf("aborted")
		}
		for _, v := range validChoices {
			if choice == v {
				return choice, nil
			}
		}
		fmt.Fprintf(pio.out, "Please enter one of %s, or c to cancel.\n", strings.Join(validChoices, ", "))
	}
}

func resolveAndApplyBind(cfg *config.Config, targetProfile string, newRule config.MatchRule, cwd string, remotes aidectx.RemoteLookup, pio *promptIO) (bool, string, error) {
	if remotes == nil {
		remotes = aidectx.GitRemotes(cwd)
	}
	newRuleCopy := newRule
	newScore := aidectx.ScoreRule(&newRuleCopy, cwd, remotes)
	finalDesc := formatRuleDesc(newRule)

	// Same-profile pre-check: check if targetProfile already has an identical or covering rule.
	targetAlreadyCovers := false
	bestTargetScore := newScore
	for _, r := range cfg.Contexts[targetProfile].Match {
		rCopy := r
		if s := aidectx.ScoreRule(&rCopy, cwd, remotes); s > bestTargetScore {
			bestTargetScore = s
		}
		if rulesCollide(r, newRule) || remoteRuleCovers(r, newRule) {
			targetAlreadyCovers = true
		}
	}
	if targetAlreadyCovers && len(findShadowingRules(cfg, targetProfile, bestTargetScore, cwd, remotes)) == 0 {
		return false, finalDesc, nil
	}

	// Stage A: Exact cross-profile collision.
	if existing := findExistingBind(cfg, targetProfile, newRule); existing != nil {
		if pio == nil || !pio.tty {
			return false, "", fmt.Errorf("folder already bound to profile %q; re-run interactively to resolve, or edit config.yaml", existing.ctxName)
		}
		fmt.Fprintf(pio.out, "Rule %s is already bound to profile %q.\n", formatRuleShort(newRule), existing.ctxName)
		fmt.Fprintf(pio.out, "  [1] Move rule from %q to %q (default)\n", existing.ctxName, targetProfile)
		fmt.Fprintln(pio.out, "  [c] Cancel")
		if _, err := promptMenuChoice(pio, []string{"1"}); err != nil {
			return false, "", err
		}
		otherCtx := cfg.Contexts[existing.ctxName]
		otherCtx.Match = removeMatchRule(otherCtx.Match, existing.rule)
		cfg.Contexts[existing.ctxName] = otherCtx
	}

	// Stage B: Shadowing conflict at cwd.
	var extraOverride *config.MatchRule
	shadowing := findShadowingRules(cfg, targetProfile, newScore, cwd, remotes)
	if len(shadowing) > 0 {
		top := shadowing[0]
		if pio == nil || !pio.tty {
			return false, "", fmt.Errorf("profile %q (%s) outscores %q (%s) for this folder; re-run interactively to resolve",
				top.profile, formatRuleShort(top.rule), targetProfile, formatRuleShort(newRule))
		}
		var descList []string
		for _, c := range shadowing {
			descList = append(descList, fmt.Sprintf("%s (%s)", c.profile, formatRuleShort(c.rule)))
		}
		summary := strings.Join(descList, ", ")

		// Determine if a narrower override candidate for cwd is eligible.
		var candidate *config.MatchRule
		originRemote := canonicalOriginRemote(remotes(aidectx.DefaultRemoteName))
		if originRemote != "" && !rulesCollide(newRule, config.MatchRule{Remote: originRemote}) {
			cand := config.MatchRule{Remote: originRemote}
			candScore := aidectx.ScoreRule(&cand, cwd, remotes)
			if findExistingBind(cfg, targetProfile, cand) == nil && scoreBeats(candScore, targetProfile, top.score, top.profile) {
				candidate = &cand
			}
		}
		if candidate == nil && !rulesCollide(newRule, config.MatchRule{Path: cwd}) {
			cand := config.MatchRule{Path: cwd}
			candScore := aidectx.ScoreRule(&cand, cwd, remotes)
			if findExistingBind(cfg, targetProfile, cand) == nil && scoreBeats(candScore, targetProfile, top.score, top.profile) {
				candidate = &cand
			}
		}

		fmt.Fprintf(pio.out, "Conflict: %s outscores %q (%s) for this folder.\n", summary, targetProfile, formatRuleShort(newRule))
		fmt.Fprintf(pio.out, "  [1] Remove shadowing rule(s) (%s) so %q applies here (default)\n", summary, targetProfile)
		var validChoices []string
		if candidate != nil {
			fmt.Fprintf(pio.out, "  [2] Also bind %s to %q so %q overrides %q here\n", formatRuleShort(*candidate), targetProfile, targetProfile, top.profile)
			fmt.Fprintf(pio.out, "  [3] Keep both as-is (save %s on %q, leaving %q active here)\n", formatRuleShort(newRule), targetProfile, top.profile)
			validChoices = []string{"1", "2", "3"}
		} else {
			fmt.Fprintf(pio.out, "  [2] Keep both as-is (save %s on %q, leaving %q active here)\n", formatRuleShort(newRule), targetProfile, top.profile)
			validChoices = []string{"1", "2"}
		}
		fmt.Fprintln(pio.out, "  [c] Cancel")

		choice, err := promptMenuChoice(pio, validChoices)
		if err != nil {
			return false, "", err
		}
		if choice == "1" {
			for _, c := range shadowing {
				otherCtx := cfg.Contexts[c.profile]
				otherCtx.Match = removeMatchRule(otherCtx.Match, c.rule)
				cfg.Contexts[c.profile] = otherCtx
			}
		} else if choice == "2" && candidate != nil {
			extraOverride = candidate
			finalDesc = fmt.Sprintf("%s and %s", formatRuleDesc(newRule), formatRuleDesc(*candidate))
		}
	}

	// Stage C: Narrower overlapping rules on other profiles.
	overlaps := findNarrowerOverlaps(cfg, targetProfile, newRule, shadowing)
	if len(overlaps) > 0 && pio != nil && pio.tty {
		var descList []string
		for _, o := range overlaps {
			descList = append(descList, fmt.Sprintf("%s (%s)", o.profile, formatRuleShort(o.rule)))
		}
		summary := strings.Join(descList, ", ")
		fmt.Fprintf(pio.out, "Other profile(s) have narrower rules under %s: %s.\n", newRule.Remote, summary)
		fmt.Fprintln(pio.out, "  [1] Keep existing rules on other profiles as overrides (default)")
		fmt.Fprintf(pio.out, "  [2] Remove overlapping rules (%s) so %q covers all of %s\n", summary, targetProfile, newRule.Remote)
		fmt.Fprintln(pio.out, "  [c] Cancel")

		choice, err := promptMenuChoice(pio, []string{"1", "2"})
		if err != nil {
			return false, "", err
		}
		if choice == "2" {
			for _, o := range overlaps {
				otherCtx := cfg.Contexts[o.profile]
				otherCtx.Match = removeMatchRule(otherCtx.Match, o.rule)
				cfg.Contexts[o.profile] = otherCtx
			}
		}
	}

	// Stage D: Same-profile redundancy pruning and append.
	ctx := cfg.Contexts[targetProfile]
	kept, _ := pruneSubsumedRemoteRules(ctx.Match, newRule)
	ctx.Match = kept
	alreadyHasNew := false
	for _, r := range ctx.Match {
		if rulesCollide(r, newRule) {
			alreadyHasNew = true
			break
		}
	}
	if !alreadyHasNew {
		ctx.Match = append(ctx.Match, newRule)
	}
	if extraOverride != nil {
		alreadyHasOverride := false
		for _, r := range ctx.Match {
			if rulesCollide(r, *extraOverride) {
				alreadyHasOverride = true
				break
			}
		}
		if !alreadyHasOverride {
			ctx.Match = append(ctx.Match, *extraOverride)
		}
	}
	cfg.Contexts[targetProfile] = ctx
	return true, finalDesc, nil
}
