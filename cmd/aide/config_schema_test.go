package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/approvalstore"
	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/output"
	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/trust"
	"github.com/jskswamy/aide/schemas"
)

func runConfigCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := configCmd()
	output.RegisterFlag(cmd)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func TestConfigSchemaShow(t *testing.T) {
	out, err := runConfigCmd(t, "schema", "show", "profile")
	if err != nil {
		t.Fatalf("schema show profile: %v\n%s", err, out)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}
	if got, _ := doc["$id"].(string); got != schemas.DefaultURL(schemas.KindProfile) {
		t.Errorf("$id = %q, want %q", got, schemas.DefaultURL(schemas.KindProfile))
	}

	if _, err := runConfigCmd(t, "schema", "show", "bogus"); err == nil {
		t.Error("expected error for unknown schema kind")
	}
}

func TestConfigSchemaExport(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)

	// Default export dir (~/.config/aide/schemas).
	out, err := runConfigCmd(t, "schema", "export", "--format", "json")
	if err != nil {
		t.Fatalf("schema export: %v\n%s", err, out)
	}
	var res schemaExportResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("unmarshal export result: %v\n%s", err, out)
	}
	wantDir := filepath.Join(xdg, "aide", "schemas")
	if res.Dir != wantDir {
		t.Errorf("Dir = %q, want %q", res.Dir, wantDir)
	}
	for _, k := range schemas.AllKinds() {
		p := res.Files[string(k)]
		if _, err := os.Stat(p); err != nil {
			t.Errorf("exported schema %s missing at %q: %v", k, p, err)
		}
	}

	// Custom --dir.
	customDir := filepath.Join(t.TempDir(), "my schemas")
	if _, err := runConfigCmd(t, "schema", "export", "--dir", customDir); err != nil {
		t.Fatalf("schema export --dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(customDir, "profile.schema.json")); err != nil {
		t.Errorf("custom export missing profile.schema.json: %v", err)
	}
}

func TestConfigSchemaStamp_DefaultLocalForceAndProjectTrust(t *testing.T) {
	xdg := t.TempDir()
	stateDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("XDG_STATE_HOME", stateDir)
	t.Setenv("XDG_DATA_HOME", stateDir)

	cfgDir := filepath.Join(xdg, "aide")
	profDir := filepath.Join(cfgDir, "profiles")
	if err := os.MkdirAll(profDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(cfgDir, "config.yaml")
	workPath := filepath.Join(profDir, "work.yaml")
	sbxPath := filepath.Join(cfgDir, "sandbox.yaml")

	if err := os.WriteFile(cfgPath, []byte("default_profile: work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workPath, []byte("agent: claude\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	customLine := "# yaml-language-server: $schema=file:///keep-me.json"
	if err := os.WriteFile(sbxPath, []byte(customLine+"\nnever_allow:\n  - ~/.ssh\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// 1. Default stamp: stamps config.yaml & profiles/work.yaml, skips sandbox.yaml.
	out, err := runConfigCmd(t, "schema", "stamp", "--format", "json")
	if err != nil {
		t.Fatalf("schema stamp: %v\n%s", err, out)
	}
	var res schemaStampResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("unmarshal stamp result: %v", err)
	}
	if len(res.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d: %+v", len(res.Entries), res.Entries)
	}
	gotWork, _ := os.ReadFile(workPath)
	if !strings.HasPrefix(string(gotWork), schemas.Modeline(schemas.DefaultURL(schemas.KindProfile))+"\n") {
		t.Errorf("work.yaml not stamped with default URL:\n%s", gotWork)
	}
	gotSbx, _ := os.ReadFile(sbxPath)
	if !strings.HasPrefix(string(gotSbx), customLine+"\n") {
		t.Errorf("sandbox.yaml custom header was overwritten without --force:\n%s", gotSbx)
	}

	// 2. --local --force replaces headers with percent-encoded file:// URIs.
	localDir := filepath.Join(t.TempDir(), "local schemas")
	if _, err := runConfigCmd(t, "schema", "stamp", "--local", "--dir", localDir, "--force"); err != nil {
		t.Fatalf("schema stamp --local --force: %v", err)
	}
	wantLocalWork := schemas.Modeline(schemas.FileURI(filepath.Join(localDir, "profile.schema.json"))) + "\n"
	gotWorkLocal, _ := os.ReadFile(workPath)
	if !strings.HasPrefix(string(gotWorkLocal), wantLocalWork) {
		t.Errorf("work.yaml missing local file:// URI:\ngot:  %s\nwant: %s", gotWorkLocal, wantLocalWork)
	}

	// 3. --project preserves trust on trusted .aide.yaml and does NOT auto-trust untrusted one.
	projDir := t.TempDir()
	projFile := filepath.Join(projDir, config.ProjectConfigFileName)
	if err := os.WriteFile(projFile, []byte("agent: claude\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := trust.DefaultStore()
	if info, err := store.CheckFile(projFile); err != nil {
		t.Fatal(err)
	} else if err := store.Trust(info.Path, info.Contents); err != nil {
		t.Fatal(err)
	}

	origWD, _ := os.Getwd()
	if err := os.Chdir(projDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	if _, err := runConfigCmd(t, "schema", "stamp", "--project"); err != nil {
		t.Fatalf("schema stamp --project: %v", err)
	}
	gotProj, _ := os.ReadFile(projFile)
	if !strings.HasPrefix(string(gotProj), schemas.Modeline(schemas.DefaultURL(schemas.KindProject))+"\n") {
		t.Errorf(".aide.yaml not stamped:\n%s", gotProj)
	}
	afterTrust, err := store.CheckFile(projFile)
	if err != nil {
		t.Fatal(err)
	}
	if afterTrust.Status != trust.Trusted {
		t.Errorf("trusted .aide.yaml became %v after stamp --project, want Trusted", afterTrust.Status)
	}

	// Verify only the exact stamped bytes were trusted (replacement settings stay Untrusted).
	if store.Check(afterTrust.Path, append(gotProj, []byte("yolo: true\n")...)) != trust.Untrusted {
		t.Error("modified replacement contents should not be trusted")
	}

	// Untrusted .aide.yaml must remain Untrusted after stamp --project.
	untrustedDir := t.TempDir()
	untrustedFile := filepath.Join(untrustedDir, config.ProjectConfigFileName)
	if err := os.WriteFile(untrustedFile, []byte("agent: claude\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(untrustedDir); err != nil {
		t.Fatal(err)
	}
	if _, err := runConfigCmd(t, "schema", "stamp", "--project"); err != nil {
		t.Fatalf("schema stamp --project (untrusted): %v", err)
	}
	if check, err := store.CheckFile(untrustedFile); err != nil || check.Status != trust.Untrusted {
		t.Errorf("untrusted .aide.yaml became (%v, %v), want Untrusted", check.Status, err)
	}
}

func TestConfigSchemaStamp_ProjectTrustWriteFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod 0o500 directory permissions")
	}

	xdg := t.TempDir()
	stateDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("XDG_STATE_HOME", stateDir)
	t.Setenv("XDG_DATA_HOME", stateDir)

	projDir := t.TempDir()
	projFile := filepath.Join(projDir, config.ProjectConfigFileName)
	if err := os.WriteFile(projFile, []byte("agent: claude\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := trust.DefaultStore()
	info, err := store.CheckFile(projFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Trust(info.Path, info.Contents); err != nil {
		t.Fatal(err)
	}

	// Make the trust store directory read-only so restoring trust fails after stamping.
	trustDir := filepath.Join(approvalstore.DefaultRoot(), "trust")
	if err := os.Chmod(trustDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(trustDir, 0o700) })

	origWD, _ := os.Getwd()
	if err := os.Chdir(projDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	_, err = runConfigCmd(t, "schema", "stamp", "--project")
	if err == nil {
		t.Fatal("expected error when restoring trust fails, got nil")
	}
	if !strings.Contains(err.Error(), "failed to restore trust") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestConfigSchemaStamp_RehashesProvisioningState(t *testing.T) {
	home := t.TempDir()
	xdg := filepath.Join(home, ".config")
	stateDir := filepath.Join(home, ".local", "state")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("XDG_STATE_HOME", stateDir)

	cfgDir := filepath.Join(xdg, "aide")
	profDir := filepath.Join(cfgDir, "profiles")
	if err := os.MkdirAll(profDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(cfgDir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("default_profile: work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profDir, "work.yaml"), []byte("agent: claude\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	oldHash, err := provision.ConfigHash(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	statePath := provision.DefaultStatePath(home)
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatal(err)
	}
	stateJSON := `{"version":1,"contexts":{"work":{"config_hash":"` + oldHash + `"},"drifted":{"config_hash":"sha256:stale"}}}`
	if err := os.WriteFile(statePath, []byte(stateJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := runConfigCmd(t, "schema", "stamp"); err != nil {
		t.Fatalf("schema stamp: %v", err)
	}

	newHash, err := provision.ConfigHash(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if newHash == oldHash {
		t.Fatal("expected config hash to change after stamping")
	}
	st, err := provision.LoadState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Contexts["work"].ConfigHash; got != newHash {
		t.Errorf("in-sync profile hash = %q, want %q", got, newHash)
	}
	if got := st.Contexts["drifted"].ConfigHash; got != "sha256:stale" {
		t.Errorf("drifted profile hash = %q, want sha256:stale", got)
	}

	// Verify HashConfigFiles matches ConfigHash for single-file configs too.
	singleDir := t.TempDir()
	singleCfg := filepath.Join(singleDir, "config.yaml")
	singleBytes := []byte("default_profile: default\n")
	if err := os.WriteFile(singleCfg, singleBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	wantSingle, err := provision.ConfigHash(singleCfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := provision.HashConfigFiles([]string{"config.yaml"}, map[string][]byte{"config.yaml": singleBytes}); got != wantSingle {
		t.Errorf("HashConfigFiles single-file = %q, want %q", got, wantSingle)
	}
}
