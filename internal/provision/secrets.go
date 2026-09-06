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

// MCPTemplatesSatisfiedByInstalled reports whether every {{ }}-templated
// MCP env value in desired has a matching key already present in
// installed — the precondition for a caller (e.g. aide sync's secrets
// hash gate) to skip decryption safely. Pure check, no mutation:
// callers must not apply a partial substitution when this returns
// false, since a decrypt-based fallback needs the original
// {{ .secrets.X }} literals intact to re-resolve everything from
// scratch (see SubstituteFromInstalled).
func MCPTemplatesSatisfiedByInstalled(desired *Desired, installed Installed) bool {
	for name, server := range desired.MCPServers {
		if len(server.Env) == 0 {
			continue
		}
		inst, instOK := installed.MCPServers[name]
		for k, v := range server.Env {
			if !config.IsTemplate(v) {
				continue
			}
			if !instOK {
				return false
			}
			if _, exists := inst.Env[k]; !exists {
				return false
			}
		}
	}
	return true
}

// SubstituteFromInstalled fills every {{ }}-templated MCP env value in
// desired with the matching key's value from installed. It builds a
// fresh Env map per server rather than writing into server.Env's
// existing entries: that map may be shared by reference with other
// in-memory structures (e.g. a parsed config struct, via a shallow
// copy upstream), so mutating it in place could leak a resolved
// secret into shared state. Callers must only call this after
// MCPTemplatesSatisfiedByInstalled has returned true for the same
// (desired, installed) pair — it does not re-check.
func SubstituteFromInstalled(desired *Desired, installed Installed) {
	for name, server := range desired.MCPServers {
		if len(server.Env) == 0 {
			continue
		}
		inst := installed.MCPServers[name]
		resolved := make(map[string]string, len(server.Env))
		for k, v := range server.Env {
			if config.IsTemplate(v) {
				resolved[k] = inst.Env[k]
			} else {
				resolved[k] = v
			}
		}
		server.Env = resolved
		desired.MCPServers[name] = server
	}
}
