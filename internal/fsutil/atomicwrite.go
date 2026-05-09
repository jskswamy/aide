// Package fsutil provides filesystem utility helpers shared across aide
// packages. The exported helpers pin durability semantics in one place so
// that callers cannot drift on permissions, parent-directory creation, or
// cleanup-on-failure behavior.
package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
)

// AtomicWrite writes data to path atomically.
//
// Steps:
//  1. Parent directories of path are created with mode 0o750 if missing.
//  2. A unique temporary file is created in the same directory.
//  3. The temporary file is chmodded to 0o600 and data is written.
//  4. The temporary file is renamed over path. On failure at any step
//     after creation, the temporary file is best-effort removed.
//
// Same-directory placement keeps the rename within one filesystem so it is
// an atomic inode swap on POSIX. The overwrite path always ends at 0o600
// regardless of the prior mode.
func AtomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("fsutil: creating parent directory: %w", err)
	}
	f, err := os.CreateTemp(dir, ".fsutil-*")
	if err != nil {
		return fmt.Errorf("fsutil: creating temp file: %w", err)
	}
	tmp := f.Name()
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("fsutil: chmod temp file: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("fsutil: writing temp file: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("fsutil: closing temp file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("fsutil: renaming temp file: %w", err)
	}
	return nil
}
