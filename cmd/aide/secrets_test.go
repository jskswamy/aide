// Package main tests for aide CLI commands.
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/config"
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

func TestSecretsList_RecipientsError_Surfaced(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)

	secretsDir := config.SecretsDirFrom(xdg)
	if err := os.MkdirAll(secretsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Not a valid sops-encrypted file, so secrets.ListRecipients fails
	// to parse it — this is the "corrupted/unreadable .enc.yaml"
	// diagnostic case this test exists to cover.
	badPath := filepath.Join(secretsDir, "bad.enc.yaml")
	if err := os.WriteFile(badPath, []byte("not a valid sops file\n"), 0o644); err != nil {
		t.Fatal(err)
	}

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
	if len(got) != 1 {
		t.Fatalf("expected 1 entry, got %+v", got)
	}
	if got[0].Error == "" {
		t.Errorf("expected Error to be set for unparseable file, got %+v", got[0])
	}
	if len(got[0].Recipients) != 0 {
		t.Errorf("expected no Recipients when Error is set, got %+v", got[0])
	}

	// Human mode must show the error line, not render as zero recipients.
	buf.Reset()
	cmd = secretsCmd()
	output.RegisterFlag(cmd)
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\noutput: %s", err, buf.String())
	}
	if !strings.Contains(buf.String(), "Recipients: (error:") {
		t.Errorf("expected human output to show recipients error, got:\n%s", buf.String())
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
