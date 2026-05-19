package provision

import (
	"fmt"

	"github.com/jskswamy/aide/internal/config"
)

// ResolveSecretsInMCPEnv template-resolves every MCP server's env map
// in desired against td, mutating desired in place. Plain string values
// (no `{{ }}`) pass through unchanged; templated values are resolved
// via config.ResolveTemplates with the same syntax and semantics the
// launcher uses for context env (Go text/template, missingkey=error).
//
// td may be nil — this signals "no secrets file is configured for this
// context." In that case every env map is scanned for template syntax;
// if any is found, ResolveSecretsInMCPEnv errors loudly naming the
// offending MCP server so the user understands they referenced a
// secret without supplying secrets to resolve against.
//
// Resolution happens between ResolveDesired and ComputePlan in the sync
// pipeline so the plan diff compares already-resolved values against
// the agent's installed state — otherwise a `{{ .secrets.X }}` desired
// value would never equal the agent's "the real value" installed value
// and aide sync would propose an unnecessary update on every run.
//
// Plan output remains safe: renderPlan only prints op kind + name, not
// env values, so resolving before plan does not leak secrets to stdout
// / journal files.
func ResolveSecretsInMCPEnv(desired *Desired, td *config.TemplateData) error {
	if desired == nil {
		return nil
	}
	for name, server := range desired.MCPServers {
		if len(server.Env) == 0 {
			continue
		}
		if td == nil {
			for k, v := range server.Env {
				if config.IsTemplate(v) {
					return fmt.Errorf("MCP server %q env %q references a {{ }} template but no secrets file is configured for this context", name, k)
				}
			}
			continue
		}
		resolved, err := config.ResolveTemplates(server.Env, td)
		if err != nil {
			return fmt.Errorf("MCP server %q: %w", name, err)
		}
		server.Env = resolved
		desired.MCPServers[name] = server
	}
	return nil
}
