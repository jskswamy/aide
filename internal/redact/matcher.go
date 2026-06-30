package redact

import "strings"

// sensitiveFlagSubstrings names flag-name and env-var fragments that imply a
// secret value follows. Match is case-insensitive.
// This is the union of keywords from both explain.credentialIndicators and
// diag.sensitiveFlagSubstrings, ensuring consistent redaction across all
// packages.
var sensitiveFlagSubstrings = []string{
	"key", "token", "secret", "password", "passwd", "credential", "cert", "private",
	"api-key", "apikey", "auth-token", "authorization", "passphrase", "private-key",
}

// LooksSensitive reports whether name contains any known credential token
// (case-insensitive substring match).
func LooksSensitive(name string) bool {
	lower := strings.ToLower(name)
	for _, ind := range sensitiveFlagSubstrings {
		if strings.Contains(lower, ind) {
			return true
		}
	}
	return false
}
