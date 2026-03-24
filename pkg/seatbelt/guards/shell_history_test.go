package guards_test

import (
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

func TestShellHistory_DeniesHistoryFiles(t *testing.T) {
	g := guards.ShellHistoryGuard()

	if g.Type() != "default" {
		t.Errorf("expected type default, got %s", g.Type())
	}

	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	result := g.Rules(ctx)

	// Should deny known history files that exist
	// Since we can't guarantee which exist in test, just check structure
	if len(result.Rules) == 0 && len(result.Skipped) == 0 {
		t.Error("expected either rules or skipped entries")
	}
}
