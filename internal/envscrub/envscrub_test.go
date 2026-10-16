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
