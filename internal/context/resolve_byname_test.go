package context

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/config"
)

func byNameConfig() *config.Config {
	return &config.Config{
		Contexts: map[string]config.Context{
			"work":     {Agent: "claude", Match: []config.MatchRule{{Path: "/work/**"}}},
			"personal": {Agent: "pi", Env: map[string]string{"A": "1"}},
		},
		DefaultContext: "work",
	}
}

func TestResolveByName_SelectsNamedProfile(t *testing.T) {
	cfg := byNameConfig()
	cfg.ProjectOverride = &config.ProjectOverride{Env: map[string]string{"B": "2"}}

	rc, err := ResolveByName(cfg, "personal")
	if err != nil {
		t.Fatalf("ResolveByName() error = %v", err)
	}
	if rc.Name != "personal" || rc.Context.Agent != "pi" {
		t.Errorf("got %q agent %q, want personal/pi", rc.Name, rc.Context.Agent)
	}
	if rc.Source != SourceFlag {
		t.Errorf("Source = %q, want %q", rc.Source, SourceFlag)
	}
	if rc.Context.Env["B"] != "2" {
		t.Error("project override not applied on top of the named profile")
	}
}

func TestResolveByName_UnknownProfileListsDefined(t *testing.T) {
	_, err := ResolveByName(byNameConfig(), "nope")
	if err == nil {
		t.Fatal("expected error for unknown profile")
	}
	if !strings.Contains(err.Error(), "personal, work") {
		t.Errorf("error should list defined profiles: %v", err)
	}
}

func TestResolveByName_MinimalConfigDefault(t *testing.T) {
	cfg := &config.Config{Agent: "claude"}
	rc, err := ResolveByName(cfg, "default")
	if err != nil || rc.Name != "default" {
		t.Fatalf("ResolveByName(minimal, default) = %v, %v", rc, err)
	}
	if _, err := ResolveByName(cfg, "work"); err == nil {
		t.Error("minimal config has no profile work")
	}
}

func TestResolveSelected_ExplicitBeatsMatcher(t *testing.T) {
	cfg := byNameConfig()
	rc, err := ResolveSelected(cfg, "/work/repo", nil, "personal")
	if err != nil || rc.Name != "personal" {
		t.Fatalf("ResolveSelected(explicit) = %v, %v; want personal", rc, err)
	}
	rc, err = ResolveSelected(cfg, "/work/repo", nil, "")
	if err != nil || rc.Name != "work" || rc.Source != SourceMatch {
		t.Fatalf("ResolveSelected(matcher) = %+v, %v; want work from match", rc, err)
	}
	rc, err = ResolveSelected(cfg, "/elsewhere", nil, "")
	if err != nil || rc.Name != "work" || rc.Source != SourceDefault {
		t.Fatalf("ResolveSelected(default) = %+v, %v; want work from default", rc, err)
	}
}
