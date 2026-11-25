package context

import (
	"slices"
	"testing"

	"github.com/jskswamy/aide/internal/config"
)

func TestTiedMatches(t *testing.T) {
	cfg := &config.Config{Contexts: map[string]config.Context{
		"a":     {Match: []config.MatchRule{{Path: "/src/*"}}},
		"b":     {Match: []config.MatchRule{{Path: "/src/*"}}},
		"exact": {Match: []config.MatchRule{{Path: "/other"}}},
		"none":  {},
	}}
	none := func(string) string { return "" }
	if got := TiedMatches(cfg, "/src/x", none); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("tied = %v, want [a b]", got)
	}
	if got := TiedMatches(cfg, "/other", none); got != nil {
		t.Errorf("tied = %v, want none for a unique match", got)
	}
}
