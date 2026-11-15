package config

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/jskswamy/aide/internal/gatewaypreset"
)

// Wire protocol names as the api: field spells them.
const (
	apiAnthropicMessages = string(gatewaypreset.APIAnthropicMessages)
	apiOpenAIResponses   = string(gatewaypreset.APIOpenAIResponses)
)

// LegacyConversion is what a profile's legacy shapes become in the new
// gateway form: legacy gateways (one base_url serving Anthropic
// Messages and, for Codex, the OpenAI API), provider credentials:
// entries, provider switches and endpoints in env:, and the choice
// default_credentials: made. It reads only the profile, so the result
// never depends on the directory a command runs in.
type LegacyConversion struct {
	// Gateways holds every gateway of the profile in the new form:
	// entries already in it pass through.
	Gateways map[string]GatewayConfig
	// Renames maps harness, then old name, to the gateway that now
	// serves that harness.
	Renames map[string]map[string]string
	// Choice is, per gateway harness, the gateway today's default rule
	// injects ("none" when it injects none); absent when no converted
	// gateway is marked for the harness. A provider switch in env: wins.
	Choice map[string]string
	// Inherited marks the harnesses whose Choice came from the sorted
	// gateway order, not from default_credentials:.
	Inherited map[string]bool
	// Legacy is true when a legacy gateways: entry was converted.
	Legacy bool
	// Credentials and Env are the credentials: and env: entries that
	// became gateways and leave the profile.
	Credentials, Env []string
	// Converted has one line per credential or env: entry converted.
	Converted []string
	// CodexLost are credentials that served Codex and no longer do.
	CodexLost []CodexLoss
}

// CodexLoss is a converted credential whose preset does not serve
// Codex: Name, the endpoint it used and its key.
type CodexLoss struct{ Name, URL, Store, Key string }

// NeedsMigration reports whether the profile has anything to convert.
func (c LegacyConversion) NeedsMigration() bool {
	return c.Legacy || len(c.Credentials) > 0 || len(c.Env) > 0
}

// Apply returns ctx with the derived fields the loader and launch read:
// the converted gateways, the choice they derive and the entries that
// left credentials: and env:. Nothing it sets is serialized.
func (c LegacyConversion) Apply(ctx Context) Context {
	ctx.ConvertedGateways, ctx.DerivedChoice = c.Gateways, c.Choice
	ctx.ConvertedCredentials, ctx.ConvertedEnv = c.Credentials, c.Env
	return ctx
}

// Residual returns ctx without what became a gateway: the converted
// credentials, their default_credentials: names and the converted env:
// keys, which launch must not inject a second time.
func (c Context) Residual() Context {
	if len(c.ConvertedCredentials) == 0 && len(c.ConvertedEnv) == 0 {
		return c
	}
	gone := func(n string) bool { return slices.Contains(c.ConvertedCredentials, n) }
	c.Credentials = maps.Clone(c.Credentials)
	maps.DeleteFunc(c.Credentials, func(n string, _ CredentialBinding) bool { return gone(n) })
	c.DefaultCredentials = slices.DeleteFunc(slices.Clone(c.DefaultCredentials), gone)
	c.Env = maps.Clone(c.Env)
	for _, k := range c.ConvertedEnv {
		delete(c.Env, k)
	}
	return c
}

// ConvertLegacyGateways is ConvertLegacy's gateways and renames.
func ConvertLegacyGateways(ctx Context) (gateways map[string]GatewayConfig, renames map[string]map[string]string) {
	conv := ConvertLegacy(ctx)
	return conv.Gateways, conv.Renames
}

// ConvertLegacy translates ctx into the new gateway form. Entries
// already in it pass through. Codex *used* a legacy gateway when
// codex is listed in its harnesses: or default_credentials: selects it
// for Codex. Credentials are converted by convertCredential.
func ConvertLegacy(ctx Context) LegacyConversion {
	cv := &converter{ctx: ctx, LegacyConversion: LegacyConversion{Gateways: map[string]GatewayConfig{}}}
	cv.ctx2 = ctx
	if t, l, err := TranslateLegacyCredentials(ctx, ctx.Secret); err == nil {
		cv.ctx2, cv.implicit = t, l
	}
	cv.synth, cv.err = synthesizeGatewayCredentials(cv.ctx2)
	if cv.err != nil {
		cv.synth = cv.ctx2
	}
	cv.taken = slices.Sorted(maps.Keys(ctx.Gateways))
	cv.gateways()
	cv.envSwitch()
	cv.envEndpoint()
	cv.credentials()
	cv.choices()
	return cv.LegacyConversion
}

type converter struct {
	LegacyConversion
	ctx, ctx2, synth Context
	err              error
	taken            []string
	// names are the legacy gateway and credential names that became
	// gateways, and so derive a choice.
	names map[string]bool
	// made are the gateways the conversion created; forced are choices
	// an env: provider switch makes; implicit are the credentials from
	// env: secret references.
	made     []string
	forced   map[string]string
	implicit []LegacyCredential
}

func (cv *converter) rename(h, from, to string) {
	if cv.Renames == nil {
		cv.Renames = map[string]map[string]string{}
	}
	if cv.Renames[h] == nil {
		cv.Renames[h] = map[string]string{}
	}
	cv.Renames[h][from] = to
}

// free reports whether candidate collides with no gateway, no reserved
// name and no gateway's AIDE_GW_<NAME>_KEY.
func (cv *converter) free(candidate string) bool {
	return !reservedGatewayName(candidate) && !slices.ContainsFunc(cv.taken, func(o string) bool {
		return GatewayKeyVar(o) == GatewayKeyVar(candidate)
	})
}

func (cv *converter) unique(base string) string {
	name := base
	for n := 2; !cv.free(name); n++ {
		name = fmt.Sprintf("%s-%d", base, n)
	}
	cv.taken = append(cv.taken, name)
	return name
}

func (cv *converter) markConverted(name string) {
	if cv.names == nil {
		cv.names = map[string]bool{}
	}
	cv.names[name] = true
}

func (cv *converter) gateways() {
	ctx, gateways := cv.ctx, cv.Gateways
	for _, old := range slices.Sorted(maps.Keys(ctx.Gateways)) {
		gw := ctx.Gateways[old]
		if !gw.IsLegacy() {
			gateways[old] = gw
			continue
		}
		cv.Legacy = true
		cv.markConverted(old)
		name := old
		if reservedGatewayName(old) {
			name = cv.unique(reservedBase(old))
		}
		cv.made = append(cv.made, name)
		served := gw.EffectiveHarnesses()
		codexUsed := slices.Contains(gw.Harnesses, "codex") || slices.Contains(DefaultCredentialsForHarness(cv.synth, "codex"), old)
		conv := gw
		conv.BaseURL, conv.URL, conv.NoAuth = "", gw.BaseURL, gw.Key == ""
		conv.API, conv.Preset = apiAnthropicMessages, "" // converted gateways are custom url+api
		codexOnly := codexUsed && slices.Equal(gw.Harnesses, []string{"codex"})
		codexURL := NormalizeGatewayBaseURL(gw.BaseURL) + "/v1"
		switch {
		case codexOnly:
			conv.URL, conv.API = codexURL, apiOpenAIResponses
		case codexUsed:
			sib := conv
			sib.URL, sib.API, sib.Model, sib.Harnesses = codexURL, apiOpenAIResponses, "", []string{"codex"}
			sibName := cv.unique(name + "-codex")
			gateways[sibName] = sib
			cv.made = append(cv.made, sibName)
			cv.rename("codex", old, sibName)
			fallthrough
		default:
			conv.Harnesses = slices.DeleteFunc(slices.Clone(served), func(h string) bool { return h == "codex" })
		}
		gateways[name] = conv
		for _, h := range served {
			if name != old && (h != "codex" || codexOnly) {
				cv.rename(h, old, name)
			}
		}
	}
}

// reservedBase is the name a reserved gateway name starts from when renamed.
func reservedBase(old string) string {
	if strings.HasPrefix(strings.ToLower(old), "aide-") {
		return "gw-" + old // old+"-gw" would still be reserved
	}
	return old + "-gw"
}

// reservedGatewayName reports names a gateway may not use: "none" is a
// choice value and "aide-<gw>" names the entries aide writes into harness files.
func reservedGatewayName(name string) bool {
	n := strings.ToLower(name)
	return n == "none" || strings.HasPrefix(n, "aide-")
}

// GatewayKeyVar is the variable that carries gateway name's key into a
// launch and that harness files name as the key's source.
func GatewayKeyVar(name string) string {
	return "AIDE_GW_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_")) + "_KEY"
}

// ChoiceIsUnset reports whether harness has no explicit gateway choice.
func (c Context) ChoiceIsUnset(harness string) bool { return c.GatewayChoice[harness] == "" }

// ChoiceIsNone reports whether harness is explicitly set to use no gateway.
func (c Context) ChoiceIsNone(harness string) bool { return c.GatewayChoice[harness] == "none" }

// GatewayCandidates returns, sorted, the gateways of ctx marked for
// harness h, in the converted (one API per gateway) form.
func (c Context) GatewayCandidates(h string) []string {
	var out []string
	for _, name := range slices.Sorted(maps.Keys(c.ConvertedGateways)) {
		if slices.Contains(c.ConvertedGateways[name].EffectiveHarnesses(), h) {
			out = append(out, name)
		}
	}
	return out
}

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

// translateLegacyGateways fills each context's derived fields (see
// LegacyConversion.Apply) and the config's LegacyRenames. Nothing it
// sets is serialized.
func (c *Config) translateLegacyGateways() {
	for name, ctx := range c.Contexts {
		conv := ConvertLegacy(ctx)
		c.NeedsGatewayMigration = c.NeedsGatewayMigration || conv.NeedsMigration()
		c.Contexts[name] = conv.Apply(ctx)
		if conv.Renames != nil {
			if c.LegacyRenames == nil {
				c.LegacyRenames = map[string]map[string]map[string]string{}
			}
			c.LegacyRenames[name] = conv.Renames
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
