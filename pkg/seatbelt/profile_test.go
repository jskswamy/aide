package seatbelt

import (
	"strings"
	"testing"
)

// testModule is a simple module for testing.
type testModule struct {
	name   string
	rules  []Rule
	result GuardResult
}

func (m *testModule) Name() string { return m.name }
func (m *testModule) Rules(_ *Context) GuardResult {
	if len(m.result.Rules) > 0 || len(m.result.Protected) > 0 || len(m.result.Skipped) > 0 {
		return m.result
	}
	return GuardResult{Rules: m.rules}
}

func TestProfile_Render_EmptyProfile(t *testing.T) {
	p := New("/home/user")
	result, err := p.Render()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Profile != "" {
		t.Errorf("empty profile should render empty string, got %q", result.Profile)
	}
}

func TestProfile_Render_SingleModule(t *testing.T) {
	p := New("/home/user").Use(&testModule{
		name:  "test",
		rules: []Rule{AllowOp("process-exec")},
	})
	result, err := p.Render()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Profile, "(allow process-exec)") {
		t.Errorf("expected allow rule in output, got %q", result.Profile)
	}
	if !strings.Contains(result.Profile, "=== test ===") {
		t.Errorf("expected module name header, got %q", result.Profile)
	}
}

func TestProfile_Render_ModuleOrder(t *testing.T) {
	p := New("/home/user").Use(
		&testModule{name: "first", rules: []Rule{AllowOp("process-exec")}},
		&testModule{name: "second", rules: []Rule{AllowOp("process-fork")}},
	)
	result, err := p.Render()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	firstIdx := strings.Index(result.Profile, "first")
	secondIdx := strings.Index(result.Profile, "second")
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

func TestRenderReturnsProfileResult(t *testing.T) {
	m := &testModule{
		name: "test-guard",
		result: GuardResult{
			Rules:     []Rule{AllowRule(`(allow file-read* (subpath "/usr"))`)},
			Protected: []string{"/home/.ssh/id_rsa"},
			Skipped:   []string{"~/.config/op not found"},
		},
	}
	p := New("/home/user").Use(m)
	result, err := p.Render()
	if err != nil {
		t.Fatal(err)
	}
	if result.Profile == "" {
		t.Error("ProfileResult.Profile is empty")
	}
	if len(result.Guards) != 1 {
		t.Fatalf("expected 1 guard result, got %d", len(result.Guards))
	}
	if result.Guards[0].Name != "test-guard" {
		t.Errorf("guard name = %q, want %q", result.Guards[0].Name, "test-guard")
	}
	if len(result.Guards[0].Protected) != 1 {
		t.Errorf("expected 1 protected path, got %d", len(result.Guards[0].Protected))
	}
}
