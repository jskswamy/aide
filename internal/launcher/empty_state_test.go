// internal/launcher/empty_state_test.go
package launcher

import (
	"bytes"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/config"
)

// fakeActions records which method was called and returns the
// configured error.
type fakeActions struct {
	bindCalled, createCalled bool
	bindErr, createErr       error
}

func (f *fakeActions) Bind(_ string) error {
	f.bindCalled = true
	return f.bindErr
}

func (f *fakeActions) Create(_ string) error {
	f.createCalled = true
	return f.createErr
}

func TestHandleEmptyState_NonTTY_Errors_WithFourHints(t *testing.T) {
	cfg := &config.Config{Contexts: map[string]config.Context{
		"work": {Agent: "claude"},
	}}
	var out bytes.Buffer
	_, err := handleEmptyState(cfg, strings.NewReader(""), &out, false, &fakeActions{})
	if err == nil {
		t.Fatal("expected error in non-TTY mode")
	}
	for _, want := range []string{
		"aide context bind",
		"aide context create",
		"aide use",
		"aide context set-default",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("non-TTY error must mention %q, got: %v", want, err)
		}
	}
}

func TestHandleEmptyState_TTY_Cancel_ReturnsCancelled(t *testing.T) {
	cfg := &config.Config{Contexts: map[string]config.Context{"work": {Agent: "claude"}}}
	var out bytes.Buffer
	_, err := handleEmptyState(cfg, strings.NewReader("c\n"), &out, true, &fakeActions{})
	if err != ErrEmptyStateCancelled {
		t.Errorf("expected ErrEmptyStateCancelled, got: %v", err)
	}
}

func TestHandleEmptyState_TTY_LaunchOnce_ReturnsContextWithoutPersisting(t *testing.T) {
	cfg := &config.Config{Contexts: map[string]config.Context{"work": {Agent: "claude"}}}
	var out bytes.Buffer
	// Choice [3], then pick the only context (default [1]).
	rc, err := handleEmptyState(cfg, strings.NewReader("3\n\n"), &out, true, &fakeActions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rc == nil || rc.Name != "work" {
		t.Errorf("expected work context, got: %+v", rc)
	}
	if rc.MatchReason != "empty-state launch-once" {
		t.Errorf("launch-once should be marked in MatchReason, got: %q", rc.MatchReason)
	}
}

func TestHandleEmptyState_TTY_BindChoice_DispatchesAction(t *testing.T) {
	cfg := &config.Config{Contexts: map[string]config.Context{"work": {Agent: "claude"}}}
	var out bytes.Buffer
	actions := &fakeActions{}
	// Choice [1], then pick the only context.
	_, err := handleEmptyState(cfg, strings.NewReader("1\n\n"), &out, true, actions)
	if err != ErrEmptyStateActionRanReloadNeeded {
		t.Errorf("choice [1] should signal reload-needed, got: %v", err)
	}
	if !actions.bindCalled {
		t.Errorf("Bind action should have been invoked")
	}
}

func TestHandleEmptyState_TTY_CreateChoice_DispatchesAction(t *testing.T) {
	cfg := &config.Config{Contexts: map[string]config.Context{}}
	var out bytes.Buffer
	actions := &fakeActions{}
	// Choice [2], wizard runs through fake.
	_, err := handleEmptyState(cfg, strings.NewReader("2\n"), &out, true, actions)
	if err != ErrEmptyStateActionRanReloadNeeded {
		t.Errorf("choice [2] should signal reload-needed, got: %v", err)
	}
	if !actions.createCalled {
		t.Errorf("Create action should have been invoked")
	}
}
