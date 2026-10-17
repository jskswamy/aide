package skillshare

import (
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const userConfig = `# yaml-language-server: $schema=https://example.invalid/config.schema.json

sources:
  skills: ~/.config/skillshare/skills # where skills live
mode: merge
targets:
  claude:
    path: ~/.claude/skills
  aide-notes:
    path: ~/notes/skills
  aide-old-claude:
    agent: claude
    config_dir: ~/.claude-old
  aide-work-claude:
    agent: claude
    config_dir: ~/.claude-moved
mcp:
  targets: [claude, aide-old-claude]
ignore:
  - .DS_Store
`

func plan(targets ...Target) Plan { return Plan{Targets: targets} }

func decode(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := yaml.Unmarshal(data, &m); err != nil {
		t.Fatalf("output is not YAML: %v\n%s", err, data)
	}
	return m
}

func TestMerge_AddsUpdatesRemovesOnlyAideTargets(t *testing.T) {
	p := plan(
		Target{Name: "aide-work-claude", Profile: "work", Agent: "claude", ConfigDir: "~/.claude-work"},
		Target{Name: "aide-work-codex", Profile: "work", Agent: "codex", ConfigDir: "~/.codex-work"},
	)

	res, err := Merge([]byte(userConfig), p, "/home/u")
	if err != nil {
		t.Fatal(err)
	}

	if got := strings.Join(res.Added, ","); got != "aide-work-codex" {
		t.Errorf("added = %s", got)
	}
	if got := strings.Join(res.Updated, ","); got != "aide-work-claude" {
		t.Errorf("updated = %s", got)
	}
	if got := strings.Join(res.Removed, ","); got != "aide-old-claude" {
		t.Errorf("removed = %s", got)
	}
	out := string(res.Output)
	for _, keep := range []string{"# yaml-language-server", "# where skills live", "mode: merge", "- .DS_Store"} {
		if !strings.Contains(out, keep) {
			t.Errorf("output lost %q:\n%s", keep, out)
		}
	}
	targets, ok := decode(t, res.Output)["targets"].(map[string]any)
	if !ok {
		t.Fatalf("targets is not a mapping:\n%s", out)
	}
	if _, ok := targets["claude"]; !ok {
		t.Errorf("user target claude removed:\n%s", out)
	}
	if _, ok := targets["aide-notes"]; !ok {
		t.Errorf("non-account aide-notes target removed:\n%s", out)
	}
	if _, ok := targets["aide-old-claude"]; ok {
		t.Errorf("stale aide target kept:\n%s", out)
	}
	work, ok := targets["aide-work-claude"].(map[string]any)
	if !ok || work["agent"] != "claude" || work["config_dir"] != "~/.claude-work" {
		t.Errorf("aide-work-claude = %v", work)
	}
	if got := strings.Join(res.References, ","); !strings.Contains(got, "aide-old-claude (mcp.targets)") {
		t.Errorf("references = %q", got)
	}
}

func TestMerge_IsIdempotent(t *testing.T) {
	p := plan(Target{Name: "aide-work-pi", Profile: "work", Agent: "pi", ConfigDir: "~/.pi-work"})
	first, err := Merge([]byte(userConfig), p, "/home/u")
	if err != nil {
		t.Fatal(err)
	}

	second, err := Merge(first.Output, p, "/home/u")
	if err != nil {
		t.Fatal(err)
	}

	if second.Changed || len(second.Added)+len(second.Updated)+len(second.Removed) != 0 {
		t.Errorf("second run changed something: %+v", second)
	}
	if string(second.Output) != string(first.Output) {
		t.Errorf("second run rewrote the file:\n%s\n---\n%s", first.Output, second.Output)
	}
	if got := strings.Join(second.Unchanged, ","); got != "aide-work-pi" {
		t.Errorf("unchanged = %s", got)
	}
}

func TestMerge_UserTargetOwningTheDirWins(t *testing.T) {
	cfg := `targets:
  claude-work:
    agent: claude
    config_dir: /home/u/.claude-work
  aide-work-pi:
    path: ~/somewhere
`
	p := plan(
		Target{Name: "aide-work-claude", Profile: "work", Agent: "claude", ConfigDir: "~/.claude-work"},
		Target{Name: "aide-work-pi", Profile: "work", Agent: "pi", ConfigDir: "~/.pi-work"},
	)

	res, err := Merge([]byte(cfg), p, "/home/u")
	if err != nil {
		t.Fatal(err)
	}

	if res.Changed || len(res.Added) != 0 {
		t.Errorf("merged despite conflicts: %+v\n%s", res, res.Output)
	}
	got := skipText(res.Conflicts)
	for _, want := range []string{
		"work/claude: ~/.claude-work is already the config_dir of your Skillshare target claude-work",
		"work/pi: your Skillshare target aide-work-pi is not an account target",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("conflicts lack %q:\n%s", want, got)
		}
	}
}

func TestMerge_EmptyFlowTargetsBecomeBlock(t *testing.T) {
	cfg := "sources:\n  skills: /s\ntargets: {}\n"
	p := plan(Target{Name: "aide-w-codex", Profile: "w", Agent: "codex", ConfigDir: "~/.codex-w"})

	res, err := Merge([]byte(cfg), p, "/home/u")
	if err != nil {
		t.Fatal(err)
	}

	want := "targets:\n  aide-w-codex:\n    agent: codex\n    config_dir: ~/.codex-w\n"
	if !strings.Contains(string(res.Output), want) {
		t.Errorf("output:\n%s\nwant block:\n%s", res.Output, want)
	}
}

func TestMerge_RefusesNonMappingDocument(t *testing.T) {
	if _, err := Merge([]byte("- a\n- b\n"), plan(), "/home/u"); err == nil {
		t.Error("Merge accepted a sequence document")
	}
	if _, err := Merge([]byte("targets: [a]\n"), plan(), "/home/u"); err == nil {
		t.Error("Merge accepted a sequence targets:")
	}
}

func TestConfigPath(t *testing.T) {
	home := "/home/u"
	for name, tc := range map[string]struct {
		env  map[string]string
		want string
	}{
		"default":  {nil, filepath.Join(home, ".config", "skillshare", "config.yaml")},
		"xdg":      {map[string]string{"XDG_CONFIG_HOME": "/x"}, "/x/skillshare/config.yaml"},
		"relative": {map[string]string{"XDG_CONFIG_HOME": "x"}, filepath.Join(home, ".config", "skillshare", "config.yaml")},
		"override": {map[string]string{"SKILLSHARE_CONFIG": "~/ss.yaml", "XDG_CONFIG_HOME": "/x"}, "/home/u/ss.yaml"},
	} {
		t.Run(name, func(t *testing.T) {
			got := ConfigPath(home, func(k string) string { return tc.env[k] })
			if got != tc.want {
				t.Errorf("ConfigPath = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestMerge_UpdatesAgentAndDirInOneRun(t *testing.T) {
	cfg := "targets:\n  aide-work-claude:\n    agent: codex\n    config_dir: ~/.wrong\n"
	p := plan(Target{Name: "aide-work-claude", Profile: "work", Agent: "claude", ConfigDir: "~/.claude-work"})

	res, err := Merge([]byte(cfg), p, "/home/u")
	if err != nil {
		t.Fatal(err)
	}

	want := "aide-work-claude:\n    agent: claude\n    config_dir: ~/.claude-work\n"
	if !strings.Contains(string(res.Output), want) {
		t.Errorf("output:\n%s\nwant:\n%s", res.Output, want)
	}
}
