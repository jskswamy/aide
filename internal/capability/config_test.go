package capability

import (
	"testing"

	"github.com/jskswamy/aide/internal/config"
)

func TestFromConfigDefs(t *testing.T) {
	defs := map[string]config.CapabilityDef{
		"k8s-dev": {
			Extends:  "k8s",
			Readable: []string{"~/.kube/dev-config"},
			Deny:     []string{"~/.kube/prod-config"},
			EnvAllow: []string{"KUBECONFIG"},
		},
	}

	caps := FromConfigDefs(defs)
	if len(caps) != 1 {
		t.Fatalf("expected 1 capability, got %d", len(caps))
	}
	k8sDev := caps["k8s-dev"]
	if k8sDev.Extends != "k8s" {
		t.Errorf("expected extends k8s, got %s", k8sDev.Extends)
	}
}
