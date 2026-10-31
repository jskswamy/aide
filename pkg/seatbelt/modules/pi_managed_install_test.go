package modules

import (
	"github.com/jskswamy/aide/pkg/seatbelt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestPiAgentInstallationAncestorMetadata(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "custom", "agent", "install")
	launcher := filepath.Join(home, "custom", "agent", "bin", "pi")
	ctx := &seatbelt.Context{HomeDir: home, Env: []string{"PI_CODING_AGENT_DIR=" + filepath.Join(home, ".pi-work")}}
	result := PiAgentWithInstallation(root, []string{launcher}).Rules(ctx)
	rules := rulesToString(result.Rules)
	for _, parent := range []string{filepath.Dir(root), filepath.Dir(filepath.Dir(home))} {
		want := `(allow file-read-metadata (literal "` + parent + `"))`
		if !strings.Contains(rules, want) {
			t.Errorf("missing ancestor traversal %s in rules", parent)
		}
		if slices.Contains(result.Readable, parent) || slices.Contains(result.Writable, parent) {
			t.Errorf("metadata traversal widened content access: %s", parent)
		}
	}
}

func TestPiAgentWithInstallation(t *testing.T) {
	home := t.TempDir()
	work := filepath.Join(home, ".pi-work")
	root := filepath.Join(home, ".pi", "agent", "install")
	launcher := filepath.Join(home, ".pi", "agent", "bin", "pi")
	node := filepath.Join(home, "data", "pi-node", "node-v22")
	ctx := &seatbelt.Context{HomeDir: home, Env: []string{"PI_CODING_AGENT_DIR=" + work}}
	m := PiAgentWithInstallation(root, []string{launcher, node})
	result := m.Rules(ctx)
	rules := rulesToString(result.Rules)
	if !strings.Contains(rules, root) || !strings.Contains(rules, work) || !strings.Contains(rules, launcher) {
		t.Fatalf("missing rules: %s", rules)
	}
	if !slices.Contains(result.Writable, root) {
		t.Errorf("missing root: %v", result.Writable)
	}
	for _, path := range []string{launcher, node, filepath.Dir(root), home} {
		if slices.Contains(result.Writable, path) {
			t.Errorf("unexpected writable path: %s", path)
		}
	}
	if !slices.Contains(result.Readable, launcher) || !slices.Contains(result.Readable, node) {
		t.Errorf("missing readable: %v", result.Readable)
	}
	keys, ok := m.(seatbelt.EnvKeyProvider)
	if !ok || !slices.Contains(keys.AgentEnvKeys(ctx), "PI_CODING_AGENT_DIR") {
		t.Fatal("lost Pi environment key")
	}
	if strings.Contains(rulesToString(PiAgent().Rules(ctx).Rules), root) {
		t.Fatal("non-managed Pi changed")
	}
}
