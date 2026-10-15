package provision

import (
	"encoding/json"
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
// command) pair, plus `// aide-matcher:` when the hook is
// matcher-scoped, and Decode reads those instead.
type CommentHookCodec struct {
	// Label prefixes errors, e.g. "pi hooks".
	Label string
	// Artifact names and owns the generated files.
	Artifact HookArtifact
	// Events maps aide's normalized events to the agent's native ones.
	// Hooks for unmapped events are skipped silently rather than
	// guessing at an unverified mapping.
	Events map[string]string
	// Matchers maps aide's normalized matchers to the agent's native
	// tool names. Matchers it doesn't list pass through unchanged, as
	// they do for Claude.
	Matchers map[string]string
	// Body renders the script that follows the comment header.
	// toolGuard is "" for a hook that fires on every tool, otherwise a
	// JS string literal naming the native tool the script must check
	// for before running command.
	Body func(nativeEvent, toolGuard, command string) string
}

const (
	commentHookEventPrefix   = "// aide-event: "
	commentHookMatcherPrefix = "// aide-matcher: "
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
	var command, event, matcher string
	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case strings.HasPrefix(line, commentHookEventPrefix):
			native := strings.TrimPrefix(line, commentHookEventPrefix)
			event = ReverseLookup(c.Events, native, native)
		case strings.HasPrefix(line, commentHookMatcherPrefix):
			native := strings.TrimPrefix(line, commentHookMatcherPrefix)
			matcher = ReverseLookup(c.Matchers, native, native)
		case strings.HasPrefix(line, commentHookCommandPrefix):
			command = strings.TrimPrefix(line, commentHookCommandPrefix)
		}
	}
	return Hook{Event: event, Matcher: matcher, Command: command}, nil
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
	header := "// Managed by aide. Do not edit manually.\n" +
		commentHookEventPrefix + nativeEvent + "\n"
	// The name hashes the command alone for unscoped hooks, so their
	// files keep the names they had before matchers were supported.
	name := c.Artifact.Name(h.Command)
	var toolGuard string
	if h.Matcher != "" {
		if strings.ContainsAny(h.Matcher, "\r\n") {
			return fmt.Errorf("%s: matcher %q contains a line break", c.Label, h.Matcher)
		}
		nativeMatcher := h.Matcher
		if n, ok := c.Matchers[h.Matcher]; ok {
			nativeMatcher = n
		}
		quoted, err := json.Marshal(nativeMatcher)
		if err != nil {
			return fmt.Errorf("%s: %w", c.Label, err)
		}
		toolGuard = string(quoted)
		header += commentHookMatcherPrefix + nativeMatcher + "\n"
		name = c.Artifact.Name(HookKey(h.Event, h.Matcher, h.Command))
	}
	script := header +
		commentHookCommandPrefix + h.Command + "\n" +
		c.Body(nativeEvent, toolGuard, h.Command)
	if err := fsutil.AtomicWrite(filepath.Join(dir, name), []byte(script)); err != nil {
		return fmt.Errorf("%s: write hook file: %w", c.Label, err)
	}
	return nil
}

// Remove implements HookCodec.
func (c *CommentHookCodec) Remove(path string) error {
	return os.Remove(path)
}
