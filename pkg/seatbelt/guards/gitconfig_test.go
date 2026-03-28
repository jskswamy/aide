package guards_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

func TestParseGitConfig_BasicConfig(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	gitconfig := filepath.Join(home, ".gitconfig")
	if err := os.WriteFile(gitconfig, []byte("[user]\n\tname = Test User\n\temail = test@example.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := guards.ParseGitConfig(home, "", nil)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	assertContains(t, result.ConfigFiles, gitconfig)
}

func TestParseGitConfig_CustomExcludesAndAttributes(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	gitconfig := filepath.Join(home, ".gitconfig")
	if err := os.WriteFile(gitconfig, []byte("[core]\n\texcludesFile = ~/my-ignores\n\tattributesFile = ~/my-attributes\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := guards.ParseGitConfig(home, "", nil)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}

	expectedExcludes := filepath.Join(home, "my-ignores")
	if result.ExcludesFile != expectedExcludes {
		t.Errorf("expected excludesFile %q, got %q", expectedExcludes, result.ExcludesFile)
	}

	expectedAttributes := filepath.Join(home, "my-attributes")
	if result.AttributesFile != expectedAttributes {
		t.Errorf("expected attributesFile %q, got %q", expectedAttributes, result.AttributesFile)
	}
}

func TestParseGitConfig_IncludeResolution(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	workConfig := filepath.Join(home, ".gitconfig-work")
	if err := os.WriteFile(workConfig, []byte("[user]\n\temail = work@company.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	gitconfig := filepath.Join(home, ".gitconfig")
	if err := os.WriteFile(gitconfig, []byte("[include]\n\tpath = ~/.gitconfig-work\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := guards.ParseGitConfig(home, "", nil)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	assertContains(t, result.ConfigFiles, workConfig)
}

func TestParseGitConfig_IncludeIfGitdir(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	projectRoot := filepath.Join(tmp, "work", "myproject")
	if err := os.MkdirAll(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	workConfig := filepath.Join(home, ".gitconfig-work")
	if err := os.WriteFile(workConfig, []byte("[user]\n\temail = work@company.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	gitconfig := filepath.Join(home, ".gitconfig")
	content := "[includeIf \"gitdir:" + filepath.Join(tmp, "work") + "/\"]\n\tpath = ~/.gitconfig-work\n"
	if err := os.WriteFile(gitconfig, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	result := guards.ParseGitConfig(home, projectRoot, nil)
	assertContains(t, result.ConfigFiles, workConfig)

	otherProject := filepath.Join(tmp, "personal", "myproject")
	if err := os.MkdirAll(filepath.Join(otherProject, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	result2 := guards.ParseGitConfig(home, otherProject, nil)
	assertNotContains(t, result2.ConfigFiles, workConfig)
}

func TestParseGitConfig_IncludedConfigOverridesExcludesFile(t *testing.T) {
	// Regression test: core.excludesFile set in an included config (via
	// includeIf) was ignored because resolveIncludes didn't extract core
	// values from included files. Git uses last-one-wins semantics, so
	// a value in an included file overrides the top-level config.
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	projectRoot := filepath.Join(home, "source", "org", "myrepo")
	if err := os.MkdirAll(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Top-level config sets one excludesFile
	topExcludes := filepath.Join(home, "global-gitignore")
	if err := os.WriteFile(topExcludes, []byte("*.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Included config overrides with a different excludesFile
	orgExcludes := filepath.Join(home, "source", "org", ".gitignore")
	if err := os.MkdirAll(filepath.Dir(orgExcludes), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(orgExcludes, []byte("*.tmp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create the included config that sets core.excludesFile
	orgConfig := filepath.Join(home, "org-gitconfig")
	if err := os.WriteFile(orgConfig, []byte("[core]\n\texcludesFile = "+orgExcludes+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Main gitconfig: sets top-level excludesFile + includeIf
	gitconfig := filepath.Join(home, ".gitconfig")
	content := "[core]\n\texcludesFile = " + topExcludes + "\n" +
		"[includeIf \"gitdir:~/source/org/\"]\n\tpath = " + orgConfig + "\n"
	if err := os.WriteFile(gitconfig, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	result := guards.ParseGitConfig(home, projectRoot, nil)

	// The included config's excludesFile should win (last-one-wins)
	// resolveSymlink resolves /var → /private/var on macOS
	resolvedOrgExcludes := resolvedPath(orgExcludes)
	if result.ExcludesFile != resolvedOrgExcludes {
		t.Errorf("expected ExcludesFile %q (from included config), got %q",
			resolvedOrgExcludes, result.ExcludesFile)
	}

	// Both excludes files should be discoverable in AllPaths
	allPaths := result.AllPaths()
	assertContains(t, allPaths, resolvedOrgExcludes)
}

func TestParseGitConfig_IncludeIfGitdirTildeTrailingSlash(t *testing.T) {
	// Regression test: gitdir patterns like "gitdir:~/work/" use tilde +
	// trailing slash. expandTilde used filepath.Join which strips trailing
	// slashes, causing the prefix match to fall through to exact match
	// and silently fail. This is the most common real-world includeIf
	// pattern (e.g., [includeIf "gitdir:~/source/github.com/org/"]).
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	projectRoot := filepath.Join(home, "source", "org", "myrepo")
	if err := os.MkdirAll(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	workConfig := filepath.Join(home, "dot-files", "work-gitconfig")
	if err := os.MkdirAll(filepath.Dir(workConfig), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workConfig, []byte("[user]\n\temail = work@company.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Use ~/source/org/ with tilde — this is the exact pattern that broke
	gitconfig := filepath.Join(home, ".gitconfig")
	content := "[includeIf \"gitdir:~/source/org/\"]\n\tpath = ~/dot-files/work-gitconfig\n"
	if err := os.WriteFile(gitconfig, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	result := guards.ParseGitConfig(home, projectRoot, nil)
	assertContains(t, result.ConfigFiles, workConfig)

	// Non-matching org should NOT include the file
	otherProject := filepath.Join(home, "source", "other-org", "repo")
	if err := os.MkdirAll(filepath.Join(otherProject, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	result2 := guards.ParseGitConfig(home, otherProject, nil)
	assertNotContains(t, result2.ConfigFiles, workConfig)
}

func TestParseGitConfig_SymlinkResolution(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	real := filepath.Join(tmp, "real")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}

	realConfig := filepath.Join(real, "gitconfig-work")
	if err := os.WriteFile(realConfig, []byte("[user]\n\temail = work@company.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	symlink := filepath.Join(home, ".gitconfig-work")
	if err := os.Symlink(realConfig, symlink); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte("[include]\n\tpath = ~/.gitconfig-work\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := guards.ParseGitConfig(home, "", nil)
	assertContains(t, result.ConfigFiles, realConfig)
}

func TestParseGitConfigWithEnv_GlobalOverride(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	customConfig := filepath.Join(tmp, "custom-gitconfig")
	if err := os.WriteFile(customConfig, []byte("[core]\n\texcludesFile = ~/custom-ignores\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	envLookup := func(key string) (string, bool) {
		if key == "GIT_CONFIG_GLOBAL" {
			return customConfig, true
		}
		return "", false
	}

	result := guards.ParseGitConfigWithEnv(home, "", envLookup)
	assertContains(t, result.ConfigFiles, customConfig)

	// The replacement: default ~/.gitconfig must NOT be in results when overridden.
	assertNotContains(t, result.ConfigFiles, filepath.Join(home, ".gitconfig"))

	expectedExcludes := filepath.Join(home, "custom-ignores")
	if result.ExcludesFile != expectedExcludes {
		t.Errorf("expected excludesFile %q, got %q", expectedExcludes, result.ExcludesFile)
	}
}

func TestParseGitConfigWithEnv_SystemOverride(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	customSystem := filepath.Join(tmp, "system-gitconfig")
	envLookup := func(key string) (string, bool) {
		if key == "GIT_CONFIG_SYSTEM" {
			return customSystem, true
		}
		return "", false
	}

	result := guards.ParseGitConfigWithEnv(home, "", envLookup)
	assertContains(t, result.ConfigFiles, customSystem)

	// The replacement: default /etc/gitconfig must NOT be in results when overridden.
	assertNotContains(t, result.ConfigFiles, "/etc/gitconfig")
}

func TestParseGitConfig_GPGSignDetection(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	// Config with commit.gpgsign = true
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(
		"[commit]\n\tgpgsign = true\n[user]\n\tsigningkey = ABC123\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := guards.ParseGitConfig(home, "", nil)
	if !result.GPGSign {
		t.Error("expected GPGSign = true when commit.gpgsign is set")
	}
}

func TestParseGitConfig_GPGSignFromIncludedConfig(t *testing.T) {
	// GPG signing can be set in an included config (e.g., org-specific)
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	projectRoot := filepath.Join(home, "source", "org", "repo")
	if err := os.MkdirAll(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	orgConfig := filepath.Join(home, "org-gitconfig")
	if err := os.WriteFile(orgConfig, []byte(
		"[commit]\n\tgpgsign = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(
		"[includeIf \"gitdir:~/source/org/\"]\n\tpath = "+orgConfig+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := guards.ParseGitConfig(home, projectRoot, nil)
	if !result.GPGSign {
		t.Error("expected GPGSign = true from included config")
	}

	// Different project should NOT have GPGSign
	otherProject := filepath.Join(home, "source", "other", "repo")
	if err := os.MkdirAll(filepath.Join(otherProject, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	result2 := guards.ParseGitConfig(home, otherProject, nil)
	if result2.GPGSign {
		t.Error("expected GPGSign = false for non-matching project")
	}
}

func TestParseGitConfig_NoGPGSign(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(
		"[user]\n\tname = Test\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := guards.ParseGitConfig(home, "", nil)
	if result.GPGSign {
		t.Error("expected GPGSign = false when not configured")
	}
}

func TestParseGitConfig_MissingConfig(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "empty-home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	result := guards.ParseGitConfig(home, "", nil)
	if result.Err != nil {
		t.Fatalf("should not error on missing config: %v", result.Err)
	}
	if result.ExcludesFile != filepath.Join(home, ".gitignore") {
		t.Errorf("expected default excludesFile, got %q", result.ExcludesFile)
	}
	if len(result.Warnings) == 0 {
		t.Error("expected warning about missing config")
	}
}

func TestParseGitConfig_MaxIncludeDepth(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	gitconfig := filepath.Join(home, ".gitconfig")
	if err := os.WriteFile(gitconfig, []byte("[include]\n\tpath = ~/.gitconfig\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := guards.ParseGitConfig(home, "", nil)
	if result.Err != nil {
		t.Fatalf("should not error: %v", result.Err)
	}

	hasDepthWarning := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "max include depth") {
			hasDepthWarning = true
			break
		}
	}
	if !hasDepthWarning {
		t.Error("expected max include depth warning")
	}
}

func TestParseGitConfig_CorruptedConfigFallback(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write garbage bytes to .gitconfig.
	gitconfig := filepath.Join(home, ".gitconfig")
	if err := os.WriteFile(gitconfig, []byte("\x00\xff\xfe garbage [[[ not valid git config"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := guards.ParseGitConfig(home, "", nil)
	if result.Err != nil {
		t.Fatalf("should not return error on corrupted config: %v", result.Err)
	}
	// Well-known defaults must still be returned.
	if result.ExcludesFile == "" {
		t.Error("expected default ExcludesFile even with corrupted config")
	}
	if len(result.ConfigFiles) == 0 {
		t.Error("expected well-known config file paths even with corrupted config")
	}
	// A warning must be emitted.
	if len(result.Warnings) == 0 {
		t.Error("expected warning when config cannot be parsed")
	}
}

func TestParseGitConfigWithEnv_XDGConfigHome(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	xdgDir := filepath.Join(tmp, "xdg")
	if err := os.MkdirAll(filepath.Join(xdgDir, "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	xdgConfig := filepath.Join(xdgDir, "git", "config")
	if err := os.WriteFile(xdgConfig, []byte("[user]\n\tname = XDG User\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	envLookup := func(key string) (string, bool) {
		if key == "XDG_CONFIG_HOME" {
			return xdgDir, true
		}
		return "", false
	}

	result := guards.ParseGitConfigWithEnv(home, "", envLookup)
	assertContains(t, result.ConfigFiles, xdgConfig)

	// The default XDG path under home must NOT appear when XDG_CONFIG_HOME overrides it.
	defaultXDG := filepath.Join(home, ".config", "git", "config")
	assertNotContains(t, result.ConfigFiles, defaultXDG)
}

func resolvedPath(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	return p
}

func assertContains(t *testing.T, slice []string, item string) {
	t.Helper()
	resolvedItem := resolvedPath(item)
	if slices.Contains(slice, resolvedItem) || slices.Contains(slice, item) {
		return
	}
	t.Errorf("expected %v to contain %q", slice, item)
}

func assertNotContains(t *testing.T, slice []string, item string) {
	t.Helper()
	resolvedItem := resolvedPath(item)
	if slices.Contains(slice, resolvedItem) || slices.Contains(slice, item) {
		t.Errorf("expected %v to NOT contain %q", slice, item)
	}
}
