package doctor

import (
	"fmt"
	"os"
	"strings"
	"text/template"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/sandbox"
)

// ConfigChecks runs the config-wide checks behind aide validate over
// every profile and agent. lookPath finds agent binaries (exec.LookPath
// in production). Messages are validate's, word for word.
func ConfigChecks(cfg *config.Config, lookPath func(string) (string, error)) []Finding {
	var out []Finding
	add := func(id, subject, profile string, sev Severity, msg string, steps ...string) {
		out = append(out, Finding{ID: id, Subject: subject, Profile: profile, Severity: sev, Message: msg, Fix: Fix{Steps: steps}})
	}
	for name, ctx := range cfg.Contexts {
		if ctx.Agent != "" && len(cfg.Agents) > 0 {
			if _, ok := cfg.Agents[ctx.Agent]; !ok {
				add("config.unknown-agent", name, name, SeverityError,
					fmt.Sprintf("profile %q references unknown agent %q", name, ctx.Agent),
					fmt.Sprintf("Add agent %q under agents:, or change the profile's agent", ctx.Agent))
			}
		}
		if ctx.Secret != "" {
			if _, err := os.Stat(config.ResolveSecretPath(ctx.Secret)); os.IsNotExist(err) {
				add("config.secret-missing", name, name, SeverityError,
					fmt.Sprintf("profile %q references secret %q which does not exist", name, ctx.Secret),
					fmt.Sprintf("Create the secret store %q, or run: aide profile remove-secret --profile %s", ctx.Secret, name))
			}
		}
		if ctx.Sandbox != nil {
			if err := sandbox.ValidateSandboxRef(ctx.Sandbox, cfg.Sandboxes); err != nil {
				add("config.sandbox-ref-invalid", name, name, SeverityError,
					fmt.Sprintf("profile %q has invalid sandbox config: %s", name, err),
					"Fix the profile's sandbox: reference to name a defined sandbox")
			}
		}
		if name != cfg.DefaultContext && name != "default" && len(ctx.Match) == 0 {
			add("config.no-match-rules", name, name, SeverityWarning,
				fmt.Sprintf("profile %q has no match rules (will never activate)", name),
				fmt.Sprintf("Run aide profile bind %s in a directory it should match", name))
		}
		for key, val := range ctx.Env {
			if !strings.Contains(val, "{{") {
				continue
			}
			if _, err := template.New("").Parse(val); err != nil {
				add("config.template-invalid", name+"."+key, name, SeverityError,
					fmt.Sprintf("profile %q env var %q has invalid template syntax: %s", name, key, err),
					fmt.Sprintf("Fix the template in env var %s", key))
			} else if strings.Contains(val, ".secrets.") && ctx.Secret == "" {
				add("config.secret-without-store", name+"."+key, name, SeverityError,
					fmt.Sprintf("profile %q env var %q references secrets but no secret is configured", name, key),
					fmt.Sprintf("Run: aide profile set-secret <store> --profile %s", name))
			}
		}
	}
	for name, def := range cfg.Agents {
		binary := def.Binary
		if binary == "" {
			binary = name
		}
		if _, err := lookPath(binary); err != nil {
			add("config.agent-not-on-path", name, "", SeverityWarning,
				fmt.Sprintf("agent %q binary %q not found on PATH", name, binary),
				fmt.Sprintf("Install %s, or set agents.%s.binary", binary, name))
		}
	}
	return out
}
