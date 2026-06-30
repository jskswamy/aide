package provision_test

import (
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
)

func TestHookArtifactNameOwnsRoundTrip(t *testing.T) {
	// Core invariant: Name(cmd) generates an artifact name, and Owns(Name(cmd)) must return true.
	// This invariant protects against future changes that could break name generation/matching agreement.
	tests := []struct {
		name     string
		artifact provision.HookArtifact
		command  string
	}{
		{
			name:     "copilot .json files",
			artifact: provision.HookArtifact{Prefix: "aide-", Ext: ".json"},
			command:  "rtk hook copilot",
		},
		{
			name:     "gemini .sh scripts",
			artifact: provision.HookArtifact{Prefix: "aide_", Ext: ".sh"},
			command:  "rtk hook gemini",
		},
		{
			name:     "hermes directory (no ext)",
			artifact: provision.HookArtifact{Prefix: "aide_", Ext: ""},
			command:  "rtk hook hermes",
		},
		{
			name:     "empty command (edge case)",
			artifact: provision.HookArtifact{Prefix: "aide-", Ext: ".json"},
			command:  "",
		},
		{
			name:     "complex command with spaces",
			artifact: provision.HookArtifact{Prefix: "aide_", Ext: ".sh"},
			command:  "rtk hook gemini --option value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Generate the artifact name
			artifactName := tt.artifact.Name(tt.command)

			// The invariant: Owns must return true for the name we just generated
			if !tt.artifact.Owns(artifactName) {
				t.Errorf("Owns(Name(%q)) = false, want true; artifact name=%q", tt.command, artifactName)
			}

			// Verify the format (prefix + hex + ext)
			if !validateArtifactFormat(artifactName, tt.artifact) {
				t.Errorf("artifact name %q doesn't match expected format", artifactName)
			}
		})
	}
}

func TestHookArtifactOwns(t *testing.T) {
	tests := []struct {
		name     string
		artifact provision.HookArtifact
		input    string
		want     bool
	}{
		{
			name:     "copilot exact match",
			artifact: provision.HookArtifact{Prefix: "aide-", Ext: ".json"},
			input:    "aide-1234567890abcdef.json",
			want:     true,
		},
		{
			name:     "copilot wrong prefix",
			artifact: provision.HookArtifact{Prefix: "aide-", Ext: ".json"},
			input:    "aide_1234567890abcdef.json",
			want:     false,
		},
		{
			name:     "copilot wrong extension",
			artifact: provision.HookArtifact{Prefix: "aide-", Ext: ".json"},
			input:    "aide-1234567890abcdef.txt",
			want:     false,
		},
		{
			name:     "copilot non-hex hash",
			artifact: provision.HookArtifact{Prefix: "aide-", Ext: ".json"},
			input:    "aide-zzzzzzzzzzzzzzzz.json",
			want:     false,
		},
		{
			name:     "gemini exact match",
			artifact: provision.HookArtifact{Prefix: "aide_", Ext: ".sh"},
			input:    "aide_1234567890abcdef.sh",
			want:     true,
		},
		{
			name:     "gemini wrong prefix",
			artifact: provision.HookArtifact{Prefix: "aide_", Ext: ".sh"},
			input:    "aide-1234567890abcdef.sh",
			want:     false,
		},
		{
			name:     "gemini wrong extension",
			artifact: provision.HookArtifact{Prefix: "aide_", Ext: ".sh"},
			input:    "aide_1234567890abcdef.bash",
			want:     false,
		},
		{
			name:     "hermes directory match",
			artifact: provision.HookArtifact{Prefix: "aide_", Ext: ""},
			input:    "aide_1234567890abcdef",
			want:     true,
		},
		{
			name:     "hermes directory wrong prefix",
			artifact: provision.HookArtifact{Prefix: "aide_", Ext: ""},
			input:    "aide-1234567890abcdef",
			want:     false,
		},
		{
			name:     "hermes directory with extension (not dir)",
			artifact: provision.HookArtifact{Prefix: "aide_", Ext: ""},
			input:    "aide_1234567890abcdef.txt",
			want:     false,
		},
		{
			name:     "hermes directory wrong hash length",
			artifact: provision.HookArtifact{Prefix: "aide_", Ext: ""},
			input:    "aide_123",
			want:     false,
		},
		{
			name:     "user file not owned",
			artifact: provision.HookArtifact{Prefix: "aide-", Ext: ".json"},
			input:    "my-hook.json",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.artifact.Owns(tt.input)
			if got != tt.want {
				t.Errorf("Owns(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestHookArtifactNameFormat(t *testing.T) {
	artifact := provision.HookArtifact{Prefix: "aide-", Ext: ".json"}
	command := "test-command"
	name := artifact.Name(command)

	// Manually compute what we expect
	sum := sha256.Sum256([]byte(command))
	expected := fmt.Sprintf("aide-%x.json", sum[:8])

	if name != expected {
		t.Errorf("Name(%q) = %q, want %q", command, name, expected)
	}
}

// Helper to validate artifact format matches expected structure
func validateArtifactFormat(name string, artifact provision.HookArtifact) bool {
	if !artifact.Owns(name) {
		return false
	}
	// Additional structural check
	if artifact.Ext == "" {
		// Directory: should be prefix + 16 hex chars
		remainder := name[len(artifact.Prefix):]
		return len(remainder) == 16
	}
	// File: should be prefix + 16 hex chars + extension
	start := len(artifact.Prefix)
	end := len(name) - len(artifact.Ext)
	if end <= start {
		return false
	}
	hex := name[start:end]
	return len(hex) == 16
}
