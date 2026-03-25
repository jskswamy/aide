// Package capability defines task-oriented permission bundles and their resolution logic.
package capability

import "fmt"

// credentialEnvVars lists env vars known to contain credentials.
var credentialEnvVars = map[string]bool{
	"AWS_SECRET_ACCESS_KEY":          true,
	"AWS_SESSION_TOKEN":              true,
	"VAULT_TOKEN":                    true,
	"DIGITALOCEAN_ACCESS_TOKEN":      true,
	"NPM_TOKEN":                     true,
	"NODE_AUTH_TOKEN":                true,
	"GOOGLE_APPLICATION_CREDENTIALS": true,
}

// networkUnguards lists unguard values that imply network/egress access.
var networkUnguards = map[string]bool{
	"cloud-aws":          true,
	"cloud-gcp":          true,
	"cloud-azure":        true,
	"cloud-digitalocean": true,
	"cloud-oci":          true,
	"docker":             true,
	"kubernetes":         true,
	"terraform":          true,
	"vault":              true,
	"ssh-keys":           true,
	"npm":                true,
}

// CredentialWarnings returns env var names from envAllow that are known credential bearers.
func CredentialWarnings(envAllow []string) []string {
	var warnings []string
	for _, env := range envAllow {
		if credentialEnvVars[env] {
			warnings = append(warnings, env)
		}
	}
	return warnings
}

// CompositionWarnings checks if capabilities combine credential + network access.
// It returns human-readable warning strings when dangerous combinations are detected.
func CompositionWarnings(caps []ResolvedCapability) []string {
	var allEnvAllow []string
	var allUnguard []string
	for _, cap := range caps {
		allEnvAllow = append(allEnvAllow, cap.EnvAllow...)
		allUnguard = append(allUnguard, cap.Unguard...)
	}

	hasCredential := len(CredentialWarnings(allEnvAllow)) > 0
	hasNetwork := false
	for _, u := range allUnguard {
		if networkUnguards[u] {
			hasNetwork = true
			break
		}
	}

	var warnings []string
	if hasCredential && hasNetwork {
		credVars := CredentialWarnings(allEnvAllow)
		warnings = append(warnings,
			fmt.Sprintf("credential env vars %v combined with network-capable unguards — credentials could be exfiltrated", credVars))
	}
	return warnings
}
