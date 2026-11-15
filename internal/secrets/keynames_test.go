package secrets

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestKeyNames(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	yamlStore := write("a.yaml", "zeta: ENC[x]\nalpha: ENC[y]\nsops:\n  version: 3.9.0\n")
	jsonStore := write("b.json", `{"corp": "ENC[x]", "sops": {"version": "3"}}`)
	other := write("c.env", "A=ENC[x]\n")

	if got, ok := KeyNames(yamlStore); !ok || !slices.Equal(got, []string{"alpha", "zeta"}) {
		t.Errorf("yaml = %v, %v", got, ok)
	}
	if got, ok := KeyNames(jsonStore); !ok || !slices.Equal(got, []string{"corp"}) {
		t.Errorf("json = %v, %v", got, ok)
	}
	if _, ok := KeyNames(other); ok {
		t.Error("dotenv store must report unknown")
	}
	if _, ok := KeyNames(filepath.Join(dir, "missing.yaml")); ok {
		t.Error("missing store must report unknown")
	}
}
