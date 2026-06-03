package gemini_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/provision/agents/gemini"
)

func TestGeminiWriteHooksThenRead(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := gemini.New(&fakeRunner{})

	hooks := []provision.Hook{
		{Event: "pre_tool", Command: "rtk hook gemini"},
	}
	if err := d.WriteHooks(ctx, nil, hooks); err != nil {
		t.Fatal(err)
	}
	got, err := d.ReadHooks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Command != "rtk hook gemini" {
		t.Errorf("ReadHooks = %+v", got)
	}
	// Script file should exist.
	entries, _ := os.ReadDir(filepath.Join(home, ".gemini", "hooks"))
	hasScript := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "aide_") {
			hasScript = true
		}
	}
	if !hasScript {
		t.Error("expected aide_*.sh script in ~/.gemini/hooks/")
	}
}

func TestGeminiWriteHooksRejectsMetacharacters(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := gemini.New(&fakeRunner{})

	err := d.WriteHooks(ctx, nil, []provision.Hook{{Event: "pre_tool", Command: "rtk hook; rm -rf ~"}})
	if err == nil {
		t.Error("expected error for command containing shell metacharacters")
	}
}

func TestGeminiWriteHooksClearsPrevious(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := gemini.New(&fakeRunner{})

	_ = d.WriteHooks(ctx, nil, []provision.Hook{{Event: "pre_tool", Command: "old-hook"}})
	_ = d.WriteHooks(ctx, nil, []provision.Hook{{Event: "pre_tool", Command: "new-hook"}})

	got, _ := d.ReadHooks(ctx)
	if len(got) != 1 || got[0].Command != "new-hook" {
		t.Errorf("ReadHooks = %+v, want [new-hook]", got)
	}
	entries, _ := os.ReadDir(filepath.Join(home, ".gemini", "hooks"))
	aideCount := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "aide_") {
			aideCount++
		}
	}
	if aideCount != 1 {
		t.Errorf("expected 1 aide_ script, found %d", aideCount)
	}
}
