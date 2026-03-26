package capability

import (
	"reflect"
	"testing"
)

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
	if len(Builtins()) != 19 {
		t.Errorf("expected 19 built-in capabilities, got %d", len(Builtins()))
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

func TestBuiltins_NoUnguardRefs(t *testing.T) {
	for name, cap := range Builtins() {
		if len(cap.Unguard) != 0 {
			t.Errorf("capability %q has Unguard %v "+
				"(all guards removed, Unguard should be empty)", name, cap.Unguard)
		}
	}
}

func TestBuiltins_LanguageRuntimes(t *testing.T) {
	bs := Builtins()
	cases := []struct {
		name     string
		writable []string
	}{
		{"go", []string{"~/go"}},
		{"rust", []string{"~/.cargo", "~/.rustup"}},
		{"python", []string{"~/.pyenv"}},
		{"ruby", []string{"~/.rbenv"}},
		{"java", []string{"~/.sdkman", "~/.gradle", "~/.m2"}},
		{"github", []string{"~/.config/gh"}},
		{"gpg", []string{"~/.gnupg"}},
	}
	for _, tc := range cases {
		cap, ok := bs[tc.name]
		if !ok {
			t.Errorf("missing capability %q", tc.name)
			continue
		}
		if !reflect.DeepEqual(cap.Writable, tc.writable) {
			t.Errorf("%s writable: got %v, want %v",
				tc.name, cap.Writable, tc.writable)
		}
	}
}

func TestBuiltin_K8s_NoUnguard(t *testing.T) {
	k8s := Builtins()["k8s"]
	if len(k8s.Unguard) != 0 {
		t.Errorf("k8s should have no Unguard, got %v", k8s.Unguard)
	}
}

func TestBuiltin_Helm_NoUnguard(t *testing.T) {
	helm := Builtins()["helm"]
	if len(helm.Unguard) != 0 {
		t.Errorf("helm should have no Unguard, got %v", helm.Unguard)
	}
}
