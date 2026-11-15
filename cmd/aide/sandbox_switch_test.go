package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A provider-switch gateway opens its capability for show and test as
// it does for a launch, so they agree with what the launch applies.
func TestSandboxShowAndTest_ProviderSwitchOpensCapability(t *testing.T) {
	dir := isolatedConfigDir(t)
	yaml := "profiles:\n  cloud:\n    agent: claude\n    gateways:\n      aws:\n        preset: bedrock\n        region: eu-west-1\n" +
		"    gateway_choice:\n      claude: aws\ndefault_profile: cloud\n"
	if err := os.WriteFile(filepath.Join(dir, "xdg", "aide", "config.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, ".aws"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, sub := range []string{"show", "test"} {
		out, err := runSandboxCmd(t, sub)
		if err != nil {
			t.Fatalf("sandbox %s: %v\n%s", sub, err, out)
		}
		if !strings.Contains(out, ".aws") {
			t.Errorf("sandbox %s does not grant the aws capability's paths:\n%s", sub, out)
		}
	}
}
