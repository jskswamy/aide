package gateway

import (
	"regexp"
	"strings"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/gatewaypreset"
)

// EndpointSpec is where a gateway is reached and how a key is sent.
// Every caller that needs a gateway's URL, wire protocol or auth header
// takes it from Endpoint, so none builds them on its own.
type EndpointSpec struct {
	// BaseURL is exact: a path such as /v1 or /api/paas/v4 is kept.
	BaseURL string
	API     gatewaypreset.API
	Header  gatewaypreset.Header
}

// Endpoint resolves gw. Today's gateways are Anthropic Messages proxies
// taking a Bearer token, with a base URL normalized to drop /v1.
func Endpoint(gw config.GatewayConfig) EndpointSpec {
	return EndpointSpec{
		BaseURL: NormalizeBaseURL(gw.BaseURL),
		API:     gatewaypreset.APIAnthropicMessages,
		Header:  gatewaypreset.HeaderBearer,
	}
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
