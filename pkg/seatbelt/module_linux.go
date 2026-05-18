//go:build linux

package seatbelt

// LinuxPathProvider lets the active AgentModule seed Landlock's explicit
// allow-list (Landlock is deny-by-default with no rule analogue of Seatbelt
// guards). Guards must not implement this — they still go through
// Rules/Protected/Allowed so deny-wins is preserved.
type LinuxPathProvider interface {
	LinuxReadable(ctx *Context) []string
	LinuxWritable(ctx *Context) []string
}

// LinuxAtomicWriteProvider lets agents declare files updated via
// open-tmp + rename when their parent directory must stay protected.
// The Linux backend overlays each parent with a tmpfs and bind-mounts only
// the listed files, so $HOME can hold ~/.ssh/~/.aws/etc. and still permit
// atomic rewrites of, e.g., ~/.claude.json.
type LinuxAtomicWriteProvider interface {
	LinuxAtomicWritableFiles(ctx *Context) []string
}
