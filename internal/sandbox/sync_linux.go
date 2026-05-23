//go:build linux

package sandbox

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// RunSandboxSync is the __sandbox-sync re-exec target. It runs the wrapped
// child command (which is the bwrap+Landlock+agent chain), waits for it to
// exit, then synchronises allow-listed paths from the overlay's upper layer
// back to the host. Argument shape:
//
//	__sandbox-sync --upper UPPER --home HOMEDIR
//	               --sync-file PATH [--sync-file PATH...]
//	               --                                       (separator)
//	               <child-cmd> [args...]
//
// Sync runs even when the child exits non-zero: the agent may have done
// useful writes before crashing. After sync, the overlay directories
// (upper, work, lower, and their parent) are removed. The exit code
// propagates the child's.
func RunSandboxSync(args []string) error {
	var (
		upperDir   string
		homeDir    string
		overlayDir string
		syncFiles  []string
	)
	i := 0
parseLoop:
	for i < len(args) {
		switch args[i] {
		case "--upper":
			if i+1 >= len(args) {
				return fmt.Errorf("--upper requires a value")
			}
			upperDir = args[i+1]
			i += 2
		case "--home":
			if i+1 >= len(args) {
				return fmt.Errorf("--home requires a value")
			}
			homeDir = args[i+1]
			i += 2
		case "--overlay-root":
			if i+1 >= len(args) {
				return fmt.Errorf("--overlay-root requires a value")
			}
			overlayDir = args[i+1]
			i += 2
		case "--sync-file":
			if i+1 >= len(args) {
				return fmt.Errorf("--sync-file requires a value")
			}
			syncFiles = append(syncFiles, args[i+1])
			i += 2
		case "--":
			i++
			break parseLoop
		default:
			return fmt.Errorf("unknown sandbox-sync argument: %q", args[i])
		}
	}
	childArgs := args[i:]
	if len(childArgs) == 0 {
		return fmt.Errorf("no child command after --")
	}

	cmd := exec.Command(childArgs[0], childArgs[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	childErr := cmd.Run()

	if upperDir != "" && homeDir != "" {
		for _, f := range syncFiles {
			if err := syncOverlayFile(upperDir, homeDir, f); err != nil {
				fmt.Fprintf(os.Stderr, "aide: sync-back failed for %s: %v\n", f, err)
			}
		}
	}
	if overlayDir != "" {
		_ = os.RemoveAll(overlayDir)
	}

	if childErr != nil {
		if exitErr, ok := childErr.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		return childErr
	}
	return nil
}

// syncOverlayFile atomically copies upperDir/<rel> to hostPath, where rel
// is hostPath's path relative to homeDir. If the agent did not touch the
// file (no entry in upper), this is a no-op. The copy is write-tmp+rename
// so a crash mid-copy leaves hostPath untouched.
func syncOverlayFile(upperDir, homeDir, hostPath string) error {
	rel, ok := relUnder(homeDir, hostPath)
	if !ok {
		return nil
	}
	src := filepath.Join(upperDir, rel)
	st, err := os.Stat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat upper: %w", err)
	}
	if !st.Mode().IsRegular() {
		return nil
	}

	in, err := os.Open(src) // #nosec G304 -- src is a path we constructed
	if err != nil {
		return fmt.Errorf("open upper: %w", err)
	}
	defer func() { _ = in.Close() }()

	tmp := hostPath + ".aide-sync.tmp"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create sync tmp: %w", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("copy to sync tmp: %w", err)
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("fsync sync tmp: %w", err)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close sync tmp: %w", err)
	}
	if err := os.Rename(tmp, hostPath); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename sync tmp: %w", err)
	}
	return nil
}
