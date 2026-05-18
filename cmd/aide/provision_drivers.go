package main

// Blank-import each agent provisioner so its init() registers the
// driver with internal/provision's registry. Linking happens here
// (in cmd/aide) to avoid a cycle between internal/provision and its
// agent subpackages.
import (
	_ "github.com/jskswamy/aide/internal/provision/agents/claude"
	_ "github.com/jskswamy/aide/internal/provision/agents/codex"
	_ "github.com/jskswamy/aide/internal/provision/agents/copilot"
	_ "github.com/jskswamy/aide/internal/provision/agents/gemini"
)
