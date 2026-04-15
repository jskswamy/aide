package main

import (
	"os"
	"testing"
)

func TestEffectiveBannerStyle_TTYPreservesPreference(t *testing.T) {
	got := effectiveBannerStyle("boxed", true, "")
	if got != "boxed" {
		t.Errorf("TTY + preference=boxed → %q, want boxed", got)
	}
}

func TestEffectiveBannerStyle_NonTTYForcesCompact(t *testing.T) {
	got := effectiveBannerStyle("boxed", false, "")
	if got != "compact" {
		t.Errorf("non-TTY + preference=boxed → %q, want compact", got)
	}
}

func TestEffectiveBannerStyle_ExplicitOverrideWinsOverNonTTY(t *testing.T) {
	got := effectiveBannerStyle("compact", false, "boxed")
	if got != "boxed" {
		t.Errorf("non-TTY + explicit boxed → %q, want boxed", got)
	}
}

func TestEffectiveBannerStyle_ExplicitOverrideWinsOverTTY(t *testing.T) {
	got := effectiveBannerStyle("clean", true, "compact")
	if got != "compact" {
		t.Errorf("TTY + explicit compact → %q, want compact", got)
	}
}

func TestIsInteractiveStdout(t *testing.T) {
	_ = isInteractiveTerminal(os.Stdout)
}
