package approvalstore_test

import "os"

// readFile is a small helper that lets the Sub test verify nested
// placement without importing os in the main test file.
func readFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}
