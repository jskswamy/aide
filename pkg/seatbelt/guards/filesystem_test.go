package guards_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

func TestFilesystem_WritablePaths(t *testing.T) {
	tmp := t.TempDir()
	dir1 := filepath.Join(tmp, "writable1")
	dir2 := filepath.Join(tmp, "writable2")
	if err := os.Mkdir(dir1, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir2, 0o755); err != nil {
		t.Fatal(err)
	}

	m := guards.Filesystem(guards.FilesystemConfig{
		Writable: []string{dir1, dir2},
	})

	if m.Name() != "Filesystem" {
		t.Errorf("expected Name() = %q, got %q", "Filesystem", m.Name())
	}

	output := renderTestRules(m.Rules(nil))

	if !strings.Contains(output, "(allow file-read* file-write*") {
		t.Error("expected allow file-read* file-write* block")
	}
	if !strings.Contains(output, `(subpath "`+dir1+`")`) {
		t.Errorf("expected subpath for %s", dir1)
	}
	if !strings.Contains(output, `(subpath "`+dir2+`")`) {
		t.Errorf("expected subpath for %s", dir2)
	}
}

func TestFilesystem_ReadablePaths(t *testing.T) {
	tmp := t.TempDir()
	dir1 := filepath.Join(tmp, "readonly")
	if err := os.Mkdir(dir1, 0o755); err != nil {
		t.Fatal(err)
	}

	m := guards.Filesystem(guards.FilesystemConfig{
		Readable: []string{dir1},
	})

	output := renderTestRules(m.Rules(nil))

	// Should have file-read* but NOT file-write*
	if !strings.Contains(output, "(allow file-read*") {
		t.Error("expected allow file-read* block")
	}
	if !strings.Contains(output, `(subpath "`+dir1+`")`) {
		t.Errorf("expected subpath for %s", dir1)
	}
	// Ensure the read block doesn't also have write
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "file-read*") && strings.Contains(line, "file-write*") {
			t.Error("readable paths should not include file-write*")
		}
	}
}

func TestFilesystem_DeniedPaths(t *testing.T) {
	tmp := t.TempDir()
	file1 := filepath.Join(tmp, "secret.txt")
	if err := os.WriteFile(file1, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := guards.Filesystem(guards.FilesystemConfig{
		Denied: []string{file1},
	})

	output := renderTestRules(m.Rules(nil))

	if !strings.Contains(output, "(deny file-read-data") {
		t.Error("expected deny file-read-data for denied path")
	}
	if !strings.Contains(output, "(deny file-write*") {
		t.Error("expected deny file-write* for denied path")
	}
	if !strings.Contains(output, `(literal "`+file1+`")`) {
		t.Errorf("expected literal for file %s", file1)
	}
}

func TestFilesystem_GlobExpansion(t *testing.T) {
	tmp := t.TempDir()
	// Create files matching a glob
	if err := os.WriteFile(filepath.Join(tmp, "a.env"), []byte("A=1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "b.env"), []byte("B=2"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := guards.Filesystem(guards.FilesystemConfig{
		Denied: []string{filepath.Join(tmp, "*.env")},
	})

	output := renderTestRules(m.Rules(nil))

	if !strings.Contains(output, "a.env") {
		t.Error("expected expanded glob to include a.env")
	}
	if !strings.Contains(output, "b.env") {
		t.Error("expected expanded glob to include b.env")
	}
}

func TestFilesystem_MixedConfig(t *testing.T) {
	tmp := t.TempDir()
	wdir := filepath.Join(tmp, "work")
	rdir := filepath.Join(tmp, "docs")
	denied := filepath.Join(tmp, "secret.key")
	if err := os.Mkdir(wdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(rdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(denied, []byte("key"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := guards.Filesystem(guards.FilesystemConfig{
		Writable: []string{wdir},
		Readable: []string{rdir},
		Denied:   []string{denied},
	})

	output := renderTestRules(m.Rules(nil))

	if !strings.Contains(output, "(allow file-read* file-write*") {
		t.Error("expected writable block")
	}
	if !strings.Contains(output, `(subpath "`+wdir+`")`) {
		t.Error("expected writable dir path")
	}
	if !strings.Contains(output, `(subpath "`+rdir+`")`) {
		t.Error("expected readable dir path")
	}
	if !strings.Contains(output, "(deny file-read-data") {
		t.Error("expected deny block")
	}
}

func TestGuard_Filesystem_Metadata(t *testing.T) {
	g := guards.FilesystemGuard()

	if g.Name() != "filesystem" {
		t.Errorf("expected Name() = %q, got %q", "filesystem", g.Name())
	}
	if g.Type() != "always" {
		t.Errorf("expected Type() = %q, got %q", "always", g.Type())
	}
	if g.Description() == "" {
		t.Error("expected non-empty Description()")
	}
}

func TestGuard_Filesystem_CtxPaths(t *testing.T) {
	tmp := t.TempDir()
	project := filepath.Join(tmp, "project")
	home := filepath.Join(tmp, "home")
	runtime := filepath.Join(tmp, "runtime")
	denied := filepath.Join(tmp, "secret.key")

	for _, d := range []string{project, home, runtime} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(denied, []byte("key"), 0o644); err != nil {
		t.Fatal(err)
	}

	g := guards.FilesystemGuard()
	ctx := &seatbelt.Context{
		ProjectRoot: project,
		HomeDir:     home,
		RuntimeDir:  runtime,
		ExtraDenied: []string{denied},
	}
	output := renderTestRules(g.Rules(ctx))

	if !strings.Contains(output, "(allow file-read* file-write*") {
		t.Error("expected writable block for ProjectRoot/RuntimeDir")
	}
	if !strings.Contains(output, `(subpath "`+project+`")`) {
		t.Errorf("expected ProjectRoot %s in output", project)
	}
	if !strings.Contains(output, "(allow file-read*") {
		t.Error("expected readable block for HomeDir")
	}
	if !strings.Contains(output, "(deny file-read-data") {
		t.Error("expected deny block for ExtraDenied")
	}
	if !strings.Contains(output, `(literal "`+denied+`")`) {
		t.Errorf("expected denied path %s in output", denied)
	}
}
