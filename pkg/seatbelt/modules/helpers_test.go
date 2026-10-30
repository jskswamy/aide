package modules

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/testutil"
	"github.com/jskswamy/aide/pkg/seatbelt"
)

func TestResolveConfigDirs_EnvOverride(t *testing.T) {
	ctx := &seatbelt.Context{
		HomeDir: "/home/user",
		Env:     []string{"CLAUDE_CONFIG_DIR=/custom/claude"},
	}
	dirs := resolveConfigDirs(ctx, "CLAUDE_CONFIG_DIR", []string{
		"/home/user/.claude",
		"/home/user/.config/claude",
	})
	if len(dirs) != 1 || dirs[0] != "/custom/claude" {
		t.Errorf("expected [/custom/claude], got %v", dirs)
	}
}

// TestResolveConfigDirs_TildeExpansion verifies env values like
// CLAUDE_CONFIG_DIR=~/.claude-prod are expanded to absolute paths
// before being returned for sandbox rule emission. Without expansion,
// Seatbelt subpath rules containing "~/..." literal strings never
// match the absolute paths the agent actually opens.
func TestResolveConfigDirs_TildeExpansion(t *testing.T) {
	ctx := &seatbelt.Context{
		HomeDir: "/home/user",
		Env:     []string{"CLAUDE_CONFIG_DIR=~/.claude-prod"},
	}
	dirs := resolveConfigDirs(ctx, "CLAUDE_CONFIG_DIR", nil)
	if len(dirs) != 1 || dirs[0] != "/home/user/.claude-prod" {
		t.Errorf("expected [/home/user/.claude-prod], got %v", dirs)
	}
}

func TestResolveConfigDirs_EmptyEnv(t *testing.T) {
	ctx := &seatbelt.Context{
		HomeDir: "/home/user",
		Env:     []string{"CLAUDE_CONFIG_DIR="},
	}
	// Empty env var is treated as unset — falls through to defaults.
	// Only candidates under homeDir pass ExistsOrUnderHome.
	dirs := resolveConfigDirs(ctx, "CLAUDE_CONFIG_DIR", []string{
		"/home/user/.claude",
		"/home/user/.config/claude",
	})
	if len(dirs) != 2 {
		t.Errorf("expected 2 default dirs, got %v", dirs)
	}
}

func TestResolveConfigDirs_NoEnvKey(t *testing.T) {
	ctx := &seatbelt.Context{
		HomeDir: "/home/user",
	}
	dirs := resolveConfigDirs(ctx, "", []string{
		"/home/user/.claude",
		"/somewhere/else",
	})
	// /somewhere/else doesn't exist and isn't under home
	if len(dirs) != 1 || dirs[0] != "/home/user/.claude" {
		t.Errorf("expected [/home/user/.claude], got %v", dirs)
	}
}

func TestResolveConfigDirs_ExistingOutsideHome(t *testing.T) {
	// Create a temp dir that exists on disk but is outside homeDir.
	tmpDir := t.TempDir()
	ctx := &seatbelt.Context{
		HomeDir: "/home/user",
	}
	dirs := resolveConfigDirs(ctx, "", []string{tmpDir})
	if len(dirs) != 1 || dirs[0] != tmpDir {
		t.Errorf("expected existing dir %s to be included, got %v", tmpDir, dirs)
	}
}

func TestResolveConfigDirs_NonExistentOutsideHome(t *testing.T) {
	ctx := &seatbelt.Context{
		HomeDir: "/home/user",
	}
	dirs := resolveConfigDirs(ctx, "", []string{
		filepath.Join(os.TempDir(), "nonexistent-aide-test-path"),
	})
	if len(dirs) != 0 {
		t.Errorf("expected no dirs for non-existent path outside home, got %v", dirs)
	}
}

func TestResolveConfigDirsAdditive_SafeOverride(t *testing.T) {
	ctx := &seatbelt.Context{
		HomeDir: "/home/user",
		Env:     []string{"CURSOR_CONFIG_DIR=/home/user/my-cursor"},
	}
	defaults := []string{"/home/user/.cursor"}

	dirs := resolveConfigDirsAdditive(ctx, "CURSOR_CONFIG_DIR", "/home/user/.config/cursor", defaults)

	if len(dirs) != 1 || dirs[0] != "/home/user/my-cursor" {
		t.Errorf("expected [/home/user/my-cursor], got %v", dirs)
	}
}

func TestResolveConfigDirsAdditive_RejectsSensitiveOverride(t *testing.T) {
	ctx := &seatbelt.Context{
		HomeDir: "/home/user",
		Env:     []string{"CURSOR_CONFIG_DIR=/home/user/.ssh"},
	}
	defaults := []string{"/home/user/.cursor"}
	xdg := "/home/user/.config/cursor"

	dirs := resolveConfigDirsAdditive(ctx, "CURSOR_CONFIG_DIR", xdg, defaults)

	for _, d := range dirs {
		if strings.Contains(d, ".ssh") {
			t.Errorf("resolveConfigDirsAdditive must not include .ssh path; got %v", dirs)
		}
	}
	if len(dirs) != 2 {
		t.Errorf("expected 2 fallback dirs (default + xdg), got %v", dirs)
	}
}

func TestResolveConfigDirsAdditive_RejectsSensitiveXdgCandidate(t *testing.T) {
	ctx := &seatbelt.Context{
		HomeDir: "/home/user",
	}
	dirs := resolveConfigDirsAdditive(ctx, "CURSOR_CONFIG_DIR", "/home/user/.ssh/cursor", []string{"/home/user/.cursor"})

	for _, d := range dirs {
		if strings.Contains(d, ".ssh") {
			t.Errorf("resolveConfigDirsAdditive must not include .ssh path; got %v", dirs)
		}
	}
	if len(dirs) != 1 || dirs[0] != "/home/user/.cursor" {
		t.Errorf("expected only default dir when xdgCandidate is unsafe, got %v", dirs)
	}
}

func TestResolveConfigDirsAdditive_RejectsOutsideHome(t *testing.T) {
	ctx := &seatbelt.Context{
		HomeDir: "/home/user",
		Env:     []string{"CURSOR_CONFIG_DIR=/etc/cursor"},
	}
	defaults := []string{"/home/user/.cursor"}
	xdg := "/home/user/.config/cursor"

	dirs := resolveConfigDirsAdditive(ctx, "CURSOR_CONFIG_DIR", xdg, defaults)

	if len(dirs) != 2 {
		t.Errorf("expected 2 fallback dirs, got %v", dirs)
	}
	for _, d := range dirs {
		if d == "/etc/cursor" {
			t.Errorf("override outside $HOME must be rejected; got %v", dirs)
		}
	}
}

func TestResolveConfigDirsAdditive_NoOverride(t *testing.T) {
	ctx := &seatbelt.Context{HomeDir: "/home/user"}
	defaults := []string{"/home/user/.cursor"}
	xdg := "/home/user/.config/cursor"

	dirs := resolveConfigDirsAdditive(ctx, "CURSOR_CONFIG_DIR", xdg, defaults)

	if len(dirs) != 2 || dirs[0] != "/home/user/.cursor" || dirs[1] != xdg {
		t.Errorf("expected [default, xdg], got %v", dirs)
	}
}

func TestConfigDirRules_Empty(t *testing.T) {
	rules := configDirRules("Claude", "/home/user", nil)
	if rules != nil {
		t.Errorf("expected nil for empty dirs, got %v", rules)
	}
}

func TestConfigDirRules_Single(t *testing.T) {
	rules := configDirRules("Claude", "/home/user", []string{"/home/user/.claude"})
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules (section + grant), got %d", len(rules))
	}
	// First rule is section header
	if !strings.Contains(rules[0].String(), "Claude config") {
		t.Errorf("expected section header with 'Claude config', got %q", rules[0].String())
	}
	// Second rule is the grant
	got := rules[1].String()
	want := fmt.Sprintf(`(allow file-read* file-write* (subpath %q))`, "/home/user/.claude")
	if !strings.Contains(got, want) {
		t.Errorf("expected grant rule containing %q, got %q", want, got)
	}
	if rules[1].Intent() != seatbelt.Allow {
		t.Errorf("expected Allow intent, got %d", rules[1].Intent())
	}
}

func TestConfigDirRules_Multiple(t *testing.T) {
	dirs := []string{"/home/user/.claude", "/home/user/.config/claude"}
	rules := configDirRules("Claude", "/home/user", dirs)
	// 1 section + 2 grant rules
	if len(rules) != 3 {
		t.Fatalf("expected 3 rules, got %d", len(rules))
	}
	for i, dir := range dirs {
		got := rules[i+1].String()
		want := fmt.Sprintf(`(subpath %q)`, dir)
		if !strings.Contains(got, want) {
			t.Errorf("rule[%d]: expected %q in %q", i+1, want, got)
		}
	}
}

// allRuleStrings collects every emitted rule body for substring assertions.
func allRuleStrings(rules []seatbelt.Rule) string {
	var b strings.Builder
	for _, r := range rules {
		b.WriteString(r.String())
		b.WriteString("\n")
	}
	return b.String()
}

// TestConfigDirRules_SymlinkedDir covers home-manager's whole-dir pattern:
// ~/.claude is a symlink to ~/dotfiles/claude/. The canonical allow rule
// alone doesn't cover writes — macOS seatbelt fires policy on the kernel-
// resolved path, which lands under ~/dotfiles/. Rule generator must emit
// an additional rule for the resolved target.
func TestConfigDirRules_SymlinkedDir(t *testing.T) {
	home := testutil.CanonicalTempDir(t)
	realDir := filepath.Join(home, "dotfiles", "claude")
	canonical := filepath.Join(home, ".claude")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatalf("mkdir realDir: %v", err)
	}
	if err := os.Symlink(realDir, canonical); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	rules := configDirRules("Claude", home, []string{canonical})
	body := allRuleStrings(rules)

	if !strings.Contains(body, fmt.Sprintf(`(subpath %q)`, canonical)) {
		t.Errorf("rules should still allow canonical %q; got:\n%s", canonical, body)
	}
	if !strings.Contains(body, fmt.Sprintf(`(subpath %q)`, realDir)) {
		t.Errorf("rules must allow resolved target %q; got:\n%s", realDir, body)
	}
}

// TestConfigDirRules_SymlinkedFile covers stow / chezmoi-in-symlink-mode:
// ~/.claude/settings.json is a symlink to ~/dotfiles/claude/settings.json.
// The rule generator must emit an allow rule for the target's parent dir
// (so atomic-write tmp+rename siblings under that dir also work).
func TestConfigDirRules_SymlinkedFile(t *testing.T) {
	home := testutil.CanonicalTempDir(t)
	canonical := filepath.Join(home, ".claude")
	dotfilesDir := filepath.Join(home, "dotfiles", "claude")
	if err := os.MkdirAll(canonical, 0o755); err != nil {
		t.Fatalf("mkdir canonical: %v", err)
	}
	if err := os.MkdirAll(dotfilesDir, 0o755); err != nil {
		t.Fatalf("mkdir dotfilesDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dotfilesDir, "settings.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	if err := os.Symlink(filepath.Join(dotfilesDir, "settings.json"), filepath.Join(canonical, "settings.json")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	rules := configDirRules("Claude", home, []string{canonical})
	body := allRuleStrings(rules)

	if !strings.Contains(body, fmt.Sprintf(`(subpath %q)`, canonical)) {
		t.Errorf("rules should allow canonical dir %q; got:\n%s", canonical, body)
	}
	if !strings.Contains(body, fmt.Sprintf(`(subpath %q)`, dotfilesDir)) {
		t.Errorf("rules must allow target parent %q so sibling writes work; got:\n%s", dotfilesDir, body)
	}
}

// TestConfigDirRules_Depth1DirSymlinkTightScope verifies that on macOS, a
// symlinked subdirectory inside an agent config dir (e.g. ~/.claude/skills ->
// ~/dotfiles/claude-skills) grants the target directory itself, NOT its parent
// (~/dotfiles), matching Linux's expandConfigDirWritable behavior.
func TestConfigDirRules_Depth1DirSymlinkTightScope(t *testing.T) {
	home := testutil.CanonicalTempDir(t)
	canonical := filepath.Join(home, ".claude")
	skillsTarget := filepath.Join(home, "dotfiles", "claude-skills")
	if err := os.MkdirAll(canonical, 0o755); err != nil {
		t.Fatalf("mkdir canonical: %v", err)
	}
	if err := os.MkdirAll(skillsTarget, 0o755); err != nil {
		t.Fatalf("mkdir skillsTarget: %v", err)
	}
	if err := os.Symlink(skillsTarget, filepath.Join(canonical, "skills")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	rules := configDirRules("Claude", home, []string{canonical})
	body := allRuleStrings(rules)

	if !strings.Contains(body, fmt.Sprintf(`(subpath %q)`, skillsTarget)) {
		t.Errorf("dir-symlink should grant target %q; got:\n%s", skillsTarget, body)
	}
	parent := filepath.Dir(skillsTarget)
	if strings.Contains(body, fmt.Sprintf(`(subpath %q)`, parent)) {
		t.Errorf("dir-symlink must NOT grant parent %q; got:\n%s", parent, body)
	}
}

// TestConfigDirRules_RefusesProtectedSymlinkTargets verifies AIDE-tft4 on
// macOS Seatbelt: symlinks inside an agent config dir must not grant write
// access to directories that contain sensitive/protected dirs (like ~/.config
// or ~/.local/share), Skillshare dirs (~/.config/skillshare), aide's own dirs
// (~/.config/aide), other harness default dirs (~/.codex, ~/.pi, etc.), or
// other profile dirs (~/.claude-personal, ~/.codex-work).
func TestConfigDirRules_RefusesProtectedSymlinkTargets(t *testing.T) {
	cases := map[string]struct {
		targetRel string
		isFile    bool
	}{
		"contains sensitive/protected (.config)":      {targetRel: ".config"},
		"contains protected (.local/share)":           {targetRel: ".local/share"},
		"contains protected (.local/state)":           {targetRel: ".local/state"},
		"file directly in .config (parent=.config)":   {targetRel: ".config/some.yaml", isFile: true},
		"aide config dir":                             {targetRel: ".config/aide"},
		"aide data dir":                               {targetRel: ".local/share/aide"},
		"aide state dir":                              {targetRel: ".local/state/aide"},
		"skillshare config dir":                       {targetRel: ".config/skillshare"},
		"skillshare skills subdir":                    {targetRel: ".config/skillshare/skills"},
		"skillshare state dir":                        {targetRel: ".local/state/skillshare"},
		"skillshare file (parent=.config/skillshare)": {targetRel: ".config/skillshare/config.yaml", isFile: true},
		"other harness default (.codex)":              {targetRel: ".codex"},
		"other harness subdir (.codex/skills)":        {targetRel: ".codex/skills"},
		"other harness default (.pi)":                 {targetRel: ".pi"},
		"other harness default (.config/opencode)":    {targetRel: ".config/opencode"},
		"other profile dir (.claude-personal)":        {targetRel: ".claude-personal"},
		"other profile subdir (.claude-personal/sub)": {targetRel: ".claude-personal/sub"},
		"other harness profile (.codex-work)":         {targetRel: ".codex-work"},
		"gh credentials (.config/gh)":                 {targetRel: ".config/gh"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			home := testutil.CanonicalTempDir(t)
			canonical := filepath.Join(home, ".claude-work")
			if err := os.MkdirAll(canonical, 0o700); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(home, filepath.FromSlash(tc.targetRel))
			if tc.isFile {
				if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.MkdirAll(target, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(target, filepath.Join(canonical, "link")); err != nil {
				t.Fatal(err)
			}

			rules := configDirRules("Claude", home, []string{canonical})
			body := allRuleStrings(rules)

			// Only canonical itself should be granted; no widened subpath.
			if len(rules) != 2 {
				t.Errorf("expected only section + canonical rule (2 rules), got %d:\n%s", len(rules), body)
			}
		})
	}
}

// TestConfigDirRules_SymlinkTargetOutsideHome rejects outside-$HOME targets.
// Per AIDE-0jx design decision: we don't widen the sandbox to cover dotfiles
// repos placed outside $HOME. If a user reports breakage we revisit.
func TestConfigDirRules_SymlinkTargetOutsideHome(t *testing.T) {
	home := testutil.CanonicalTempDir(t)
	// outside is a sibling tempdir, deliberately NOT under home.
	outside := testutil.CanonicalTempDir(t)
	canonical := filepath.Join(home, ".claude")
	if err := os.Symlink(outside, canonical); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	rules := configDirRules("Claude", home, []string{canonical})
	body := allRuleStrings(rules)

	if strings.Contains(body, outside) {
		t.Errorf("outside-$HOME target %q must NOT be allow-listed; got:\n%s", outside, body)
	}
}

// TestConfigDirRules_SymlinkTargetInSensitiveDir rejects targets that land
// under sensitive home dirs (.ssh, .aws, .gnupg, etc.) even though they
// are under $HOME. A malicious or careless symlink must not grant sandbox
// write access to credentials.
func TestConfigDirRules_SymlinkTargetInSensitiveDir(t *testing.T) {
	home := testutil.CanonicalTempDir(t)
	ssh := filepath.Join(home, ".ssh", "claude-stash")
	if err := os.MkdirAll(ssh, 0o700); err != nil {
		t.Fatalf("mkdir ssh: %v", err)
	}
	canonical := filepath.Join(home, ".claude")
	if err := os.Symlink(ssh, canonical); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	rules := configDirRules("Claude", home, []string{canonical})
	body := allRuleStrings(rules)

	if strings.Contains(body, ".ssh") {
		t.Errorf("sensitive target under .ssh must NOT be allow-listed; got:\n%s", body)
	}
}

// TestConfigDirRules_BrokenSymlink ensures EvalSymlinks failure does not
// crash or pollute the rule set. The canonical dir still gets an allow
// rule; the broken link is silently skipped.
func TestConfigDirRules_BrokenSymlink(t *testing.T) {
	home := testutil.CanonicalTempDir(t)
	canonical := filepath.Join(home, ".claude")
	if err := os.MkdirAll(canonical, 0o755); err != nil {
		t.Fatalf("mkdir canonical: %v", err)
	}
	if err := os.Symlink(filepath.Join(home, "does-not-exist"), filepath.Join(canonical, "broken")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	rules := configDirRules("Claude", home, []string{canonical})
	body := allRuleStrings(rules)

	if !strings.Contains(body, fmt.Sprintf(`(subpath %q)`, canonical)) {
		t.Errorf("canonical rule still expected; got:\n%s", body)
	}
}

// TestConfigDirRules_DedupesOverlappingTargets ensures we don't emit
// redundant rules when the dir is a symlink AND a file inside it also
// symlinks to a path already covered by the resolved-dir rule.
func TestConfigDirRules_DedupesOverlappingTargets(t *testing.T) {
	home := testutil.CanonicalTempDir(t)
	realDir := filepath.Join(home, "dotfiles", "claude")
	canonical := filepath.Join(home, ".claude")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatalf("mkdir realDir: %v", err)
	}
	if err := os.Symlink(realDir, canonical); err != nil {
		t.Fatalf("symlink dir: %v", err)
	}
	// A file inside the symlinked dir, itself pointing to a sibling under realDir.
	inner := filepath.Join(realDir, "settings.json")
	if err := os.WriteFile(inner, []byte("{}"), 0o600); err != nil {
		t.Fatalf("seed inner: %v", err)
	}
	if err := os.Symlink(inner, filepath.Join(realDir, "alias.json")); err != nil {
		t.Fatalf("inner symlink: %v", err)
	}

	rules := configDirRules("Claude", home, []string{canonical})
	body := allRuleStrings(rules)

	// realDir should appear exactly once — the alias-symlink target's parent
	// is realDir itself, already covered by the dir-resolution rule.
	count := strings.Count(body, fmt.Sprintf(`(subpath %q)`, realDir))
	if count != 1 {
		t.Errorf("expected 1 rule for realDir, got %d; body:\n%s", count, body)
	}
}
