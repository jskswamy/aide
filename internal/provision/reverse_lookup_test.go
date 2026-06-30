package provision

import "testing"

func TestReverseLookup(t *testing.T) {
	m := map[string]string{
		"pre_tool":      "PreToolUse",
		"post_tool":     "PostToolUse",
		"session_start": "SessionStart",
	}

	tests := []struct {
		name     string
		value    string
		fallback string
		want     string
	}{
		{
			name:     "found in map",
			value:    "PreToolUse",
			fallback: "",
			want:     "pre_tool",
		},
		{
			name:     "found another value",
			value:    "SessionStart",
			fallback: "",
			want:     "session_start",
		},
		{
			name:     "not found returns fallback",
			value:    "NonExistent",
			fallback: "NonExistent",
			want:     "NonExistent",
		},
		{
			name:     "empty string fallback when not found",
			value:    "Unknown",
			fallback: "",
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ReverseLookup(m, tt.value, tt.fallback)
			if got != tt.want {
				t.Errorf("ReverseLookup(%q, %q) = %q, want %q", tt.value, tt.fallback, got, tt.want)
			}
		})
	}
}
