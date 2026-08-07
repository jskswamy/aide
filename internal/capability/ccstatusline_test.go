package capability

import (
	"os"
	"path/filepath"
	"testing"
)

func writeCcstatuslineSettings(t *testing.T, homeDir string) {
	t.Helper()
	dir := filepath.Join(homeDir, ".config", "ccstatusline")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAutoIncludeCcstatusline_AddsWhenSettingsFileExists(t *testing.T) {
	homeDir := t.TempDir()
	writeCcstatuslineSettings(t, homeDir)
	got := AutoIncludeCcstatusline([]string{"k8s"}, nil, homeDir)
	if len(got) != 2 || got[0] != "k8s" || got[1] != "ccstatusline" {
		t.Errorf("got %v, want [k8s ccstatusline]", got)
	}
}

func TestAutoIncludeCcstatusline_NoOpWhenSettingsFileMissing(t *testing.T) {
	homeDir := t.TempDir()
	got := AutoIncludeCcstatusline([]string{"k8s"}, nil, homeDir)
	if len(got) != 1 || got[0] != "k8s" {
		t.Errorf("got %v, want [k8s]", got)
	}
}

func TestAutoIncludeCcstatusline_NoOpWhenAlreadyPresent(t *testing.T) {
	homeDir := t.TempDir()
	writeCcstatuslineSettings(t, homeDir)
	got := AutoIncludeCcstatusline([]string{"ccstatusline"}, nil, homeDir)
	if len(got) != 1 || got[0] != "ccstatusline" {
		t.Errorf("got %v, want [ccstatusline] (not duplicated)", got)
	}
}

func TestAutoIncludeCcstatusline_RespectsExplicitExclusion(t *testing.T) {
	homeDir := t.TempDir()
	writeCcstatuslineSettings(t, homeDir)
	got := AutoIncludeCcstatusline([]string{"k8s"}, []string{"ccstatusline"}, homeDir)
	if len(got) != 1 || got[0] != "k8s" {
		t.Errorf("got %v, want [k8s] (explicit --without honored)", got)
	}
}
