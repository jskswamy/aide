package redact

import "testing"

func TestLooksSensitive(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantMatch bool
	}{
		// From explain.credentialIndicators
		{"has KEY", "api_key", true},
		{"has TOKEN", "GITHUB_TOKEN", true},
		{"has SECRET", "db_secret", true},
		{"has PASSWORD", "password", true},
		{"has PASSWD", "root_passwd", true},
		{"has CREDENTIAL", "aws_credential", true},
		{"has CERT", "TLS_CERT", true},
		{"has PRIVATE", "private_key", true},

		// From diag.sensitiveFlagSubstrings
		{"has api-key", "api-key", true},
		{"has apikey", "apikey", true},
		{"has auth-token", "auth-token", true},
		{"has authorization", "authorization", true},
		{"has passphrase", "passphrase", true},
		{"has private-key", "private-key", true},

		// Case-insensitive matching
		{"uppercase KEY", "MY_KEY", true},
		{"mixed case SECRET", "MySecret", true},
		{"lowercase api-key", "api-key", true},

		// Non-matches
		{"benign model name", "ANTHROPIC_MODEL", false},
		{"benign path", "DATA_DIR", false},
		{"partial match no", "auth_method", false},
		{"partial match no author", "author", false},
		{"empty", "", false},

		// Edge cases from collector_test
		{"underscore form api_key", "api_key", true},
		{"underscore form auth_token", "auth_token", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LooksSensitive(tt.input)
			if got != tt.wantMatch {
				t.Errorf("LooksSensitive(%q) = %v, want %v", tt.input, got, tt.wantMatch)
			}
		})
	}
}
