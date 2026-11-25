package testutil

import (
	"path/filepath"
	"testing"
)

func TestNormalizeGolden_ReplacesRootAndSymlinkForm(t *testing.T) {
	d := t.TempDir()
	resolved, err := filepath.EvalSymlinks(d)
	if err != nil {
		t.Fatal(err)
	}
	got := NormalizeGolden(t, d+"/a "+resolved+"/b", map[string]string{d + "/": "$HOME"})
	if got != "$HOME/a $HOME/b" {
		t.Errorf("NormalizeGolden = %q, want %q", got, "$HOME/a $HOME/b")
	}
}
