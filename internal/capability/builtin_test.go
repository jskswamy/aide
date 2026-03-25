package capability

import "testing"

func TestBuiltins_AllPresent(t *testing.T) {
	expected := []string{
		"aws", "gcp", "azure", "digitalocean", "oci",
		"docker", "k8s", "helm",
		"terraform", "vault",
		"ssh", "npm",
	}
	for _, name := range expected {
		if _, ok := Builtins()[name]; !ok {
			t.Errorf("missing built-in capability %q", name)
		}
	}
}

func TestBuiltins_Count(t *testing.T) {
	if len(Builtins()) != 12 {
		t.Errorf("expected 12 built-in capabilities, got %d", len(Builtins()))
	}
}

func TestBuiltins_EachResolvable(t *testing.T) {
	registry := Builtins()
	for name := range registry {
		_, err := ResolveOne(name, registry)
		if err != nil {
			t.Errorf("built-in %q failed to resolve: %v", name, err)
		}
	}
}

func TestBuiltin_K8s_HasCorrectGuard(t *testing.T) {
	k8s := Builtins()["k8s"]
	if len(k8s.Unguard) != 1 || k8s.Unguard[0] != "kubernetes" {
		t.Errorf("k8s should unguard [kubernetes], got %v", k8s.Unguard)
	}
}

func TestBuiltin_Helm_ExtendsK8sGuard(t *testing.T) {
	helm := Builtins()["helm"]
	if len(helm.Unguard) != 1 || helm.Unguard[0] != "kubernetes" {
		t.Errorf("helm should unguard [kubernetes], got %v", helm.Unguard)
	}
}
