//go:build linux

package seatbelt

// LinuxPathProvider lets the active AgentModule contribute its Linux
// filesystem grants to Landlock's explicit allow-list (Landlock is
// deny-by-default with no rule analogue of Seatbelt's profile language —
// it needs typed path lists, not opaque rule strings). The Linux sandbox
// pipeline routes this through EvaluateGuards as a synthetic GuardResult
// so the paths land in GrantedPathSet via the same path as every other
// path-vouching evaluator, picking up audit (OriginGuard) and conflict
// detection along the way.
//
// Guards must not implement this. Guards express their decisions through
// GuardResult's Protected/Readable/Writable fields directly.
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
