package claude_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/provision/agents/claude"
)

func TestClaudeWriteHooksThenReadBack(t *testing.T) {
	dir := t.TempDir()
	ctx := provision.Context{Env: map[string]string{"CLAUDE_CONFIG_DIR": dir}}

	d := claude.New(&fakeRunner{})

	hooks := []provision.Hook{
		{Event: "pre_tool", Matcher: "shell", Command: "rtk hook claude"},
		{Event: "session_start", Command: "bd prime"},
	}
	if err := d.WriteHooks(ctx, nil, hooks); err != nil {
		t.Fatal(err)
	}

	got, err := d.ReadHooks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("ReadHooks = %d entries, want 2: %+v", len(got), got)
	}
}

func TestClaudeWriteHooksTranslatesEventNames(t *testing.T) {
	dir := t.TempDir()
	ctx := provision.Context{Env: map[string]string{"CLAUDE_CONFIG_DIR": dir}}
	d := claude.New(&fakeRunner{})

	if err := d.WriteHooks(ctx, nil, []provision.Hook{
		{Event: "pre_tool", Matcher: "shell", Command: "rtk hook claude"},
	}); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	hooksMap, ok := raw["hooks"].(map[string]interface{})
	if !ok {
		t.Fatalf("settings.json has no hooks object: %s", data)
	}
	if _, ok := hooksMap["PreToolUse"]; !ok {
		t.Errorf("expected PreToolUse key, got: %v", hooksMap)
	}
}

func TestClaudeWriteHooksPreservesUnknownMatchers(t *testing.T) {
	// Matchers not in claudeMatcherMap (e.g. "Grep|Glob", "clear", "compact")
	// must be written to settings.json as-is and survive the ReadHooks round-
	// trip. Previously they were zeroed to "" (map-miss default), causing a
	// desired/installed HookKey mismatch and a perpetual install op every sync.
	dir := t.TempDir()
	ctx := provision.Context{Env: map[string]string{"CLAUDE_CONFIG_DIR": dir}}
	d := claude.New(&fakeRunner{})

	hooks := []provision.Hook{
		{Event: "pre_tool", Matcher: "Grep|Glob", Command: "/usr/local/bin/gate"},
		{Event: "session_start", Matcher: "compact", Command: "/usr/local/bin/remind"},
	}
	if err := d.WriteHooks(ctx, nil, hooks); err != nil {
		t.Fatal(err)
	}

	// Verify the raw JSON has the matcher fields.
	data, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	if !strings.Contains(string(data), `"Grep|Glob"`) {
		t.Errorf("settings.json missing Grep|Glob matcher: %s", data)
	}
	if !strings.Contains(string(data), `"compact"`) {
		t.Errorf("settings.json missing compact matcher: %s", data)
	}

	// Verify ReadHooks returns the matchers unchanged.
	got, err := d.ReadHooks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("ReadHooks = %d hooks, want 2: %+v", len(got), got)
	}
	matchers := map[string]bool{}
	for _, h := range got {
		matchers[h.Matcher] = true
	}
	if !matchers["Grep|Glob"] {
		t.Errorf("Grep|Glob matcher lost after round-trip: %+v", got)
	}
	if !matchers["compact"] {
		t.Errorf("compact matcher lost after round-trip: %+v", got)
	}
}

func TestClaudeWriteHooksMultipleCommandsSameMatcher(t *testing.T) {
	// When two hooks share the same event+matcher (e.g. session_start:compact
	// for both ~/.claude/hooks/foo and ~/.claude-work/hooks/foo), both commands
	// must end up in the correct matcher bucket. The old code used
	// buckets[len-1] which appended the second command to whichever bucket
	// happened to be last — typically the wrong one.
	dir := t.TempDir()
	ctx := provision.Context{Env: map[string]string{"CLAUDE_CONFIG_DIR": dir}}
	d := claude.New(&fakeRunner{})

	hooks := []provision.Hook{
		{Event: "session_start", Matcher: "compact", Command: "/a/hooks/remind"},
		{Event: "session_start", Matcher: "startup", Command: "/a/hooks/remind"},
		{Event: "session_start", Matcher: "compact", Command: "/b/hooks/remind"},
		{Event: "session_start", Matcher: "startup", Command: "/b/hooks/remind"},
	}
	if err := d.WriteHooks(ctx, nil, hooks); err != nil {
		t.Fatal(err)
	}
	got, err := d.ReadHooks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("ReadHooks = %d hooks, want 4: %+v", len(got), got)
	}
	// Both compact commands must survive with the compact matcher.
	compact := map[string]bool{}
	startup := map[string]bool{}
	for _, h := range got {
		switch h.Matcher {
		case "compact":
			compact[h.Command] = true
		case "startup":
			startup[h.Command] = true
		default:
			t.Errorf("unexpected matcher %q for hook %+v", h.Matcher, h)
		}
	}
	if !compact["/a/hooks/remind"] || !compact["/b/hooks/remind"] {
		t.Errorf("compact commands = %v, want both /a and /b", compact)
	}
	if !startup["/a/hooks/remind"] || !startup["/b/hooks/remind"] {
		t.Errorf("startup commands = %v, want both /a and /b", startup)
	}
}

func TestClaudeWriteHooksProfileContextScenario(t *testing.T) {
	// Reproduces the perpetual-install bug that occurred when a profile context
	// (e.g. {agent_dir} hooks) was removed from config.yaml:
	// - settings.json has user-installed hooks WITH matchers (Grep|Glob, Bash, startup…)
	//   for both ~/.claude/ and ~/.claude-ctx/ paths.
	// - prevManaged = 13 hooks (both default and ctx-specific paths).
	// - desired = 5 hooks (only ~/.claude/ path, with matchers).
	// After WriteHooks: ReadHooks must return exactly the 5 desired hooks.
	// A second WriteHooks(prevManaged=5, desired=5) must be idempotent.
	dir := t.TempDir()
	ctx := provision.Context{
		HomeDir: "/Users/u",
		Env:     map[string]string{"CLAUDE_CONFIG_DIR": dir},
	}
	d := claude.New(&fakeRunner{})

	// Seed settings.json with the user-installed hooks (as they exist before any aide run).
	initial := map[string]interface{}{
		"hooks": map[string]interface{}{
			"PreToolUse": []interface{}{
				map[string]interface{}{
					"matcher": "Grep|Glob",
					"hooks": []interface{}{
						map[string]interface{}{"type": "command", "command": "/Users/u/.claude-ctx/hooks/gate", "timeout": float64(5)},
					},
				},
				map[string]interface{}{
					"matcher": "Bash",
					"hooks": []interface{}{
						map[string]interface{}{"type": "command", "command": "rtk hook claude"},
					},
				},
			},
			"SessionStart": []interface{}{
				map[string]interface{}{"matcher": "startup", "hooks": []interface{}{map[string]interface{}{"type": "command", "command": "/Users/u/.claude-ctx/hooks/remind"}}},
				map[string]interface{}{"matcher": "resume", "hooks": []interface{}{map[string]interface{}{"type": "command", "command": "/Users/u/.claude-ctx/hooks/remind"}}},
				map[string]interface{}{"matcher": "clear", "hooks": []interface{}{map[string]interface{}{"type": "command", "command": "/Users/u/.claude-ctx/hooks/remind"}}},
				map[string]interface{}{"matcher": "compact", "hooks": []interface{}{map[string]interface{}{"type": "command", "command": "/Users/u/.claude-ctx/hooks/remind"}}},
			},
		},
	}
	data, _ := json.MarshalIndent(initial, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	// prevManaged: 13-hook managed state covering both default and ctx-specific paths.
	prevManaged := []provision.Hook{
		{Event: "pre_tool", Matcher: "shell", Command: "rtk hook claude"},
		{Event: "pre_tool", Matcher: "Grep|Glob", Command: "/Users/u/.claude/hooks/gate"},
		{Event: "pre_tool", Matcher: "Grep|Glob", Command: "/Users/u/.claude-ctx/hooks/gate"},
		{Event: "session_start", Matcher: "clear", Command: "/Users/u/.claude/hooks/remind"},
		{Event: "session_start", Matcher: "compact", Command: "/Users/u/.claude/hooks/remind"},
		{Event: "session_start", Matcher: "resume", Command: "/Users/u/.claude/hooks/remind"},
		{Event: "session_start", Matcher: "startup", Command: "/Users/u/.claude/hooks/remind"},
		{Event: "session_start", Matcher: "clear", Command: "/Users/u/.claude-ctx/hooks/remind"},
		{Event: "session_start", Matcher: "compact", Command: "/Users/u/.claude-ctx/hooks/remind"},
		{Event: "session_start", Matcher: "resume", Command: "/Users/u/.claude-ctx/hooks/remind"},
		{Event: "session_start", Matcher: "startup", Command: "/Users/u/.claude-ctx/hooks/remind"},
		{Event: "SubagentStart", Matcher: "", Command: "/Users/u/.claude/hooks/remind"},
		{Event: "SubagentStart", Matcher: "", Command: "/Users/u/.claude-ctx/hooks/remind"},
	}
	desired := []provision.Hook{
		{Event: "pre_tool", Matcher: "Grep|Glob", Command: "/Users/u/.claude/hooks/gate"},
		{Event: "session_start", Matcher: "clear", Command: "/Users/u/.claude/hooks/remind"},
		{Event: "session_start", Matcher: "compact", Command: "/Users/u/.claude/hooks/remind"},
		{Event: "session_start", Matcher: "resume", Command: "/Users/u/.claude/hooks/remind"},
		{Event: "session_start", Matcher: "startup", Command: "/Users/u/.claude/hooks/remind"},
	}

	// First sync.
	if err := d.WriteHooks(ctx, prevManaged, desired); err != nil {
		t.Fatalf("WriteHooks (first sync): %v", err)
	}
	got, err := d.ReadHooks(ctx)
	if err != nil {
		t.Fatalf("ReadHooks after first sync: %v", err)
	}
	checkHooks(t, "first sync", got, desired)

	// Second sync (idempotent): prevManaged = desired from first sync.
	if err := d.WriteHooks(ctx, desired, desired); err != nil {
		t.Fatalf("WriteHooks (second sync): %v", err)
	}
	got2, err := d.ReadHooks(ctx)
	if err != nil {
		t.Fatalf("ReadHooks after second sync: %v", err)
	}
	checkHooks(t, "second sync", got2, desired)
}

func checkHooks(t *testing.T, label string, got []provision.Hook, want []provision.Hook) {
	t.Helper()
	gotSet := map[string]bool{}
	for _, h := range got {
		gotSet[h.Event+":"+h.Matcher+":"+h.Command] = true
	}
	wantSet := map[string]bool{}
	for _, h := range want {
		wantSet[h.Event+":"+h.Matcher+":"+h.Command] = true
	}
	for k := range wantSet {
		if !gotSet[k] {
			t.Errorf("[%s] missing hook %s; got: %v", label, k, got)
		}
	}
	for k := range gotSet {
		if !wantSet[k] {
			t.Errorf("[%s] unexpected hook %s", label, k)
		}
	}
}

func TestClaudeReadHooksExpandsTilde(t *testing.T) {
	dir := t.TempDir()
	ctx := provision.Context{
		HomeDir: "/Users/u",
		Env:     map[string]string{"CLAUDE_CONFIG_DIR": dir},
	}
	d := claude.New(&fakeRunner{})

	settings := `{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"~/.claude/hooks/cbm-gate"}]}]}}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := d.ReadHooks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 hook, got %d: %+v", len(got), got)
	}
	if got[0].Command != "/Users/u/.claude/hooks/cbm-gate" {
		t.Errorf("Command = %q, want /Users/u/.claude/hooks/cbm-gate", got[0].Command)
	}
}

func TestClaudeWriteHooksPreservesUserHooks(t *testing.T) {
	dir := t.TempDir()
	// Pre-populate a user-added hook (no _aide marker).
	initial := map[string]interface{}{
		"hooks": map[string]interface{}{
			"PreToolUse": []interface{}{
				map[string]interface{}{
					"matcher": "Bash",
					"hooks": []interface{}{
						map[string]interface{}{"type": "command", "command": "user-hook"},
					},
				},
			},
		},
	}
	data, _ := json.MarshalIndent(initial, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	ctx := provision.Context{Env: map[string]string{"CLAUDE_CONFIG_DIR": dir}}
	d := claude.New(&fakeRunner{})

	if err := d.WriteHooks(ctx, nil, []provision.Hook{
		{Event: "pre_tool", Matcher: "shell", Command: "rtk hook claude"},
	}); err != nil {
		t.Fatal(err)
	}

	got, err := d.ReadHooks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// ReadHooks now returns ALL hooks (user + aide); expect user-hook + aide hook.
	if len(got) != 2 {
		t.Errorf("ReadHooks = %+v, want 2 hooks (user-hook + aide hook)", got)
	}

	// Raw file should still contain user-hook.
	raw, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	if !strings.Contains(string(raw), "user-hook") {
		t.Errorf("user-hook should survive in settings.json: %s", raw)
	}
	if !strings.Contains(string(raw), "rtk hook claude") {
		t.Errorf("aide hook should be in settings.json: %s", raw)
	}
}
