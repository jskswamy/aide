package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/jskswamy/aide/internal/trust"
)

// aide trust <path> approves a named file, keyed on its
// symlink-resolved path, so trust given through a symlink holds for
// the real file.
func TestTrustCmdTakesPathAndResolvesSymlinks(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	realDir := filepath.Join(dir, "main")
	if err := os.Mkdir(realDir, 0o750); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(realDir, ".aide.yaml")
	if err := os.WriteFile(file, []byte("capabilities: [go]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(realDir, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	cmd := trustCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{filepath.Join(link, ".aide.yaml")})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("aide trust: %v", err)
	}
	fc, err := trust.DefaultStore().CheckFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if fc.Status != trust.Trusted {
		t.Errorf("status = %v, want trusted", fc.Status)
	}

	deny := denyCmd()
	deny.SetOut(&out)
	deny.SetArgs([]string{file})
	if err := deny.Execute(); err != nil {
		t.Fatalf("aide deny: %v", err)
	}
	fc, _ = trust.DefaultStore().CheckFile(filepath.Join(link, ".aide.yaml"))
	if fc.Status != trust.Denied {
		t.Errorf("status after deny = %v, want denied", fc.Status)
	}
}
