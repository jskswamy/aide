package provision

import (
	"fmt"
	"strings"
)

// HookKey returns the canonical deduplication key for a hook triple.
// This is the single authoritative identity function used across all packages;
// use it instead of rolling local string-concatenation in callers.
func HookKey(event, matcher, command string) string {
	return event + "\x00" + matcher + "\x00" + command
}

// ValidateHookCommand returns an error if cmd contains characters that are
// unsafe to embed in generated agent hook scripts (bash exec lines, Python
// subprocess lists, JSON command fields). Valid commands are restricted to
// printable characters excluding shell metacharacters.
//
// Call this on already-expanded commands. For commands that may still
// contain template variables (e.g. {agent}), use ValidateHookCommandTemplate.
func ValidateHookCommand(cmd string) error {
	if cmd == "" {
		return fmt.Errorf("hook command cannot be empty")
	}
	const dangerous = ";|&`$(){}!<>\\\"'\n\r\t*?[]#~"
	for _, ch := range dangerous {
		if strings.ContainsRune(cmd, ch) {
			return fmt.Errorf("hook command contains disallowed character %q (use executable paths and plain arguments only)", string(ch))
		}
	}
	return nil
}

// ValidateHookCommandTemplate validates a hook command that may still contain
// aide template variables (currently only {agent}). Known variables are
// replaced with safe placeholders before the metacharacter check, so a
// command like "rtk hook {agent}" passes validation while injection attempts
// like "cmd{a,b}" are still rejected.
func ValidateHookCommandTemplate(cmd string) error {
	expanded := strings.ReplaceAll(cmd, "{agent}", "aide")
	return ValidateHookCommand(expanded)
}
