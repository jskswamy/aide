package approvalstore_test

import (
	"path/filepath"
	"testing"

	"github.com/jskswamy/aide/internal/approvalstore"
)

func TestStoreSubNests(t *testing.T) {
	root := t.TempDir()
	parent := approvalstore.NewStore(root)
	child := parent.Sub("trust")
	if child == nil {
		t.Fatal("Sub returned nil")
	}
	if err := child.Add("abc", []byte("body")); err != nil {
		t.Fatalf("child.Add: %v", err)
	}
	// File must land under root/trust/
	want := filepath.Join(root, "trust", "abc")
	if _, err := readSubFile(want); err != nil {
		t.Errorf("expected %s: %v", want, err)
	}
}

func readSubFile(path string) ([]byte, error) {
	return readFile(path)
}
