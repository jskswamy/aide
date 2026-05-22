//go:build linux

package seatbelt

// LinuxAtomicWriteProvider lets agents declare files updated via
// open-tmp + rename when their parent directory must stay protected.
// The Linux backend overlays each parent with a tmpfs and bind-mounts only
// the listed files, so $HOME can hold ~/.ssh/~/.aws/etc. and still permit
// atomic rewrites of, e.g., ~/.claude.json.
type LinuxAtomicWriteProvider interface {
	LinuxAtomicWritableFiles(ctx *Context) []string
}
