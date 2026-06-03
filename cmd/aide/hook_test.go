package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
)

func TestHookListEmpty(t *testing.T) {
	fakeProvReset(t)
	dir := isolatedConfigDir(t)
	cwd, _ := os.Getwd()
	body := "contexts:\n" +
		"  work:\n" +
		"    agent: fakeagent\n" +
		"    match:\n" +
		"      - path: " + cwd + "\n"
	if err := os.WriteFile(filepath.Join(dir, "xdg", "aide", "config.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	cmd := hookListCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--context", "work"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\n%s", err, buf.String())
	}
	out := buf.String()
	if !strings.Contains(out, "(no hooks declared)") {
		t.Errorf("missing '(no hooks declared)' in output:\n%s", out)
	}
	if !strings.Contains(out, "Context: work") {
		t.Errorf("missing 'Context: work' in output:\n%s", out)
	}
}

func TestIsValidEvent(t *testing.T) {
	valid := []string{"pre_tool", "post_tool", "session_start", "session_end", "notification", "stop"}
	for _, e := range valid {
		if !isValidEvent(e) {
			t.Errorf("isValidEvent(%q) = false, want true", e)
		}
	}
	if isValidEvent("invalid") {
		t.Error("isValidEvent(\"invalid\") = true, want false")
	}
}

func TestHookKey(t *testing.T) {
	k1 := provision.HookKey("pre_tool", "shell", "rtk hook agent")
	k2 := provision.HookKey("pre_tool", "shell", "rtk hook agent")
	k3 := provision.HookKey("pre_tool", "", "rtk hook agent")
	if k1 != k2 {
		t.Error("same inputs should produce same key")
	}
	if k1 == k3 {
		t.Error("different matcher should produce different key")
	}
	// Verify pipe in event doesn't collide with separator.
	k4 := provision.HookKey("pre_tool|shell", "", "cmd")
	k5 := provision.HookKey("pre_tool", "shell", "cmd")
	if k4 == k5 {
		t.Error("pipe in event should not collide with separator")
	}
}

func TestRenderHookTableEmpty(t *testing.T) {
	out := &bytes.Buffer{}
	renderHookTable(out, nil, nil)
	if !strings.Contains(out.String(), "(no hooks declared)") {
		t.Errorf("expected '(no hooks declared)', got: %s", out.String())
	}
}

func TestHookAddRejectsMetacharacters(t *testing.T) {
	fakeProvReset(t)
	dir := isolatedConfigDir(t)
	cwd, _ := os.Getwd()
	body := "contexts:\n  work:\n    agent: fakeagent\n    match:\n      - path: " + cwd + "\n"
	if err := os.WriteFile(filepath.Join(dir, "xdg", "aide", "config.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := hookAddCmd()
	cmd.SetArgs([]string{"--event", "pre_tool", "--command", "rtk hook; rm -rf ~"})
	err := cmd.Execute()
	if err == nil {
		t.Error("expected error for command with shell metacharacters")
	}
}

func TestHookAddRejectsDuplicate(t *testing.T) {
	fakeProvReset(t)
	dir := isolatedConfigDir(t)
	cwd, _ := os.Getwd()
	body := "contexts:\n  work:\n    agent: fakeagent\n    match:\n      - path: " + cwd + "\n"
	cfgPath := filepath.Join(dir, "xdg", "aide", "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd1 := hookAddCmd()
	cmd1.SetArgs([]string{"--context", "work", "--event", "pre_tool", "--command", "rtk hook fakeagent"})
	if err := cmd1.Execute(); err != nil {
		t.Fatalf("first add failed: %v", err)
	}

	cmd2 := hookAddCmd()
	cmd2.SetArgs([]string{"--context", "work", "--event", "pre_tool", "--command", "rtk hook fakeagent"})
	if err := cmd2.Execute(); err == nil {
		t.Error("expected error on duplicate hook add, got nil")
	}
}

func TestRenderHookTableWithHooks(t *testing.T) {
	out := &bytes.Buffer{}
	hooks := []provision.Hook{
		{Event: "pre_tool", Matcher: "shell", Command: "rtk hook claude"},
		{Event: "session_start", Matcher: "", Command: "bd prime"},
	}
	managed := []provision.ManagedHook{
		{Event: "pre_tool", Matcher: "shell", Command: "rtk hook claude"},
	}
	renderHookTable(out, hooks, managed)
	s := out.String()
	if !strings.Contains(s, "pre_tool") {
		t.Errorf("expected pre_tool in output: %s", s)
	}
	if !strings.Contains(s, "✓") {
		t.Errorf("expected managed ✓ in output: %s", s)
	}
}
