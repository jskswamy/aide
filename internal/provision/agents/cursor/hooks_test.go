package cursor_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/provision/agents/cursor"
)

func TestCursorWriteReadHooks(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := cursor.New()

	hooks := []provision.Hook{
		{Event: "pre_tool", Matcher: "shell", Command: "rtk hook cursor"},
	}
	if err := d.WriteHooks(ctx, nil, hooks); err != nil {
		t.Fatal(err)
	}
	got, err := d.ReadHooks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Command != "rtk hook cursor" {
		t.Errorf("ReadHooks = %+v", got)
	}
}

func TestCursorWriteHooksTranslatesEvent(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := cursor.New()

	_ = d.WriteHooks(ctx, nil, []provision.Hook{{Event: "pre_tool", Matcher: "shell", Command: "rtk hook cursor"}})

	data, _ := os.ReadFile(filepath.Join(home, ".cursor", "hooks.json"))
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal hooks.json: %v", err)
	}
	hooks, _ := raw["hooks"].(map[string]interface{})
	if _, ok := hooks["preToolUse"]; !ok {
		t.Errorf("expected preToolUse key in hooks.json: %s", data)
	}
}

func TestCursorWriteHooksPreservesUserHooks(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := cursor.New()

	// Write a user-managed hook directly (no _aide marker)
	hookPath := filepath.Join(home, ".cursor", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(hookPath), 0o750); err != nil {
		t.Fatal(err)
	}
	userContent := `{"version":1,"hooks":{"preToolUse":[{"command":"user-hook"}]}}`
	if err := os.WriteFile(hookPath, []byte(userContent), 0o600); err != nil {
		t.Fatal(err)
	}

	// WriteHooks should add an aide hook without removing the user hook
	if err := d.WriteHooks(ctx, nil, []provision.Hook{{Event: "pre_tool", Command: "aide-hook"}}); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("user-hook")) {
		t.Errorf("user-hook was removed from hooks.json: %s", data)
	}
	if !bytes.Contains(data, []byte("aide-hook")) {
		t.Errorf("aide-hook was not written to hooks.json: %s", data)
	}
}
