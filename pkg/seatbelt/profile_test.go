package seatbelt

import (
	"strings"
	"testing"
)

// testModule is a simple module for testing.
type testModule struct {
	name  string
	rules []Rule
}

func (m *testModule) Name() string { return m.name }
func (m *testModule) Rules(_ *Context) GuardResult {
	return GuardResult{Rules: m.rules}
}

func TestProfile_Render_EmptyProfile(t *testing.T) {
	p := New("/home/user")
	out, err := p.Render()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "" {
		t.Errorf("empty profile should render empty string, got %q", out)
	}
}

func TestProfile_Render_SingleModule(t *testing.T) {
	p := New("/home/user").Use(&testModule{
		name:  "test",
		rules: []Rule{AllowOp("process-exec")},
	})
	out, err := p.Render()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "(allow process-exec)") {
		t.Errorf("expected allow rule in output, got %q", out)
	}
	if !strings.Contains(out, "=== test ===") {
		t.Errorf("expected module name header, got %q", out)
	}
}

func TestProfile_Render_ModuleOrder(t *testing.T) {
	p := New("/home/user").Use(
		&testModule{name: "first", rules: []Rule{AllowOp("process-exec")}},
		&testModule{name: "second", rules: []Rule{AllowOp("process-fork")}},
	)
	out, err := p.Render()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	firstIdx := strings.Index(out, "first")
	secondIdx := strings.Index(out, "second")
	if firstIdx > secondIdx {
		t.Error("modules should render in Use() order")
	}
}

func TestProfile_WithContext(t *testing.T) {
	var captured Context
	captureModule := &contextCapture{captured: &captured}
	p := New("/home/user").
		WithContext(func(ctx *Context) {
			ctx.ProjectRoot = "/tmp/project"
			ctx.TempDir = "/tmp"
		}).
		Use(captureModule)
	_, _ = p.Render()
	if captured.HomeDir != "/home/user" {
		t.Errorf("expected HomeDir=/home/user, got %q", captured.HomeDir)
	}
	if captured.ProjectRoot != "/tmp/project" {
		t.Errorf("expected ProjectRoot=/tmp/project, got %q", captured.ProjectRoot)
	}
}

type contextCapture struct {
	captured *Context
}

func (c *contextCapture) Name() string { return "capture" }
func (c *contextCapture) Rules(ctx *Context) GuardResult {
	*c.captured = *ctx
	return GuardResult{}
}
