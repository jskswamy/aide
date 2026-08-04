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

func tempClaudeCtx(t *testing.T) (provision.Context, string) {
	t.Helper()
	dir := t.TempDir()
	return provision.Context{
		HomeDir: dir,
		Env:     map[string]string{"CLAUDE_CONFIG_DIR": dir},
	}, dir
}

func TestReadStatusLine_EmptyWhenNoSettings(t *testing.T) {
	ctx, _ := tempClaudeCtx(t)
	got, err := claude.ReadStatusLine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestWriteStatusLine_SetsCommand(t *testing.T) {
	ctx, dir := tempClaudeCtx(t)
	if err := claude.WriteStatusLine(ctx, "aide statusline claude"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	sl, _ := raw["statusLine"].(map[string]interface{})
	if sl["command"] != "aide statusline claude" {
		t.Errorf("command = %v", sl["command"])
	}
}

func TestWriteStatusLine_PreservesExistingKeys(t *testing.T) {
	ctx, dir := tempClaudeCtx(t)
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"model":"claude-sonnet-4-6"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := claude.WriteStatusLine(ctx, "aide statusline claude"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if raw["model"] != "claude-sonnet-4-6" {
		t.Error("model key was lost")
	}
}

func TestReadStatusLine_RoundTrip(t *testing.T) {
	ctx, _ := tempClaudeCtx(t)
	if err := claude.WriteStatusLine(ctx, "aide statusline claude"); err != nil {
		t.Fatal(err)
	}
	got, err := claude.ReadStatusLine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != "aide statusline claude" {
		t.Errorf("ReadStatusLine = %q", got)
	}
}

func TestRemoveStatusLine_ReturnsPrevAndClearsKey(t *testing.T) {
	ctx, dir := tempClaudeCtx(t)
	if err := claude.WriteStatusLine(ctx, "aide statusline claude"); err != nil {
		t.Fatal(err)
	}
	prev, err := claude.RemoveStatusLine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if prev != "aide statusline claude" {
		t.Errorf("prev = %q", prev)
	}
	got, _ := claude.ReadStatusLine(ctx)
	if got != "" {
		t.Errorf("after remove, ReadStatusLine = %q, want empty", got)
	}
	// Verify key is gone from JSON
	data, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["statusLine"]; ok {
		t.Error("statusLine key still present after remove")
	}
}

func TestRemoveStatusLine_EmptyWhenNeverSet(t *testing.T) {
	ctx, _ := tempClaudeCtx(t)
	prev, err := claude.RemoveStatusLine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if prev != "" {
		t.Errorf("prev = %q, want empty", prev)
	}
}

func TestWriteWrapper_ContainsBothCommands(t *testing.T) {
	dir := t.TempDir()
	path, err := claude.WriteWrapper(dir, "npx ccstatusline")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	content := string(data)
	if !strings.Contains(content, "npx ccstatusline") {
		t.Errorf("wrapper missing existing command:\n%s", content)
	}
	if !strings.Contains(content, "aide statusline claude") {
		t.Errorf("wrapper missing aide command:\n%s", content)
	}
}

func TestWriteWrapper_IsExecutable(t *testing.T) {
	dir := t.TempDir()
	path, err := claude.WriteWrapper(dir, "npx ccstatusline")
	if err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode()&0100 == 0 {
		t.Errorf("wrapper is not executable: mode %v", info.Mode())
	}
}

func TestWrapperScriptPath_IsUnderConfig(t *testing.T) {
	got := claude.WrapperScriptPath("/home/user")
	want := "/home/user/.config/aide/statusline-wrapper.sh"
	if got != want {
		t.Errorf("WrapperScriptPath = %q, want %q", got, want)
	}
}
