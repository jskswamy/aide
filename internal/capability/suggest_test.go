package capability

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestSuggestForPath_KubeConfig(t *testing.T) {
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".kube", "config")

	registry := Builtins()
	suggestions := SuggestForPath(path, registry)
	sort.Strings(suggestions)

	expected := map[string]bool{"k8s": true, "helm": true}
	if len(suggestions) != len(expected) {
		t.Fatalf("expected %d suggestions, got %d: %v", len(expected), len(suggestions), suggestions)
	}
	for _, s := range suggestions {
		if !expected[s] {
			t.Errorf("unexpected suggestion: %s", s)
		}
	}
}

func TestSuggestForPath_AWSCredentials(t *testing.T) {
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".aws", "credentials")

	registry := Builtins()
	suggestions := SuggestForPath(path, registry)

	if len(suggestions) != 1 || suggestions[0] != "aws" {
		t.Errorf("expected [aws], got %v", suggestions)
	}
}

func TestSuggestForPath_UnknownPath(t *testing.T) {
	path := "/some/random/unknown/path"

	registry := Builtins()
	suggestions := SuggestForPath(path, registry)

	if len(suggestions) != 0 {
		t.Errorf("expected no suggestions, got %v", suggestions)
	}
}

func TestSuggestForPath_IncludesWritablePaths(t *testing.T) {
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".mytools", "data")

	registry := map[string]Capability{
		"mytools": {
			Name:     "mytools",
			Writable: []string{"~/.mytools"},
		},
	}
	suggestions := SuggestForPath(path, registry)

	if len(suggestions) != 1 || suggestions[0] != "mytools" {
		t.Errorf("expected [mytools], got %v", suggestions)
	}
}
