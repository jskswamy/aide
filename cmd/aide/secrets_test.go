// Package main tests for aide CLI commands.
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"filippo.io/age"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/output"
	"github.com/jskswamy/aide/internal/secrets"
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

func setupTestEditorScript(t *testing.T) {
	t.Helper()
	binDir := t.TempDir()
	editorScript := filepath.Join(binDir, "fake-editor.sh")
	script := "#!/bin/sh\nprintf 'api_key: test-secret\\n' > \"$1\"\n"
	if err := os.WriteFile(editorScript, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EDITOR", editorScript)
}

func TestSecretsCreate_WithoutAgeKeyFlag(t *testing.T) {
	setupTestEditorScript(t)

	id1, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	id2, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}

	t.Run("single key auto-selected", func(t *testing.T) {
		home := t.TempDir()
		xdg := filepath.Join(home, ".config")
		t.Setenv("HOME", home)
		t.Setenv("XDG_CONFIG_HOME", xdg)
		t.Setenv("SOPS_AGE_KEY", id1.String())
		t.Setenv("SOPS_AGE_KEY_FILE", "")

		cmd := secretsCmd()
		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)
		cmd.SetArgs([]string{"create", "single"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute: %v\noutput: %s", err, buf.String())
		}
		if !strings.Contains(buf.String(), "Using age key: "+id1.Recipient().String()) {
			t.Errorf("expected 'Using age key' output, got:\n%s", buf.String())
		}
		encPath := filepath.Join(config.SecretsDirFrom(xdg), "single.enc.yaml")
		recs, err := secrets.ListRecipients(encPath)
		if err != nil || len(recs) != 1 || recs[0] != id1.Recipient().String() {
			t.Errorf("ListRecipients = %v, %v; want [%s]", recs, err, id1.Recipient().String())
		}
	})

	t.Run("multiple keys on TTY prompts and selects choice", func(t *testing.T) {
		home := t.TempDir()
		xdg := filepath.Join(home, ".config")
		t.Setenv("HOME", home)
		t.Setenv("XDG_CONFIG_HOME", xdg)
		t.Setenv("SOPS_AGE_KEY", id1.String()+"\n"+id2.String()+"\n")
		t.Setenv("SOPS_AGE_KEY_FILE", "")

		origTTY := isInputTTY
		isInputTTY = func(_ *cobra.Command) bool { return true }
		t.Cleanup(func() { isInputTTY = origTTY })

		cmd := secretsCmd()
		var buf bytes.Buffer
		cmd.SetIn(strings.NewReader("2\n"))
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)
		cmd.SetArgs([]string{"create", "multi"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute: %v\noutput: %s", err, buf.String())
		}
		if !strings.Contains(buf.String(), "Multiple age keys found:") {
			t.Errorf("expected multi-key prompt, got:\n%s", buf.String())
		}
		encPath := filepath.Join(config.SecretsDirFrom(xdg), "multi.enc.yaml")
		recs, err := secrets.ListRecipients(encPath)
		if err != nil || len(recs) != 1 || recs[0] != id2.Recipient().String() {
			t.Errorf("ListRecipients = %v, %v; want [%s]", recs, err, id2.Recipient().String())
		}
	})

	t.Run("zero keys on TTY generates key and encrypts", func(t *testing.T) {
		home := t.TempDir()
		xdg := filepath.Join(home, ".config")
		t.Setenv("HOME", home)
		t.Setenv("XDG_CONFIG_HOME", xdg)
		t.Setenv("SOPS_AGE_KEY", "")
		t.Setenv("SOPS_AGE_KEY_FILE", "")

		origTTY := isInputTTY
		isInputTTY = func(_ *cobra.Command) bool { return true }
		t.Cleanup(func() { isInputTTY = origTTY })

		cmd := secretsCmd()
		var buf bytes.Buffer
		cmd.SetIn(strings.NewReader("\n"))
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)
		cmd.SetArgs([]string{"create", "zerokey"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute: %v\noutput: %s", err, buf.String())
		}
		if !strings.Contains(buf.String(), "✓ Generated age key at") {
			t.Errorf("expected generated key message, got:\n%s", buf.String())
		}
		encPath := filepath.Join(config.SecretsDirFrom(xdg), "zerokey.enc.yaml")
		recs, err := secrets.ListRecipients(encPath)
		if err != nil || len(recs) != 1 {
			t.Fatalf("ListRecipients = %v, %v; want 1 recipient", recs, err)
		}
	})
}
