// Package testfixture builds local installer-shaped trees, never a real Pi install.
package testfixture

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Fixture names the paths of a test-owned managed installation.
type Fixture struct{ Home, Agent, Root, Launcher, Release string }

// Write creates fixture contents and any missing parent directories.
func Write(t *testing.T, path, text string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), mode); err != nil {
		t.Fatal(err)
	}
}

// Link creates a fixture symlink and its parent directory.
func Link(t *testing.T, target, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}

// New builds the default installer layout beneath a canonical temporary HOME.
func New(t *testing.T) Fixture {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return At(t, home, filepath.Join(home, ".pi", "agent"))
}

// At builds an installer layout beneath explicitly test-owned paths.
func At(t *testing.T, home, agent string) Fixture {
	t.Helper()
	f := Fixture{Home: home, Agent: agent, Root: filepath.Join(agent, "install"), Launcher: filepath.Join(agent, "bin", "pi")}
	f.Release = filepath.Join(f.Root, "releases", "1.4.0")
	_, source, _, _ := runtime.Caller(0)
	launcher, err := os.ReadFile(filepath.Join(filepath.Dir(source), "..", "testdata", "launcher.sh"))
	if err != nil {
		t.Fatal(err)
	}
	Write(t, f.Launcher, string(launcher), 0755)
	marker, _ := json.Marshal(map[string]any{"kind": "pi-managed-install", "schemaVersion": 1, "layout": "releases-v1", "entrypoint": map[string]string{"type": "script", "path": f.Launcher}})
	Write(t, filepath.Join(f.Root, "managed-install.json"), string(marker), 0600)
	Write(t, filepath.Join(f.Root, "current-version"), "1.4.0\n", 0600)
	Write(t, filepath.Join(f.Release, "node_modules", "@earendil-works", "pi-coding-agent", "dist", "cli.js"), "#!/bin/sh\necho 1.4.0\n", 0755)
	Link(t, "../@earendil-works/pi-coding-agent/dist/cli.js", filepath.Join(f.Release, "node_modules", ".bin", "pi"))
	return f
}

// Node adds an installer-shaped runtime and returns its versioned root.
func Node(t *testing.T, dataHome string) string {
	t.Helper()
	root := filepath.Join(dataHome, "pi-node", "node-v22.19.0")
	Write(t, filepath.Join(root, "bin", "node"), "#!/bin/sh\nexit 0\n", 0755)
	Write(t, filepath.Join(root, "lib", "node_modules", "npm", "bin", "npm-cli.js"), "fixture npm\n", 0644)
	Link(t, root, filepath.Join(dataHome, "pi-node", "current"))
	return root
}
