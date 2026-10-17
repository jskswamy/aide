package fsutil

// SetCaseInsensitiveDirs lets tests exercise both filesystem kinds.
func SetCaseInsensitiveDirs(v bool) (restore func()) {
	prev := caseInsensitiveDirs
	caseInsensitiveDirs = v
	return func() { caseInsensitiveDirs = prev }
}
