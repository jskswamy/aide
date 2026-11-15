// Package gatewaymigrate moves saved state from the legacy credential
// selection to per-harness gateway choices.
package gatewaymigrate

import (
	"fmt"
	"slices"
	"strings"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/prefs"
)

// TranslatePreference returns rec with its version 1 credential
// selection translated into gateway choices, and a notice for each
// credential it could not translate. An empty selection becomes "none"
// for claude and codex; a credential that is (or became, through
// renames: harness, then old name, to new name) a gateway serving a
// harness becomes that harness's choice. Anything else is dropped. A
// choice rec already holds wins. It translates in memory only; rec's
// own list is cleared in the result, never in the caller's copy.
func TranslatePreference(rec prefs.Preference, ctx config.Context, renames map[string]map[string]string) (prefs.Preference, []string) {
	if rec.LegacyCredentials == nil {
		return rec, nil
	}
	creds := *rec.LegacyCredentials
	scope := config.DefaultGatewayHarnesses
	if slices.Contains(scope, rec.Harness) {
		scope = []string{rec.Harness}
	}
	out := rec
	out.LegacyCredentials = nil
	out.Gateway = map[string]string{}
	for h, v := range rec.Gateway {
		out.Gateway[h] = v
	}
	set := func(h, v string) {
		if out.Gateway[h] == "" {
			out.Gateway[h] = v
		}
	}
	if len(creds) == 0 {
		for _, h := range scope {
			if h == "claude" || h == "codex" {
				set(h, "none")
			}
		}
	}
	var notices []string
	for _, c := range creds {
		served := false
		for _, h := range scope {
			name := c
			if to, ok := renames[h][c]; ok {
				name = to
			}
			if gw, ok := ctx.ConvertedGateways[name]; ok && slices.Contains(gw.EffectiveHarnesses(), h) {
				set(h, name)
				served = true
			}
		}
		if !served {
			notices = append(notices, fmt.Sprintf("saved credential %q names no gateway that serves %s; dropped "+
				"(add one with: aide gateway add %s --harnesses %s)", c, strings.Join(scope, ", "), c, scope[0]))
		}
	}
	if len(out.Gateway) == 0 {
		out.Gateway = nil
	}
	return out, notices
}
