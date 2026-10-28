package config

import (
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
	"github.com/jskswamy/aide/internal/harness"
)

// GatewayConfig is a custom LLM gateway on a profile: where it is,
// which secret holds its bearer token, and which harnesses use it.
// Launches inject it through a credential binding synthesized in memory
// (SynthesizeGatewayCredentials); nothing is duplicated on disk.
type GatewayConfig struct {
	BaseURL    string   `yaml:"base_url" json:"base_url"`
	RefreshTTL string   `yaml:"refresh_ttl,omitempty" json:"refresh_ttl,omitempty"`
	Store      string   `yaml:"store" json:"store"`
	Key        string   `yaml:"key" json:"key"`
	Harnesses  []string `yaml:"harnesses,omitempty" json:"harnesses,omitempty"`
}

// DefaultGatewayHarnesses are the harnesses a gateway serves when its
// harnesses: list is omitted: the profile-capable harnesses that accept
// a custom endpoint and model catalogue.
var DefaultGatewayHarnesses = []string{"claude", "pi", "opencode", "codex"}

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

// TokenEnv is the variable harness h reads gw's token from: Claude
// Code's fixed ANTHROPIC_AUTH_TOKEN, else EnvVarName.
func (gw GatewayConfig) TokenEnv(h string) string {
	if h == "claude" {
		return claudeGatewayTokenEnv
	}
	return gw.EnvVarName()
}

// IsGatewayName reports whether name is a valid gateway name, safe to
// use as a path segment.
func IsGatewayName(name string) bool { return gatewayIdentRE.MatchString(name) }

// EnvVarName is the variable pi, opencode and codex read the token
// from: Key uppercased.
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
	if !gatewayIdentRE.MatchString(gw.Store) {
		return fmt.Errorf("store: %q must be a secret store name (letters, digits, '-' or '_')", gw.Store)
	}
	if _, err := gw.EffectiveTTL(); err != nil {
		return fmt.Errorf("refresh_ttl: %v (use a Go duration such as 12h or 24h)", err)
	}
	if err := checkGatewayKey(gw, neverAllowEnv); err != nil {
		return err
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

// gatewayBinding is the credential binding gw injects: Claude Code gets
// its own fixed variables plus the endpoint, the other harnesses get
// the token as EnvVarName.
func gatewayBinding(gw GatewayConfig) CredentialBinding {
	routes := map[string]CredentialRoute{}
	for _, h := range gw.EffectiveHarnesses() {
		if h == "claude" {
			routes[h] = CredentialRoute{
				Env: []string{gw.TokenEnv(h)},
				Set: map[string]string{
					claudeGatewayBaseURLEnv:   NormalizeGatewayBaseURL(gw.BaseURL),
					claudeGatewayDiscoveryEnv: "1",
					claudeGatewayAPIKeyEnv:    "",
				},
			}
			continue
		}
		routes[h] = CredentialRoute{Env: []string{gw.TokenEnv(h)}}
	}
	return CredentialBinding{Backend: CredentialBackendAge, Store: gw.Store, Key: gw.Key, For: routes}
}

// SynthesizeGatewayCredentials returns ctx with a credential binding
// per gateway, named after the gateway. A gateway becomes a default
// credential unless a binding already in default_credentials (or an
// earlier gateway, in name order) sets one of the same variables for
// the same harness; select it per launch with --credential instead.
// ctx is not modified and nothing is written, so the bindings never
// reach config.yaml.
func SynthesizeGatewayCredentials(ctx Context) (Context, error) {
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
		ctx.Credentials[name] = gatewayBinding(ctx.Gateways[name])
	}
	claimed := claimedDefaultVars(ctx)
	for _, name := range names {
		if slices.Contains(ctx.DefaultCredentials, name) {
			continue
		}
		vars := routeVars(name, ctx.Credentials[name])
		if collides(claimed, vars) {
			continue
		}
		ctx.DefaultCredentials = append(ctx.DefaultCredentials, name)
		for h, vs := range vars {
			claimed[h] = append(claimed[h], vs...)
		}
	}
	return ctx, nil
}

// routeVars maps each launchable harness b serves to the variables its
// route sets.
func routeVars(name string, b CredentialBinding) map[string][]string {
	out := map[string][]string{}
	for _, h := range launchableHarnesses() {
		if r, ok := b.Route(name, h); ok {
			out[h] = append(r.Env, slices.Sorted(maps.Keys(r.Set))...)
		}
	}
	return out
}

func launchableHarnesses() []string {
	var out []string
	for _, d := range harness.All() {
		if d.Launchable() {
			out = append(out, d.Name)
		}
	}
	return out
}

// claimedDefaultVars maps each harness to the variables ctx's default
// bindings already set for it.
func claimedDefaultVars(ctx Context) map[string][]string {
	claimed := map[string][]string{}
	for _, name := range ctx.DefaultCredentials {
		b, ok := ctx.Credentials[name]
		if !ok {
			continue
		}
		for h, vs := range routeVars(name, b) {
			claimed[h] = append(claimed[h], vs...)
		}
	}
	return claimed
}

func collides(claimed, vars map[string][]string) bool {
	for h, vs := range vars {
		for _, v := range vs {
			if slices.Contains(claimed[h], v) {
				return true
			}
		}
	}
	return false
}

// gatewayScrubNames are the variables any gateway may inject, scrubbed
// from an inherited environment whenever a profile declares a gateway.
func gatewayScrubNames(gw GatewayConfig) []string {
	return []string{gw.EnvVarName(), claudeGatewayTokenEnv, claudeGatewayBaseURLEnv, claudeGatewayAPIKeyEnv, claudeGatewayDiscoveryEnv}
}
