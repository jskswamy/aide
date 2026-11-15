package config

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jskswamy/aide/internal/envscrub"
	"gopkg.in/yaml.v3"
)

// GatewayConfig is a custom LLM gateway on a profile: where it is,
// which secret holds its bearer token, and which harnesses use it.
// Launches inject its key through the harness's gateway choice
// (gateway_choice:); nothing is duplicated on disk.
type GatewayConfig struct {
	// BaseURL is the legacy single URL; URL and API replace it, and
	// the loader translates one into the other in memory (see
	// ConvertLegacyGateways).
	BaseURL    string   `yaml:"base_url,omitempty" json:"base_url,omitempty"`
	Preset     string   `yaml:"preset,omitempty" json:"preset,omitempty"`
	URL        string   `yaml:"url,omitempty" json:"url,omitempty"`
	API        string   `yaml:"api,omitempty" json:"api,omitempty"`
	NoAuth     bool     `yaml:"no_auth,omitempty" json:"no_auth,omitempty"`
	Model      string   `yaml:"model,omitempty" json:"model,omitempty"`
	Region     string   `yaml:"region,omitempty" json:"region,omitempty"`
	Project    string   `yaml:"project,omitempty" json:"project,omitempty"`
	RefreshTTL string   `yaml:"refresh_ttl,omitempty" json:"refresh_ttl,omitempty"`
	Store      string   `yaml:"store,omitempty" json:"store,omitempty"`
	Key        string   `yaml:"key,omitempty" json:"key,omitempty"`
	Harnesses  []string `yaml:"harnesses,omitempty" json:"harnesses,omitempty"`
}

// UnmarshalYAML decodes a gateway and mirrors a new-form url: into
// BaseURL, the field launch code and validation read; a legacy entry
// has base_url: and no url:.
func (gw *GatewayConfig) UnmarshalYAML(node *yaml.Node) error {
	type plain GatewayConfig
	if err := node.Decode((*plain)(gw)); err != nil {
		return err
	}
	if gw.URL != "" && gw.BaseURL == "" {
		gw.BaseURL = gw.URL
	}
	return nil
}

// WithBaseURL returns gw with its url: mirrored into BaseURL, the form
// launch code and validation read.
func (gw GatewayConfig) WithBaseURL() GatewayConfig {
	if gw.BaseURL == "" {
		gw.BaseURL = gw.URL
	}
	return gw
}

// MarshalYAML always writes the new form: url: and api:, never
// base_url:. BaseURL, which edits change, wins over a stale URL; a
// gateway still in its legacy form is written as an Anthropic Messages
// gateway.
func (gw GatewayConfig) MarshalYAML() (any, error) {
	type plain GatewayConfig
	gw.URL = cmp.Or(gw.BaseURL, gw.URL)
	if gw.URL != "" && gw.API == "" {
		gw.API = apiAnthropicMessages
	}
	gw.BaseURL = ""
	return plain(gw), nil
}

// IsLegacy reports whether gw is in the legacy single-URL form that
// ConvertLegacyGateways translates.
func (gw GatewayConfig) IsLegacy() bool { return gw.BaseURL != "" && gw.URL == "" }

// DefaultGatewayHarnesses are the harnesses a gateway serves when its
// harnesses: list is omitted: the profile-capable harnesses that accept
// a custom endpoint and model catalogue.
var DefaultGatewayHarnesses = []string{"claude", "pi", "omp", "opencode", "codex"}

// DefaultGatewayRefreshTTL is how long a cached model catalogue stays
// fresh when refresh_ttl is omitted.
const DefaultGatewayRefreshTTL = 24 * time.Hour

// ErrInvalidGateway reports a malformed gateways: entry.
var ErrInvalidGateway = errors.New("invalid gateway")

// Claude Code reads fixed variable names, so its gateway route uses
// these instead of the gateway's key.
const (
	claudeGatewayTokenEnv     = "ANTHROPIC_AUTH_TOKEN"
	claudeGatewayBaseURLEnv   = "ANTHROPIC_BASE_URL"
	claudeGatewayDiscoveryEnv = "CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY"
	claudeGatewayAPIKeyEnv    = "ANTHROPIC_API_KEY"
)

// KeylessPlaceholder is the ANTHROPIC_AUTH_TOKEN a keyless Claude
// gateway route sets so Claude Code does not send its saved login.
const KeylessPlaceholder = "aide-keyless"

var (
	// gatewayIdentRE guards names that become file path segments.
	gatewayIdentRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)
	gatewayKeyRE   = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
)

// gatewayDangerousEnv are variables that change how a process loads
// code, trusts TLS or routes traffic; a gateway token must never be
// injected under one of them. A trailing "*" matches a prefix.
var gatewayDangerousEnv = []string{
	"LD_*", "DYLD_*",
	"NODE_OPTIONS", "NODE_EXTRA_CA_CERTS", "PYTHONPATH", "GOFLAGS",
	"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY",
	"SSL_CERT_FILE", "SSL_CERT_DIR", "CURL_CA_BUNDLE", "REQUESTS_CA_BUNDLE",
	"BASH_ENV", "ENV", "GIT_SSH_COMMAND",
}

// EffectiveHarnesses returns the harnesses gw serves: Harnesses, or
// DefaultGatewayHarnesses when omitted.
func (gw GatewayConfig) EffectiveHarnesses() []string {
	if len(gw.Harnesses) > 0 {
		return slices.Clone(gw.Harnesses)
	}
	return slices.Clone(DefaultGatewayHarnesses)
}

// EffectiveTTL returns RefreshTTL parsed, or DefaultGatewayRefreshTTL
// when omitted.
func (gw GatewayConfig) EffectiveTTL() (time.Duration, error) {
	if gw.RefreshTTL == "" {
		return DefaultGatewayRefreshTTL, nil
	}
	d, err := time.ParseDuration(gw.RefreshTTL)
	if err != nil {
		return 0, err
	}
	if d <= 0 {
		return 0, fmt.Errorf("%q is not a positive duration", gw.RefreshTTL)
	}
	return d, nil
}

// TTL is EffectiveTTL for a validated gateway: an unparseable
// refresh_ttl falls back to DefaultGatewayRefreshTTL.
func (gw GatewayConfig) TTL() time.Duration {
	ttl, err := gw.EffectiveTTL()
	if err != nil {
		return DefaultGatewayRefreshTTL
	}
	return ttl
}

// RequiresToken reports whether gw stores a bearer secret.
func (gw GatewayConfig) RequiresToken() bool { return gw.Key != "" }

// IsLoopback reports whether gw.BaseURL is localhost, 127.0.0.1, or ::1.
func (gw GatewayConfig) IsLoopback() bool {
	u, err := url.Parse(gw.BaseURL)
	if err != nil {
		return false
	}
	return isLoopbackHost(u.Hostname())
}

// TokenEnv is the variable harness h reads the token of gateway name
// from: Claude Code's fixed ANTHROPIC_AUTH_TOKEN, else
// AIDE_GW_<NAME>_KEY. Empty when gw has no token.
func (gw GatewayConfig) TokenEnv(name, h string) string {
	if !gw.RequiresToken() {
		return ""
	}
	if h == "claude" {
		return claudeGatewayTokenEnv
	}
	return GatewayKeyVar(name)
}

// ValidateGatewayTarget rejects a keyless gateway that is not on
// loopback: with no token to send, it would receive the harness's
// request unauthenticated over the network. Load only warns about it so
// the gateway stays removable and editable; add, edit, sync and launch
// call this.
func ValidateGatewayTarget(name string, gw GatewayConfig) error {
	if gw.RequiresToken() || gw.IsLoopback() {
		return nil
	}
	return fmt.Errorf("gateway %q: %s is keyless but not on localhost, 127.0.0.1 or ::1; give it a token, "+
		"point it at loopback, or remove it with: aide gateway remove %s", name, gw.BaseURL, name)
}

// IsGatewayName reports whether name is a valid gateway name, safe to
// use as a path segment.
func IsGatewayName(name string) bool { return gatewayIdentRE.MatchString(name) }

// EnvVarName is Key uppercased: the variable the key's name must be
// safe as. Launches inject the token as GatewayKeyVar, not under it.
func (gw GatewayConfig) EnvVarName() string {
	return strings.ToUpper(gw.Key)
}

// NormalizeGatewayBaseURL trims surrounding space, trailing slashes and
// one trailing /v1 segment, so https://gw.example.com/v1/ and
// https://gw.example.com name the same gateway.
func NormalizeGatewayBaseURL(raw string) string {
	s := strings.TrimRight(strings.TrimSpace(raw), "/")
	s = strings.TrimSuffix(s, "/v1")
	return strings.TrimRight(s, "/")
}

// ValidateGateways checks ctx's gateways: names and stores are safe path
// segments, base_url is https (http only on loopback) without
// credentials, query or fragment, refresh_ttl is a positive duration,
// key yields a variable a credential may set and that is not blocked,
// and harnesses lists supported harnesses once each. at prefixes errors
// (e.g. "profiles.work").
func ValidateGateways(at string, ctx Context, neverAllowEnv []string) error {
	for _, name := range slices.Sorted(maps.Keys(ctx.Gateways)) {
		if err := validateGateway(name, ctx.Gateways[name], ctx, neverAllowEnv); err != nil {
			return fmt.Errorf("%w: %s: gateways.%s: %s", ErrInvalidGateway, at, name, err)
		}
	}
	return nil
}

func validateGateway(name string, gw GatewayConfig, ctx Context, neverAllowEnv []string) error {
	if !IsGatewayName(name) {
		return errors.New("name must be letters, digits, '-' or '_', starting with a letter or digit")
	}
	if _, taken := ctx.Credentials[name]; taken {
		return fmt.Errorf("credentials.%s already exists; a gateway and a credential cannot share a name", name)
	}
	for _, k := range slices.Sorted(maps.Keys(ctx.Env)) {
		if envscrub.IsProviderSecretName(k) && legacyBindingName(k) == name {
			return fmt.Errorf("env: %s already becomes credential %q at launch; give the gateway another name", k, name)
		}
	}
	if err := checkGatewayURL(gw.BaseURL); err != nil {
		return err
	}
	switch {
	case gw.Store == "" && gw.Key == "":
		// no-auth
	case gw.Store == "":
		return errors.New("store: is required when key is set")
	case gw.Key == "":
		return errors.New("key: is required when store is set")
	default:
		if !gatewayIdentRE.MatchString(gw.Store) {
			return fmt.Errorf("store: %q must be a secret store name (letters, digits, '-' or '_')", gw.Store)
		}
		if err := checkGatewayKey(gw, neverAllowEnv); err != nil {
			return err
		}
	}
	if _, err := gw.EffectiveTTL(); err != nil {
		return fmt.Errorf("refresh_ttl: %v (use a Go duration such as 12h or 24h)", err)
	}
	seen := map[string]bool{}
	for _, h := range gw.Harnesses {
		if !slices.Contains(DefaultGatewayHarnesses, h) {
			return fmt.Errorf("harnesses: %q is not a gateway harness (use %s)", h, strings.Join(DefaultGatewayHarnesses, ", "))
		}
		if seen[h] {
			return fmt.Errorf("harnesses: %q is listed twice", h)
		}
		seen[h] = true
	}
	return nil
}

// checkGatewayURL enforces https except on loopback, and rejects
// embedded credentials, query strings and fragments.
func checkGatewayURL(raw string) error {
	if raw == "" {
		return errors.New("base_url: is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("base_url: %v", err)
	}
	switch {
	case u.User != nil:
		return errors.New("base_url: must not embed credentials; the token comes from store and key")
	case u.RawQuery != "" || u.ForceQuery:
		return errors.New("base_url: must not carry a query string")
	case u.Fragment != "":
		return errors.New("base_url: must not carry a fragment")
	case u.Hostname() == "":
		return fmt.Errorf("base_url: %q has no host", raw)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if isLoopbackHost(u.Hostname()) {
			return nil
		}
	}
	return fmt.Errorf("base_url: %q must use https (http is allowed only for localhost, 127.0.0.1 and ::1)", raw)
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.Equal(net.IPv4(127, 0, 0, 1)) || ip.Equal(net.IPv6loopback))
}

// checkGatewayKey makes sure the key's variable is one a credential may
// set and not a loader, proxy, TLS or blocked variable.
func checkGatewayKey(gw GatewayConfig, neverAllowEnv []string) error {
	if !gatewayKeyRE.MatchString(gw.Key) {
		return fmt.Errorf("key: %q must be letters, digits and '_', not starting with a digit", gw.Key)
	}
	env := gw.EnvVarName()
	if err := checkBindingEnv("key", env); err != nil {
		return err
	}
	if envscrub.IsProviderName(env) {
		return fmt.Errorf("key: %s is a provider variable that agents send to their own endpoints; choose another key", env)
	}
	if slices.Contains(neverAllowEnv, env) {
		return fmt.Errorf("key: %s is in never_allow_env", env)
	}
	for _, d := range gatewayDangerousEnv {
		if p, ok := strings.CutSuffix(d, "*"); ok && strings.HasPrefix(env, p) || env == d {
			return fmt.Errorf("key: %s changes how agents load code, trust TLS or route traffic; choose another key", env)
		}
	}
	return nil
}

// GatewayBinding is the credential binding gw injects: Claude Code gets
// its own fixed variables plus the endpoint, the other harnesses get
// the token as GatewayKeyVar(name).
func GatewayBinding(name string, gw GatewayConfig) CredentialBinding {
	routes := map[string]CredentialRoute{}
	for _, h := range gw.EffectiveHarnesses() {
		if h == "claude" {
			env := []string{}
			set := map[string]string{
				claudeGatewayBaseURLEnv:   NormalizeGatewayBaseURL(gw.BaseURL),
				claudeGatewayDiscoveryEnv: "1",
				claudeGatewayAPIKeyEnv:    "",
			}
			if e := gw.TokenEnv(name, h); e != "" {
				env = []string{e}
			} else {
				set[claudeGatewayTokenEnv] = KeylessPlaceholder
			}
			routes[h] = CredentialRoute{Env: env, Set: set}
			continue
		}
		if e := gw.TokenEnv(name, h); e != "" {
			routes[h] = CredentialRoute{Env: []string{e}}
		} else {
			routes[h] = CredentialRoute{}
		}
	}
	b := CredentialBinding{Store: gw.Store, Key: gw.Key, For: routes}
	if gw.RequiresToken() {
		b.Backend = CredentialBackendAge
	}
	return b
}

// synthesizeGatewayCredentials, used only to translate a legacy profile in
// memory, returns ctx with a credential binding
// per gateway, named after the gateway. A gateway is appended to
// DefaultCredentials whenever at least one of its target harnesses is
// not already claimed by an earlier default credential (or earlier
// gateway in sorted order). DefaultCredentialsForHarness resolves the
// effective default list per harness.
// ctx is not modified and nothing is written, so the bindings never
// reach config.yaml.
func synthesizeGatewayCredentials(ctx Context) (Context, error) {
	if len(ctx.Gateways) == 0 {
		return ctx, nil
	}
	ctx.Credentials = maps.Clone(ctx.Credentials)
	if ctx.Credentials == nil {
		ctx.Credentials = map[string]CredentialBinding{}
	}
	ctx.DefaultCredentials = slices.Clone(ctx.DefaultCredentials)
	names := slices.Sorted(maps.Keys(ctx.Gateways))
	for _, name := range names {
		if _, taken := ctx.Credentials[name]; taken {
			return ctx, fmt.Errorf("%w: gateways.%s: credentials.%s already exists", ErrInvalidGateway, name, name)
		}
		ctx.Credentials[name] = GatewayBinding(name, ctx.Gateways[name])
	}
	for _, name := range names {
		if slices.Contains(ctx.DefaultCredentials, name) {
			continue
		}
		candidate := ctx
		candidate.DefaultCredentials = append(slices.Clone(ctx.DefaultCredentials), name)
		servesAny := false
		for _, h := range ctx.Gateways[name].EffectiveHarnesses() {
			if slices.Contains(DefaultCredentialsForHarness(candidate, h), name) {
				servesAny = true
				break
			}
		}
		if servesAny {
			ctx.DefaultCredentials = candidate.DefaultCredentials
		}
	}
	return ctx, nil
}

// DefaultCredentialsForHarness returns the ordered slice of default
// credential names from synthCtx.DefaultCredentials that apply to
// harnessName, skipping any binding whose route variables on
// harnessName collide with an earlier binding or that would claim a
// second LLM provider/gateway slot on harnessName.
func DefaultCredentialsForHarness(synthCtx Context, harnessName string) []string {
	if harnessName == "" {
		return slices.Clone(synthCtx.DefaultCredentials)
	}
	h := canonicalHarness(harnessName)
	var out []string
	claimedVars := map[string]bool{}
	providerClaimed := false
	gatewayClaimed := false
	for _, name := range slices.Compact(slices.Clone(synthCtx.DefaultCredentials)) {
		b, ok := synthCtx.Credentials[name]
		if !ok {
			// Preserve unknown names so PlanCredentials can report ErrCredentialNotFound.
			out = append(out, name)
			continue
		}
		if !b.DefaultAllowsHarness(name, h) {
			continue
		}
		r, ok := b.Route(name, h)
		if !ok {
			continue
		}
		_, isGW := synthCtx.Gateways[name]
		isProvider := isGW || b.IsLLMProvider(name)
		if (isGW && providerClaimed) || (isProvider && gatewayClaimed) {
			continue
		}
		vars := append(slices.Clone(r.Env), slices.Sorted(maps.Keys(r.Set))...)
		conflict := false
		for _, v := range vars {
			if claimedVars[v] {
				conflict = true
				break
			}
		}
		if conflict {
			continue
		}
		out = append(out, name)
		for _, v := range vars {
			claimedVars[v] = true
		}
		if isProvider {
			providerClaimed = true
		}
		if isGW {
			gatewayClaimed = true
		}
	}
	return out
}

// gatewayScrubNames are the variables any gateway may inject, scrubbed
// from an inherited environment whenever a profile declares a gateway.
func gatewayScrubNames(name string, gw GatewayConfig) []string {
	return []string{gw.EnvVarName(), GatewayKeyVar(name), claudeGatewayTokenEnv, claudeGatewayBaseURLEnv, claudeGatewayAPIKeyEnv, claudeGatewayDiscoveryEnv}
}
