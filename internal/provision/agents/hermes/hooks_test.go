package hermes_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/provision/agents/hermes"
)

func TestHermesWriteHooksPreservesUserPlugins(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := hermes.New()

	// Create a user-managed plugin directory (no aide_ prefix)
	userPlugin := filepath.Join(home, ".hermes", "plugins", "my_custom_plugin")
	if err := os.MkdirAll(userPlugin, 0o750); err != nil {
		t.Fatal(err)
	}

	// WriteHooks should not remove user plugin directories
	if err := d.WriteHooks(ctx, nil, []provision.Hook{{Event: "pre_tool", Command: "aide-hook"}}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(userPlugin); err != nil {
		t.Errorf("user plugin directory was removed: %v", err)
	}
}

func TestHermesWriteHooksRejectsMetacharacters(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := hermes.New()

	err := d.WriteHooks(ctx, nil, []provision.Hook{{Event: "pre_tool", Command: `rtk hook; rm -rf ~`}})
	if err == nil {
		t.Error("expected error for command containing shell metacharacters")
	}
}

func TestHermesWriteReadHooks(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := hermes.New()

	hooks := []provision.Hook{
		{Event: "pre_tool", Matcher: "", Command: "rtk hook hermes"},
	}
	if err := d.WriteHooks(ctx, nil, hooks); err != nil {
		t.Fatal(err)
	}
	got, err := d.ReadHooks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Command != "rtk hook hermes" {
		t.Errorf("ReadHooks = %+v, want command 'rtk hook hermes'", got)
	}
}

func TestHermesWriteHooksCreatesFiles(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := hermes.New()

	_ = d.WriteHooks(ctx, nil, []provision.Hook{{Event: "pre_tool", Matcher: "", Command: "rtk hook hermes"}})

	// Check that __init__.py exists
	pluginsDir := filepath.Join(home, ".hermes", "plugins")
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		t.Fatalf("readdir plugins: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one aide_* directory")
	}
	hookDir := filepath.Join(pluginsDir, entries[0].Name())
	initPy := filepath.Join(hookDir, "__init__.py")
	if _, err := os.Stat(initPy); err != nil {
		t.Fatalf("__init__.py not found: %v", err)
	}

	// Check that plugin.yaml exists and contains pre_tool_call
	pluginYaml := filepath.Join(hookDir, "plugin.yaml")
	data, err := os.ReadFile(pluginYaml)
	if err != nil {
		t.Fatalf("plugin.yaml not found: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "pre_tool_call") {
		t.Errorf("plugin.yaml missing 'pre_tool_call': %s", content)
	}
}
