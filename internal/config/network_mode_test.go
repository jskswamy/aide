package config_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/config"
)

func TestValidNetworkModesContents(t *testing.T) {
	want := []string{"outbound", "none", "unrestricted"}
	if !slices.Equal(config.ValidNetworkModes, want) {
		t.Errorf("ValidNetworkModes = %v, want %v", config.ValidNetworkModes, want)
	}
}

func TestValidateNetworkMode(t *testing.T) {
	for _, mode := range config.ValidNetworkModes {
		if err := config.ValidateNetworkMode(mode); err != nil {
			t.Errorf("ValidateNetworkMode(%q) returned error: %v", mode, err)
		}
	}
	err := config.ValidateNetworkMode("bogus")
	if err == nil {
		t.Fatal("ValidateNetworkMode(\"bogus\") returned nil, want error")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("error message %q should mention the invalid mode", err)
	}
	for _, mode := range config.ValidNetworkModes {
		if !strings.Contains(err.Error(), mode) {
			t.Errorf("error message %q should mention valid mode %q", err, mode)
		}
	}
}
