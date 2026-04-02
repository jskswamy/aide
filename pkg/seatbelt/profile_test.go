package seatbelt_test

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/mocks"
	"go.uber.org/mock/gomock"
)

func TestProfile_Render_EmptyProfile(t *testing.T) {
	p := seatbelt.New("/home/user")
	result, err := p.Render()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Profile != "" {
		t.Errorf("empty profile should render empty string, got %q", result.Profile)
	}
}

func TestProfile_Render_SingleModule(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockMod := mocks.NewMockModule(ctrl)
	mockMod.EXPECT().Name().Return("test").AnyTimes()
	mockMod.EXPECT().Rules(gomock.Any()).Return(seatbelt.GuardResult{
		Rules: []seatbelt.Rule{seatbelt.AllowOp("process-exec")},
	}).AnyTimes()

	p := seatbelt.New("/home/user").Use(mockMod)
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
	ctrl := gomock.NewController(t)
	mockFirst := mocks.NewMockModule(ctrl)
	mockFirst.EXPECT().Name().Return("first").AnyTimes()
	mockFirst.EXPECT().Rules(gomock.Any()).Return(seatbelt.GuardResult{
		Rules: []seatbelt.Rule{seatbelt.AllowOp("process-exec")},
	}).AnyTimes()

	mockSecond := mocks.NewMockModule(ctrl)
	mockSecond.EXPECT().Name().Return("second").AnyTimes()
	mockSecond.EXPECT().Rules(gomock.Any()).Return(seatbelt.GuardResult{
		Rules: []seatbelt.Rule{seatbelt.AllowOp("process-fork")},
	}).AnyTimes()

	p := seatbelt.New("/home/user").Use(mockFirst, mockSecond)
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
	ctrl := gomock.NewController(t)
	mockMod := mocks.NewMockModule(ctrl)
	mockMod.EXPECT().Name().Return("capture").AnyTimes()

	var captured seatbelt.Context
	mockMod.EXPECT().Rules(gomock.Any()).DoAndReturn(func(ctx *seatbelt.Context) seatbelt.GuardResult {
		captured = *ctx
		return seatbelt.GuardResult{}
	}).AnyTimes()

	p := seatbelt.New("/home/user").
		WithContext(func(ctx *seatbelt.Context) {
			ctx.ProjectRoot = "/tmp/project"
			ctx.TempDir = "/tmp"
		}).
		Use(mockMod)
	_, _ = p.Render()
	if captured.HomeDir != "/home/user" {
		t.Errorf("expected HomeDir=/home/user, got %q", captured.HomeDir)
	}
	if captured.ProjectRoot != "/tmp/project" {
		t.Errorf("expected ProjectRoot=/tmp/project, got %q", captured.ProjectRoot)
	}
}

func TestRenderReturnsProfileResult(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockMod := mocks.NewMockModule(ctrl)
	mockMod.EXPECT().Name().Return("test-guard").AnyTimes()
	mockMod.EXPECT().Rules(gomock.Any()).Return(seatbelt.GuardResult{
		Rules:     []seatbelt.Rule{seatbelt.AllowRule(`(allow file-read* (subpath "/usr"))`)},
		Protected: []string{"/home/.ssh/id_rsa"},
		Skipped:   []string{"~/.config/op not found"},
	}).AnyTimes()

	p := seatbelt.New("/home/user").Use(mockMod)
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
