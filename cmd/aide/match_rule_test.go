// cmd/aide/match_rule_test.go
package main

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/config"
)

func TestAutoDetectMatchRule_NonGitFolder_PathRule(t *testing.T) {
	dir := t.TempDir()
	rule, desc := autoDetectMatchRule(dir)
	if rule.Path != dir || rule.Remote != "" {
		t.Errorf("non-git folder: got %+v, want Path=%s", rule, dir)
	}
	if desc == "" {
		t.Error("description must be non-empty")
	}
}

func TestAutoDetectMatchRule_GitRepoWithRemote_RemoteRule(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "git", "init")
	mustRun(t, dir, "git", "remote", "add", "origin", "git@example.com:foo/bar.git")
	rule, _ := autoDetectMatchRule(dir)
	if rule.Remote != "example.com/foo/bar" {
		t.Errorf("git repo with remote: got %+v, want Remote=example.com/foo/bar", rule)
	}
	if rule.Path != "" {
		t.Errorf("git repo with remote should not set Path: got %q", rule.Path)
	}
}

func TestAutoDetectMatchRule_RemoteWithCredentials_PersistsNormalizedForm(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "git", "init")
	mustRun(t, dir, "git", "remote", "add", "origin", "https://user:s3cret@GitHub.com/org/repo.git")
	rule, desc := autoDetectMatchRule(dir)
	if rule.Remote != "github.com/org/repo" {
		t.Errorf("got %+v, want Remote=github.com/org/repo", rule)
	}
	if strings.Contains(desc, "s3cret") || strings.Contains(rule.Remote, "s3cret") {
		t.Errorf("credentials leaked: rule=%+v desc=%q", rule, desc)
	}
}

func TestAutoDetectMatchRule_LocalPathRemote_PathRule(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "git", "init")
	mustRun(t, dir, "git", "remote", "add", "origin", "/srv/git/repo.git")
	rule, _ := autoDetectMatchRule(dir)
	if rule.Path != dir || rule.Remote != "" {
		t.Errorf("host-less remote: got %+v, want Path=%s", rule, dir)
	}
}

func TestAutoDetectMatchRule_GitRepoNoRemote_PathRule(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "git", "init")
	rule, _ := autoDetectMatchRule(dir)
	if rule.Path != dir {
		t.Errorf("git repo no remote: got %+v, want Path=%s", rule, dir)
	}
}

func mustRun(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=t@t",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, string(out))
	}
}

func TestRulesCollide_RemoteVariants(t *testing.T) {
	cases := []struct {
		a, b      string
		wantMatch bool
	}{
		{"git@github.com:org/repo.git", "https://github.com/org/repo.git", true},
		{"git@github.com:org/repo.git", "ssh://git@github.com/org/repo.git", true},
		{"https://github.com/org/repo", "https://github.com/org/repo.git", true},
		{"git@github.com:org/repo.git", "git@github.com:org/other.git", false},
		{"git@github.com:org/repo.git", "git@gitlab.com:org/repo.git", false},
		{"github.com/org/repo", "git@GitHub.com:org/repo.git", true},
		{"github.com/org/repo", "github.com/org/other", false},
		{"/srv/git/a.git", "/srv/git/b.git", false},
		{"/srv/git/a.git", "/srv/git/a.git", true},
	}
	for _, tc := range cases {
		a := config.MatchRule{Remote: tc.a}
		b := config.MatchRule{Remote: tc.b}
		if got := rulesCollide(a, b); got != tc.wantMatch {
			t.Errorf("rulesCollide(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.wantMatch)
		}
	}
}

func TestRulesCollide_PathMatch(t *testing.T) {
	a := config.MatchRule{Path: "/home/user/project"}
	b := config.MatchRule{Path: "/home/user/project"}
	if !rulesCollide(a, b) {
		t.Error("same path should collide")
	}
	if rulesCollide(a, config.MatchRule{Path: "/home/user/other"}) {
		t.Error("different path should not collide")
	}
}

func TestRulesCollide_RemoteVsPath_NoCollision(t *testing.T) {
	a := config.MatchRule{Remote: "git@github.com:org/repo.git"}
	b := config.MatchRule{Path: "/home/user/project"}
	if rulesCollide(a, b) {
		t.Error("remote vs path should never collide")
	}
}

func TestFindExistingBind_DetectsCrossContextCollision(t *testing.T) {
	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"work": {Match: []config.MatchRule{{Remote: "https://github.com/org/repo.git"}}},
		},
	}
	newRule := config.MatchRule{Remote: "git@github.com:org/repo.git"}
	got := findExistingBind(cfg, "personal", newRule)
	if got == nil || got.ctxName != "work" {
		t.Errorf("expected collision in 'work', got %v", got)
	}
}

func TestFindExistingBind_SkipsTargetContext(t *testing.T) {
	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"work": {Match: []config.MatchRule{{Remote: "https://github.com/org/repo.git"}}},
		},
	}
	newRule := config.MatchRule{Remote: "git@github.com:org/repo.git"}
	got := findExistingBind(cfg, "work", newRule)
	if got != nil {
		t.Errorf("expected nil when collision is in skip context, got %+v", got)
	}
}

func TestRemoveMatchRule_RemovesByNormalizedRemote(t *testing.T) {
	rules := []config.MatchRule{
		{Remote: "https://github.com/org/repo.git"},
		{Path: "/keep/this"},
	}
	// Remove by the ssh variant — should still match via ParseRemoteHost
	result := removeMatchRule(rules, config.MatchRule{Remote: "git@github.com:org/repo.git"})
	if len(result) != 1 || result[0].Path != "/keep/this" {
		t.Errorf("expected only path rule to remain, got %+v", result)
	}
}

// Unused-import shield: keep filepath/config referenced even if a future test
// shrinks; harmless during plan execution.
var _ = filepath.Join

func TestBuildBindScopeOptions(t *testing.T) {
	cwd := "/tmp/work/repo"

	// 1. Non-git directory
	opts := buildBindScopeOptions(cwd, "")
	if len(opts) != 1 || opts[0].Shorthand != "path" || opts[0].Rule.Path != cwd {
		t.Fatalf("non-git opts = %+v", opts)
	}

	// 2. 2-segment host/repo (no org segment -> never emit host/*)
	opts = buildBindScopeOptions(cwd, "git.internal/monorepo")
	if len(opts) != 2 || opts[0].Shorthand != "repo" || opts[1].Shorthand != "path" {
		t.Fatalf("2-segment opts = %+v", opts)
	}

	// 3. Standard 3-segment github.com/acme/api
	opts = buildBindScopeOptions(cwd, "github.com/acme/api")
	if len(opts) != 3 {
		t.Fatalf("expected 3 opts for github.com/acme/api, got %+v", opts)
	}
	if opts[0].Shorthand != "repo" || opts[0].Rule.Remote != "github.com/acme/api" {
		t.Errorf("opt[0] = %+v", opts[0])
	}
	if opts[1].Shorthand != "org" || opts[1].Rule.Remote != "github.com/acme/*" {
		t.Errorf("opt[1] = %+v", opts[1])
	}
	if opts[2].Shorthand != "path" || opts[2].Rule.Path != cwd {
		t.Errorf("opt[2] = %+v", opts[2])
	}

	// 4. Nested 5-segment gitlab.com/acme/platform/auth/token-service
	opts = buildBindScopeOptions(cwd, "gitlab.com/acme/platform/auth/token-service")
	if len(opts) != 5 {
		t.Fatalf("expected 5 opts for nested gitlab remote, got %+v", opts)
	}
	wantRemotes := []string{
		"gitlab.com/acme/platform/auth/token-service",
		"gitlab.com/acme/platform/auth/*",
		"gitlab.com/acme/platform/*",
		"gitlab.com/acme/*",
		"",
	}
	wantShorthands := []string{"repo", "acme/platform/auth", "acme/platform", "org", "path"}
	for i := range opts {
		if opts[i].Rule.Remote != wantRemotes[i] || opts[i].Shorthand != wantShorthands[i] {
			t.Errorf("opts[%d] = %+v, want remote=%q shorthand=%q", i, opts[i], wantRemotes[i], wantShorthands[i])
		}
	}
}

func TestResolveScopeFlag(t *testing.T) {
	cwd := "/tmp/work/token-service"
	remote := "gitlab.com/acme/platform/auth/token-service"
	opts := buildBindScopeOptions(cwd, remote)

	cases := []struct {
		flag       string
		wantRemote string
		wantPath   string
	}{
		{"repo", "gitlab.com/acme/platform/auth/token-service", ""},
		{"org", "gitlab.com/acme/*", ""},
		{"path", "", cwd},
		{"acme/platform", "gitlab.com/acme/platform/*", ""},
		{"gitlab.com/acme/platform/*", "gitlab.com/acme/platform/*", ""},
		{"gitlab.com/acme/platform", "gitlab.com/acme/platform/*", ""},
	}
	for _, tc := range cases {
		rule, _, err := resolveScopeFlag(tc.flag, cwd, remote, opts)
		if err != nil {
			t.Errorf("resolveScopeFlag(%q) unexpected error: %v", tc.flag, err)
			continue
		}
		if rule.Remote != tc.wantRemote || rule.Path != tc.wantPath {
			t.Errorf("resolveScopeFlag(%q) = %+v, want remote=%q path=%q", tc.flag, rule, tc.wantRemote, tc.wantPath)
		}
	}

	if _, _, err := resolveScopeFlag("gitlab.com/other/*", cwd, remote, opts); err == nil {
		t.Error("expected error for unrelated remote prefix")
	}

	nonGitOpts := buildBindScopeOptions(cwd, "")
	if _, _, err := resolveScopeFlag("repo", cwd, "", nonGitOpts); err == nil {
		t.Error("expected error for --scope repo in non-git dir")
	}
}

func TestRulesCollide_RemoteNameAndGlob(t *testing.T) {
	rOrigin := config.MatchRule{Remote: "github.com/acme/*"}
	rExplicitOrigin := config.MatchRule{Remote: "GitHub.com/acme/*/", RemoteName: "origin"}
	rUpstream := config.MatchRule{Remote: "github.com/acme/*", RemoteName: "upstream"}

	if !rulesCollide(rOrigin, rExplicitOrigin) {
		t.Error("empty RemoteName and 'origin' with same canonical glob should collide")
	}
	if rulesCollide(rOrigin, rUpstream) {
		t.Error("different RemoteName should not collide")
	}
}

func TestRemoteRuleCoversAndPrune(t *testing.T) {
	broad := config.MatchRule{Remote: "github.com/acme/*"}
	narrowRepo := config.MatchRule{Remote: "github.com/acme/service-a"}
	narrowSubgroup := config.MatchRule{Remote: "github.com/acme/team/*"}
	otherOrg := config.MatchRule{Remote: "github.com/acme-corp/service-a"}
	upstreamRepo := config.MatchRule{Remote: "github.com/acme/service-a", RemoteName: "upstream"}
	pathRule := config.MatchRule{Path: "/tmp/acme"}

	if !remoteRuleCovers(broad, narrowRepo) {
		t.Error("expected broad to cover narrowRepo")
	}
	if !remoteRuleCovers(broad, narrowSubgroup) {
		t.Error("expected broad to cover narrowSubgroup")
	}
	if remoteRuleCovers(broad, broad) {
		t.Error("broad should not cover itself (strictly narrower only)")
	}
	if remoteRuleCovers(broad, otherOrg) {
		t.Error("github.com/acme/* must not cover github.com/acme-corp/service-a")
	}
	if remoteRuleCovers(broad, upstreamRepo) {
		t.Error("origin broad rule must not cover upstream narrow rule")
	}
	if remoteRuleCovers(broad, pathRule) {
		t.Error("remote broad rule must not cover path rule")
	}

	existing := []config.MatchRule{pathRule, narrowRepo, narrowSubgroup, otherOrg, upstreamRepo}
	kept, pruned := pruneSubsumedRemoteRules(existing, broad)
	if len(pruned) != 2 {
		t.Fatalf("expected 2 pruned rules, got %+v", pruned)
	}
	if len(kept) != 3 {
		t.Fatalf("expected 3 kept rules, got %+v", kept)
	}
}

func initGitRepoWithOrigin(t *testing.T, originURL string) string {
	t.Helper()
	dir := t.TempDir()
	mustRun(t, dir, "git", "init")
	mustRun(t, dir, "git", "remote", "add", "origin", originURL)
	return dir
}

func newTestPromptIO(input string, tty bool, out *bytes.Buffer) *promptIO {
	return &promptIO{
		reader: bufio.NewReader(strings.NewReader(input)),
		out:    out,
		tty:    tty,
	}
}

func TestSelectBindScope_InteractiveAndDefault(t *testing.T) {
	dir := initGitRepoWithOrigin(t, "git@github.com:acme/payments.git")

	// Default on Enter -> [1] repo
	var out bytes.Buffer
	pio := newTestPromptIO("\n", true, &out)
	rule, desc, err := selectBindScope(dir, "", pio)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rule.Remote != "github.com/acme/payments" || !strings.Contains(desc, "github.com/acme/payments") {
		t.Errorf("default Enter got rule=%+v desc=%q", rule, desc)
	}

	// Choice 2 -> org glob
	out.Reset()
	pio = newTestPromptIO("2\n", true, &out)
	rule, desc, err = selectBindScope(dir, "", pio)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rule.Remote != "github.com/acme/*" || !strings.Contains(desc, "github.com/acme/*") {
		t.Errorf("choice 2 got rule=%+v desc=%q", rule, desc)
	}
}

func TestResolveAndApplyBind_Stages(t *testing.T) {
	cwd := "/Users/dev/source/github.com/acme/payments"
	remotes := func(name string) string {
		if name == "origin" {
			return "github.com/acme/payments"
		}
		return ""
	}

	t.Run("SameProfileAlreadyCovered_NoOp", func(t *testing.T) {
		cfg := &config.Config{
			Contexts: map[string]config.Context{
				"work": {Match: []config.MatchRule{{Remote: "github.com/acme/*"}}},
			},
		}
		var out bytes.Buffer
		applied, _, err := resolveAndApplyBind(cfg, "work", config.MatchRule{Remote: "github.com/acme/payments"}, cwd, remotes, newTestPromptIO("", true, &out))
		if err != nil || applied {
			t.Fatalf("expected applied=false err=nil, got applied=%v err=%v", applied, err)
		}
	})

	t.Run("StageA_ExactCollision_MoveToTarget", func(t *testing.T) {
		cfg := &config.Config{
			Contexts: map[string]config.Context{
				"personal": {Match: []config.MatchRule{{Remote: "github.com/acme/payments"}}},
				"work":     {},
			},
		}
		var out bytes.Buffer
		applied, _, err := resolveAndApplyBind(cfg, "work", config.MatchRule{Remote: "github.com/acme/payments"}, cwd, remotes, newTestPromptIO("1\n", true, &out))
		if err != nil || !applied {
			t.Fatalf("expected applied=true err=nil, got applied=%v err=%v", applied, err)
		}
		if len(cfg.Contexts["personal"].Match) != 0 {
			t.Errorf("expected personal match rules cleared, got %+v", cfg.Contexts["personal"].Match)
		}
		if len(cfg.Contexts["work"].Match) != 1 || cfg.Contexts["work"].Match[0].Remote != "github.com/acme/payments" {
			t.Errorf("expected rule moved to work, got %+v", cfg.Contexts["work"].Match)
		}
	})

	t.Run("StageB_ShadowingPathGlob_Option2AddRepoOverride", func(t *testing.T) {
		cfg := &config.Config{
			Contexts: map[string]config.Context{
				"personal": {Match: []config.MatchRule{{Path: "/Users/dev/source/*"}}},
				"work":     {},
			},
		}
		var out bytes.Buffer
		applied, _, err := resolveAndApplyBind(cfg, "work", config.MatchRule{Remote: "github.com/acme/*"}, cwd, remotes, newTestPromptIO("2\n", true, &out))
		if err != nil || !applied {
			t.Fatalf("expected applied=true err=nil, got applied=%v err=%v", applied, err)
		}
		if len(cfg.Contexts["personal"].Match) != 1 {
			t.Errorf("personal path glob should be preserved, got %+v", cfg.Contexts["personal"].Match)
		}
		if len(cfg.Contexts["work"].Match) != 2 {
			t.Fatalf("expected work to have both org glob and repo override, got %+v", cfg.Contexts["work"].Match)
		}
	})

	t.Run("StageC_NarrowerOverlapOnOtherProfile_Option2Remove", func(t *testing.T) {
		cfg := &config.Config{
			Contexts: map[string]config.Context{
				"personal": {Match: []config.MatchRule{{Remote: "github.com/acme/oss-tool"}}},
				"work":     {Match: []config.MatchRule{{Remote: "github.com/acme/old-service"}}},
			},
		}
		var out bytes.Buffer
		applied, _, err := resolveAndApplyBind(cfg, "work", config.MatchRule{Remote: "github.com/acme/*"}, cwd, remotes, newTestPromptIO("2\n", true, &out))
		if err != nil || !applied {
			t.Fatalf("expected applied=true err=nil, got applied=%v err=%v", applied, err)
		}
		if len(cfg.Contexts["personal"].Match) != 0 {
			t.Errorf("expected personal narrower rule removed, got %+v", cfg.Contexts["personal"].Match)
		}
		// Stage D also pruned github.com/acme/old-service on work!
		if len(cfg.Contexts["work"].Match) != 1 || cfg.Contexts["work"].Match[0].Remote != "github.com/acme/*" {
			t.Errorf("expected work to have only github.com/acme/* after Stage D pruning, got %+v", cfg.Contexts["work"].Match)
		}
	})

	t.Run("StageC_SkipsRuleAlreadyHandledInStageB", func(t *testing.T) {
		cfg := &config.Config{
			Contexts: map[string]config.Context{
				"personal": {Match: []config.MatchRule{{Remote: "github.com/acme/payments"}}},
				"work":     {},
			},
		}
		var out bytes.Buffer
		// Only one prompt response ("3\n" = keep both in Stage B). Stage C must not prompt a second time for the same rule.
		applied, _, err := resolveAndApplyBind(cfg, "work", config.MatchRule{Remote: "github.com/acme/*"}, cwd, remotes, newTestPromptIO("3\n", true, &out))
		if err != nil || !applied {
			t.Fatalf("expected applied=true err=nil without second prompt in Stage C, got applied=%v err=%v\n%s", applied, err, out.String())
		}
		if strings.Contains(out.String(), "Other profile(s) have narrower rules") {
			t.Errorf("Stage C should not re-prompt for a rule already handled in Stage B:\n%s", out.String())
		}
	})

	t.Run("StageB_LocalPathOriginFallsBackToPathOverride", func(t *testing.T) {
		localRemotes := func(name string) string {
			if name == "origin" {
				return "/srv/git/payments.git"
			}
			return ""
		}
		cfg := &config.Config{
			Contexts: map[string]config.Context{
				"personal": {Match: []config.MatchRule{{Path: "/Users/dev/source/*"}}},
				"work":     {},
			},
		}
		var out bytes.Buffer
		applied, _, err := resolveAndApplyBind(cfg, "work", config.MatchRule{Remote: "github.com/acme/*"}, cwd, localRemotes, newTestPromptIO("2\n", true, &out))
		if err != nil || !applied {
			t.Fatalf("expected applied=true err=nil, got applied=%v err=%v", applied, err)
		}
		wantOverride := config.MatchRule{Path: cwd}
		foundPathOverride := false
		for _, r := range cfg.Contexts["work"].Match {
			if r == wantOverride {
				foundPathOverride = true
			}
			if r.Remote == "/srv/git/payments.git" {
				t.Errorf("must not bind unparsed local remote as Remote rule: %+v", cfg.Contexts["work"].Match)
			}
		}
		if !foundPathOverride {
			t.Errorf("expected path override %+v on work, got %+v", wantOverride, cfg.Contexts["work"].Match)
		}
	})
}
