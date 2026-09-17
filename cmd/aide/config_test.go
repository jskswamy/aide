package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jskswamy/aide/internal/output"
)

func TestConfigShow_JSONFormat(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	cfgDir := filepath.Join(xdg, "aide")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte("default_context: work\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := configCmd()
	output.RegisterFlag(cmd)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"show", "--format", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\noutput: %s", err, buf.String())
	}

	var got configShowResult
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, buf.String())
	}
	if !got.Exists {
		t.Error("expected Exists=true")
	}
	if got.Raw != "default_context: work\n" {
		t.Errorf("Raw = %q", got.Raw)
	}
}

func TestConfigShow_JSONFormat_NotExist(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	cmd := configCmd()
	output.RegisterFlag(cmd)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"show", "--format", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\noutput: %s", err, buf.String())
	}

	var got configShowResult
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, buf.String())
	}
	if got.Exists {
		t.Error("expected Exists=false")
	}
}
