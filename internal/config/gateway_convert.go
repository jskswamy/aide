package config

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Wire protocol names as in gatewaypreset, which config cannot import
// (gatewaypreset's tests reach config through capability).
const (
	apiAnthropicMessages = "anthropic-messages"
	apiOpenAIResponses   = "openai-responses"
)

// ConvertLegacyGateways translates ctx's legacy gateways (one base_url
// serving Anthropic Messages and, for Codex, the OpenAI API) into the
// one-API-per-gateway form. Entries already in the new form pass
// through. renames maps harness, then old name, to the gateway that
// now serves that harness. It reads only ctx, so the result never
// depends on the directory a command runs in.
func ConvertLegacyGateways(ctx Context) (gateways map[string]GatewayConfig, renames map[string]map[string]string) {
	gateways = map[string]GatewayConfig{}
	rename := func(h, from, to string) {
		if renames == nil {
			renames = map[string]map[string]string{}
		}
		if renames[h] == nil {
			renames[h] = map[string]string{}
		}
		renames[h][from] = to
	}
	synth, err := SynthesizeGatewayCredentials(ctx)
	if err != nil {
		synth = ctx
	}
	taken := slices.Sorted(maps.Keys(ctx.Gateways))
	// free reports whether candidate collides with no gateway, no
	// reserved name and no gateway's AIDE_GW_<NAME>_KEY.
	free := func(candidate string) bool {
		return !reservedGatewayName(candidate) && !slices.ContainsFunc(taken, func(o string) bool {
			return gatewayKeyVar(o) == gatewayKeyVar(candidate)
		})
	}
	unique := func(base string) string {
		name := base
		for n := 2; !free(name); n++ {
			name = fmt.Sprintf("%s-%d", base, n)
		}
		taken = append(taken, name)
		return name
	}
	for _, old := range slices.Sorted(maps.Keys(ctx.Gateways)) {
		gw := ctx.Gateways[old]
		if gw.BaseURL == "" {
			gateways[old] = gw
			continue
		}
		name := old
		if reservedGatewayName(old) {
			base := old + "-gw"
			if strings.HasPrefix(strings.ToLower(old), "aide-") {
				base = "gw-" + old // old+"-gw" would still be reserved
			}
			name = unique(base)
		}
		served := gw.EffectiveHarnesses()
		codexUsed := slices.Contains(gw.Harnesses, "codex") || slices.Contains(DefaultCredentialsForHarness(synth, "codex"), old)
		conv := gw
		conv.BaseURL, conv.URL, conv.NoAuth = "", gw.BaseURL, gw.Key == ""
		conv.API = apiAnthropicMessages
		codexOnly := codexUsed && slices.Equal(gw.Harnesses, []string{"codex"})
		codexURL := NormalizeGatewayBaseURL(gw.BaseURL) + "/v1"
		switch {
		case codexOnly:
			conv.URL, conv.API = codexURL, apiOpenAIResponses
		case codexUsed:
			sib := conv
			sib.URL, sib.API, sib.Model, sib.Harnesses = codexURL, apiOpenAIResponses, "", []string{"codex"}
			sibName := unique(name + "-codex")
			gateways[sibName] = sib
			rename("codex", old, sibName)
			fallthrough
		default:
			conv.Harnesses = slices.DeleteFunc(slices.Clone(served), func(h string) bool { return h == "codex" })
		}
		gateways[name] = conv
		for _, h := range served {
			if name != old && (h != "codex" || codexOnly) {
				rename(h, old, name)
			}
		}
	}
	return gateways, renames
}

// reservedGatewayName reports names a gateway may not use: "none" is a
// choice value and "aide-<gw>" names the entries aide writes into harness files.
func reservedGatewayName(name string) bool {
	n := strings.ToLower(name)
	return n == "none" || strings.HasPrefix(n, "aide-")
}

// gatewayKeyVar is the variable name that would carry gateway name's key.
func gatewayKeyVar(name string) string {
	return "AIDE_GW_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_")) + "_KEY"
}

// ChoiceIsUnset reports whether harness has no explicit gateway choice.
func (c Context) ChoiceIsUnset(harness string) bool { return c.GatewayChoice[harness] == "" }

// ChoiceIsNone reports whether harness is explicitly set to use no gateway.
func (c Context) ChoiceIsNone(harness string) bool { return c.GatewayChoice[harness] == "none" }

// ValidateGatewayChoices reports gateway_choice entries that name a
// gateway that is missing or does not serve the harness. Stale choices
// are reported, never fatal, so the config stays editable.
func ValidateGatewayChoices(ctx Context) []string {
	var out []string
	for _, h := range slices.Sorted(maps.Keys(ctx.GatewayChoice)) {
		choice := ctx.GatewayChoice[h]
		if choice == "" || choice == "none" {
			continue
		}
		gw, ok := ctx.ConvertedGateways[choice]
		switch {
		case !ok:
			out = append(out, fmt.Sprintf("gateway_choice.%s: gateway %q is not defined", h, choice))
		case !slices.Contains(gw.EffectiveHarnesses(), h):
			out = append(out, fmt.Sprintf("gateway_choice.%s: gateway %q does not serve %s", h, choice, h))
		}
	}
	return out
}

// translateLegacyGateways fills each context's derived ConvertedGateways
// and the config's LegacyRenames. Nothing it sets is serialized.
func (c *Config) translateLegacyGateways() {
	for name, ctx := range c.Contexts {
		var renames map[string]map[string]string
		ctx.ConvertedGateways, renames = ConvertLegacyGateways(ctx)
		c.Contexts[name] = ctx
		if renames != nil {
			if c.LegacyRenames == nil {
				c.LegacyRenames = map[string]map[string]map[string]string{}
			}
			c.LegacyRenames[name] = renames
		}
	}
}

// warnStaleGatewayChoices warns about stale gateway_choice entries
// without failing Load.
func (c *Config) warnStaleGatewayChoices() {
	for _, name := range sortedKeys(c.Contexts) {
		for _, msg := range ValidateGatewayChoices(c.Contexts[name]) {
			fmt.Fprintf(loadWarnings, "aide: warning: profile %q: %s\n", name, msg)
		}
	}
}
