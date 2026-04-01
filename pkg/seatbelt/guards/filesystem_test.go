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

	g := guards.FilesystemGuard()
	ctx := &seatbelt.Context{
		ProjectRoot: dir1,
		RuntimeDir:  dir2,
	}

	if g.Name() != "filesystem" {
		t.Errorf("expected Name() = %q, got %q", "filesystem", g.Name())
	}

	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

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

func TestFilesystem_ScopedReadablePaths(t *testing.T) {
	g := guards.FilesystemGuard()
	ctx := &seatbelt.Context{
		HomeDir: "/Users/testuser",
	}

	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	// Should have narrow baseline paths, not broad $HOME read
	if !strings.Contains(output, "(allow file-read*") {
		t.Error("expected allow file-read* block")
	}
	if !strings.Contains(output, `"/Users/testuser/.config/aide"`) {
		t.Error("expected .config/aide subpath")
	}
}

func TestFilesystem_DeniedPaths(t *testing.T) {
	tmp := t.TempDir()
	file1 := filepath.Join(tmp, "secret.txt")
	if err := os.WriteFile(file1, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	g := guards.FilesystemGuard()
	ctx := &seatbelt.Context{
		ExtraDenied: []string{file1},
	}

	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

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

	g := guards.FilesystemGuard()
	ctx := &seatbelt.Context{
		ExtraDenied: []string{filepath.Join(tmp, "*.env")},
	}

	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

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
	denied := filepath.Join(tmp, "secret.key")
	if err := os.Mkdir(wdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(denied, []byte("key"), 0o644); err != nil {
		t.Fatal(err)
	}

	g := guards.FilesystemGuard()
	ctx := &seatbelt.Context{
		ProjectRoot: wdir,
		HomeDir:     "/Users/testuser",
		ExtraDenied: []string{denied},
	}

	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, "(allow file-read* file-write*") {
		t.Error("expected writable block")
	}
	if !strings.Contains(output, `(subpath "`+wdir+`")`) {
		t.Error("expected writable dir path")
	}
	if !strings.Contains(output, "(deny file-read-data") {
		t.Error("expected deny block")
	}
}

func TestFilesystemGuard_ExtraWritable(t *testing.T) {
	g := guards.FilesystemGuard()
	ctx := &seatbelt.Context{
		HomeDir:       "/Users/testuser",
		ProjectRoot:   "/project",
		ExtraWritable: []string{"/custom/writable"},
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, `"/custom/writable"`) {
		t.Error("expected /custom/writable in filesystem guard output")
	}
	if !strings.Contains(output, "file-write*") {
		t.Error("expected file-write* rule for writable path")
	}
}

func TestFilesystemGuard_ExtraReadable(t *testing.T) {
	g := guards.FilesystemGuard()
	ctx := &seatbelt.Context{
		HomeDir:       "/Users/testuser",
		ProjectRoot:   "/project",
		ExtraReadable: []string{"/custom/readable"},
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, `"/custom/readable"`) {
		t.Error("expected /custom/readable in filesystem guard output")
	}
	// ExtraReadable produces individual allow rules
	if !strings.Contains(output, "(allow file-read*") {
		t.Error("expected file-read* rule for extra readable path")
	}
}

func TestFilesystemGuard_ScopedHomeReads(t *testing.T) {
	g := guards.FilesystemGuard()
	ctx := &seatbelt.Context{
		HomeDir:     "/Users/testuser",
		ProjectRoot: "/project",
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	// Should NOT have broad $HOME read
	if strings.Contains(output, `(subpath "/Users/testuser")`) &&
		!strings.Contains(output, `(subpath "/Users/testuser/`) {
		t.Error("should NOT have broad $HOME subpath read")
	}

	// Should have narrow baseline paths
	narrowPaths := []string{
		`"/Users/testuser/.config/aide"`,
		`"/Users/testuser/.local/share/aide"`,
		`"/Users/testuser/.cache"`,
		`"/Users/testuser/Library/Caches"`,
	}
	for _, p := range narrowPaths {
		if !strings.Contains(output, p) {
			t.Errorf("expected narrow baseline path %s in output", p)
		}
	}

	// Should NOT have broad dev paths
	broadPaths := []string{
		`"/Users/testuser/.ssh"`,
		`"/Users/testuser/.cargo"`,
		`"/Users/testuser/.gnupg"`,
	}
	for _, p := range broadPaths {
		if strings.Contains(output, p) {
			t.Errorf("should NOT have broad path %s in output", p)
		}
	}

	// Should NOT have dotfile regex
	if strings.Contains(output, "regex") {
		t.Error("should NOT have regex rule for home dotfiles")
	}

	// Project root should still be writable
	if !strings.Contains(output, `"/project"`) {
		t.Error("expected project root in writable paths")
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
	runtime := filepath.Join(tmp, "runtime")
	denied := filepath.Join(tmp, "secret.key")

	for _, d := range []string{project, runtime} {
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
		HomeDir:     "/Users/testuser",
		RuntimeDir:  runtime,
		ExtraDenied: []string{denied},
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, "(allow file-read* file-write*") {
		t.Error("expected writable block for ProjectRoot/RuntimeDir")
	}
	if !strings.Contains(output, `(subpath "`+project+`")`) {
		t.Errorf("expected ProjectRoot %s in output", project)
	}
	if !strings.Contains(output, "(deny file-read-data") {
		t.Error("expected deny block for ExtraDenied")
	}
	if !strings.Contains(output, `(literal "`+denied+`")`) {
		t.Errorf("expected denied path %s in output", denied)
	}
}

func TestFilesystem_ExtraAllow(t *testing.T) {
	g := guards.FilesystemGuard()
	ctx := &seatbelt.Context{
		HomeDir:     "/Users/test",
		ProjectRoot: "/tmp/project",
		GOOS:        "darwin",
		ExtraAllow:  []string{"mach-lookup", "iokit-open", "signal"},
	}
	result := g.Rules(ctx)
	profile := renderTestRules(result.Rules)

	expected := []string{
		"(allow mach-lookup)",
		"(allow iokit-open)",
		"(allow signal)",
	}
	for _, want := range expected {
		if !strings.Contains(profile, want) {
			t.Errorf("expected %q in profile, got:\n%s", want, profile)
		}
	}
}

func TestFilesystem_ExtraAllow_Empty(t *testing.T) {
	g := guards.FilesystemGuard()
	ctx := &seatbelt.Context{
		HomeDir:     "/Users/test",
		ProjectRoot: "/tmp/project",
		GOOS:        "darwin",
	}
	result := g.Rules(ctx)
	profile := renderTestRules(result.Rules)

	if strings.Contains(profile, "Non-filesystem operations") {
		t.Error("should not emit non-filesystem section when ExtraAllow is empty")
	}
}

func TestFilesystemGuard_NarrowBaseline(t *testing.T) {
	g := guards.FilesystemGuard()
	ctx := &seatbelt.Context{
		HomeDir:     "/Users/testuser",
		ProjectRoot: "/project",
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	// Should have narrow baseline reads
	allowedPaths := []struct {
		path    string
		kind    string // "literal" or "subpath"
	}{
		{`(subpath "/Users/testuser/.config/aide")`, "aide config"},
		{`(subpath "/Users/testuser/.local/share/aide")`, "aide data"},
		{`(subpath "/Users/testuser/.cache")`, "cache dir"},
		{`(subpath "/Users/testuser/Library/Caches")`, "Library Caches"},
	}
	for _, ap := range allowedPaths {
		if !strings.Contains(output, ap.path) {
			t.Errorf("expected %s: %s", ap.kind, ap.path)
		}
	}

	// Should NOT have broad paths that are now removed
	broadPaths := []struct {
		path string
		desc string
	}{
		{`"/Users/testuser/.config"`, "broad .config"},
		{`"/Users/testuser/.local"`, "broad .local"},
		{`"/Users/testuser/.ssh"`, "broad .ssh"},
		{`"/Users/testuser/.cargo"`, "broad .cargo"},
		{`"/Users/testuser/.rustup"`, "broad .rustup"},
		{`"/Users/testuser/go"`, "broad go"},
		{`"/Users/testuser/.pyenv"`, "broad .pyenv"},
		{`"/Users/testuser/.rbenv"`, "broad .rbenv"},
		{`"/Users/testuser/.sdkman"`, "broad .sdkman"},
		{`"/Users/testuser/.gradle"`, "broad .gradle"},
		{`"/Users/testuser/.m2"`, "broad .m2"},
		{`"/Users/testuser/.gnupg"`, "broad .gnupg"},
		{`"/Users/testuser/.nix-profile"`, "broad .nix-profile"},
		{`"/Users/testuser/.nix-defexpr"`, "broad .nix-defexpr"},
		{`"/Users/testuser/Library/Keychains"`, "broad Library/Keychains"},
		{`"/Users/testuser/Library/Preferences"`, "broad Library/Preferences"},
	}
	for _, bp := range broadPaths {
		if strings.Contains(output, bp.path) {
			t.Errorf("should NOT have %s: %s", bp.desc, bp.path)
		}
	}

	// Should NOT have dotfile regex
	if strings.Contains(output, "regex") {
		t.Error("should NOT have regex rule for home dotfiles")
	}

	// Should still have home directory traversal
	if !strings.Contains(output, `(literal "/Users/testuser")`) {
		t.Error("expected home directory listing literal")
	}
	if !strings.Contains(output, `(allow file-read-metadata`) {
		t.Error("expected home metadata traversal")
	}
}
