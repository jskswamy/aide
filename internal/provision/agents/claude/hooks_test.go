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
