package config

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/jskswamy/aide/internal/envscrub"
	"github.com/jskswamy/aide/internal/gatewaypreset"
)

// credSpec is where one harness's route of a credential goes: a preset,
// or a custom url with its wire protocol.
type credSpec struct {
	preset, api, url string
}

// switchGateway reads a bedrock or vertex switch out of set: the
// preset's switch variables with their values, the region and, for
// vertex, the project. keys are the variables of set it consumed.
func switchGateway(set map[string]string) (gw GatewayConfig, keys []string, ok bool) {
	for _, preset := range []string{"bedrock", "vertex"} {
		p, _ := gatewaypreset.Lookup(preset)
		sw := p.Switch
		on := len(sw.Env) > 0
		for k, v := range sw.Env {
			on = on && set[k] == v
		}
		plain := func(k string) bool { return k == "" || set[k] != "" && !strings.Contains(set[k], "{{") }
		if !on || !plain(sw.RegionEnv) || !plain(sw.ProjectEnv) {
			continue
		}
		keys = append(slices.Collect(maps.Keys(sw.Env)), sw.RegionEnv)
		gw = GatewayConfig{Preset: preset, Region: set[sw.RegionEnv], Harnesses: []string{"claude"}}
		if sw.ProjectEnv != "" {
			keys, gw.Project = append(keys, sw.ProjectEnv), set[sw.ProjectEnv]
		}
		return gw, keys, true
	}
	return GatewayConfig{}, nil, false
}

// valid reports whether gw would load as gateway name.
func valid(name string, gw GatewayConfig) bool {
	return IsGatewayName(name) && validateGateway(name, gw, Context{}, nil) == nil
}

// envSwitch turns a bedrock or vertex switch in env: into a gateway
// that serves Claude and is Claude's choice, as the switch applied to
// every Claude launch.
func (cv *converter) envSwitch() {
	gw, _, ok := switchGateway(cv.ctx.Env)
	if !ok || !valid(gw.Preset, gw) {
		return
	}
	name := cv.unique(gw.Preset)
	cv.Gateways[name] = gw
	cv.made = append(cv.made, name)
	cv.forced = map[string]string{"claude": name}
	keys := slices.Sorted(maps.Keys(gw.SwitchSpec().Env))
	cv.Env = append(cv.Env, keys...) // the region and project stay: other harnesses read them
	cv.Converted = append(cv.Converted, fmt.Sprintf("env: %s became gateway %q (preset %s, serving claude)", strings.Join(keys, ", "), name, gw.Preset))
}

// envEndpoint turns an ANTHROPIC_BASE_URL in env: into a custom gateway
// when a key from an env: secret reference travels to it. Without such
// a key the endpoint stays: a keyless gateway would stop Claude sending
// its saved login.
func (cv *converter) envEndpoint() {
	u := strings.TrimSpace(cv.ctx.Env["ANTHROPIC_BASE_URL"])
	if u == "" || strings.Contains(u, "{{") {
		return
	}
	for _, l := range cv.implicit {
		gw := GatewayConfig{URL: u, API: apiAnthropicMessages, Store: l.Binding.Store, Key: l.Binding.Key,
			Harnesses: []string{"claude", "pi", "omp", "opencode"}}
		switch p, _ := gatewaypreset.Lookup("anthropic"); {
		case l.EnvVar == "ANTHROPIC_AUTH_TOKEN":
		case l.EnvVar == "ANTHROPIC_API_KEY" && NormalizeGatewayBaseURL(u) == p.BaseURL:
			gw.URL, gw.API, gw.Preset = "", "", "anthropic" // keeps the x-api-key header
		default:
			continue // a custom gateway would send the key as a Bearer token instead
		}
		if !valid(l.Name, gw) {
			return
		}
		name := cv.gatewayName(l.Name)
		cv.Gateways[name] = gw
		cv.finish(name, l.Name, l.EnvVar, fmt.Sprintf("env: ANTHROPIC_BASE_URL with %s became gateway %q (anthropic-messages, serving %s)", l.EnvVar, name, strings.Join(gw.Harnesses, ", ")))
		cv.Env = append(cv.Env, "ANTHROPIC_BASE_URL")
		return
	}
}

// gatewayName is the name credential old keeps as a gateway: itself, or
// a free variation when it is reserved or collides with another
// gateway's AIDE_GW_<NAME>_KEY.
func (cv *converter) gatewayName(old string) string {
	if cv.free(old) {
		cv.taken = append(cv.taken, old)
		return old
	}
	return cv.unique(reservedBase(old))
}

// finish records credential old (a credential from env: when it came
// from the variable envVar) as converted into gateway name.
func (cv *converter) finish(name, old, envVar, line string) {
	cv.markConverted(old)
	if name != "" {
		cv.made = append(cv.made, name)
	}
	cv.Converted = append(cv.Converted, line)
	if envVar != "" {
		cv.Env = append(cv.Env, envVar) // one gateway per credential for env: ones
	} else if !slices.Contains(cv.Credentials, old) {
		cv.Credentials = append(cv.Credentials, old)
	}
}

// implicitVar is the env: variable credential name came from, or "".
func (cv *converter) implicitVar(name string) string {
	for _, l := range cv.implicit {
		if l.Name == name {
			return l.EnvVar
		}
	}
	return ""
}

// credentials converts the LLM provider credentials the profile
// defines, and the keys its env: references, into gateways. One the
// gateway form cannot express exactly (another variable set, a harness
// that is no gateway harness, capabilities, an endpoint that would not
// load) is left as it is.
func (cv *converter) credentials() {
	endpointLeft := slices.ContainsFunc(slices.Collect(maps.Keys(cv.ctx.Env)), func(k string) bool {
		return envscrub.IsProviderEndpointName(k) && !slices.Contains(cv.Env, k)
	})
	for _, name := range slices.Sorted(maps.Keys(cv.ctx2.Credentials)) {
		b := cv.ctx2.Credentials[name]
		if cv.names[name] || name == "gemini" || !b.IsLLMProvider(name) {
			continue
		}
		if endpointLeft && cv.implicitVar(name) != "" {
			continue // its key would leave for the vendor, not for the endpoint env: keeps
		}
		if len(b.Capabilities) > 0 {
			cv.credentialSwitch(name, b)
		} else {
			cv.credential(name, b)
		}
	}
}

// credentialHarnesses are the harnesses binding name serves: its for:
// keys, its harnesses:, or a built-in's default set.
func credentialHarnesses(name string, b CredentialBinding) []string {
	var hs []string
	switch {
	case len(b.For) > 0:
		hs = slices.Collect(maps.Keys(b.For))
	case len(b.Harnesses) > 0:
		hs = slices.Clone(b.Harnesses)
	default:
		hs = slices.Clone(builtinCredentialHarnesses[name])
	}
	for i := range hs {
		hs[i] = canonicalHarness(hs[i])
	}
	return hs
}

// credentialSpec says where harness h's route r goes. ok is false when
// the gateway form cannot express the route; serves is false when it
// can but the target does not serve h.
func credentialSpec(name string, r CredentialRoute, h string) (s credSpec, serves, ok bool) {
	for k, v := range r.Set {
		if v == "" && envscrub.IsProviderSecretName(k) {
			continue // blanks a key, as a gateway does
		}
		if v == "" || k != "ANTHROPIC_BASE_URL" && k != "OPENAI_BASE_URL" {
			return s, false, false
		}
	}
	anthropic, openai := strings.TrimSpace(r.Set["ANTHROPIC_BASE_URL"]), strings.TrimSpace(r.Set["OPENAI_BASE_URL"])
	switch {
	case anthropic != "" && openai != "":
		return s, false, false
	case anthropic != "":
		s = credSpec{api: apiAnthropicMessages, url: anthropic}
		if slices.Contains(r.Env, "ANTHROPIC_API_KEY") { // a custom gateway sends Bearer, not x-api-key
			p, _ := gatewaypreset.Lookup("anthropic")
			return credSpec{preset: p.Name}, slices.Contains(p.Harnesses, h), NormalizeGatewayBaseURL(anthropic) == p.BaseURL
		}
	case openai != "":
		s = credSpec{api: string(gatewaypreset.APIOpenAIChat), url: openai}
		if h == "codex" {
			s.api = apiOpenAIResponses
		}
	default:
		var p gatewaypreset.Preset
		found := false
		if IsBuiltinCredential(name) && slices.Equal(r.Env, builtinCredentialEnv[name]) {
			p, found = gatewaypreset.Lookup(name)
		} else if len(r.Env) == 1 {
			p, found = gatewaypreset.ByShellVar(r.Env[0])
		}
		return credSpec{preset: p.Name}, slices.Contains(p.Harnesses, h), found
	}
	if checkGatewayURL(s.url) != nil {
		return s, false, false
	}
	want, constrained := harnessAPI[h]
	return s, !constrained || string(want) == s.api, true
}

// credential converts binding name when the gateway form can express
// every route of it: harnesses that share a destination share a
// gateway; Codex, which only speaks the Responses API, gets its own.
func (cv *converter) credential(name string, b CredentialBinding) {
	hs := credentialHarnesses(name, b)
	if len(hs) == 0 || slices.ContainsFunc(hs, func(h string) bool { return !slices.Contains(DefaultGatewayHarnesses, h) }) {
		return
	}
	groups, order, lost := map[credSpec][]string{}, []credSpec{}, []CodexLoss{}
	for _, h := range DefaultGatewayHarnesses {
		if !slices.Contains(hs, h) {
			continue
		}
		r, _ := b.Route(name, h)
		s, serves, ok := credentialSpec(name, r, h)
		switch {
		case !ok || !serves && h != "codex":
			return
		case h == "claude" && s.api == apiAnthropicMessages && b.Keyless():
			return // a keyless gateway would stop Claude sending its saved login
		case !serves: // only a preset whose url speaks the OpenAI API gets an add command
			p, _ := gatewaypreset.Lookup(s.preset)
			loss := CodexLoss{Name: name}
			if p.API == gatewaypreset.APIOpenAIChat {
				loss = CodexLoss{Name: name, URL: p.BaseURL, Store: b.Store, Key: b.Key}
			}
			lost = append(lost, loss)
		default:
			if groups[s] == nil {
				order = append(order, s)
			}
			groups[s] = append(groups[s], h)
		}
	}
	if len(order) == 0 && len(lost) == 0 {
		return
	}
	gws := make([]GatewayConfig, len(order))
	for i, s := range order {
		gws[i] = GatewayConfig{Store: b.Store, Key: b.Key, Harnesses: groups[s]}
		if s.preset != "" {
			gws[i].Preset = s.preset
		} else {
			gws[i].URL, gws[i].API, gws[i].NoAuth = s.url, s.api, b.Keyless()
		}
		if !valid(name, gws[i]) {
			return
		}
	}
	for i, s := range order {
		gwName := cv.gatewayName(name)
		if i > 0 {
			suffix := cmp.Or(s.preset, s.api)
			if s.preset == "" && s.api == apiOpenAIResponses {
				suffix = "codex"
			}
			gwName = cv.unique(name + "-" + suffix)
		}
		cv.Gateways[gwName] = gws[i]
		if gwName != name {
			for _, h := range groups[s] {
				cv.rename(h, name, gwName)
			}
		}
		what := "preset " + s.preset
		if s.preset == "" {
			what = s.api + " " + s.url
		}
		cv.finish(gwName, name, cv.implicitVar(name), fmt.Sprintf("credential %q became gateway %q (%s, serving %s)", name, gwName, what, strings.Join(groups[s], ", ")))
	}
	if len(order) == 0 {
		cv.finish("", name, cv.implicitVar(name), fmt.Sprintf("credential %q was removed: no gateway serves %s", name, strings.Join(hs, ", ")))
	}
	cv.CodexLost = append(cv.CodexLost, lost...)
}

// credentialSwitch converts a keyless bedrock or vertex binding that
// does nothing but set the switch for Claude.
func (cv *converter) credentialSwitch(name string, b CredentialBinding) {
	r, ok := b.For["claude"]
	gw, keys, found := switchGateway(r.Set)
	if !ok || !found || len(b.For) != 1 || len(r.Env) > 0 || len(keys) != len(r.Set) || !b.Keyless() ||
		!slices.Equal(b.Capabilities, []string{gw.SwitchSpec().Capability}) || !valid(name, gw) {
		return
	}
	gwName := cv.gatewayName(name)
	cv.Gateways[gwName] = gw
	if gwName != name {
		cv.rename("claude", name, gwName)
	}
	cv.finish(gwName, name, "", fmt.Sprintf("credential %q became gateway %q (preset %s, serving claude)", name, gwName, gw.Preset))
}

// choices derives each harness's choice the way today's default rule
// does: the first default_credentials: entry that became a gateway
// serving it, or none when the rule injects no such gateway. A harness
// no converted gateway serves keeps no choice.
func (cv *converter) choices() {
	set := func(h, v string) {
		if cv.Choice == nil {
			cv.Choice = map[string]string{}
		}
		cv.Choice[h] = v
	}
	for _, h := range DefaultGatewayHarnesses {
		cands := slices.DeleteFunc(slices.Sorted(maps.Keys(cv.Gateways)), func(n string) bool {
			return !slices.Contains(cv.Gateways[n].EffectiveHarnesses(), h)
		})
		if cv.err != nil || !slices.ContainsFunc(cands, func(n string) bool { return slices.Contains(cv.made, n) }) {
			continue
		}
		v := "none"
		for _, name := range defaultCredentialsForHarness(cv.synth, h) {
			if !cv.names[name] {
				continue
			}
			to := cmp.Or(cv.Renames[h][name], name)
			if slices.Contains(cands, to) {
				v = to
				if !slices.Contains(cv.ctx2.DefaultCredentials, name) {
					if cv.Inherited == nil {
						cv.Inherited = map[string]bool{}
					}
					cv.Inherited[h] = true
				}
				break
			}
		}
		set(h, v)
	}
	for h, v := range cv.forced {
		set(h, v)
		delete(cv.Inherited, h)
	}
}
