package claude_test

import (
	"testing"

	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/provision/agents/claude"
)

func TestClaudeMCPHandlerNonNil(t *testing.T) {
	d := claude.New(&fakeRunner{})
	if d.MCPHandler(provision.Context{ProjectRoot: "/p/root"}) == nil {
		t.Error("MCPHandler must return non-nil handler")
	}
}
