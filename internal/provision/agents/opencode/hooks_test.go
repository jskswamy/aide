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

func TestOpenCodeWriteHooksAppliesMatcher(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := opencode.New(fakeRunner{})

	hooks := []provision.Hook{
		{Event: "pre_tool", Matcher: "shell", Command: "rtk hook opencode"},
		{Event: "pre_tool", Command: "rtk hook opencode"},
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

	dir := filepath.Join(home, ".config", "opencode", "plugin")
	entries, _ := os.ReadDir(dir)
	guarded := 0
	for _, e := range entries {
		data, _ := os.ReadFile(filepath.Join(dir, e.Name()))
		if strings.Contains(string(data), `if (input.tool !== "bash") return`) {
			guarded++
		}
	}
	if guarded != 1 {
		t.Errorf("want exactly one plugin guarded on tool bash, got %d", guarded)
	}
}

// Session events were verified against opencode 1.19.0: session.created
// reaches the generic event hook when a session starts, and the plugin
// dispose hook runs when opencode exits normally.
func TestOpenCodeSessionEventsMapToEventAndDispose(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := opencode.New(fakeRunner{})

	hooks := []provision.Hook{
		{Event: "session_start", Command: "notify start"},
		{Event: "session_end", Command: "notify end"},
	}
	if err := d.WriteHooks(ctx, nil, hooks); err != nil {
		t.Fatal(err)
	}
	got, err := d.ReadHooks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	events := map[string]string{}
	for _, h := range got {
		events[h.Command] = h.Event
	}
	if events["notify start"] != "session_start" || events["notify end"] != "session_end" {
		t.Fatalf("ReadHooks = %+v, want both session hooks round-tripped", got)
	}

	scripts := readAll(t, filepath.Join(home, ".config", "opencode", "plugin"))
	for _, want := range []string{
		`"event": async ({ event }) => {`,
		`if (event.type !== "session.created" || event.properties.info?.parentID) return`,
		`"dispose": async () => {`,
	} {
		if !strings.Contains(scripts, want) {
			t.Errorf("generated plugins missing %q:\n%s", want, scripts)
		}
	}
}

func TestOpenCodeHooksDirRespectsOpencodeConfigDir(t *testing.T) {
	home := t.TempDir()
	custom := filepath.Join(home, "custom-opencode")
	ctx := provision.Context{
		HomeDir: home,
		Env:     map[string]string{"OPENCODE_CONFIG_DIR": custom},
	}
	d := opencode.New(fakeRunner{})
	err := d.WriteHooks(ctx, nil, []provision.Hook{
		{Event: "pre_tool", Command: "rtk hook opencode"},
	})
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Join(custom, "plugin"))
	if len(entries) == 0 {
		t.Error("expected hook in custom plugin dir")
	}
}

func readAll(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		b.Write(data)
	}
	return b.String()
}
