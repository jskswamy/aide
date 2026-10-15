package pi_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
	pidriver "github.com/jskswamy/aide/internal/provision/agents/pi"
)

func TestPiWriteHooksThenRead(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := pidriver.New(&fakeRunner{})

	hooks := []provision.Hook{
		{Event: "pre_tool", Command: "rtk hook pi"},
	}
	if err := d.WriteHooks(ctx, nil, hooks); err != nil {
		t.Fatal(err)
	}
	got, err := d.ReadHooks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Command != "rtk hook pi" || got[0].Event != "pre_tool" {
		t.Errorf("ReadHooks = %+v", got)
	}

	entries, _ := os.ReadDir(filepath.Join(home, ".pi", "agent", "extensions"))
	hasExtension := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "aide-") && strings.HasSuffix(e.Name(), ".ts") {
			hasExtension = true
		}
	}
	if !hasExtension {
		t.Error("expected aide-*.ts extension file in ~/.pi/agent/extensions/")
	}
}

func TestPiWriteHooksUsesAgentDirOverride(t *testing.T) {
	home := t.TempDir()
	customDir := filepath.Join(home, "custom-pi-agent")
	ctx := provision.Context{HomeDir: home, Env: map[string]string{"PI_CODING_AGENT_DIR": customDir}}
	d := pidriver.New(&fakeRunner{})

	if err := d.WriteHooks(ctx, nil, []provision.Hook{{Event: "pre_tool", Command: "rtk hook pi"}}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(customDir, "extensions"))
	if err != nil {
		t.Fatalf("expected extensions dir under PI_CODING_AGENT_DIR override: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 extension file, found %d", len(entries))
	}
}

func TestPiWriteHooksRejectsMetacharacters(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := pidriver.New(&fakeRunner{})

	err := d.WriteHooks(ctx, nil, []provision.Hook{{Event: "pre_tool", Command: "rtk hook; rm -rf ~"}})
	if err == nil {
		t.Error("expected error for command containing shell metacharacters")
	}
}

func TestPiWriteHooksClearsPrevious(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := pidriver.New(&fakeRunner{})

	_ = d.WriteHooks(ctx, nil, []provision.Hook{{Event: "pre_tool", Command: "old-hook"}})
	_ = d.WriteHooks(ctx, nil, []provision.Hook{{Event: "pre_tool", Command: "new-hook"}})

	got, _ := d.ReadHooks(ctx)
	if len(got) != 1 || got[0].Command != "new-hook" {
		t.Errorf("ReadHooks = %+v, want [new-hook]", got)
	}
}

func TestPiPostToolEventMapsCorrectly(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := pidriver.New(&fakeRunner{})

	if err := d.WriteHooks(ctx, nil, []provision.Hook{{Event: "post_tool", Command: "rtk hook post"}}); err != nil {
		t.Fatal(err)
	}
	got, err := d.ReadHooks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Event != "post_tool" {
		t.Errorf("ReadHooks = %+v, want event=post_tool", got)
	}
}

func TestPiWriteHooksSkipsUnsupportedEventSilently(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := pidriver.New(&fakeRunner{})

	if err := d.WriteHooks(ctx, nil, []provision.Hook{{Event: "totally_unknown_event", Command: "noop"}}); err != nil {
		t.Fatalf("unsupported event should be skipped silently, got %v", err)
	}
	got, err := d.ReadHooks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected no hooks written for an unsupported event, got %+v", got)
	}
}

func TestPiWriteHooksAppliesMatcher(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := pidriver.New(&fakeRunner{})

	hooks := []provision.Hook{
		{Event: "pre_tool", Matcher: "shell", Command: "rtk hook pi"},
		{Event: "pre_tool", Command: "rtk hook pi"},
	}
	if err := d.WriteHooks(ctx, nil, hooks); err != nil {
		t.Fatal(err)
	}
	got, err := d.ReadHooks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("ReadHooks = %+v, want both hooks round-tripped", got)
	}
	keys := map[string]bool{}
	for _, h := range got {
		keys[provision.HookKey(h.Event, h.Matcher, h.Command)] = true
	}
	for _, h := range hooks {
		if !keys[provision.HookKey(h.Event, h.Matcher, h.Command)] {
			t.Errorf("hook %+v did not round-trip; got %+v", h, got)
		}
	}

	dir := filepath.Join(home, ".pi", "agent", "extensions")
	entries, _ := os.ReadDir(dir)
	guarded := 0
	for _, e := range entries {
		data, _ := os.ReadFile(filepath.Join(dir, e.Name()))
		if strings.Contains(string(data), `if (event.toolName !== "bash") return;`) {
			guarded++
		}
	}
	if guarded != 1 {
		t.Errorf("want exactly one extension guarded on toolName bash, got %d", guarded)
	}
}
