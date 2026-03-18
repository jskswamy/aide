package config

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"text/template"
)

// TemplateData holds the data available inside {{ }} expressions.
type TemplateData struct {
	Secrets     map[string]string // from sops decryption (Task 7)
	ProjectRoot string            // git root or cwd
	RuntimeDir  string            // ephemeral dir (Task 9)
}

// ResolveTemplates processes a map of env var definitions, resolving
// any {{ }} template expressions against the provided data.
// Values without template syntax are passed through unchanged (DD-11).
// Returns a new map with all templates resolved.
func ResolveTemplates(env map[string]string, data *TemplateData) (map[string]string, error) {
	result := make(map[string]string, len(env))

	// Build template data as a map for lowercase field access in templates
	templateData := map[string]interface{}{
		"secrets":      data.Secrets,
		"project_root": data.ProjectRoot,
		"runtime_dir":  data.RuntimeDir,
	}

	for key, value := range env {
		if !IsTemplate(value) {
			result[key] = value
			continue
		}

		resolved, err := resolveTemplate(key, value, templateData, data.Secrets)
		if err != nil {
			return nil, err
		}
		result[key] = resolved
	}

	return result, nil
}

// IsTemplate returns true if the string contains {{ }} template syntax.
func IsTemplate(s string) bool {
	return strings.Contains(s, "{{")
}

// resolveTemplate parses and executes a single template string.
func resolveTemplate(envKey, tmplStr string, data map[string]interface{}, secrets map[string]string) (string, error) {
	tmpl, err := template.New(envKey).Option("missingkey=error").Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("template error in env var %q: %w", envKey, err)
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, data)
	if err != nil {
		// Check if this is a missing key error and provide actionable message
		errStr := err.Error()
		if strings.Contains(errStr, "map has no entry for key") {
			return "", missingKeyError(envKey, errStr, secrets)
		}
		// For nil map access (secrets is nil)
		if strings.Contains(errStr, "nil pointer") || strings.Contains(errStr, "can't evaluate field") {
			return "", missingKeyError(envKey, errStr, secrets)
		}
		return "", fmt.Errorf("template error in env var %q: %w", envKey, err)
	}

	return buf.String(), nil
}

// missingKeyError constructs an actionable error listing available keys.
func missingKeyError(envKey, originalErr string, secrets map[string]string) error {
	var available []string
	for k := range secrets {
		available = append(available, k)
	}
	sort.Strings(available)

	keyList := strings.Join(available, ", ")
	if keyList == "" {
		keyList = "(none)"
	}

	return fmt.Errorf("template error in env var %q: %s. Available keys: %s",
		envKey, originalErr, keyList)
}
