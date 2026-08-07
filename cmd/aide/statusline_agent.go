package main

import (
	"encoding/json"
	"os"
)

// claudeStatuslineProbe matches enough of the JSON Claude Code sends to
// statusLine.command on stdin to identify the caller as Claude Code,
// without committing to its full schema.
type claudeStatuslineProbe struct {
	SessionID string          `json:"session_id"`
	Model     json.RawMessage `json:"model"`
	Workspace json.RawMessage `json:"workspace"`
}

// looksLikeClaudeStatuslineJSON reports whether data matches Claude Code's
// statusline JSON shape closely enough to identify the caller as claude.
// Used when AIDE_AGENT is unset (statusline invoked outside an
// aide-launched session) and stdin is piped.
func looksLikeClaudeStatuslineJSON(data []byte) bool {
	var probe claudeStatuslineProbe
	if err := json.Unmarshal(data, &probe); err != nil {
		return false
	}
	return probe.SessionID != "" || len(probe.Model) > 0 || len(probe.Workspace) > 0
}

// contextAgentForCWD resolves the agent configured for the aide context
// matching the current working directory. Used as a TTY-preview fallback
// when no explicit agent, AIDE_AGENT, or stdin JSON signal is available.
// Returns "" on any resolution failure (no config, no matching context) so
// callers can fall back further.
func contextAgentForCWD() string {
	_, _, ctx, err := resolveContextForMutation("")
	if err != nil {
		return ""
	}
	return ctx.Agent
}

// resolveStatuslineAgent picks the coding agent for statusline rendering.
// Order: explicit flag/positional, AIDE_AGENT env (aide-launched session),
// stdin JSON shape (piped mode only, identifies Claude Code), CWD-matched
// aide context (TTY preview only), then "claude" as the final default —
// the only agent with statusline rendering support today.
func resolveStatuslineAgent(explicitAgent string, isTTY bool, stdinData []byte) string {
	if explicitAgent != "" {
		return explicitAgent
	}
	if v := os.Getenv("AIDE_AGENT"); v != "" {
		return v
	}
	if !isTTY && looksLikeClaudeStatuslineJSON(stdinData) {
		return "claude"
	}
	if isTTY {
		if a := contextAgentForCWD(); a != "" {
			return a
		}
	}
	return "claude"
}
