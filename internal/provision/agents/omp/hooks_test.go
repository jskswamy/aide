package omp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
	ompdriver "github.com/jskswamy/aide/internal/provision/agents/omp"
)

func TestOMPWriteHooksThenReadAndMatcher(t *testing.T) {
	home := t.TempDir()
	workDir := filepath.Join(home, ".omp-work")
	ctx := provision.Context{HomeDir: home, Env: map[string]string{"PI_CODING_AGENT_DIR": workDir}}
	d := ompdriver.New(&fakeRunner{})

	hooks := []provision.Hook{
		{Event: "pre_tool", Matcher: "shell", Command: "rtk hook omp"},
		{Event: "session_start", Command: "rtk session start"},
	}
	if err := d.WriteHooks(ctx, nil, hooks); err != nil {
		t.Fatal(err)
	}
	got, err := d.ReadHooks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("ReadHooks = %+v, want 2 hooks", got)
	}

	entries, err := os.ReadDir(filepath.Join(workDir, "extensions"))
	if err != nil || len(entries) != 2 {
		t.Fatalf("extensions entries = %v, %v; want 2 files", entries, err)
	}
	var allContent strings.Builder
	for _, e := range entries {
		data, rerr := os.ReadFile(filepath.Join(workDir, "extensions", e.Name()))
		if rerr != nil {
			t.Fatal(rerr)
		}
		allContent.Write(data)
	}
	text := allContent.String()
	for _, substr := range []string{
		`@oh-my-pi/pi-coding-agent`,
		`event.toolName !== "bash"`,
		`event.reason === "reload"`,
	} {
		if !strings.Contains(text, substr) {
			t.Errorf("generated extensions missing %q:\n%s", substr, text)
		}
	}

	// Subsequent WriteHooks replaces old artifacts cleanly.
	if err := d.WriteHooks(ctx, nil, []provision.Hook{{Event: "post_tool", Command: "rtk post"}}); err != nil {
		t.Fatal(err)
	}
	got, err = d.ReadHooks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Event != "post_tool" || got[0].Command != "rtk post" {
		t.Errorf("ReadHooks after replace = %+v", got)
	}
}
