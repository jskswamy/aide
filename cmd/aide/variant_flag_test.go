package main

import (
	"strings"
	"testing"
)

func TestParseVariantFlag_Valid(t *testing.T) {
	got, err := parseVariantFlag([]string{"python=uv", "node=pnpm"}, []string{"python", "node"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got["python"]) != 1 || got["python"][0] != "uv" {
		t.Errorf("python = %v, want [uv]", got["python"])
	}
	if len(got["node"]) != 1 || got["node"][0] != "pnpm" {
		t.Errorf("node = %v, want [pnpm]", got["node"])
	}
}

func TestParseVariantFlag_MultiPerCapability(t *testing.T) {
	got, err := parseVariantFlag([]string{"python=uv", "python=conda"}, []string{"python"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got["python"]) != 2 {
		t.Errorf("python = %v, want 2 entries", got["python"])
	}
}

func TestParseVariantFlag_CapabilityNotActive(t *testing.T) {
	_, err := parseVariantFlag([]string{"python=uv"}, []string{"node"})
	if err == nil {
		t.Fatal("want error when capability not in --with")
	}
	if !strings.Contains(err.Error(), "requires --with python") {
		t.Errorf("error message missing help hint; got: %v", err)
	}
}

func TestParseVariantFlag_Malformed(t *testing.T) {
	cases := []string{"python", "=uv", "python="}
	for _, c := range cases {
		_, err := parseVariantFlag([]string{c}, []string{"python"})
		if err == nil {
			t.Errorf("%q: want error, got nil", c)
		}
	}
}
