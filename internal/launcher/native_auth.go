package launcher

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jskswamy/aide/internal/config"
)

// nativeAuthNotice describes an authentication source in the selected
// profile's native config that aide cannot scrub, or "" when there is
// none. harnessName is the canonical harness the launch runs
// (config.HarnessOf, so agent aliases count). Only launches that select
// their own config dir are checked.
func nativeAuthNotice(harnessName string, ctx config.Context, home string) string {
	if !ctx.SelectsConfigDir() || harnessName != "claude" {
		return ""
	}
	return claudeAPIKeyHelperNotice(ctx.NativeConfigDir(harnessName, home))
}

// claudeAPIKeyHelperNotice reports an apiKeyHelper in dir/settings.json
// (Claude Code's user settings under CLAUDE_CONFIG_DIR). The helper is
// a command Claude runs for a key, so no environment scrub reaches it.
// The notice names the setting, never its command.
func claudeAPIKeyHelperNotice(dir string) string {
	path := filepath.Join(dir, "settings.json")
	data, err := os.ReadFile(path) // #nosec G304 -- the profile's own Claude settings file
	if err != nil {
		return ""
	}
	var settings struct {
		APIKeyHelper string `json:"apiKeyHelper"`
	}
	if json.Unmarshal(data, &settings) != nil || settings.APIKeyHelper == "" {
		return ""
	}
	return fmt.Sprintf("%s sets apiKeyHelper: Claude Code uses the key it returns ahead of "+
		"this profile's native login, and aide cannot scrub it", path)
}

// selectedKeyNotice describes a native credential in the selected
// profile's config that the harness uses ahead of a credential the
// launch injects from plan, or "" when there is none. Only launches
// that select their own config dir are checked.
func selectedKeyNotice(harnessName string, ctx config.Context, home string, plan CredentialPlan) string {
	if !ctx.SelectsConfigDir() || harnessName != "pi" {
		return ""
	}
	var envNames []string
	for _, b := range plan.Bindings {
		envNames = append(envNames, b.Env...)
	}
	return piStoredLoginNotice(ctx.NativeConfigDir(harnessName, home), envNames)
}

// piProvidersByEnv maps a provider credential variable to the Pi
// provider ids whose auth.json entry Pi reads ahead of it. Pi resolves
// a provider's credential from auth.json before the environment
// (https://pi.dev/docs/latest/providers, "Resolution Order"); ids are
// those Pi's provider docs list for each variable. Variables of
// providers with no documented id are not listed, so they get no note.
var piProvidersByEnv = map[string][]string{
	"ANTHROPIC_API_KEY":             {"anthropic"},
	"ANTHROPIC_OAUTH_TOKEN":         {"anthropic"},
	"ANTHROPIC_AUTH_TOKEN":          {"anthropic"},
	"OPENAI_API_KEY":                {"openai"},
	"AZURE_OPENAI_API_KEY":          {"azure", "azure-openai-responses"},
	"DEEPSEEK_API_KEY":              {"deepseek"},
	"GEMINI_API_KEY":                {"google"},
	"MISTRAL_API_KEY":               {"mistral"},
	"GROQ_API_KEY":                  {"groq"},
	"CEREBRAS_API_KEY":              {"cerebras"},
	"CLOUDFLARE_API_KEY":            {"cloudflare-ai-gateway", "cloudflare-workers-ai"},
	"XAI_API_KEY":                   {"xai"},
	"OPENROUTER_API_KEY":            {"openrouter"},
	"AI_GATEWAY_API_KEY":            {"vercel-ai-gateway"},
	"ZAI_API_KEY":                   {"zai"},
	"OPENCODE_API_KEY":              {"opencode", "opencode-go"},
	"HF_TOKEN":                      {"huggingface"},
	"FIREWORKS_API_KEY":             {"fireworks"},
	"KIMI_API_KEY":                  {"kimi-coding"},
	"MINIMAX_API_KEY":               {"minimax"},
	"MINIMAX_CN_API_KEY":            {"minimax-cn"},
	"XIAOMI_API_KEY":                {"xiaomi"},
	"XIAOMI_TOKEN_PLAN_CN_API_KEY":  {"xiaomi-token-plan-cn"},
	"XIAOMI_TOKEN_PLAN_AMS_API_KEY": {"xiaomi-token-plan-ams"},
	"XIAOMI_TOKEN_PLAN_SGP_API_KEY": {"xiaomi-token-plan-sgp"},
}

// piStoredLoginNotice reports the providers whose credential the launch
// injects (envNames, from the selected bindings) but whose stored entry
// in dir/auth.json (Pi's PI_CODING_AGENT_DIR) Pi uses instead. Only
// provider ids are read and named; stored values never are. A missing
// or unreadable auth.json gives no notice: Pi then has nothing stored.
func piStoredLoginNotice(dir string, envNames []string) string {
	if len(envNames) == 0 {
		return ""
	}
	path := filepath.Join(dir, "auth.json")
	data, err := os.ReadFile(path) // #nosec G304 -- the profile's own Pi auth file
	if err != nil {
		return ""
	}
	var stored map[string]json.RawMessage
	if json.Unmarshal(data, &stored) != nil {
		return ""
	}
	var shadowed []string
	for _, e := range envNames {
		for _, id := range piProvidersByEnv[e] {
			if _, ok := stored[id]; ok && !slices.Contains(shadowed, id) {
				shadowed = append(shadowed, id)
			}
		}
	}
	if len(shadowed) == 0 {
		return ""
	}
	slices.Sort(shadowed)
	return fmt.Sprintf("%s has a stored Pi login for %s: Pi uses it ahead of the selected API key "+
		"(remove it with /logout in Pi, or launch with --auth login)", path, strings.Join(shadowed, ", "))
}
