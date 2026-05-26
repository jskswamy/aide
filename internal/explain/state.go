package explain

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/display"
)

// StateFromConfig builds a redacted snapshot. It is pure and read-only and
// never resolves or decrypts secrets — it classifies env values via
// display.ClassifyEnvSource only.
func StateFromConfig(cfg *config.Config) ConfigState {
	if cfg == nil {
		return ConfigState{Loaded: false}
	}
	st := ConfigState{Loaded: true, DefaultContext: cfg.DefaultContext}

	names := make([]string, 0, len(cfg.Contexts))
	for name := range cfg.Contexts {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		st.Contexts = append(st.Contexts, contextState(name, cfg.Contexts[name]))
	}

	mcpNames := make([]string, 0, len(cfg.MCPServers))
	for name := range cfg.MCPServers {
		mcpNames = append(mcpNames, name)
	}
	sort.Strings(mcpNames)
	for _, name := range mcpNames {
		st.TopLevelMCP = append(st.TopLevelMCP, mcpState(name, cfg.MCPServers[name]))
	}

	st.TopLevelHooks = topLevelHooksSummary(cfg.Hooks)
	return st
}

func contextState(name string, ctx config.Context) ContextState {
	cs := ContextState{
		Name:         name,
		Agent:        ctx.Agent,
		Profile:      ctx.Profile,
		Secret:       ctx.Secret,
		Capabilities: ctx.Capabilities,
		MCPServers:   mcpServerNames(ctx),
		Env:          redactEnv(ctx.Env),
		SandboxNote:  sandboxNote(ctx.Sandbox),
		Hooks:        contextHooksSummary(ctx.Hooks),
	}
	for _, m := range ctx.Match {
		if m.Path != "" {
			cs.Matches = append(cs.Matches, "path: "+m.Path)
		} else if m.Remote != "" {
			cs.Matches = append(cs.Matches, "remote: "+m.Remote)
		}
	}
	return cs
}

func mcpState(name string, m config.MCPServer) MCPState {
	transport := "stdio"
	if m.URL != "" {
		transport = "http"
	}
	return MCPState{Name: name, Transport: transport, Env: redactEnv(m.Env)}
}

// redactEnv converts an env map into sorted, redacted EnvRefs (T1).
func redactEnv(env map[string]string) []EnvRef {
	if len(env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	refs := make([]EnvRef, 0, len(keys))
	for _, k := range keys {
		val := env[k]
		source, secretKey := display.ClassifyEnvSource(val)
		ref := EnvRef{Key: k}
		switch {
		case secretKey != "":
			ref.SecretRef = secretKey
		case strings.TrimSpace(val) == "":
			// empty value: leave all fields zero
		case source == "from project_root" || source == "from runtime_dir":
			ref.Template = val // affirmatively-recognized safe template — show verbatim
		case source == "template":
			// Unrecognized template form (stray "{{" that isn't .project_root
			// or .runtime_dir) — fail closed regardless of key name because
			// the value could embed a secret e.g. "sk-live-{{ .unknown }}".
			ref.Redacted = true
		default:
			// Plain literal: use the key name to decide. Keys whose names
			// suggest a credential (TOKEN, SECRET, PASSWORD, etc.) are
			// redacted; everything else (e.g. ANTHROPIC_MODEL) is safe to
			// show verbatim.
			if isCredentialKey(k) {
				ref.Redacted = true
			} else {
				ref.Template = val
			}
		}
		refs = append(refs, ref)
	}
	return refs
}

// credentialIndicators are substrings that, when present in an env-var key
// name (case-insensitive), indicate the value is likely a credential.
var credentialIndicators = []string{
	"KEY", "TOKEN", "SECRET", "PASSWORD", "PASSWD", "CREDENTIAL", "CERT", "PRIVATE",
}

func isCredentialKey(key string) bool {
	up := strings.ToUpper(key)
	for _, ind := range credentialIndicators {
		if strings.Contains(up, ind) {
			return true
		}
	}
	return false
}

// mcpServerNames summarizes a context's MCP selection. It prefers the v1
// list-of-names form; when only the v2 delta form is present it renders the
// only/exclude/extra entries as readable strings (sorted for stable output).
func mcpServerNames(ctx config.Context) []string {
	if len(ctx.MCPServers) > 0 {
		return ctx.MCPServers
	}
	ov := ctx.MCPServersOverride
	if ov == nil {
		return nil
	}
	var out []string
	for _, n := range ov.Only {
		out = append(out, "only: "+n)
	}
	for _, n := range ov.Exclude {
		out = append(out, "exclude: "+n)
	}
	extra := make([]string, 0, len(ov.Extra))
	for n := range ov.Extra {
		extra = append(extra, n)
	}
	sort.Strings(extra)
	for _, n := range extra {
		out = append(out, "extra: "+n)
	}
	return out
}

// topLevelHooksSummary returns one entry per event ("pre_tool: 2") sorted by event name.
func topLevelHooksSummary(hooks config.HooksMap) []string {
	if len(hooks) == 0 {
		return nil
	}
	events := make([]string, 0, len(hooks))
	for event := range hooks {
		events = append(events, event)
	}
	sort.Strings(events)
	out := make([]string, 0, len(events))
	for _, event := range events {
		out = append(out, fmt.Sprintf("%s: %d", event, len(hooks[event])))
	}
	return out
}

// contextHooksSummary summarises a per-context hook override (exclude/extra).
func contextHooksSummary(ov *config.HooksOverride) []string {
	if ov == nil {
		return nil
	}
	var out []string
	for _, event := range ov.Exclude {
		out = append(out, "exclude: "+event)
	}
	extra := make([]string, 0, len(ov.Extra))
	for event := range ov.Extra {
		extra = append(extra, event)
	}
	sort.Strings(extra)
	for _, event := range extra {
		out = append(out, fmt.Sprintf("extra: %s (%d)", event, len(ov.Extra[event])))
	}
	return out
}

func sandboxNote(ref *config.SandboxRef) string {
	if ref == nil {
		return "default"
	}
	if ref.Disabled {
		return "disabled"
	}
	if ref.ProfileName != "" {
		return fmt.Sprintf("profile: %s", ref.ProfileName)
	}
	if ref.Inline != nil {
		return "custom (inline)"
	}
	return "default"
}
