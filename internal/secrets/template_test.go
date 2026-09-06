package secrets_test

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/secrets"
)

func repoTestdataDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("unable to determine test file path")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "testdata")
}

func TestLoadSecretsMap_NoKeyAvailable(t *testing.T) {
	t.Setenv("PATH", "")
	t.Setenv("SOPS_AGE_KEY", "")
	t.Setenv("SOPS_AGE_KEY_FILE", "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	_, err := secrets.LoadSecretsMap("/nonexistent/secrets.enc.yaml")
	if err == nil {
		t.Fatal("expected error when no age identity is discoverable")
	}
	if !strings.Contains(err.Error(), "discovering age key") {
		t.Errorf("expected discovering age key error, got: %v", err)
	}
}

func TestLoadSecretsMap_Success(t *testing.T) {
	td := repoTestdataDir(t)
	keyFile := filepath.Join(td, "age-key.txt")
	encFile := filepath.Join(td, "test-secrets.enc.yaml")
	t.Setenv("SOPS_AGE_KEY_FILE", keyFile)

	got, err := secrets.LoadSecretsMap(encFile)
	if err != nil {
		t.Fatalf("LoadSecretsMap: %v", err)
	}
	if got["anthropic_api_key"] == "" {
		t.Errorf("expected anthropic_api_key to be decrypted, got map: %+v", got)
	}
}
