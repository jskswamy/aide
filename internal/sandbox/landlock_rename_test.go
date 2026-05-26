//go:build linux

package sandbox

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/landlock-lsm/go-landlock/landlock"
	"golang.org/x/sys/unix"
)

// dispatchLandlockRenameHelper runs the in-subprocess rename probe used by
// TestLandlock_RenameAcrossDirs_RequiresRefer. The probe applies the same
// Landlock configuration the production sandbox uses (V5.BestEffort plus an
// RWDirs rule on the project root) and then attempts a rename whose source
// and destination directories differ. Without LANDLOCK_ACCESS_FS_REFER the
// kernel returns synthetic EXDEV; with REFER granted the rename succeeds.
//
// We run inside a subprocess because Landlock layers are irreversible — once
// the test process is restricted, every subsequent test would inherit the
// restriction.
func dispatchLandlockRenameHelper() {
	root := os.Getenv("AIDE_TEST_LANDLOCK_RENAME_ROOT")
	if root == "" {
		return
	}

	rule := buildLandlockRenameRule(root)
	if err := landlock.V5.BestEffort().RestrictPaths(rule); err != nil {
		fmt.Printf("RESTRICT_FAIL:%v\n", err)
		os.Exit(2)
	}

	src := filepath.Join(root, "target/debug/deps/sub/full.rmeta")
	dst := filepath.Join(root, "target/debug/deps/libfake.rmeta")
	if err := os.Rename(src, dst); err != nil {
		var errno syscall.Errno
		if errors.As(err, &errno) {
			fmt.Printf("RENAME_FAIL:errno=%d (%s)\n", int(errno), unix.ErrnoName(errno))
			os.Exit(3)
		}
		fmt.Printf("RENAME_FAIL:%v\n", err)
		os.Exit(4)
	}
	fmt.Println("PASS")
	os.Exit(0)
}

// buildLandlockRenameRule must mirror the production rule shape in
// applyLandlock. The production rule is the source of truth — this helper
// is updated in lockstep when applyLandlock changes so the regression test
// reflects what production actually does.
func buildLandlockRenameRule(root string) landlock.FSRule {
	return landlock.RWDirs(root).WithRefer()
}

// TestLandlock_RenameAcrossDirs_RequiresRefer reproduces the cargo/rustc
// cross-directory rename pattern (e.g.
// target/debug/deps/rmetaXXX/full.rmeta → target/debug/deps/libfake.rmeta)
// in a Landlock-restricted subprocess. Without LANDLOCK_ACCESS_FS_REFER on
// the writable rule, every such rename fails with synthetic EXDEV even
// though source and destination share a filesystem; with REFER granted it
// succeeds.
func TestLandlock_RenameAcrossDirs_RequiresRefer(t *testing.T) {
	if os.Getenv("AIDE_TEST_LANDLOCK_RENAME_ROOT") != "" {
		t.Skip("subprocess helper invocation; primary assertion runs in parent")
	}
	if !landlockSupported(t) {
		t.Skip("Landlock not supported on this kernel")
	}

	root := t.TempDir()
	deepSub := filepath.Join(root, "target/debug/deps/sub")
	if err := os.MkdirAll(deepSub, 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	src := filepath.Join(deepSub, "full.rmeta")
	if err := os.WriteFile(src, []byte("rmeta"), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestLandlock_RenameAcrossDirs_RequiresRefer")
	cmd.Env = append(os.Environ(), "AIDE_TEST_LANDLOCK_RENAME_ROOT="+root)
	out, err := cmd.CombinedOutput()
	output := string(out)

	if err != nil {
		t.Fatalf("subprocess helper failed: %v\n  output: %s", err, output)
	}
	if !strings.Contains(output, "PASS") {
		t.Fatalf("rename across writable subdirs blocked by Landlock\n  output: %s", output)
	}
}

func landlockSupported(t *testing.T) bool {
	t.Helper()
	caps := DetectKernelCapabilities()
	return caps.LandlockABI > 0
}
