//go:build (darwin || linux) && integration

package sandbox

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jskswamy/aide/internal/piinstall"
	"github.com/jskswamy/aide/internal/piinstall/testfixture"
	"github.com/jskswamy/aide/pkg/seatbelt/modules"
	"golang.org/x/sys/unix"
)

const piFixtureCLI = `#!/bin/sh
set -eu
case "${1:-}" in
 --version) echo VERSION ;;
 --probe-read) exec /bin/cat "$2" ;;
 --probe-write) printf 'write' >> "$2" ;;
 --node) exec node "$PI_MANAGED_INSTALL_ROOT/artifacts/npm-lib-path" ;;
 update|hold-update)
  root=$PI_MANAGED_INSTALL_ROOT
  mkdir "$root/update.lock" 2>/dev/null || exit 73
  trap 'rmdir "$root/update.lock"' EXIT
  if [ "$1" = hold-update ]; then
   echo locked
   read -r resume < "$FIXTURE_BARRIER"
  fi
  mkdir -p "$root/staging"
  stage=$(mktemp -d "$root/staging/update-XXXXXX")
  (cd "$stage" && npm ci)
  test "$("$stage/node_modules/.bin/pi" --version)" = 1.5.0
  mv "$stage" "$root/releases/1.5.0"
  printf '1.5.0\n' > "$root/current-version.tmp"
  mv "$root/current-version.tmp" "$root/current-version"
  rm -rf "$root/releases/1.3.0"
 ;;
 *) exit 2 ;;
esac
`

type piIntegration struct {
	f             testfixture.Fixture
	policy        Policy
	node, barrier string
}

func newPiIntegration(t *testing.T) piIntegration {
	t.Helper()
	// /tmp is broadly writable on macOS; fixtures there would hide isolation bugs.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(cwd, ".pi-managed-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, broad := range []string{"/private", "/tmp", "/usr", "/opt", "/nix"} {
		if dir == broad || strings.HasPrefix(dir, broad+"/") {
			t.Skipf("fixture path %s is covered by broad system grants", dir)
		}
	}
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0700); err != nil {
		t.Fatal(err)
	}
	f := testfixture.At(t, home, filepath.Join(home, ".pi", "agent"))
	node := testfixture.Node(t, filepath.Join(home, ".local", "share"))
	cli := filepath.Join(f.Release, "node_modules", "@earendil-works", "pi-coding-agent", "dist", "cli.js")
	testfixture.Write(t, cli, strings.ReplaceAll(piFixtureCLI, "VERSION", "1.4.0"), 0755)
	testfixture.Write(t, filepath.Join(f.Root, "artifacts", "pi-next"), strings.ReplaceAll(piFixtureCLI, "VERSION", "1.5.0"), 0755)
	testfixture.Write(t, filepath.Join(f.Root, "releases", "1.3.0", "old"), "stale", 0600)
	testfixture.Write(t, filepath.Join(node, "bin", "node"), "#!/bin/sh\nexec /bin/cat \"$1\"\n", 0755)
	testfixture.Write(t, filepath.Join(node, "bin", "npm"), `#!/bin/sh
set -eu
node "$(dirname "$0")/../lib/node_modules/npm/bin/npm-cli.js" > /dev/null
mkdir -p node_modules/.bin
cp "$PI_MANAGED_INSTALL_ROOT/artifacts/pi-next" node_modules/.bin/pi
chmod 755 node_modules/.bin/pi
`, 0755)
	testfixture.Write(t, filepath.Join(f.Root, "artifacts", "npm-lib-path"), "fixture Node read succeeded\n", 0600)
	paths := Paths{HomeDir: home, ProjectRoot: filepath.Join(dir, "project"), RuntimeDir: filepath.Join(dir, "runtime"), TempDir: filepath.Join(dir, "tmp")}
	for _, p := range []string{paths.ProjectRoot, paths.RuntimeDir, paths.TempDir, filepath.Join(home, ".pi-work")} {
		if err := os.MkdirAll(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	barrier := filepath.Join(paths.ProjectRoot, "barrier")
	env := []string{"HOME=" + home, "PATH=/usr/bin:/bin", "PI_CODING_AGENT_DIR=" + filepath.Join(home, ".pi-work"), "FIXTURE_BARRIER=" + barrier}
	p := DefaultPolicy(paths, env)
	p.AgentModule = modules.PiAgent()
	return piIntegration{f: f, policy: p, node: node, barrier: barrier}
}

func (f piIntegration) command(t *testing.T, backend string, args ...string) *exec.Cmd {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, f.f.Launcher, args...)
	cmd.Dir = f.policy.ProjectRoot
	cmd.Env = f.policy.Env
	in, err := piinstall.Discover(f.f.Launcher, f.f.Home, f.policy.Env, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidatePiManagedInstall(f.policy, *in, backend); err != nil {
		t.Fatal(err)
	}
	policy := f.policy
	policy.AgentModule = modules.PiAgentWithInstallation(in.Root, in.Readable)
	piManagedApply(t, cmd, policy, backend)
	return cmd
}

func TestPiManagedInstallStartup(t *testing.T) {
	for _, backend := range piManagedBackends() {
		t.Run(backend, func(t *testing.T) {
			f := newPiIntegration(t)
			out, err := f.command(t, backend, "--version").CombinedOutput()
			if err != nil || string(out) != "1.4.0\n" {
				t.Fatalf("startup: %v %s", err, out)
			}
			// The same launcher reached by a PATH-style symlink must still resolve install.
			alias := filepath.Join(f.f.Home, ".local", "bin", "pi")
			testfixture.Link(t, "../../.pi/agent/bin/pi", alias)
			linked := f
			linked.f.Launcher = alias
			out, err = linked.command(t, backend, "--version").CombinedOutput()
			if err != nil || string(out) != "1.4.0\n" {
				t.Fatalf("PATH startup: %v %s", err, out)
			}
			out, err = f.command(t, backend, "--node").CombinedOutput()
			if err != nil || !bytes.Contains(out, []byte("Node read succeeded")) {
				t.Fatalf("Node runtime: %v %s", err, out)
			}
		})
	}
}

func TestPiManagedInstallUpdate(t *testing.T) {
	for _, backend := range piManagedBackends() {
		t.Run(backend, func(t *testing.T) {
			f := newPiIntegration(t)
			out, err := f.command(t, backend, "update").CombinedOutput()
			if err != nil {
				t.Fatalf("update: %v %s", err, out)
			}
			out, err = f.command(t, backend, "--version").CombinedOutput()
			if err != nil || string(out) != "1.5.0\n" {
				t.Fatalf("new version: %v %s", err, out)
			}
			if _, err := os.Stat(filepath.Join(f.f.Root, "releases", "1.3.0")); !os.IsNotExist(err) {
				t.Fatal("stale release not pruned")
			}
			entries, err := os.ReadDir(filepath.Join(f.f.Root, "staging"))
			if err != nil || len(entries) != 0 {
				t.Fatalf("staging: %v %v", entries, err)
			}
		})
	}
}

func TestPiManagedInstallIsolation(t *testing.T) {
	for _, backend := range piManagedBackends() {
		t.Run(backend, func(t *testing.T) {
			f := newPiIntegration(t)
			for _, profile := range []string{filepath.Join(f.f.Home, ".pi-personal"), f.f.Agent} {
				for _, name := range []string{"auth.json", "settings.json", "sessions/private.json"} {
					path := filepath.Join(profile, name)
					testfixture.Write(t, path, "PRIVATE", 0600)
					for _, probe := range []string{"--probe-read", "--probe-write"} {
						out, err := f.command(t, backend, probe, path).CombinedOutput()
						if err == nil {
							t.Fatalf("private state accessible (%s): %s %s", probe, path, out)
						}
					}
				}
			}
			for _, path := range []string{f.f.Launcher, filepath.Join(f.node, "bin", "node"), filepath.Join(f.node, "lib", "node_modules", "npm", "bin", "npm-cli.js")} {
				out, err := f.command(t, backend, "--probe-write", path).CombinedOutput()
				if err == nil {
					t.Fatalf("bootstrap writable: %s %s", path, out)
				}
			}
			f.policy.Network = NetworkNone
			out, err := f.command(t, backend, "--version").CombinedOutput()
			if err != nil || string(out) != "1.4.0\n" {
				t.Fatalf("network-none startup: %v %s", err, out)
			}
			f.policy.AllowSubprocess = false
			out, err = f.command(t, backend, "update").CombinedOutput()
			if err == nil {
				t.Fatalf("subprocess restriction bypassed: %s", out)
			}
		})
	}
}

func TestPiManagedInstallUpdateLock(t *testing.T) {
	for _, backend := range piManagedBackends() {
		t.Run(backend, func(t *testing.T) {
			f := newPiIntegration(t)
			if err := unix.Mkfifo(f.barrier, 0600); err != nil {
				t.Fatal(err)
			}
			first := f.command(t, backend, "hold-update")
			stdout, err := first.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			var stderr bytes.Buffer
			first.Stderr = &stderr
			if err := first.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if first.Process != nil {
					_ = first.Process.Kill()
				}
			})
			line, err := bufio.NewReader(stdout).ReadString('\n')
			if err != nil || line != "locked\n" {
				t.Fatalf("lock setup: %v %q %s", err, line, stderr.String())
			}
			out, err := f.command(t, backend, "update").CombinedOutput()
			if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 73 {
				t.Fatalf("second update not locked: %v %s", err, out)
			}
			// O_RDWR prevents a failed child from leaving the test blocked opening FIFO.
			fifo, err := os.OpenFile(f.barrier, os.O_RDWR, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := fmt.Fprintln(fifo, "continue"); err != nil {
				t.Fatal(err)
			}
			_ = fifo.Close()
			if err := first.Wait(); err != nil {
				t.Fatalf("first update: %v %s", err, stderr.String())
			}
			if _, err := os.Stat(filepath.Join(f.f.Root, "update.lock")); !os.IsNotExist(err) {
				t.Fatal("update lock not released")
			}
		})
	}
}
