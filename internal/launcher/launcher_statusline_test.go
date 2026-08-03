package launcher

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/capability"
	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/ui"
)

func envMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, kv := range env {
		if i := strings.IndexByte(kv, '='); i >= 0 {
			m[kv[:i]] = kv[i+1:]
		}
	}
	return m
}

func TestInjectAideSessionEnv_SandboxOn(t *testing.T) {
	result := envMap(injectAideSessionEnv(nil, false, nil, false, nil, "", "claude", nil))
	if result["AIDE_SANDBOX"] != "on" {
		t.Errorf("AIDE_SANDBOX = %q, want on", result["AIDE_SANDBOX"])
	}
}

func TestInjectAideSessionEnv_SandboxOff(t *testing.T) {
	result := envMap(injectAideSessionEnv(nil, true, nil, false, nil, "", "claude", nil))
	if result["AIDE_SANDBOX"] != "off" {
		t.Errorf("AIDE_SANDBOX = %q, want off", result["AIDE_SANDBOX"])
	}
}

func TestInjectAideSessionEnv_NetworkOutbound(t *testing.T) {
	result := envMap(injectAideSessionEnv(nil, false, nil, false, nil, "", "claude", nil))
	if result["AIDE_NETWORK_MODE"] != "outbound" {
		t.Errorf("AIDE_NETWORK_MODE = %q, want outbound", result["AIDE_NETWORK_MODE"])
	}
}

func TestInjectAideSessionEnv_NetworkUnrestricted(t *testing.T) {
	cfg := &config.SandboxPolicy{Network: &config.NetworkPolicy{Mode: "unrestricted"}}
	result := envMap(injectAideSessionEnv(nil, false, cfg, false, nil, "", "claude", nil))
	if result["AIDE_NETWORK_MODE"] != "unrestricted" {
		t.Errorf("AIDE_NETWORK_MODE = %q, want unrestricted", result["AIDE_NETWORK_MODE"])
	}
}

func TestInjectAideSessionEnv_CapsJoined(t *testing.T) {
	caps := &capability.Set{
		Capabilities: []capability.ResolvedCapability{
			{Name: "k8s"},
			{Name: "docker"},
		},
	}
	result := envMap(injectAideSessionEnv(nil, false, nil, false, caps, "", "claude", nil))
	if result["AIDE_CAPS"] != "k8s,docker" {
		t.Errorf("AIDE_CAPS = %q, want k8s,docker", result["AIDE_CAPS"])
	}
}

func TestInjectAideSessionEnv_NilCapsMeansEmpty(t *testing.T) {
	result := envMap(injectAideSessionEnv(nil, false, nil, false, nil, "", "claude", nil))
	if result["AIDE_CAPS"] != "" {
		t.Errorf("AIDE_CAPS = %q, want empty", result["AIDE_CAPS"])
	}
}

func TestInjectAideSessionEnv_TrustNilMeansTrusted(t *testing.T) {
	result := envMap(injectAideSessionEnv(nil, false, nil, false, nil, "", "claude", nil))
	if result["AIDE_TRUST"] != "trusted" {
		t.Errorf("AIDE_TRUST = %q, want trusted", result["AIDE_TRUST"])
	}
}

func TestInjectAideSessionEnv_TrustUntrusted(t *testing.T) {
	ti := &ui.TrustInfo{Status: "untrusted"}
	result := envMap(injectAideSessionEnv(nil, false, nil, false, nil, "", "claude", ti))
	if result["AIDE_TRUST"] != "untrusted" {
		t.Errorf("AIDE_TRUST = %q, want untrusted", result["AIDE_TRUST"])
	}
}

func TestInjectAideSessionEnv_AutoApprove(t *testing.T) {
	result := envMap(injectAideSessionEnv(nil, false, nil, true, nil, "", "claude", nil))
	if result["AIDE_AUTO_APPROVE"] != "1" {
		t.Errorf("AIDE_AUTO_APPROVE = %q, want 1", result["AIDE_AUTO_APPROVE"])
	}
}

func TestInjectAideSessionEnv_NoAutoApproveWhenFalse(t *testing.T) {
	result := envMap(injectAideSessionEnv(nil, false, nil, false, nil, "", "claude", nil))
	if _, ok := result["AIDE_AUTO_APPROVE"]; ok {
		t.Error("AIDE_AUTO_APPROVE should be absent when not active")
	}
}

func TestInjectAideSessionEnv_ContextName(t *testing.T) {
	result := envMap(injectAideSessionEnv(nil, false, nil, false, nil, "my-proj", "claude", nil))
	if result["AIDE_CONTEXT"] != "my-proj" {
		t.Errorf("AIDE_CONTEXT = %q, want my-proj", result["AIDE_CONTEXT"])
	}
}

func TestInjectAideSessionEnv_AgentName(t *testing.T) {
	result := envMap(injectAideSessionEnv(nil, false, nil, false, nil, "", "claude", nil))
	if result["AIDE_AGENT"] != "claude" {
		t.Errorf("AIDE_AGENT = %q, want claude", result["AIDE_AGENT"])
	}
}

func TestInjectAideSessionEnv_MergesWithExistingEnv(t *testing.T) {
	base := []string{"PATH=/usr/bin", "HOME=/home/user"}
	result := envMap(injectAideSessionEnv(base, false, nil, false, nil, "", "claude", nil))
	if result["PATH"] != "/usr/bin" {
		t.Error("existing PATH was lost")
	}
	if result["AIDE_SANDBOX"] == "" {
		t.Error("AIDE_SANDBOX was not injected")
	}
}
