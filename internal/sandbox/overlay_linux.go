//go:build linux

package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// overlayLayout describes the on-host directory tree used by the OverlayFS-
// based atomic-write overlay. Each launch gets its own layout under
// runtimeDir; the directories are deleted after the agent exits and the
// post-exit sync-back step has copied allow-listed writes back to $HOME.
type overlayLayout struct {
	Root  string // runtimeDir/overlay
	Lower string // runtimeDir/overlay/lower  — synthetic lower; populated by bwrap binds
	Upper string // runtimeDir/overlay/upper  — accumulates writes
	Work  string // runtimeDir/overlay/work   — overlayfs scratch
}

// setupOverlayLayout creates the four host-side directories and pre-creates
// stub files / dirs in `Lower` matching each atomic-writable file and each
// readable file that lives under $HOME. bwrap's --bind options then mount
// the real source on top of these stubs inside its namespace; overlayfs
// uses Lower as a read-only lower layer.
//
// Files outside $HOME (e.g. /tmp, /var/tmp, /etc/...) are not part of the
// overlay — they're bind-mounted at their real paths separately, alongside
// the overlay mount.
func setupOverlayLayout(runtimeDir, homeDir string, atomicFiles, readableFiles []string) (overlayLayout, error) {
	layout := overlayLayout{
		Root:  filepath.Join(runtimeDir, "overlay"),
		Lower: filepath.Join(runtimeDir, "overlay", "lower"),
		Upper: filepath.Join(runtimeDir, "overlay", "upper"),
		Work:  filepath.Join(runtimeDir, "overlay", "work"),
	}
	for _, d := range []string{layout.Lower, layout.Upper, layout.Work} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return overlayLayout{}, fmt.Errorf("create overlay dir %s: %w", d, err)
		}
	}
	// Pre-create stub regular files in Lower at the relative path each
	// declared file would occupy. bwrap's later --bind mounts will replace
	// these stubs (inside its namespace only — Lower stays empty on host).
	stubs := append([]string{}, atomicFiles...)
	stubs = append(stubs, readableFiles...)
	for _, p := range stubs {
		rel, ok := relUnder(homeDir, p)
		if !ok || !pathExists(p) {
			continue
		}
		dst := filepath.Join(layout.Lower, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
			return overlayLayout{}, fmt.Errorf("create stub parent %s: %w", filepath.Dir(dst), err)
		}
		if err := os.WriteFile(dst, nil, 0o600); err != nil {
			return overlayLayout{}, fmt.Errorf("create stub %s: %w", dst, err)
		}
	}
	return layout, nil
}

// buildOverlayBwrapArgs returns the bwrap arguments for the OverlayFS-based
// atomic-write overlay. The shape is:
//
//	--bind /                                 (host fs passthrough)
//	--proc /proc / --dev /dev
//	--bind <overlay-root> <overlay-root>     (expose the per-launch dirs)
//	--bind  <host-file>   <lower>/<rel>      (populate lower for each declared
//	--ro-bind ...                             readable/atomic file)
//	--overlay-src <lower>
//	--overlay <upper> <work> <home>          (mount overlay at $HOME)
//	--bind <host-dir>     <home>/<rel>       (writable dirs bound on top of
//	                                           the overlay — writes go to the
//	                                           real fs, no sync-back needed)
//	--ro-bind ...                            (readable dirs similarly)
//
// Only the atomic-writable FILES travel through the overlay+sync-back path;
// directories and non-$HOME paths take the direct bind-mount path so the
// overlay surface stays minimal.
func buildOverlayBwrapArgs(
	layout overlayLayout,
	homeDir string,
	atomicFiles, readablePaths, writablePaths []string,
	allowSubprocess bool,
	network NetworkMode,
) []string {
	args := []string{
		"--bind", "/", "/",
		"--proc", "/proc",
		"--dev", "/dev",
		"--bind", layout.Root, layout.Root,
	}

	// Populate Lower with bind-mounted versions of declared files under $HOME.
	// These are seen by overlayfs as the lower layer's contents.
	for _, f := range atomicFiles {
		rel, ok := relUnder(homeDir, f)
		if !ok || !pathExists(f) {
			continue
		}
		args = append(args, "--bind", f, filepath.Join(layout.Lower, rel))
	}
	for _, p := range readablePaths {
		rel, ok := relUnder(homeDir, p)
		if !ok || !pathExists(p) {
			continue
		}
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		// Files go into lower (read-only via ro-bind); directories bypass
		// the overlay and are bound directly on top of $HOME after the
		// overlay is mounted (see post-overlay loop below).
		if !info.IsDir() {
			args = append(args, "--ro-bind", p, filepath.Join(layout.Lower, rel))
		}
	}

	// Mount overlayfs at $HOME with the synthetic lower.
	args = append(args,
		"--overlay-src", layout.Lower,
		"--overlay", layout.Upper, layout.Work, homeDir,
	)

	// Bind writable directories DIRECTLY on top of the overlay at $HOME.
	// Writes go straight to the real fs; no sync-back is needed for these.
	// Atomic-rename within these directories is fine — they're not on
	// per-file bind mounts, so rename(2) doesn't cross mount points.
	for _, w := range writablePaths {
		rel, ok := relUnder(homeDir, w)
		if !ok || !pathExists(w) {
			continue
		}
		info, err := os.Stat(w)
		if err != nil || !info.IsDir() {
			continue
		}
		args = append(args, "--bind", w, filepath.Join(homeDir, rel))
	}
	// Same for readable directories — direct ro-bind, not via the overlay.
	for _, r := range readablePaths {
		rel, ok := relUnder(homeDir, r)
		if !ok || !pathExists(r) {
			continue
		}
		info, err := os.Stat(r)
		if err != nil || !info.IsDir() {
			continue
		}
		args = append(args, "--ro-bind", r, filepath.Join(homeDir, rel))
	}

	// Bind non-$HOME paths at their natural locations (system tools, etc.).
	for _, w := range writablePaths {
		if _, ok := relUnder(homeDir, w); ok {
			continue
		}
		if !pathExists(w) {
			continue
		}
		args = append(args, "--bind", w, w)
	}
	for _, r := range readablePaths {
		if _, ok := relUnder(homeDir, r); ok {
			continue
		}
		if !pathExists(r) {
			continue
		}
		args = append(args, "--ro-bind", r, r)
	}

	if network == NetworkNone {
		args = append(args, "--unshare-net")
	}
	if !allowSubprocess {
		args = append(args, "--unshare-pid")
	}
	return args
}

// relUnder returns (rel, true) when child is under parent; "" / false when
// it is not. Both paths are filepath.Clean'd first; symlinks are not
// resolved — the caller is expected to have done so already.
func relUnder(parent, child string) (string, bool) {
	if parent == "" || child == "" {
		return "", false
	}
	parent = filepath.Clean(parent)
	child = filepath.Clean(child)
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return "", false
	}
	if rel == "." || strings.HasPrefix(rel, "..") {
		return "", false
	}
	return rel, true
}
