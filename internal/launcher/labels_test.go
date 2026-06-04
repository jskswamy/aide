package launcher

import (
	"testing"

	"github.com/jskswamy/aide/internal/secrets"
)

func TestAgeKeySourceLabel(t *testing.T) {
	tests := []struct {
		id   *secrets.AgeIdentity
		want string
	}{
		{&secrets.AgeIdentity{Source: secrets.SourceYubiKey}, "yubikey"},
		{&secrets.AgeIdentity{Source: secrets.SourceEnvKey}, "env:SOPS_AGE_KEY"},
		{&secrets.AgeIdentity{Source: secrets.SourceEnvKeyFile, KeyData: "/run/key"}, "env:SOPS_AGE_KEY_FILE=/run/key"},
		{&secrets.AgeIdentity{Source: secrets.SourceDefaultFile, KeyData: "/home/user/.age/key"}, "file:/home/user/.age/key"},
		{&secrets.AgeIdentity{Source: 99}, ""},
	}
	for _, tt := range tests {
		if got := ageKeySourceLabel(tt.id); got != tt.want {
			t.Errorf("ageKeySourceLabel(%+v) = %q, want %q", tt.id, got, tt.want)
		}
	}
}
