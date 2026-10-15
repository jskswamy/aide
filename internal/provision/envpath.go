package provision

import "path/filepath"

// EnvOrHomePath returns ctx.Env[key] when it is set and non-empty,
// otherwise the default path rel... under ctx.HomeDir. Drivers use it
// for agent config locations an env var can relocate, such as
// CLAUDE_CONFIG_DIR or PI_CODING_AGENT_DIR.
func EnvOrHomePath(ctx Context, key string, rel ...string) string {
	if v := ctx.Env[key]; v != "" {
		return v
	}
	return filepath.Join(append([]string{ctx.HomeDir}, rel...)...)
}
