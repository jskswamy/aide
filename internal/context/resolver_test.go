package context

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jskswamy/aide/internal/config"
)

func TestResolve_SingleRemoteMatch(t *testing.T) {
	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"work": {
				Match: []config.MatchRule{
					{Remote: "github.com/org/*"},
				},
				Agent: "claude",
			},
		},
		DefaultContext: "work",
	}

	rc, err := Resolve(cfg, "/tmp/somedir", "github.com/org/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rc.Name != "work" {
		t.Errorf("expected context name 'work', got %q", rc.Name)
	}
	if rc.Context.Agent != "claude" {
		t.Errorf("expected agent 'claude', got %q", rc.Context.Agent)
	}
	if rc.MatchReason == "" {
		t.Error("expected non-empty MatchReason")
	}
}

func TestResolve_SpecificRemoteBeatsWildcard(t *testing.T) {
	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"broad": {
				Match: []config.MatchRule{
					{Remote: "github.com/*"},
				},
				Agent: "broad-agent",
			},
			"specific": {
				Match: []config.MatchRule{
					{Remote: "github.com/jskswamy/*"},
				},
				Agent: "specific-agent",
			},
		},
		DefaultContext: "broad",
	}

	rc, err := Resolve(cfg, "/tmp/somedir", "github.com/jskswamy/aide")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rc.Name != "specific" {
		t.Errorf("expected context 'specific', got %q", rc.Name)
	}
}

func TestResolve_PathMatchBeatsRemoteMatch(t *testing.T) {
	cwd := t.TempDir()
	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"remote-ctx": {
				Match: []config.MatchRule{
					{Remote: "github.com/org/*"},
				},
				Agent: "remote-agent",
			},
			"path-ctx": {
				Match: []config.MatchRule{
					{Path: cwd},
				},
				Agent: "path-agent",
			},
		},
		DefaultContext: "remote-ctx",
	}

	rc, err := Resolve(cfg, cwd, "github.com/org/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rc.Name != "path-ctx" {
		t.Errorf("expected context 'path-ctx', got %q", rc.Name)
	}
}

func TestResolve_LongerPatternBeatsShort(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home directory")
	}
	// Use a cwd under home that would match both patterns
	cwd := filepath.Join(home, "work", "org", "project")

	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"short": {
				Match: []config.MatchRule{
					{Path: "~/work/*"},
				},
				Agent: "short-agent",
			},
			"long": {
				Match: []config.MatchRule{
					{Path: "~/work/org/*"},
				},
				Agent: "long-agent",
			},
		},
		DefaultContext: "short",
	}

	rc, err := Resolve(cfg, cwd, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rc.Name != "long" {
		t.Errorf("expected context 'long' (longer pattern), got %q", rc.Name)
	}
}

func TestResolve_FallbackToDefault(t *testing.T) {
	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"work": {
				Match: []config.MatchRule{
					{Remote: "github.com/other-org/*"},
				},
				Agent: "work-agent",
			},
			"personal": {
				Agent: "personal-agent",
				Env:   map[string]string{"KEY": "val"},
			},
		},
		DefaultContext: "personal",
	}

	rc, err := Resolve(cfg, "/tmp/unmatched", "github.com/unmatched/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rc.Name != "personal" {
		t.Errorf("expected default context 'personal', got %q", rc.Name)
	}
	if rc.Context.Agent != "personal-agent" {
		t.Errorf("expected agent 'personal-agent', got %q", rc.Context.Agent)
	}
}

func TestResolve_MinimalConfig(t *testing.T) {
	cfg := &config.Config{
		Agent: "claude",
		Env:   map[string]string{"KEY": "val"},
	}

	rc, err := Resolve(cfg, "/tmp/somedir", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rc.Name != "default" {
		t.Errorf("expected context name 'default', got %q", rc.Name)
	}
	if rc.Context.Agent != "claude" {
		t.Errorf("expected agent 'claude', got %q", rc.Context.Agent)
	}
	if rc.Context.Env["KEY"] != "val" {
		t.Errorf("expected env KEY=val, got %v", rc.Context.Env)
	}
}

func TestResolve_ProjectOverrideMergesEnvAdditively(t *testing.T) {
	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"work": {
				Match: []config.MatchRule{
					{Remote: "github.com/org/*"},
				},
				Agent: "base-agent",
				Env:   map[string]string{"A": "1", "SHARED": "global"},
			},
		},
		DefaultContext: "work",
		ProjectOverride: &config.ProjectOverride{
			Env: map[string]string{"B": "2", "SHARED": "override"},
		},
	}

	rc, err := Resolve(cfg, "/tmp/somedir", "github.com/org/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rc.Context.Env["A"] != "1" {
		t.Errorf("expected env A=1 from context, got %q", rc.Context.Env["A"])
	}
	if rc.Context.Env["B"] != "2" {
		t.Errorf("expected env B=2 from override, got %q", rc.Context.Env["B"])
	}
	if rc.Context.Env["SHARED"] != "override" {
		t.Errorf("expected env SHARED=override (override wins), got %q", rc.Context.Env["SHARED"])
	}
}

func TestResolve_ProjectOverrideReplacesAgent(t *testing.T) {
	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"work": {
				Match: []config.MatchRule{
					{Remote: "github.com/org/*"},
				},
				Agent: "original-agent",
			},
		},
		DefaultContext: "work",
		ProjectOverride: &config.ProjectOverride{
			Agent: "overridden-agent",
		},
	}

	rc, err := Resolve(cfg, "/tmp/somedir", "github.com/org/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rc.Context.Agent != "overridden-agent" {
		t.Errorf("expected agent 'overridden-agent', got %q", rc.Context.Agent)
	}
}

func TestResolve_NoProjectOverride(t *testing.T) {
	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"work": {
				Match: []config.MatchRule{
					{Remote: "github.com/org/*"},
				},
				Agent:  "work-agent",
				Secret: "work",
			},
		},
		DefaultContext: "work",
	}

	rc, err := Resolve(cfg, "/tmp/somedir", "github.com/org/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rc.Context.Agent != "work-agent" {
		t.Errorf("expected agent 'work-agent', got %q", rc.Context.Agent)
	}
	if rc.Context.Secret != "work" {
		t.Errorf("expected secret 'work', got %q", rc.Context.Secret)
	}
}

func TestResolve_NoMatchNoDefault(t *testing.T) {
	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"work": {
				Match: []config.MatchRule{
					{Remote: "github.com/specific-org/*"},
				},
				Agent: "work-agent",
			},
		},
		// No DefaultContext set
	}

	_, err := Resolve(cfg, "/tmp/somedir", "github.com/other-org/repo")
	if err == nil {
		t.Fatal("expected error when no match and no default, got nil")
	}
}

func TestResolve_ExactPathBeatsGlobPath(t *testing.T) {
	cwd := t.TempDir()
	parent := filepath.Dir(cwd)

	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"glob-ctx": {
				Match: []config.MatchRule{
					{Path: parent + "/*"},
				},
				Agent: "glob-agent",
			},
			"exact-ctx": {
				Match: []config.MatchRule{
					{Path: cwd},
				},
				Agent: "exact-agent",
			},
		},
		DefaultContext: "glob-ctx",
	}

	rc, err := Resolve(cfg, cwd, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rc.Name != "exact-ctx" {
		t.Errorf("expected exact path context 'exact-ctx', got %q", rc.Name)
	}
}

func TestResolve_RemoteExactBeatsRemoteGlob(t *testing.T) {
	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"glob-ctx": {
				Match: []config.MatchRule{
					{Remote: "github.com/org/*"},
				},
				Agent: "glob-agent",
			},
			"exact-ctx": {
				Match: []config.MatchRule{
					{Remote: "github.com/org/repo"},
				},
				Agent: "exact-agent",
			},
		},
		DefaultContext: "glob-ctx",
	}

	rc, err := Resolve(cfg, "/tmp/somedir", "github.com/org/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rc.Name != "exact-ctx" {
		t.Errorf("expected exact remote context 'exact-ctx', got %q", rc.Name)
	}
}

func TestResolve_TildeExpansion(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home directory")
	}
	cwd := filepath.Join(home, "projects", "foo")

	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"tilde-ctx": {
				Match: []config.MatchRule{
					{Path: "~/projects/*"},
				},
				Agent: "tilde-agent",
			},
		},
		DefaultContext: "tilde-ctx",
	}

	rc, err := Resolve(cfg, cwd, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rc.Name != "tilde-ctx" {
		t.Errorf("expected context 'tilde-ctx', got %q", rc.Name)
	}
}

func TestResolve_EmptyRemoteGraceful(t *testing.T) {
	cwd := t.TempDir()
	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"remote-ctx": {
				Match: []config.MatchRule{
					{Remote: "github.com/org/*"},
				},
				Agent: "remote-agent",
			},
			"path-ctx": {
				Match: []config.MatchRule{
					{Path: cwd},
				},
				Agent: "path-agent",
			},
		},
		DefaultContext: "remote-ctx",
	}

	rc, err := Resolve(cfg, cwd, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rc.Name != "path-ctx" {
		t.Errorf("expected path context when remote is empty, got %q", rc.Name)
	}
}

func TestResolve_ProjectOverrideReplacesMCPServers(t *testing.T) {
	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"work": {
				Agent:      "work-agent",
				MCPServers: []string{"git", "context7"},
			},
		},
		DefaultContext: "work",
		ProjectOverride: &config.ProjectOverride{
			MCPServers: []string{"custom-server"},
		},
	}

	rc, err := Resolve(cfg, "/tmp/somedir", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rc.Context.MCPServers) != 1 || rc.Context.MCPServers[0] != "custom-server" {
		t.Errorf("expected MCPServers to be replaced with [custom-server], got %v", rc.Context.MCPServers)
	}
}

func TestResolve_ParentWalk_GlobMatchesFromSubdir(t *testing.T) {
	// Pattern twlabs/* should match when cwd is twlabs/repo/src/pkg
	// because the resolver walks up: src/pkg -> src -> repo (matches twlabs/*)
	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	deep := filepath.Join(repo, "src", "pkg")

	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"work": {
				Match: []config.MatchRule{
					{Path: parent + "/*"},
				},
				Agent: "work-agent",
			},
		},
	}

	rc, err := Resolve(cfg, deep, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rc.Name != "work" {
		t.Errorf("expected context 'work' via parent walk, got %q", rc.Name)
	}
}

func TestResolve_ParentWalk_DoubleStarMatchesDeep(t *testing.T) {
	// Pattern parent/** should match cwd at any depth
	parent := t.TempDir()
	deep := filepath.Join(parent, "org", "repo", "src", "main")

	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"work": {
				Match: []config.MatchRule{
					{Path: parent + "/**"},
				},
				Agent: "work-agent",
			},
		},
	}

	rc, err := Resolve(cfg, deep, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rc.Name != "work" {
		t.Errorf("expected context 'work' via ** glob, got %q", rc.Name)
	}
}

func TestResolve_ParentWalk_ExactChildMatchFromSubdir(t *testing.T) {
	// Exact path match on a parent directory should work from a subdirectory
	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	subdir := filepath.Join(repo, "src")

	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"exact": {
				Match: []config.MatchRule{
					{Path: repo},
				},
				Agent: "exact-agent",
			},
		},
	}

	rc, err := Resolve(cfg, subdir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rc.Name != "exact" {
		t.Errorf("expected context 'exact' via parent walk, got %q", rc.Name)
	}
}

func TestResolve_ParentWalk_BaseOfGlobMatches(t *testing.T) {
	// When cwd IS the base directory of a glob pattern (e.g., cwd=/work
	// and pattern=/work/*), it should match. The user is at the root
	// of the context's scope.
	parent := t.TempDir()

	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"work": {
				Match: []config.MatchRule{
					{Path: parent + "/*"},
				},
				Agent: "work-agent",
			},
		},
	}

	rc, err := Resolve(cfg, parent, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rc.Name != "work" {
		t.Errorf("expected context 'work' when cwd is glob base, got %q", rc.Name)
	}
}

func TestResolve_ParentWalk_BaseOfDoubleStarMatches(t *testing.T) {
	// cwd=/work with pattern=/work/** should also match
	parent := t.TempDir()

	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"work": {
				Match: []config.MatchRule{
					{Path: parent + "/**"},
				},
				Agent: "work-agent",
			},
		},
	}

	rc, err := Resolve(cfg, parent, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rc.Name != "work" {
		t.Errorf("expected context 'work' when cwd is ** glob base, got %q", rc.Name)
	}
}

func TestResolve_ParentWalk_DoesNotMatchUnrelatedDir(t *testing.T) {
	// Pattern /foo/* should NOT match /bar/baz even with parent walking
	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"work": {
				Match: []config.MatchRule{
					{Path: "/foo/*"},
				},
				Agent: "work-agent",
			},
		},
	}

	_, err := Resolve(cfg, "/bar/baz/deep", "")
	if err == nil {
		t.Fatal("expected error for unmatched path, got nil")
	}
}

func TestResolve_ParentWalk_DirectMatchStillWorks(t *testing.T) {
	// Existing behavior: cwd directly matches glob without needing parent walk
	parent := t.TempDir()
	child := filepath.Join(parent, "repo")

	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"work": {
				Match: []config.MatchRule{
					{Path: parent + "/*"},
				},
				Agent: "work-agent",
			},
		},
	}

	rc, err := Resolve(cfg, child, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rc.Name != "work" {
		t.Errorf("expected context 'work' via direct glob match, got %q", rc.Name)
	}
}

func TestResolve_ParentWalk_ExactMatchBeatsParentGlob(t *testing.T) {
	// If cwd has an exact match, it should beat a glob that matches a parent
	parent := t.TempDir()
	child := filepath.Join(parent, "repo")

	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"glob-ctx": {
				Match: []config.MatchRule{
					{Path: parent + "/*"},
				},
				Agent: "glob-agent",
			},
			"exact-ctx": {
				Match: []config.MatchRule{
					{Path: child},
				},
				Agent: "exact-agent",
			},
		},
	}

	// From a subdir of child, both could match:
	// - exact-ctx matches child (via parent walk)
	// - glob-ctx matches child (directly)
	// Exact should win due to higher specificity tier
	subdir := filepath.Join(child, "src")
	rc, err := Resolve(cfg, subdir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rc.Name != "exact-ctx" {
		t.Errorf("expected exact match to beat glob via parent walk, got %q", rc.Name)
	}
}

func TestResolve_PreferencesFromGlobal(t *testing.T) {
	style := "boxed"
	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"work": {
				Agent: "work-agent",
			},
		},
		DefaultContext: "work",
		Preferences:    &config.Preferences{InfoStyle: style},
	}

	rc, err := Resolve(cfg, "/tmp/somedir", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rc.Preferences.InfoStyle != "boxed" {
		t.Errorf("expected InfoStyle %q, got %q", "boxed", rc.Preferences.InfoStyle)
	}
}

func TestResolve_PreferencesProjectOverride(t *testing.T) {
	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"work": {
				Agent: "work-agent",
			},
		},
		DefaultContext: "work",
		Preferences:    &config.Preferences{InfoStyle: "boxed"},
		ProjectOverride: &config.ProjectOverride{
			Preferences: &config.Preferences{InfoStyle: "clean"},
		},
	}

	rc, err := Resolve(cfg, "/tmp/somedir", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rc.Preferences.InfoStyle != "clean" {
		t.Errorf("expected InfoStyle %q after project override, got %q", "clean", rc.Preferences.InfoStyle)
	}
}

func TestResolve_PreferencesDefaults(t *testing.T) {
	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"work": {
				Agent: "work-agent",
			},
		},
		DefaultContext: "work",
	}

	rc, err := Resolve(cfg, "/tmp/somedir", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rc.Preferences.ShowInfo == nil || !*rc.Preferences.ShowInfo {
		t.Errorf("expected ShowInfo=true by default, got %v", rc.Preferences.ShowInfo)
	}
	if rc.Preferences.InfoStyle != "compact" {
		t.Errorf("expected InfoStyle %q by default, got %q", "compact", rc.Preferences.InfoStyle)
	}
	if rc.Preferences.InfoDetail != "normal" {
		t.Errorf("expected InfoDetail %q by default, got %q", "normal", rc.Preferences.InfoDetail)
	}
}

func TestResolve_MinimalConfig_YoloPassedThrough(t *testing.T) {
	tr := true
	cfg := &config.Config{
		Agent: "claude",
		Yolo:  &tr,
	}

	rc, err := Resolve(cfg, "/tmp/somedir", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rc.Context.Yolo == nil || !*rc.Context.Yolo {
		t.Errorf("expected Yolo=true in resolved context, got %v", rc.Context.Yolo)
	}
}

func TestResolve_MinimalConfig_YoloNilByDefault(t *testing.T) {
	cfg := &config.Config{
		Agent: "claude",
	}

	rc, err := Resolve(cfg, "/tmp/somedir", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rc.Context.Yolo != nil {
		t.Errorf("expected Yolo=nil in resolved context, got %v", rc.Context.Yolo)
	}
}

func TestResolve_ProjectOverrideYolo(t *testing.T) {
	f := false
	tr := true
	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"work": {
				Agent: "claude",
				Yolo:  &tr,
			},
		},
		DefaultContext: "work",
		ProjectOverride: &config.ProjectOverride{
			Yolo: &f,
		},
	}

	rc, err := Resolve(cfg, "/tmp/somedir", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rc.Context.Yolo == nil || *rc.Context.Yolo {
		t.Errorf("expected Yolo=false from project override, got %v", rc.Context.Yolo)
	}
}

func TestResolve_ProjectOverrideYoloNil_PreservesContext(t *testing.T) {
	tr := true
	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"work": {
				Agent: "claude",
				Yolo:  &tr,
			},
		},
		DefaultContext: "work",
		ProjectOverride: &config.ProjectOverride{
			Agent: "codex",
			// Yolo not set
		},
	}

	rc, err := Resolve(cfg, "/tmp/somedir", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rc.Context.Yolo == nil || !*rc.Context.Yolo {
		t.Errorf("expected Yolo=true preserved from context, got %v", rc.Context.Yolo)
	}
}

func TestResolve_ProjectOverrideReplacesSecret(t *testing.T) {
	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"work": {
				Agent:  "work-agent",
				Secret: "global",
			},
		},
		DefaultContext: "work",
		ProjectOverride: &config.ProjectOverride{
			Secret: "project",
		},
	}

	rc, err := Resolve(cfg, "/tmp/somedir", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rc.Context.Secret != "project" {
		t.Errorf("expected secret 'project', got %q", rc.Context.Secret)
	}
}
