// Package main tests for aide CLI commands.
package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/jskswamy/aide/internal/output"
)

func TestSecretsList_JSONFormat_Empty(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)

	cmd := secretsCmd()
	output.RegisterFlag(cmd)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"list", "--format", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\noutput: %s", err, buf.String())
	}

	var got []secretsFileEntry
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, buf.String())
	}
	if got == nil {
		t.Error("expected [], got JSON null")
	}
}

func TestSecretsKeys_JSONFormat_NotFound(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)

	cmd := secretsCmd()
	output.RegisterFlag(cmd)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"keys", "nope", "--format", "json"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for missing secrets file")
	}
}
