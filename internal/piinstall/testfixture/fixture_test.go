package testfixture

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteCreatesFileAndParentDirs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile", "nested", "auth.json")
	Write(t, path, "PRIVATE", 0o600)

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Write did not create file: %v", err)
	}
	if string(got) != "PRIVATE" {
		t.Errorf("content = %q, want %q", got, "PRIVATE")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o, want 0600", info.Mode().Perm())
	}
}

func TestLinkCreatesSymlinkAndParentDirs(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, "install")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "bin", "pi")

	Link(t, target, path)

	got, err := os.Readlink(path)
	if err != nil {
		t.Fatalf("Link did not create symlink: %v", err)
	}
	if got != target {
		t.Errorf("symlink target = %q, want %q", got, target)
	}
}

func TestNewBuildsDefaultLayout(t *testing.T) {
	f := New(t)

	if f.Agent != filepath.Join(f.Home, ".pi", "agent") {
		t.Errorf("Agent = %q, want %q", f.Agent, filepath.Join(f.Home, ".pi", "agent"))
	}
	if f.Root != filepath.Join(f.Agent, "install") {
		t.Errorf("Root = %q, want %q", f.Root, filepath.Join(f.Agent, "install"))
	}
	if f.Launcher != filepath.Join(f.Agent, "bin", "pi") {
		t.Errorf("Launcher = %q, want %q", f.Launcher, filepath.Join(f.Agent, "bin", "pi"))
	}
	if f.Release != filepath.Join(f.Root, "releases", "1.4.0") {
		t.Errorf("Release = %q, want %q", f.Release, filepath.Join(f.Root, "releases", "1.4.0"))
	}

	info, err := os.Stat(f.Launcher)
	if err != nil {
		t.Fatalf("launcher missing: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("launcher mode %o is not executable", info.Mode().Perm())
	}

	raw, err := os.ReadFile(filepath.Join(f.Root, "managed-install.json"))
	if err != nil {
		t.Fatalf("marker missing: %v", err)
	}
	var marker map[string]any
	if err := json.Unmarshal(raw, &marker); err != nil {
		t.Fatalf("marker is not JSON: %v", err)
	}
	if marker["kind"] != "pi-managed-install" {
		t.Errorf("marker kind = %v, want pi-managed-install", marker["kind"])
	}

	version, err := os.ReadFile(filepath.Join(f.Root, "current-version"))
	if err != nil || string(version) != "1.4.0\n" {
		t.Errorf("current-version = %q, %v; want %q", version, err, "1.4.0\n")
	}

	cli := filepath.Join(f.Release, "node_modules", "@earendil-works", "pi-coding-agent", "dist", "cli.js")
	if _, err := os.Stat(cli); err != nil {
		t.Errorf("release cli.js missing: %v", err)
	}
	link := filepath.Join(f.Release, "node_modules", ".bin", "pi")
	got, err := os.Readlink(link)
	if err != nil {
		t.Fatalf(".bin/pi symlink missing: %v", err)
	}
	if got != "../@earendil-works/pi-coding-agent/dist/cli.js" {
		t.Errorf(".bin/pi target = %q", got)
	}
}

func TestNodeAddsRuntimeAndCurrentLink(t *testing.T) {
	dataHome := t.TempDir()
	root := Node(t, dataHome)

	want := filepath.Join(dataHome, "pi-node", "node-v22.19.0")
	if root != want {
		t.Errorf("Node root = %q, want %q", root, want)
	}
	if _, err := os.Stat(filepath.Join(root, "bin", "node")); err != nil {
		t.Errorf("node binary missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "lib", "node_modules", "npm", "bin", "npm-cli.js")); err != nil {
		t.Errorf("npm-cli.js missing: %v", err)
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(dataHome, "pi-node", "current"))
	if err != nil {
		t.Fatalf("current link missing: %v", err)
	}
	// t.TempDir may contain symlinked components (/var → /private/var on
	// macOS); Node returns the path it was given, so compare resolved forms.
	wantRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != wantRoot {
		t.Errorf("current resolves to %q, want %q", resolved, wantRoot)
	}
}
