package launcher

import (
	"strings"
	"testing"
)

func TestFilterEssentialEnv_PreservesNonCredentialEnvAllow(t *testing.T) {
	env := []string{
		"PATH=/usr/bin",
		"XDG_DATA_HOME=/home/user/.share",
		"XDG_STATE_HOME=/home/user/.state",
		"TERMINAL_BROWSER_INTEROP_DIR=/home/user/.tb-interop",
		"AWS_SECRET_ACCESS_KEY=secret",
		"ANTHROPIC_API_KEY=sk-ant-secret",
		"RANDOM_VAR=drop",
	}
	filtered := filterEssentialEnv(env, []string{
		"XDG_DATA_HOME",
		"XDG_STATE_HOME",
		"TERMINAL_BROWSER_INTEROP_DIR",
		"AWS_SECRET_ACCESS_KEY",
		"ANTHROPIC_API_KEY",
	})
	got := map[string]string{}
	for _, e := range filtered {
		k, v, _ := strings.Cut(e, "=")
		got[k] = v
	}
	for _, keep := range []string{"PATH", "XDG_DATA_HOME", "XDG_STATE_HOME", "TERMINAL_BROWSER_INTEROP_DIR"} {
		if got[keep] == "" {
			t.Errorf("expected %s to survive filterEssentialEnv, got %v", keep, filtered)
		}
	}
	for _, drop := range []string{"AWS_SECRET_ACCESS_KEY", "ANTHROPIC_API_KEY", "RANDOM_VAR"} {
		if _, ok := got[drop]; ok {
			t.Errorf("expected %s to be stripped by filterEssentialEnv, got %v", drop, filtered)
		}
	}
}
