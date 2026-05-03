package launcher

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

// RuntimeDir manages an ephemeral directory for aide's runtime files.
type RuntimeDir struct {
	path    string
	pid     int
	once    sync.Once
	cleaned bool
}

// NewRuntimeDir creates a new runtime directory at
// $XDG_RUNTIME_DIR/aide-<pid>/ with mode 0700.
// Falls back to os.TempDir() if XDG_RUNTIME_DIR is not set.
func NewRuntimeDir() (*RuntimeDir, error) {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		base = os.TempDir()
	}

	pid := os.Getpid()
	dirPath := filepath.Join(base, fmt.Sprintf("aide-%d", pid))

	// If it already exists (stale from PID wraparound), remove it first
	if _, err := os.Stat(dirPath); err == nil {
		if err := os.RemoveAll(dirPath); err != nil {
			return nil, fmt.Errorf("failed to remove existing runtime dir %s: %w", dirPath, err)
		}
	}

	if err := os.MkdirAll(dirPath, 0700); err != nil {
		return nil, fmt.Errorf("failed to create runtime dir %s: %w", dirPath, err)
	}

	// Verify permissions.
	// #nosec G302 -- 0700 required for owner to traverse directory
	if err := os.Chmod(dirPath, 0700); err != nil {
		return nil, fmt.Errorf("failed to set permissions on runtime dir %s: %w", dirPath, err)
	}

	return &RuntimeDir{
		path: dirPath,
		pid:  pid,
	}, nil
}

// Path returns the runtime directory path.
func (r *RuntimeDir) Path() string {
	return r.path
}

// Cleanup removes the runtime directory and all its contents.
// Safe to call multiple times.
func (r *RuntimeDir) Cleanup() error {
	var cleanupErr error
	r.once.Do(func() {
		cleanupErr = os.RemoveAll(r.path)
		r.cleaned = true
	})
	return cleanupErr
}

// RegisterSignalHandlers sets up signal handlers for SIGTERM, SIGINT,
// SIGQUIT, and SIGHUP that trigger Cleanup before exit.
// Returns a cancel function to deregister the handlers.
func (r *RuntimeDir) RegisterSignalHandlers() context.CancelFunc {
	ctx, cancel := context.WithCancel(context.Background())
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGHUP)

	go func() {
		select {
		case sig := <-sigCh:
			if err := r.Cleanup(); err != nil {
				log.Printf("aide: failed to clean runtime dir on signal: %v", err)
			}
			signal.Stop(sigCh)
			// Re-raise signal for default behavior
			p, _ := os.FindProcess(os.Getpid())
			_ = p.Signal(sig)
		case <-ctx.Done():
			signal.Stop(sigCh)
		}
	}()

	return cancel
}

// CleanStale removes any leftover aide-* directories in
// $XDG_RUNTIME_DIR that belong to processes that no longer exist.
// Called on startup to handle SIGKILL edge cases.
func CleanStale() error {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		base = os.TempDir()
	}
	return CleanStaleIn(base)
}

// CleanStaleIn removes stale aide-* directories from the given base directory.
func CleanStaleIn(base string) error {
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read base dir %s: %w", base, err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "aide-") {
			continue
		}
		pidStr := strings.TrimPrefix(name, "aide-")
		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			continue // not a valid aide runtime dir
		}

		if isProcessAlive(pid) {
			continue
		}

		dirPath := filepath.Join(base, name)
		if err := os.RemoveAll(dirPath); err != nil {
			log.Printf("aide: failed to clean stale runtime dir %s: %v", dirPath, err)
		}
	}

	return nil
}

// isProcessAlive checks if a process with the given PID is still running.
func isProcessAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0 checks if the process exists without actually sending a signal
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}
