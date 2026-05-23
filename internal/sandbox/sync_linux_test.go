//go:build linux

package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestSyncOverlayFile_NoUpperEntryIsNoOp(t *testing.T) {
	homeDir := t.TempDir()
	upperDir := t.TempDir()
	target := filepath.Join(homeDir, ".claude.json")
	if err := os.WriteFile(target, []byte("v1"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if err := syncOverlayFile(upperDir, homeDir, target); err != nil {
		t.Fatalf("syncOverlayFile (no upper entry): %v", err)
	}
	got, _ := os.ReadFile(target) // #nosec G304 -- test path under TempDir
	if string(got) != "v1" {
		t.Errorf("host file should not change when upper has no entry; got %q", got)
	}
}

func TestSyncOverlayFile_CopiesUpperToHost(t *testing.T) {
	homeDir := t.TempDir()
	upperDir := t.TempDir()
	target := filepath.Join(homeDir, ".claude.json")
	if err := os.WriteFile(target, []byte("v1"), 0o600); err != nil {
		t.Fatalf("setup target: %v", err)
	}
	// Simulate the agent having written through the overlay: upper has the
	// new content at the matching relative path.
	upperPath := filepath.Join(upperDir, ".claude.json")
	if err := os.WriteFile(upperPath, []byte("v2-updated"), 0o600); err != nil {
		t.Fatalf("setup upper: %v", err)
	}

	if err := syncOverlayFile(upperDir, homeDir, target); err != nil {
		t.Fatalf("syncOverlayFile: %v", err)
	}
	got, err := os.ReadFile(target) // #nosec G304 -- test path under TempDir
	if err != nil {
		t.Fatalf("read host target after sync: %v", err)
	}
	if string(got) != "v2-updated" {
		t.Errorf("host target was not updated; got %q want v2-updated", got)
	}
}

func TestSyncOverlayFile_IgnoresPathsOutsideHome(t *testing.T) {
	homeDir := t.TempDir()
	upperDir := t.TempDir()
	// Path that's NOT under homeDir
	outside := filepath.Join(t.TempDir(), "elsewhere.json")
	if err := os.WriteFile(outside, []byte("untouched"), 0o600); err != nil {
		t.Fatalf("setup outside: %v", err)
	}

	if err := syncOverlayFile(upperDir, homeDir, outside); err != nil {
		t.Errorf("syncOverlayFile should silently ignore non-home paths; got: %v", err)
	}
	got, _ := os.ReadFile(outside) // #nosec G304 -- test path under TempDir
	if string(got) != "untouched" {
		t.Errorf("non-home file was unexpectedly modified; got %q", got)
	}
}

func TestSyncOverlayFile_IgnoresNonRegularUpperEntries(t *testing.T) {
	homeDir := t.TempDir()
	upperDir := t.TempDir()
	target := filepath.Join(homeDir, ".claude.json")
	if err := os.WriteFile(target, []byte("v1"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// Upper has a directory where the host expects a regular file — this
	// would happen if the agent did something unusual; the sync should not
	// follow it (could corrupt the host file).
	if err := os.MkdirAll(filepath.Join(upperDir, ".claude.json"), 0o700); err != nil {
		t.Fatalf("setup upper dir: %v", err)
	}

	if err := syncOverlayFile(upperDir, homeDir, target); err != nil {
		t.Fatalf("syncOverlayFile: %v", err)
	}
	got, _ := os.ReadFile(target) // #nosec G304 -- test path under TempDir
	if string(got) != "v1" {
		t.Errorf("host target should not change for non-regular upper; got %q", got)
	}
}

// TestRunSandboxSync_ExecsChildAndExits exercises the happy path of the
// __sandbox-sync entry point: it parses its flags, runs the child command
// successfully, and returns nil. No actual sync happens (upper not set).
func TestRunSandboxSync_ExecsChildAndExits(t *testing.T) {
	args := []string{"--", "/bin/true"}
	if err := RunSandboxSync(args); err != nil {
		t.Errorf("RunSandboxSync with /bin/true: %v", err)
	}
}

// TestRunSandboxSync_SyncsAfterChild runs a child that writes to the upper
// dir (mimicking what the agent does via overlay copy-up), then verifies
// the sync step copies the file out to the host.
func TestRunSandboxSync_SyncsAfterChild(t *testing.T) {
	homeDir := t.TempDir()
	upperDir := t.TempDir()
	target := filepath.Join(homeDir, ".claude.json")
	if err := os.WriteFile(target, []byte("v1"), 0o600); err != nil {
		t.Fatalf("setup target: %v", err)
	}
	upperPath := filepath.Join(upperDir, ".claude.json")
	// The "child" here simulates what the bwrap+agent chain does: writes
	// the post-rename content into upper. /bin/sh runs in the parent so
	// we don't need a sandbox to test the sync logic.
	args := []string{
		"--upper", upperDir,
		"--home", homeDir,
		"--sync-file", target,
		"--",
		"/bin/sh", "-c", "echo v2 > " + upperPath,
	}
	if err := RunSandboxSync(args); err != nil {
		t.Fatalf("RunSandboxSync: %v", err)
	}
	got, err := os.ReadFile(target) // #nosec G304 -- test path under TempDir
	if err != nil {
		t.Fatalf("read host target: %v", err)
	}
	if string(got) != "v2\n" {
		t.Errorf("host target was not updated by sync; got %q", got)
	}
}

func TestRunSandboxSync_RejectsUnknownFlag(t *testing.T) {
	if err := RunSandboxSync([]string{"--bogus"}); err == nil {
		t.Error("expected error for unknown flag, got nil")
	}
}

func TestRunSandboxSync_RejectsMissingFlagValues(t *testing.T) {
	cases := [][]string{
		{"--upper"},
		{"--home"},
		{"--overlay-root"},
		{"--sync-file"},
	}
	for _, c := range cases {
		if err := RunSandboxSync(c); err == nil {
			t.Errorf("expected error for missing value after %v, got nil", c)
		}
	}
}

func TestRunSandboxSync_RequiresChildCommand(t *testing.T) {
	if err := RunSandboxSync([]string{"--upper", "/tmp/x", "--home", "/tmp/y", "--"}); err == nil {
		t.Error("expected error when no child command, got nil")
	}
}

// TestRunSandboxSync_CleansOverlayRoot verifies the --overlay-root flag
// causes the directory tree to be removed after the child exits.
func TestRunSandboxSync_CleansOverlayRoot(t *testing.T) {
	overlayRoot := t.TempDir()
	scratch := filepath.Join(overlayRoot, "scratch-marker")
	if err := os.WriteFile(scratch, []byte("x"), 0o600); err != nil {
		t.Fatalf("setup marker: %v", err)
	}

	args := []string{
		"--overlay-root", overlayRoot,
		"--",
		"/bin/true",
	}
	if err := RunSandboxSync(args); err != nil {
		t.Fatalf("RunSandboxSync: %v", err)
	}
	if _, err := os.Stat(overlayRoot); !os.IsNotExist(err) {
		t.Errorf("overlay root should be removed after sync; stat err: %v", err)
	}
}

// TestRunSandboxSync_PropagatesChildExitCode (best-effort) verifies that a
// non-zero child exit causes RunSandboxSync to exit with that code. Since
// the function calls os.Exit on ExitError, we test in a sub-process.
func TestRunSandboxSync_PropagatesChildExitCode(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run", "^TestHelperSandboxSync_FailingChild$")
	cmd.Env = append(os.Environ(), "AIDE_SYNC_HELPER=fail")
	err := cmd.Run()
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected ExitError, got %v", err)
	}
	if exitErr.ExitCode() != 42 {
		t.Errorf("expected exit code 42, got %d", exitErr.ExitCode())
	}
}

// TestHelperSandboxSync_FailingChild is invoked as a subprocess by
// TestRunSandboxSync_PropagatesChildExitCode. It calls RunSandboxSync
// with a child that exits 42; the function should os.Exit(42).
func TestHelperSandboxSync_FailingChild(_ *testing.T) {
	if os.Getenv("AIDE_SYNC_HELPER") != "fail" {
		return
	}
	_ = RunSandboxSync([]string{"--", "/bin/sh", "-c", "exit 42"})
	os.Exit(99) // should not reach here
}
