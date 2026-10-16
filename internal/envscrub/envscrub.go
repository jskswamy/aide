// Package envscrub is the single registry of environment variables
// that must not leak from the user's shell into an agent: provider API
// keys, bearer/OAuth tokens, federation and cloud selectors, aide's own
// secret material, and (for launches that select a native config dir)
// harness config-dir redirects.
//
// Adding a name is a one-line change to the lists below. Harness
// config-dir redirects are not listed here; they are derived from the
// harness table (internal/harness ConfigEnvKeys).
//
// Limits: the registry can only match names it knows. Arbitrary
// variables holding credentials under unknown names pass through unless
// the user lists them (top-level scrub_env:), and files or keychains
// that scrubbed variables point at (~/.aws/credentials, gcloud ADC,
// system keychains) stay readable wherever the sandbox allows it.
package envscrub

import (
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/jskswamy/aide/internal/harness"
)

// providerSecretNames are secret-bearing provider credentials: API
// keys, bearer/OAuth tokens and auth-file/profile selectors. In a
// profile they are set only through credentials: bindings.
//
// Claude Code names follow https://code.claude.com/docs/en/env-vars,
// https://code.claude.com/docs/en/authentication and
// https://platform.claude.com/docs/en/manage-claude/wif-reference.
// Pi names follow https://pi.dev/docs/latest/providers (Pi's
// docs/providers.md).
var providerSecretNames = []string{
	// Anthropic / Claude Code
	"ANTHROPIC_API_KEY",
	"ANTHROPIC_AUTH_TOKEN",
	"CLAUDE_CODE_OAUTH_TOKEN",
	"CLAUDE_CODE_OAUTH_REFRESH_TOKEN",
	"ANTHROPIC_PROFILE",
	"ANTHROPIC_IDENTITY_TOKEN",
	"ANTHROPIC_IDENTITY_TOKEN_FILE",
	"ANTHROPIC_FOUNDRY_API_KEY",
	"ANTHROPIC_FOUNDRY_AUTH_TOKEN",
	"ANTHROPIC_AWS_API_KEY",
	"AWS_BEARER_TOKEN_BEDROCK",
	// May carry an Authorization header.
	"ANTHROPIC_CUSTOM_HEADERS",
	// mTLS client key and its passphrase.
	"CLAUDE_CODE_CLIENT_KEY",
	"CLAUDE_CODE_CLIENT_KEY_PASSPHRASE",
	// OpenAI / Codex
	"CODEX_API_KEY",
	"CODEX_ACCESS_TOKEN",
	"OPENAI_API_KEY",
	"AZURE_OPENAI_API_KEY",
	// Other model providers
	"GEMINI_API_KEY",
	"GOOGLE_API_KEY",
	"OPENROUTER_API_KEY",
	"MISTRAL_API_KEY",
	"GROQ_API_KEY",
	"XAI_API_KEY",
	"DEEPSEEK_API_KEY",
	// Pi's remaining providers
	"ANTHROPIC_OAUTH_TOKEN",
	"ANT_LING_API_KEY",
	"NVIDIA_API_KEY",
	"GOOGLE_CLOUD_API_KEY",
	"COPILOT_GITHUB_TOKEN",
	"CEREBRAS_API_KEY",
	"CLOUDFLARE_API_KEY",
	"AI_GATEWAY_API_KEY",
	"ZAI_API_KEY",
	"ZAI_CODING_CN_API_KEY",
	"OPENCODE_API_KEY",
	"RADIUS_API_KEY",
	"TYPESAFE_API_KEY",
	"HF_TOKEN",
	"FIREWORKS_API_KEY",
	"TOGETHER_API_KEY",
	"BASETEN_API_KEY",
	"KIMI_API_KEY",
	"META_API_KEY",
	"MINIMAX_API_KEY",
	"MINIMAX_CN_API_KEY",
	"MOONSHOT_API_KEY",
	"QWEN_TOKEN_PLAN_API_KEY",
	"QWEN_TOKEN_PLAN_CN_API_KEY",
	"XIAOMI_API_KEY",
	"XIAOMI_TOKEN_PLAN_CN_API_KEY",
	"XIAOMI_TOKEN_PLAN_AMS_API_KEY",
	"XIAOMI_TOKEN_PLAN_SGP_API_KEY",
}

// providerSelectorNames are non-secret provider selectors, endpoints and
// federation identifiers. A profile may set them in env: as ordinary
// configuration; an inherited shell value is still scrubbed.
var providerSelectorNames = []string{
	"ANTHROPIC_FEDERATION_RULE_ID",
	"ANTHROPIC_ORGANIZATION_ID",
	"ANTHROPIC_SERVICE_ACCOUNT_ID",
	"ANTHROPIC_WORKSPACE_ID",
	"ANTHROPIC_AWS_WORKSPACE_ID",
	"ANTHROPIC_VERTEX_PROJECT_ID",
	"CLAUDE_CODE_OAUTH_SCOPES",
	"CLAUDE_CODE_CLIENT_CERT",
	// Where Anthropic profiles (keyless Console sign-in, ant CLI) live;
	// a profile may point it inside its own config dir.
	"ANTHROPIC_CONFIG_DIR",
	"CLAUDE_CODE_USE_BEDROCK",
	"CLAUDE_CODE_USE_VERTEX",
	"CLAUDE_CODE_USE_FOUNDRY",
	"CLAUDE_CODE_USE_ANTHROPIC_AWS",
	"CLAUDE_CODE_USE_MANTLE",
	"CLAUDE_CODE_SKIP_BEDROCK_AUTH",
	"CLAUDE_CODE_SKIP_VERTEX_AUTH",
	"CLAUDE_CODE_SKIP_FOUNDRY_AUTH",
	"CLAUDE_CODE_SKIP_MANTLE_AUTH",
	"CLAUDE_CODE_SKIP_ANTHROPIC_AWS_AUTH",
	"OPENAI_BASE_URL",
	// Endpoints a key can be redirected to.
	"ANTHROPIC_BASE_URL",
	"ANTHROPIC_BEDROCK_BASE_URL",
	"ANTHROPIC_BEDROCK_MANTLE_BASE_URL",
	"ANTHROPIC_AWS_BASE_URL",
	"ANTHROPIC_VERTEX_BASE_URL",
	"ANTHROPIC_FOUNDRY_BASE_URL",
	"ANTHROPIC_FOUNDRY_RESOURCE",
	"OPENAI_API_BASE",
	"AZURE_OPENAI_ENDPOINT",
	"GOOGLE_GEMINI_BASE_URL",
	// Pi's provider endpoints and selectors
	"AZURE_OPENAI_BASE_URL",
	"AZURE_OPENAI_RESOURCE_NAME",
	"AZURE_OPENAI_DEPLOYMENT_NAME_MAP",
	"AZURE_OPENAI_API_VERSION",
	"CLOUDFLARE_ACCOUNT_ID",
	"CLOUDFLARE_GATEWAY_ID",
	"GOOGLE_CLOUD_LOCATION",
}

// cloudNames are ambient cloud credentials and selectors. A trailing
// "*" matches a prefix. Unlike provider names, an active capability's
// env_allow (e.g. aws, gcp) may re-admit them: the user granted that
// cloud access to the agent explicitly.
var cloudNames = []string{
	"AWS_*",
	"CLOUDSDK_*",
	"GOOGLE_APPLICATION_CREDENTIALS",
	"GOOGLE_CLOUD_PROJECT",
	"GCLOUD_PROJECT",
	// Azure default credential chain (Microsoft Foundry).
	"AZURE_CLIENT_ID",
	"AZURE_TENANT_ID",
	"AZURE_CLIENT_SECRET",
	"AZURE_CLIENT_CERTIFICATE_PATH",
	"AZURE_CLIENT_CERTIFICATE_PASSWORD",
	"AZURE_FEDERATED_TOKEN_FILE",
	"AZURE_USERNAME",
	"AZURE_PASSWORD",
}

// secretMaterialNames are aide's own secret-decryption inputs. They are
// scrubbed on every route, including the no-config passthrough.
var secretMaterialNames = []string{
	"SOPS_AGE_KEY",
	"SOPS_AGE_KEY_FILE",
}

// IsProviderSecretName reports whether name is a secret-bearing
// provider credential (API key, token, auth-file or profile selector).
// Such names reach an agent only through an explicit credential
// binding, never through a profile's general env:.
func IsProviderSecretName(name string) bool {
	return slices.Contains(providerSecretNames, name)
}

// IsProviderSelectorName reports whether name is a non-secret provider
// selector or endpoint, which a profile may set in env:.
func IsProviderSelectorName(name string) bool {
	return slices.Contains(providerSelectorNames, name)
}

// IsProviderName reports whether name is a provider credential or a
// provider selector/endpoint. Repository config may set neither.
func IsProviderName(name string) bool {
	return IsProviderSecretName(name) || IsProviderSelectorName(name)
}

// IsSecretMaterialName reports whether name is aide's own secret
// material: any SOPS_AGE_KEY* variable.
func IsSecretMaterialName(name string) bool {
	if strings.HasPrefix(name, "SOPS_AGE_KEY") {
		return true
	}
	return slices.Contains(secretMaterialNames, name)
}

// nameSet holds exact names and prefixes (entries ending in "*").
type nameSet struct {
	exact    map[string]bool
	prefixes []string
}

func (n nameSet) with(names ...string) nameSet {
	out := nameSet{exact: maps.Clone(n.exact), prefixes: slices.Clone(n.prefixes)}
	if out.exact == nil {
		out.exact = map[string]bool{}
	}
	for _, name := range names {
		if p, ok := strings.CutSuffix(name, "*"); ok {
			if p != "" && !slices.Contains(out.prefixes, p) {
				out.prefixes = append(out.prefixes, p)
			}
			continue
		}
		if name != "" {
			out.exact[name] = true
		}
	}
	return out
}

func (n nameSet) matches(name string) bool {
	if n.exact[name] {
		return true
	}
	for _, p := range n.prefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// Registry is a set of variable names (exact, or prefix when ending in
// "*") to scrub. Pinned names are always scrubbed; other names can be
// exempted with Permit. The zero value scrubs nothing. Methods never
// mutate the receiver.
type Registry struct {
	pinned    nameSet
	names     nameSet
	permitted map[string]bool
}

// New returns a registry of names.
func New(names ...string) Registry { return Registry{}.With(names...) }

// SecretMaterial returns the registry of aide's own secret material
// (SOPS_AGE_KEY, SOPS_AGE_KEY_FILE).
func SecretMaterial() Registry { return Registry{}.Pin(secretMaterialNames...) }

// Credentials returns the registry for a configured launch: provider
// keys and tokens, federation and cloud selectors, and secret material.
// Only the cloud names can be exempted with Permit.
func Credentials() Registry {
	return Registry{}.Pin(secretMaterialNames...).Pin(providerSecretNames...).Pin(providerSelectorNames...).With(cloudNames...)
}

// ConfigDirRedirects returns, sorted, every harness config-dir redirect
// variable from the harness table. Scrub them only when the launch
// selects its own native config dir: otherwise a shell-exported
// redirect is the user's explicit global choice.
func ConfigDirRedirects() []string {
	seen := map[string]bool{}
	for _, d := range harness.All() {
		for _, k := range d.ConfigEnvKeys {
			seen[k] = true
		}
	}
	return slices.Sorted(maps.Keys(seen))
}

// With returns a copy of r that also matches names.
func (r Registry) With(names ...string) Registry {
	r.names = r.names.with(names...)
	return r
}

// Pin returns a copy of r that always matches names: Permit cannot
// exempt them. Provider credentials, secret material and the user's
// scrub_env names are pinned.
func (r Registry) Pin(names ...string) Registry {
	r.pinned = r.pinned.with(names...)
	return r
}

// Permit returns a copy of r that no longer matches those of names that
// are ambient cloud variables. Pinned names stay matched whenever they
// were pinned.
func (r Registry) Permit(names ...string) Registry {
	cloud := nameSet{}.with(cloudNames...)
	permitted := maps.Clone(r.permitted)
	if permitted == nil {
		permitted = map[string]bool{}
	}
	for _, n := range names {
		if cloud.matches(n) {
			permitted[n] = true
		}
	}
	r.permitted = permitted
	return r
}

// Matches reports whether name is scrubbed by r.
func (r Registry) Matches(name string) bool {
	if r.pinned.matches(name) {
		return true
	}
	return !r.permitted[name] && r.names.matches(name)
}

// MatchingNames returns, sorted, the keys of env that r matches. Used
// to flag legacy env: entries that carry credentials.
func (r Registry) MatchingNames(env map[string]string) []string {
	var out []string
	for k := range env {
		if r.Matches(k) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// ScrubCredentials returns env (KEY=VALUE entries) without the entries
// whose names reg or extra match. extra holds user-configured names in
// the same exact-or-prefix form; they are pinned, so no Permit on reg
// re-admits them. env is not modified.
func ScrubCredentials(env []string, reg Registry, extra []string) []string {
	if len(extra) > 0 {
		reg = reg.Pin(extra...)
	}
	out := make([]string, 0, len(env))
	for _, e := range env {
		k, _, _ := strings.Cut(e, "=")
		if !reg.Matches(k) {
			out = append(out, e)
		}
	}
	return out
}
