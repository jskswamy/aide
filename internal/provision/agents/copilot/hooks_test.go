package copilot_test

import (
	"testing"

	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/provision/agents/copilot"
)

func TestCopilotWriteReadHooks(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := copilot.New(&fakeRunner{})

	hooks := []provision.Hook{
		{Event: "pre_tool", Command: "rtk hook copilot"},
	}
	if err := d.WriteHooks(ctx, nil, hooks); err != nil {
		t.Fatal(err)
	}
	got, err := d.ReadHooks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Command != "rtk hook copilot" {
		t.Errorf("ReadHooks = %+v", got)
	}
}
