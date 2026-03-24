package seatbelt_test

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

func TestSubpathWithParentMetadata(t *testing.T) {
	rules := seatbelt.SubpathWithParentMetadata("/nix/store")
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
	output := rules[0].String() + "\n" + rules[1].String()
	if !strings.Contains(output, `(subpath "/nix/store")`) {
		t.Error("expected subpath rule for /nix/store")
	}
	if !strings.Contains(output, `(literal "/nix")`) {
		t.Error("expected metadata rule for parent /nix")
	}
	if !strings.Contains(output, "file-read-metadata") {
		t.Error("expected file-read-metadata in parent rule")
	}
}

func TestSubpathWithParentMetadata_RootChild(t *testing.T) {
	rules := seatbelt.SubpathWithParentMetadata("/opt")
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
	output := rules[1].String()
	if !strings.Contains(output, `(literal "/")`) {
		t.Error("expected metadata rule for / when parent is root")
	}
}
