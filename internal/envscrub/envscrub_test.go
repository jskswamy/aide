package envscrub_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/envscrub"
	"github.com/jskswamy/aide/internal/harness"
)

func names(env []string) []string {
	var out []string
	for _, e := range env {
		k, _, _ := strings.Cut(e, "=")
		out = append(out, k)
	}
	return out
}

func TestCredentialsScrubsProviderTokensFederationCloudAndSops(t *testing.T) {
	env := []string{
		"PATH=/usr/bin",
		"HOME=/home/u",
		"ANTHROPIC_API_KEY=sk-ant",
		"ANTHROPIC_AUTH_TOKEN=tok",
		"CLAUDE_CODE_OAUTH_TOKEN=oauth",
		"ANTHROPIC_FEDERATION_RULE_ID=fr",
		"CLAUDE_CODE_USE_BEDROCK=1",
		"OPENAI_API_KEY=sk-oa",
		"OPENAI_BASE_URL=https://proxy",
		"CODEX_API_KEY=cx",
		"CODEX_ACCESS_TOKEN=cxa",
		"AWS_ACCESS_KEY_ID=AKIA",
		"AWS_PROFILE=prod",
		"CLOUDSDK_CORE_PROJECT=p",
		"GOOGLE_APPLICATION_CREDENTIALS=/adc.json",
		"SOPS_AGE_KEY=AGE-SECRET-KEY-1",
		"SOPS_AGE_KEY_FILE=/k.txt",
		"CLAUDE_CONFIG_DIR=/elsewhere",
		"EDITOR=vim",
	}
	got := names(envscrub.ScrubCredentials(env, envscrub.Credentials(), nil))
	want := []string{"PATH", "HOME", "CLAUDE_CONFIG_DIR", "EDITOR"}
	if !slices.Equal(got, want) {
		t.Errorf("scrubbed env = %v, want %v", got, want)
	}
}

func TestSecretMaterialScrubsOnlySops(t *testing.T) {
	env := []string{"ANTHROPIC_API_KEY=k", "SOPS_AGE_KEY=s", "SOPS_AGE_KEY_FILE=f", "AWS_PROFILE=p"}
	got := names(envscrub.ScrubCredentials(env, envscrub.SecretMaterial(), nil))
	want := []string{"ANTHROPIC_API_KEY", "AWS_PROFILE"}
	if !slices.Equal(got, want) {
		t.Errorf("scrubbed env = %v, want %v", got, want)
	}
}

func TestExtraNamesAndPrefixes(t *testing.T) {
	env := []string{"MY_PROVIDER_TOKEN=x", "CORP_A=1", "CORP_B=2", "CORPORATE=3", "KEEP=1"}
	got := names(envscrub.ScrubCredentials(env, envscrub.SecretMaterial(), []string{"MY_PROVIDER_TOKEN", "CORP_*"}))
	want := []string{"CORPORATE", "KEEP"}
	if !slices.Equal(got, want) {
		t.Errorf("scrubbed env = %v, want %v", got, want)
	}
}

func TestConfigDirRedirectsDerivedFromHarnessTable(t *testing.T) {
	got := envscrub.ConfigDirRedirects()
	for _, d := range harness.All() {
		for _, k := range d.ConfigEnvKeys {
			if !slices.Contains(got, k) {
				t.Errorf("ConfigDirRedirects() missing %s (harness %s)", k, d.Name)
			}
		}
	}
	for _, k := range []string{"CLAUDE_CONFIG_DIR", "CODEX_HOME", "PI_CODING_AGENT_DIR", "COPILOT_HOME", "OPENCODE_CONFIG"} {
		if !slices.Contains(got, k) {
			t.Errorf("ConfigDirRedirects() missing %s", k)
		}
	}
	if !slices.IsSorted(got) {
		t.Errorf("ConfigDirRedirects() not sorted: %v", got)
	}
}

func TestWithAddsNamesWithoutMutatingReceiver(t *testing.T) {
	base := envscrub.SecretMaterial()
	ext := base.With(envscrub.ConfigDirRedirects()...)
	if base.Matches("CLAUDE_CONFIG_DIR") {
		t.Error("With mutated the receiver")
	}
	if !ext.Matches("CLAUDE_CONFIG_DIR") || !ext.Matches("SOPS_AGE_KEY") {
		t.Error("With lost names")
	}
}

func TestPermitExemptsOnlyCloudNames(t *testing.T) {
	reg := envscrub.Credentials().Permit("AWS_PROFILE", "GOOGLE_APPLICATION_CREDENTIALS", "ANTHROPIC_API_KEY", "SOPS_AGE_KEY")
	for name, want := range map[string]bool{
		"AWS_PROFILE":                    false,
		"GOOGLE_APPLICATION_CREDENTIALS": false,
		"AWS_SECRET_ACCESS_KEY":          true, // not permitted
		"ANTHROPIC_API_KEY":              true, // provider keys cannot be permitted
		"SOPS_AGE_KEY":                   true, // secret material cannot be permitted
	} {
		if got := reg.Matches(name); got != want {
			t.Errorf("Matches(%s) = %v, want %v", name, got, want)
		}
	}
}

func TestLegacyCredentialNames(t *testing.T) {
	env := map[string]string{
		"OPENAI_API_KEY": "{{ .secrets.openai }}",
		"MY_TOKEN":       "x",
		"FOO":            "bar",
		"SOPS_AGE_KEY":   "k",
	}
	got := envscrub.Credentials().With("MY_TOKEN").MatchingNames(env)
	want := []string{"MY_TOKEN", "OPENAI_API_KEY", "SOPS_AGE_KEY"}
	if !slices.Equal(got, want) {
		t.Errorf("MatchingNames = %v, want %v", got, want)
	}
}

func TestScrubDoesNotAliasInput(t *testing.T) {
	env := []string{"A=1", "SOPS_AGE_KEY=2", "B=3"}
	_ = envscrub.ScrubCredentials(env, envscrub.SecretMaterial(), nil)
	if env[1] != "SOPS_AGE_KEY=2" {
		t.Errorf("input mutated: %v", env)
	}
}

func TestPermitNeverReadmitsProviderNamesInCloudNamespace(t *testing.T) {
	reg := envscrub.Credentials().Permit("AWS_BEARER_TOKEN_BEDROCK", "AWS_PROFILE")
	if !reg.Matches("AWS_BEARER_TOKEN_BEDROCK") {
		t.Error("AWS_BEARER_TOKEN_BEDROCK (a provider key) was re-admitted")
	}
	if reg.Matches("AWS_PROFILE") {
		t.Error("AWS_PROFILE should be re-admitted")
	}
	if !envscrub.Credentials().Matches("AWS_BEARER_TOKEN_BEDROCK") {
		t.Error("AWS_BEARER_TOKEN_BEDROCK not scrubbed")
	}
}

func TestPermitNeverReadmitsCustomNames(t *testing.T) {
	reg := envscrub.Credentials().Permit("AWS_PROFILE", "AWS_REGION")
	env := []string{"AWS_PROFILE=prod", "AWS_REGION=eu"}
	got := names(envscrub.ScrubCredentials(env, reg, []string{"AWS_PROFILE"}))
	if want := []string{"AWS_REGION"}; !slices.Equal(got, want) {
		t.Errorf("scrubbed env = %v, want %v (scrub_env beats a capability grant)", got, want)
	}
	// Order independent: permitting after pinning still cannot re-admit.
	pinned := envscrub.Credentials().Pin("AWS_PROFILE").Permit("AWS_PROFILE")
	if !pinned.Matches("AWS_PROFILE") {
		t.Error("Permit after Pin re-admitted a custom name")
	}
}

func TestIsProviderSecretName(t *testing.T) {
	for _, n := range []string{"ANTHROPIC_API_KEY", "OPENROUTER_API_KEY", "CODEX_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN",
		"AWS_BEARER_TOKEN_BEDROCK", "ANTHROPIC_IDENTITY_TOKEN_FILE", "ANTHROPIC_PROFILE"} {
		if !envscrub.IsProviderSecretName(n) {
			t.Errorf("IsProviderSecretName(%q) = false, want true", n)
		}
	}
	// Selectors and endpoints are configuration, not secrets.
	for _, n := range []string{"CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY",
		"ANTHROPIC_VERTEX_PROJECT_ID", "OPENAI_BASE_URL", "AWS_PROFILE", "SOPS_AGE_KEY", "FOO", ""} {
		if envscrub.IsProviderSecretName(n) {
			t.Errorf("IsProviderSecretName(%q) = true, want false", n)
		}
	}
}

func TestProviderSelectorsStayScrubbedAndPinned(t *testing.T) {
	reg := envscrub.Credentials().Permit("CLAUDE_CODE_USE_BEDROCK", "OPENAI_BASE_URL", "AWS_BEARER_TOKEN_BEDROCK")
	for _, n := range []string{"CLAUDE_CODE_USE_BEDROCK", "OPENAI_BASE_URL", "AWS_BEARER_TOKEN_BEDROCK"} {
		if !reg.Matches(n) {
			t.Errorf("%s is no longer scrubbed from the inherited env", n)
		}
	}
	if !envscrub.IsProviderSelectorName("CLAUDE_CODE_USE_BEDROCK") || envscrub.IsProviderSelectorName("ANTHROPIC_API_KEY") {
		t.Error("IsProviderSelectorName misclassifies")
	}
}

func TestEndpointSelectorsAndProviderNames(t *testing.T) {
	for _, n := range []string{"ANTHROPIC_BASE_URL", "ANTHROPIC_BEDROCK_BASE_URL", "ANTHROPIC_VERTEX_BASE_URL", "OPENAI_API_BASE", "AZURE_OPENAI_ENDPOINT"} {
		if !envscrub.IsProviderSelectorName(n) || envscrub.IsProviderSecretName(n) {
			t.Errorf("%s should be a selector", n)
		}
		if !envscrub.Credentials().Matches(n) {
			t.Errorf("%s is not scrubbed from the shell", n)
		}
	}
	for _, n := range []string{"ANTHROPIC_API_KEY", "OPENAI_BASE_URL"} {
		if !envscrub.IsProviderName(n) {
			t.Errorf("IsProviderName(%q) = false", n)
		}
	}
	if envscrub.IsProviderName("AWS_PROFILE") {
		t.Error("IsProviderName(AWS_PROFILE) = true")
	}
	if !envscrub.IsSecretMaterialName("SOPS_AGE_KEY_CMD") {
		t.Error("SOPS_AGE_KEY_CMD is not secret material")
	}
}

// Claude Code's authentication variables, from the spec's Claude
// admission row and Anthropic's docs (code.claude.com/docs/en/env-vars,
// /authentication; platform.claude.com/docs/en/manage-claude/wif-reference).
func TestClaudeCodeAuthNamesClassified(t *testing.T) {
	secrets := []string{
		"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN",
		"CLAUDE_CODE_OAUTH_REFRESH_TOKEN", "ANTHROPIC_PROFILE",
		"ANTHROPIC_IDENTITY_TOKEN_FILE", "ANTHROPIC_IDENTITY_TOKEN",
		"ANTHROPIC_FOUNDRY_API_KEY", "ANTHROPIC_FOUNDRY_AUTH_TOKEN",
		"ANTHROPIC_AWS_API_KEY", "AWS_BEARER_TOKEN_BEDROCK",
		"ANTHROPIC_CUSTOM_HEADERS", "CLAUDE_CODE_CLIENT_KEY",
		"CLAUDE_CODE_CLIENT_KEY_PASSPHRASE",
	}
	selectors := []string{
		"ANTHROPIC_FEDERATION_RULE_ID", "ANTHROPIC_ORGANIZATION_ID",
		"ANTHROPIC_SERVICE_ACCOUNT_ID", "ANTHROPIC_WORKSPACE_ID",
		"ANTHROPIC_CONFIG_DIR", "CLAUDE_CODE_OAUTH_SCOPES",
		"CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY",
		"CLAUDE_CODE_USE_ANTHROPIC_AWS", "CLAUDE_CODE_USE_MANTLE",
		"CLAUDE_CODE_SKIP_BEDROCK_AUTH", "CLAUDE_CODE_SKIP_VERTEX_AUTH",
		"CLAUDE_CODE_SKIP_FOUNDRY_AUTH", "CLAUDE_CODE_SKIP_MANTLE_AUTH",
		"CLAUDE_CODE_SKIP_ANTHROPIC_AWS_AUTH",
		"ANTHROPIC_AWS_BASE_URL", "ANTHROPIC_AWS_WORKSPACE_ID",
		"ANTHROPIC_BEDROCK_MANTLE_BASE_URL", "CLAUDE_CODE_CLIENT_CERT",
	}
	cloud := []string{
		"AWS_PROFILE", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN",
		"GOOGLE_APPLICATION_CREDENTIALS", "CLOUDSDK_CONFIG",
		"AZURE_CLIENT_ID", "AZURE_TENANT_ID", "AZURE_CLIENT_SECRET",
		"AZURE_CLIENT_CERTIFICATE_PATH", "AZURE_CLIENT_CERTIFICATE_PASSWORD",
		"AZURE_FEDERATED_TOKEN_FILE", "AZURE_USERNAME", "AZURE_PASSWORD",
	}
	reg := envscrub.Credentials()
	for _, n := range secrets {
		if !envscrub.IsProviderSecretName(n) || envscrub.IsProviderSelectorName(n) {
			t.Errorf("%s should be a secret-bearing provider name", n)
		}
		if !reg.Permit(n).Matches(n) {
			t.Errorf("%s is not pinned in the scrub registry", n)
		}
	}
	for _, n := range selectors {
		if !envscrub.IsProviderSelectorName(n) || envscrub.IsProviderSecretName(n) {
			t.Errorf("%s should be a provider selector", n)
		}
		if !reg.Permit(n).Matches(n) {
			t.Errorf("%s is not pinned in the scrub registry", n)
		}
	}
	for _, n := range cloud {
		if envscrub.IsProviderName(n) {
			t.Errorf("%s should be an ambient cloud name, not a provider name", n)
		}
		if !reg.Matches(n) {
			t.Errorf("%s is not scrubbed", n)
		}
		if reg.Permit(n).Matches(n) {
			t.Errorf("%s should be re-admittable by a capability's env_allow", n)
		}
	}
}

// piSecretNames are Pi's provider keys and tokens, from Pi's provider
// docs (pi.dev/docs/latest/providers; earendil-works/pi
// packages/coding-agent/docs/providers.md).
var piSecretNames = []string{
	"ANTHROPIC_API_KEY", "ANTHROPIC_OAUTH_TOKEN", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_IDENTITY_TOKEN_FILE",
	"ANT_LING_API_KEY", "OPENAI_API_KEY", "AZURE_OPENAI_API_KEY", "DEEPSEEK_API_KEY", "NVIDIA_API_KEY",
	"GEMINI_API_KEY", "GOOGLE_CLOUD_API_KEY", "COPILOT_GITHUB_TOKEN", "MISTRAL_API_KEY", "GROQ_API_KEY",
	"CEREBRAS_API_KEY", "CLOUDFLARE_API_KEY", "XAI_API_KEY", "OPENROUTER_API_KEY", "AI_GATEWAY_API_KEY",
	"ZAI_API_KEY", "ZAI_CODING_CN_API_KEY", "OPENCODE_API_KEY", "RADIUS_API_KEY", "TYPESAFE_API_KEY",
	"HF_TOKEN", "FIREWORKS_API_KEY", "TOGETHER_API_KEY", "BASETEN_API_KEY", "KIMI_API_KEY", "META_API_KEY",
	"MINIMAX_API_KEY", "MINIMAX_CN_API_KEY", "MOONSHOT_API_KEY", "QWEN_TOKEN_PLAN_API_KEY",
	"QWEN_TOKEN_PLAN_CN_API_KEY", "XIAOMI_API_KEY", "XIAOMI_TOKEN_PLAN_CN_API_KEY",
	"XIAOMI_TOKEN_PLAN_AMS_API_KEY", "XIAOMI_TOKEN_PLAN_SGP_API_KEY", "AWS_BEARER_TOKEN_BEDROCK",
}

// piSelectorNames are Pi's non-secret provider endpoints and selectors.
var piSelectorNames = []string{
	"AZURE_OPENAI_BASE_URL", "AZURE_OPENAI_RESOURCE_NAME", "AZURE_OPENAI_DEPLOYMENT_NAME_MAP",
	"AZURE_OPENAI_API_VERSION", "CLOUDFLARE_ACCOUNT_ID", "CLOUDFLARE_GATEWAY_ID", "GOOGLE_CLOUD_LOCATION",
	"ANTHROPIC_FEDERATION_RULE_ID", "ANTHROPIC_ORGANIZATION_ID", "ANTHROPIC_SERVICE_ACCOUNT_ID",
	"ANTHROPIC_WORKSPACE_ID",
}

func TestPiProviderNamesClassified(t *testing.T) {
	reg := envscrub.Credentials()
	for _, n := range piSecretNames {
		if !envscrub.IsProviderSecretName(n) || envscrub.IsProviderSelectorName(n) {
			t.Errorf("%s should be a secret-bearing provider name", n)
		}
		if !reg.Permit(n).Matches(n) {
			t.Errorf("%s is not pinned in the scrub registry", n)
		}
	}
	for _, n := range piSelectorNames {
		if !envscrub.IsProviderSelectorName(n) || envscrub.IsProviderSecretName(n) {
			t.Errorf("%s should be a provider selector", n)
		}
		if !reg.Permit(n).Matches(n) {
			t.Errorf("%s is not pinned in the scrub registry", n)
		}
	}
	// Bedrock's and Vertex's ambient credentials stay cloud names that
	// the aws/gcp capabilities can re-admit.
	for _, n := range []string{"AWS_PROFILE", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN",
		"AWS_REGION", "AWS_DEFAULT_REGION", "AWS_CONTAINER_CREDENTIALS_FULL_URI", "AWS_WEB_IDENTITY_TOKEN_FILE",
		"GOOGLE_APPLICATION_CREDENTIALS", "GOOGLE_CLOUD_PROJECT", "GCLOUD_PROJECT"} {
		if envscrub.IsProviderName(n) || !reg.Matches(n) || reg.Permit(n).Matches(n) {
			t.Errorf("%s should be a scrubbed, re-admittable cloud name", n)
		}
	}
}
