package piinstall_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/piinstall"
	"github.com/jskswamy/aide/internal/piinstall/testfixture"
)

func TestDiscoverManagedPi(t *testing.T) {
	for _, name := range []string{"direct", "path-links", "custom", "node", "xdg-node", "ambient-root"} {
		t.Run(name, func(t *testing.T) {
			f := testfixture.New(t)
			binary := f.Launcher
			env := []string{"HOME=" + f.Home}
			var node string
			switch name {
			case "custom":
				f = testfixture.At(t, f.Home, filepath.Join(f.Home, "custom", "agent"))
				binary = f.Launcher
			case "path-links":
				second := filepath.Join(f.Home, "links", "pi")
				testfixture.Link(t, "../.pi/agent/bin/pi", second)
				binary = filepath.Join(f.Home, "bin", "pi")
				testfixture.Link(t, "../links/pi", binary)
			case "node":
				node = testfixture.Node(t, filepath.Join(f.Home, ".local", "share"))
			case "xdg-node":
				data := filepath.Join(f.Home, "data")
				node = testfixture.Node(t, data)
				env = append(env, "XDG_DATA_HOME="+data)
			case "ambient-root":
				env = append(env, "PI_MANAGED_INSTALL_ROOT=/unrelated")
			}
			got, err := piinstall.Discover(binary, f.Home, env, nil)
			if err != nil || got == nil {
				t.Fatalf("Discover: %v, %v", got, err)
			}
			if got.Root != f.Root {
				t.Fatalf("root=%q want %q", got.Root, f.Root)
			}
			if !slices.Contains(got.Readable, f.Launcher) {
				t.Errorf("launcher not readable: %v", got.Readable)
			}
			if node != "" && !slices.Contains(got.Readable, node) {
				t.Errorf("runtime not readable: %v", got.Readable)
			}
			for _, p := range got.Readable {
				if p == f.Home || p == f.Agent || p == filepath.Dir(f.Agent) {
					t.Errorf("broad grant: %s", p)
				}
			}
		})
	}
}

func TestDiscoverNonManaged(t *testing.T) {
	home := t.TempDir()
	binary := filepath.Join(home, "node_modules", "pi", "dist", "cli.js")
	testfixture.Write(t, binary, "#!/bin/sh\nexit 0\n", 0755)
	got, err := piinstall.Discover(binary, home, []string{"PI_MANAGED_INSTALL_ROOT=/unrelated"}, nil)
	if err != nil || got != nil {
		t.Fatalf("non-managed: %v %v", got, err)
	}
}

func TestDiscoverRejectsUnsafe(t *testing.T) {
	for _, name := range []string{"marker-json", "marker-schema", "marker-entrypoint", "unknown-launcher", "missing-version", "missing-release", "version-traversal", "marker-escape", "release-escape", "private-root", "private-descendant", "private-node", "link-cycle", "broken-launcher"} {
		t.Run(name, func(t *testing.T) {
			f := testfixture.New(t)
			binary := f.Launcher
			env := []string{"HOME=" + f.Home}
			var private []string
			replaceLink := func(path, target string) {
				t.Helper()
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				testfixture.Link(t, target, path)
			}
			switch name {
			case "marker-json":
				testfixture.Write(t, filepath.Join(f.Root, "managed-install.json"), "{", 0600)
			case "marker-schema":
				testfixture.Write(t, filepath.Join(f.Root, "managed-install.json"), `{"kind":"pi-managed-install","schemaVersion":2,"layout":"releases-v1"}`, 0600)
			case "marker-entrypoint":
				testfixture.Write(t, filepath.Join(f.Root, "managed-install.json"), `{"kind":"pi-managed-install","schemaVersion":1,"layout":"releases-v1"}`, 0600)
			case "unknown-launcher":
				testfixture.Write(t, binary, "#!/bin/sh\ntouch "+filepath.Join(f.Home, "executed")+"\n", 0755)
			case "missing-version":
				if err := os.Remove(filepath.Join(f.Root, "current-version")); err != nil {
					t.Fatal(err)
				}
			case "missing-release":
				if err := os.Remove(filepath.Join(f.Release, "node_modules", ".bin", "pi")); err != nil {
					t.Fatal(err)
				}
			case "version-traversal":
				testfixture.Write(t, filepath.Join(f.Root, "current-version"), "../private\n", 0600)
			case "marker-escape":
				p := filepath.Join(f.Home, "marker")
				testfixture.Write(t, p, `{}`, 0600)
				replaceLink(filepath.Join(f.Root, "managed-install.json"), p)
			case "release-escape":
				p := filepath.Join(f.Home, "outside")
				testfixture.Write(t, p, "#!/bin/sh\nexit 0\n", 0755)
				replaceLink(filepath.Join(f.Release, "node_modules", ".bin", "pi"), p)
			case "private-root":
				private = []string{f.Root}
			case "private-descendant":
				private = []string{filepath.Join(f.Root, "releases", "future-profile")}
			case "private-node":
				private = []string{testfixture.Node(t, filepath.Join(f.Home, ".local", "share"))}
			case "link-cycle":
				replaceLink(binary, binary)
			case "broken-launcher":
				replaceLink(binary, filepath.Join(f.Home, "missing"))
			}
			got, err := piinstall.Discover(binary, f.Home, env, private)
			if err == nil || got != nil {
				t.Fatalf("unsafe %s accepted: %v %v", name, got, err)
			}
			if !strings.Contains(err.Error(), binary) {
				t.Errorf("error lacks selected executable: %v", err)
			}
			if _, err := os.Stat(filepath.Join(f.Home, "executed")); !os.IsNotExist(err) {
				t.Fatal("discovery executed launcher")
			}
		})
	}
}
