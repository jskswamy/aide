package fsutil

// ResolveWidening reports how a capability path classifies once
// symlinks are followed: (resolved target, did it differ from the
// input, does the resolved target live under home).
//
// The function exists because two unrelated callers — the seatbelt
// filesystem guard that emits widening rules and the `aide cap show`
// audit view that annotates them — were independently composing
// EvalSymlinks + an under-$HOME check, and the compositions had
// already drifted (guard used ResolveOrSelf, cap show used
// EvalSymlinks directly). Diverging here is a knowledge-duplication
// risk: when the safety floor moves the next time (escape hatch for
// outside-$HOME paths), only one site is likely to be updated and
// the audit display will silently diverge from the actual sandbox.
//
// The function is pure: it does NOT tilde-expand path. Callers
// passing user-supplied "~/..." strings must run homepath.Expand
// first. This keeps fsutil free of path-semantics dependencies,
// matching the convention established by CheckSymlinkCycle and
// ResolveOrSelf.
//
// changed is false (and underHome ignored) when EvalSymlinks errors
// (ENOENT, EACCES, ELOOP, …) or returns the input unchanged. That
// is, "no symlink to follow" and "broken or unreachable symlink"
// collapse into the same "leave the literal rule alone" outcome —
// callers should not emit a widening rule for them. Cycle handling
// is intentionally not a hard error here; cycle detection during
// config validation is a separate concern (see CheckSymlinkCycle).
//
// underHome is always false when home is empty. Callers that want
// a "no home, no safety floor" mode should handle the empty-home
// case explicitly upstream.
func ResolveWidening(path, home string) (resolved string, changed, underHome bool) {
	resolved = ResolveOrSelf(path)
	if resolved == path {
		return path, false, home != "" && IsUnderDir(path, home)
	}
	underHome = home != "" && IsUnderDir(resolved, home)
	return resolved, true, underHome
}
