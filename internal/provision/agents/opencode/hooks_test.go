package opencode_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/provision/agents/opencode"
)

func TestOpenCodeWriteHooksThenRead(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := opencode.New(fakeRunner{})

	hooks := []provision.Hook{
		{Event: "pre_tool", Command: "rtk hook opencode"},
	}
	if err := d.WriteHooks(ctx, nil, hooks); err != nil {
		t.Fatal(err)
	}
	got, err := d.ReadHooks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Command != "rtk hook opencode" || got[0].Event != "pre_tool" {
		t.Errorf("ReadHooks = %+v", got)
	}

	entries, _ := os.ReadDir(filepath.Join(home, ".config", "opencode", "plugin"))
	hasPlugin := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "aide-") && strings.HasSuffix(e.Name(), ".js") {
			hasPlugin = true
		}
	}
	if !hasPlugin {
		t.Error("expected aide-*.js plugin file in ~/.config/opencode/plugin/")
	}
}

func TestOpenCodeWriteHooksRejectsMetacharacters(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := opencode.New(fakeRunner{})

	err := d.WriteHooks(ctx, nil, []provision.Hook{{Event: "pre_tool", Command: "rtk hook; rm -rf ~"}})
	if err == nil {
		t.Error("expected error for command containing shell metacharacters")
	}
}

func TestOpenCodeWriteHooksClearsPrevious(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := opencode.New(fakeRunner{})

	_ = d.WriteHooks(ctx, nil, []provision.Hook{{Event: "pre_tool", Command: "old-hook"}})
	_ = d.WriteHooks(ctx, nil, []provision.Hook{{Event: "pre_tool", Command: "new-hook"}})

	got, _ := d.ReadHooks(ctx)
	if len(got) != 1 || got[0].Command != "new-hook" {
		t.Errorf("ReadHooks = %+v, want [new-hook]", got)
	}
	entries, _ := os.ReadDir(filepath.Join(home, ".config", "opencode", "plugin"))
	count := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "aide-") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 aide- plugin file, found %d", count)
	}
}

func TestOpenCodeWriteHooksSkipsUnsupportedEventSilently(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := opencode.New(fakeRunner{})

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

func TestOpenCodePostToolEventMapsCorrectly(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := opencode.New(fakeRunner{})

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
