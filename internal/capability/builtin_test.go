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
	if len(Builtins()) != 21 {
		t.Errorf("expected 21 built-in capabilities, got %d", len(Builtins()))
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
		{"python", nil},
		{"ruby", []string{"~/.rbenv"}},
		{"java", []string{"~/.sdkman", "~/.gradle", "~/.m2"}},
		{"github", []string{"~/.config/gh"}},
		{"gpg", []string{"~/.gnupg"}},
	}
	for _, tc := range cases {
		c, ok := bs[tc.name]
		if !ok {
			t.Errorf("missing capability %q", tc.name)
			continue
		}
		if !reflect.DeepEqual(c.Writable, tc.writable) {
			t.Errorf("%s writable: got %v, want %v",
				tc.name, c.Writable, tc.writable)
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

func TestBuiltin_Network_Exists(t *testing.T) {
	netCap, ok := Builtins()["network"]
	if !ok {
		t.Fatal("missing built-in capability 'network'")
	}
	if netCap.NetworkMode != "unrestricted" {
		t.Errorf("expected NetworkMode unrestricted, got %q", netCap.NetworkMode)
	}
	if netCap.Description == "" {
		t.Error("expected non-empty description")
	}
}

func TestBuiltins_PythonHasVariantCatalog(t *testing.T) {
	b := Builtins()
	py, ok := b["python"]
	if !ok {
		t.Fatal("builtin 'python' missing")
	}
	wantNames := map[string]bool{"uv": true, "pyenv": true, "conda": true, "poetry": true, "venv": true}
	if len(py.Variants) != len(wantNames) {
		t.Fatalf("variant count = %d, want %d (got %v)", len(py.Variants), len(wantNames), py.Variants)
	}
	for _, v := range py.Variants {
		if !wantNames[v.Name] {
			t.Errorf("unexpected variant: %s", v.Name)
		}
	}
	if len(py.DefaultVariants) != 1 || py.DefaultVariants[0] != "venv" {
		t.Errorf("DefaultVariants = %v, want [venv]", py.DefaultVariants)
	}
}

func TestBuiltins_PythonVariantMarkers(t *testing.T) {
	b := Builtins()
	py := b["python"]

	findVariant := func(name string) Variant {
		for _, v := range py.Variants {
			if v.Name == name {
				return v
			}
		}
		t.Fatalf("variant %q missing", name)
		return Variant{}
	}

	// uv detected by uv.lock
	uv := findVariant("uv")
	if len(uv.Markers) == 0 {
		t.Error("uv variant has no markers")
	}
	found := false
	for _, m := range uv.Markers {
		if m.File == "uv.lock" {
			found = true
			break
		}
	}
	if !found {
		t.Error("uv variant missing uv.lock marker")
	}

	// pyenv detected by .python-version
	pyenv := findVariant("pyenv")
	found = false
	for _, m := range pyenv.Markers {
		if m.File == ".python-version" {
			found = true
			break
		}
	}
	if !found {
		t.Error("pyenv variant missing .python-version marker")
	}

	// conda detected by environment.yml
	conda := findVariant("conda")
	found = false
	for _, m := range conda.Markers {
		if m.File == "environment.yml" {
			found = true
			break
		}
	}
	if !found {
		t.Error("conda variant missing environment.yml marker")
	}

	// poetry detected by poetry.lock
	poetry := findVariant("poetry")
	found = false
	for _, m := range poetry.Markers {
		if m.File == "poetry.lock" {
			found = true
			break
		}
	}
	if !found {
		t.Error("poetry variant missing poetry.lock marker")
	}

	// venv has no markers (never auto-selected, used as default fallback)
	venv := findVariant("venv")
	if len(venv.Markers) != 0 {
		t.Errorf("venv should have no markers; got %v", venv.Markers)
	}
}
