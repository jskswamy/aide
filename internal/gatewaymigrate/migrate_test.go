package gatewaymigrate

import (
	"slices"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/prefs"
)

func ctxWith(gws map[string]config.GatewayConfig) config.Context {
	return config.Context{ConvertedGateways: gws, Credentials: map[string]config.CredentialBinding{"github": {}}}
}

func TestTranslatePreference(t *testing.T) {
	ctx := ctxWith(map[string]config.GatewayConfig{
		"corp":       {Harnesses: []string{"claude", "pi"}},
		"corp-codex": {Harnesses: []string{"codex"}},
		"claude-gw":  {Harnesses: []string{"claude"}},
	})
	renames := map[string]map[string]string{"codex": {"corp": "corp-codex"}}
	tests := []struct {
		name       string
		rec        prefs.Preference
		want       map[string]string
		wantNotice string
	}{
		{"unset stays untouched", prefs.Preference{Harness: "pi"}, nil, ""},
		{"empty selection is none for the slot harnesses", prefs.Preference{LegacyCredentials: prefs.Selection()},
			map[string]string{"claude": "none", "codex": "none"}, ""},
		{"empty selection for pi sets nothing", prefs.Preference{Harness: "pi", LegacyCredentials: prefs.Selection()}, nil, ""},
		{"gateway follows each harness it serves", prefs.Preference{LegacyCredentials: prefs.Selection("corp")},
			map[string]string{"claude": "corp", "pi": "corp", "codex": "corp-codex"}, ""},
		{"record harness limits the translation", prefs.Preference{Harness: "codex", LegacyCredentials: prefs.Selection("corp")},
			map[string]string{"codex": "corp-codex"}, ""},
		{"plain credential selection is none without a notice", prefs.Preference{Harness: "claude", LegacyCredentials: prefs.Selection("github")},
			map[string]string{"claude": "none"}, ""},
		{"unknown credential is none with a notice", prefs.Preference{Harness: "claude", LegacyCredentials: prefs.Selection("anthropic")},
			map[string]string{"claude": "none"}, `"anthropic"`},
		{"gateway that no longer serves the harness is dropped", prefs.Preference{Harness: "codex", LegacyCredentials: prefs.Selection("claude-gw")},
			map[string]string{"codex": "none"}, "aide gateway add"},
		{"an existing choice wins", prefs.Preference{Harness: "claude", Gateway: map[string]string{"claude": "none"}, LegacyCredentials: prefs.Selection("corp")},
			map[string]string{"claude": "none"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, notices := TranslatePreference(tt.rec, ctx, renames)
			if len(got.Gateway) != len(tt.want) {
				t.Fatalf("Gateway = %v, want %v", got.Gateway, tt.want)
			}
			for h, v := range tt.want {
				if got.Gateway[h] != v {
					t.Errorf("Gateway[%s] = %q, want %q", h, got.Gateway[h], v)
				}
			}
			if tt.rec.LegacyCredentials != nil && got.LegacyCredentials != nil {
				t.Errorf("LegacyCredentials = %v, want it consumed", *got.LegacyCredentials)
			}
			if tt.wantNotice == "" && len(notices) != 0 || tt.wantNotice != "" && !slices.ContainsFunc(notices, func(n string) bool { return strings.Contains(n, tt.wantNotice) }) {
				t.Errorf("notices = %q, want one containing %q", notices, tt.wantNotice)
			}
		})
	}
}
