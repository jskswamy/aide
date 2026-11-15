package gateway

import (
	"cmp"
	"regexp"
	"strings"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/gatewaypreset"
)

// EndpointSpec is where a gateway is reached and how a key is sent.
// Every caller that needs a gateway's URL, wire protocol or auth header
// takes it from Endpoint, so none builds them on its own.
type EndpointSpec struct {
	// BaseURL is exact: a path such as /v1 or /api/paas/v4 is kept
	// (anthropic-messages alone drops a trailing /v1, as it always has).
	BaseURL string
	API     gatewaypreset.API
	Header  gatewaypreset.Header
	// Keyless means no key is sent: a keyless-loopback preset or no_auth.
	Keyless bool
	// Switch is set for a provider-switch preset, which has no URL.
	Switch *gatewaypreset.SwitchSpec
}

// Endpoint resolves gw: a preset gives its URL, wire protocol and key
// header, a custom gateway its url and api with a Bearer key (api
// defaults to anthropic-messages).
func Endpoint(gw config.GatewayConfig) EndpointSpec {
	ep := EndpointSpec{
		BaseURL: cmp.Or(gw.BaseURL, gw.URL),
		API:     gatewaypreset.API(cmp.Or(gw.API, string(gatewaypreset.APIAnthropicMessages))),
		Header:  gatewaypreset.HeaderBearer,
		Keyless: gw.NoAuth,
	}
	if p, ok := gatewaypreset.Lookup(gw.Preset); ok {
		if p.Kind == gatewaypreset.KindProviderSwitch {
			return EndpointSpec{Switch: p.Switch}
		}
		ep.BaseURL, ep.API, ep.Header = p.BaseURL, p.API, p.Header
		ep.Keyless = ep.Keyless || p.Kind == gatewaypreset.KindKeylessLoopback
	}
	if ep.API == gatewaypreset.APIAnthropicMessages {
		ep.BaseURL = NormalizeBaseURL(ep.BaseURL)
	}
	return ep
}

var versionSegment = regexp.MustCompile(`/v[0-9]+$`)

// ModelsURL is the model list: <base>/models when the base URL already
// ends in a version segment (/v1, /v4), otherwise <base>/v1/models.
func (e EndpointSpec) ModelsURL() string {
	base := strings.TrimRight(e.BaseURL, "/")
	if versionSegment.MatchString(base) {
		return base + "/models"
	}
	return base + "/v1/models"
}

// ProviderID is the entry name harness files carry for gateway gwName.
// The prefix keeps aide's entries apart from a user's own and from a
// built-in provider of the same name.
func ProviderID(gwName string) string { return "aide-" + gwName }
