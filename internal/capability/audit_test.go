package capability

import (
	"testing"
)

func TestCredentialWarnings_DetectsKnownVars(t *testing.T) {
	envAllow := []string{"AWS_PROFILE", "AWS_SECRET_ACCESS_KEY", "VAULT_TOKEN", "KUBECONFIG"}
	warnings := CredentialWarnings(envAllow)

	if len(warnings) != 2 {
		t.Fatalf("len(warnings) = %d, want 2", len(warnings))
	}

	want := map[string]bool{"AWS_SECRET_ACCESS_KEY": true, "VAULT_TOKEN": true}
	for _, w := range warnings {
		if !want[w] {
			t.Errorf("unexpected warning: %q", w)
		}
	}
}

func TestCredentialWarnings_Empty(t *testing.T) {
	warnings := CredentialWarnings([]string{"KUBECONFIG", "AWS_PROFILE"})
	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}
}

func TestCredentialWarnings_Nil(t *testing.T) {
	warnings := CredentialWarnings(nil)
	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}
}

func TestCompositionWarnings_CredentialPlusNetwork(t *testing.T) {
	caps := []ResolvedCapability{
		{
			Name:     "aws",
			Unguard:  []string{"cloud-aws"},
			EnvAllow: []string{"AWS_SECRET_ACCESS_KEY"},
		},
		{
			Name:     "k8s",
			Unguard:  []string{"kubernetes"},
			EnvAllow: []string{"KUBECONFIG"},
		},
	}

	warnings := CompositionWarnings(caps)
	if len(warnings) != 1 {
		t.Fatalf("expected 1 composition warning, got %d: %v", len(warnings), warnings)
	}
}

func TestCompositionWarnings_NoCredentials(t *testing.T) {
	caps := []ResolvedCapability{
		{
			Name:     "k8s",
			Unguard:  []string{"kubernetes"},
			EnvAllow: []string{"KUBECONFIG"},
		},
	}

	warnings := CompositionWarnings(caps)
	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}
}

func TestCompositionWarnings_CredentialWithoutNetwork(t *testing.T) {
	caps := []ResolvedCapability{
		{
			Name:     "creds-only",
			EnvAllow: []string{"AWS_SECRET_ACCESS_KEY", "VAULT_TOKEN"},
		},
	}

	warnings := CompositionWarnings(caps)
	if len(warnings) != 0 {
		t.Errorf("expected no warnings when no network unguards, got %v", warnings)
	}
}
