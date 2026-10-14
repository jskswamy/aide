package provision

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jskswamy/aide/internal/fsutil"
)

// CommentHookCodec implements HookCodec for agents whose hooks are
// generated script files: OpenCode plugins and Pi extensions. The
// script's behavior lives in agent-specific code that is impractical to
// parse back out, so every file opens with `// aide-event:` and
// `// aide-command:` comment lines carrying the canonical (event,
// command) pair, and Decode reads those instead.
type CommentHookCodec struct {
	// Label prefixes errors, e.g. "pi hooks".
	Label string
	// Artifact names and owns the generated files.
	Artifact HookArtifact
	// Events maps aide's normalized events to the agent's native ones.
	// Hooks for unmapped events are skipped silently rather than
	// guessing at an unverified mapping.
	Events map[string]string
	// Body renders the script that follows the comment header.
	Body func(nativeEvent, command string) string
}

const (
	commentHookEventPrefix   = "// aide-event: "
	commentHookCommandPrefix = "// aide-command: "
)

// Match implements HookCodec.
func (c *CommentHookCodec) Match(name string) bool {
	return c.Artifact.Owns(name)
}

// Decode implements HookCodec.
func (c *CommentHookCodec) Decode(path string) (Hook, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Hook{}, err
	}
	var command, event string
	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case strings.HasPrefix(line, commentHookEventPrefix):
			native := strings.TrimPrefix(line, commentHookEventPrefix)
			event = ReverseLookup(c.Events, native, native)
		case strings.HasPrefix(line, commentHookCommandPrefix):
			command = strings.TrimPrefix(line, commentHookCommandPrefix)
		}
	}
	return Hook{Event: event, Command: command}, nil
}

// Encode implements HookCodec.
func (c *CommentHookCodec) Encode(dir string, h Hook) error {
	nativeEvent, ok := c.Events[h.Event]
	if !ok {
		return nil
	}
	if err := ValidateHookCommand(h.Command); err != nil {
		return fmt.Errorf("%s: %w", c.Label, err)
	}
	script := "// Managed by aide. Do not edit manually.\n" +
		commentHookEventPrefix + nativeEvent + "\n" +
		commentHookCommandPrefix + h.Command + "\n" +
		c.Body(nativeEvent, h.Command)
	if err := fsutil.AtomicWrite(filepath.Join(dir, c.Artifact.Name(h.Command)), []byte(script)); err != nil {
		return fmt.Errorf("%s: write hook file: %w", c.Label, err)
	}
	return nil
}

// Remove implements HookCodec.
func (c *CommentHookCodec) Remove(path string) error {
	return os.Remove(path)
}
