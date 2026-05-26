package explain

import (
	"fmt"
	"strings"
)

// RenderHuman renders the document as a readable terminal overview.
func RenderHuman(doc Document) string {
	var b strings.Builder
	st := doc.State

	b.WriteString("aide configuration\n")
	b.WriteString(strings.Repeat("─", 40) + "\n")
	if !st.Loaded {
		b.WriteString("No config found. Run `aide init` to create one.\n\n")
	} else {
		if st.DefaultContext != "" {
			fmt.Fprintf(&b, "Default context: %s\n", st.DefaultContext)
		}
		fmt.Fprintf(&b, "Contexts: %d\n\n", len(st.Contexts))
		for _, c := range st.Contexts {
			fmt.Fprintf(&b, "  %s\n", c.Name)
			if c.Agent != "" {
				fmt.Fprintf(&b, "    agent:   %s\n", c.Agent)
			}
			if c.Secret != "" {
				fmt.Fprintf(&b, "    secret:  %s\n", c.Secret)
			}
			if c.SandboxNote != "" {
				fmt.Fprintf(&b, "    sandbox: %s\n", c.SandboxNote)
			}
			for _, e := range c.Env {
				disp := envDisplay(e)
				if disp == "" {
					continue
				}
				fmt.Fprintf(&b, "    env:     %s = %s\n", e.Key, disp)
			}
			if len(c.MCPServers) > 0 {
				fmt.Fprintf(&b, "    mcp:     %s\n", strings.Join(c.MCPServers, ", "))
			}
			if len(c.Hooks) > 0 {
				fmt.Fprintf(&b, "    hooks:   %s\n", strings.Join(c.Hooks, ", "))
			}
		}
		if len(st.TopLevelHooks) > 0 {
			b.WriteString("\nTop-level hooks:\n")
			for _, h := range st.TopLevelHooks {
				fmt.Fprintf(&b, "  %s\n", h)
			}
		}
		if len(st.TopLevelMCP) > 0 {
			b.WriteString("\nTop-level MCP servers:\n")
			for _, m := range st.TopLevelMCP {
				fmt.Fprintf(&b, "  %s (%s)\n", m.Name, m.Transport)
			}
		}
		b.WriteString("\n")
	}

	if len(doc.Recipes) > 0 {
		b.WriteString("Recipes (aide explain <topic>):\n")
		for _, r := range doc.Recipes {
			fmt.Fprintf(&b, "  %-20s %s\n", r.Topic, r.Title)
		}
	}
	return b.String()
}

// envDisplay renders an EnvRef without ever showing a literal value (T1).
func envDisplay(e EnvRef) string {
	switch {
	case e.SecretRef != "":
		return "{{ .secrets." + e.SecretRef + " }}"
	case e.Redacted:
		return "<redacted>"
	case e.Template != "":
		return e.Template
	default:
		return ""
	}
}
