package config

import (
	"sort"
	"strings"
	"testing"
)

func TestResolveTemplates_SecretRef(t *testing.T) {
	env := map[string]string{
		"API_KEY": "{{ .secrets.api_key }}",
	}
	data := &TemplateData{
		Secrets: map[string]string{"api_key": "sk-123"},
	}
	result, err := ResolveTemplates(env, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["API_KEY"] != "sk-123" {
		t.Errorf("got %q, want %q", result["API_KEY"], "sk-123")
	}
}

func TestResolveTemplates_ProjectRoot(t *testing.T) {
	env := map[string]string{
		"ROOT": "{{ .project_root }}",
	}
	data := &TemplateData{
		ProjectRoot: "/home/user/myproject",
	}
	result, err := ResolveTemplates(env, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["ROOT"] != "/home/user/myproject" {
		t.Errorf("got %q, want %q", result["ROOT"], "/home/user/myproject")
	}
}

func TestResolveTemplates_RuntimeDir(t *testing.T) {
	env := map[string]string{
		"DIR": "{{ .runtime_dir }}",
	}
	data := &TemplateData{
		RuntimeDir: "/run/user/1000/aide-12345",
	}
	result, err := ResolveTemplates(env, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["DIR"] != "/run/user/1000/aide-12345" {
		t.Errorf("got %q, want %q", result["DIR"], "/run/user/1000/aide-12345")
	}
}

func TestResolveTemplates_Literal(t *testing.T) {
	env := map[string]string{
		"FLAG": "1",
	}
	data := &TemplateData{}
	result, err := ResolveTemplates(env, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["FLAG"] != "1" {
		t.Errorf("got %q, want %q", result["FLAG"], "1")
	}
}

func TestResolveTemplates_Mixed(t *testing.T) {
	env := map[string]string{
		"API_KEY": "{{ .secrets.api_key }}",
		"FLAG":    "1",
		"ROOT":    "{{ .project_root }}",
	}
	data := &TemplateData{
		Secrets:     map[string]string{"api_key": "sk-123"},
		ProjectRoot: "/home/user/project",
	}
	result, err := ResolveTemplates(env, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["API_KEY"] != "sk-123" {
		t.Errorf("API_KEY: got %q, want %q", result["API_KEY"], "sk-123")
	}
	if result["FLAG"] != "1" {
		t.Errorf("FLAG: got %q, want %q", result["FLAG"], "1")
	}
	if result["ROOT"] != "/home/user/project" {
		t.Errorf("ROOT: got %q, want %q", result["ROOT"], "/home/user/project")
	}
}

func TestResolveTemplates_MissingKey(t *testing.T) {
	env := map[string]string{
		"API_KEY": "{{ .secrets.nonexistent }}",
	}
	data := &TemplateData{
		Secrets: map[string]string{"api_key": "sk-123", "token": "tok-456"},
	}
	_, err := ResolveTemplates(env, data)
	if err == nil {
		t.Fatal("expected error for missing key, got nil")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "API_KEY") {
		t.Errorf("error should mention env var name 'API_KEY', got: %s", errMsg)
	}
	// Check that available keys are listed
	if !strings.Contains(errMsg, "api_key") || !strings.Contains(errMsg, "token") {
		t.Errorf("error should list available keys, got: %s", errMsg)
	}
}

func TestResolveTemplates_InvalidSyntax(t *testing.T) {
	env := map[string]string{
		"BAD": "{{ .secrets.foo",
	}
	data := &TemplateData{
		Secrets: map[string]string{"foo": "bar"},
	}
	_, err := ResolveTemplates(env, data)
	if err == nil {
		t.Fatal("expected error for invalid syntax, got nil")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "BAD") {
		t.Errorf("error should mention env var name 'BAD', got: %s", errMsg)
	}
}

func TestResolveTemplates_EmptySecrets(t *testing.T) {
	env := map[string]string{
		"API_KEY": "{{ .secrets.api_key }}",
	}
	data := &TemplateData{
		Secrets: nil,
	}
	_, err := ResolveTemplates(env, data)
	if err == nil {
		t.Fatal("expected error when secrets is nil but template references secrets, got nil")
	}
}

func TestResolveTemplates_NoSecretsNeeded(t *testing.T) {
	env := map[string]string{
		"FLAG":  "1",
		"OTHER": "hello",
	}
	data := &TemplateData{
		Secrets: nil,
	}
	result, err := ResolveTemplates(env, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["FLAG"] != "1" {
		t.Errorf("FLAG: got %q, want %q", result["FLAG"], "1")
	}
	if result["OTHER"] != "hello" {
		t.Errorf("OTHER: got %q, want %q", result["OTHER"], "hello")
	}
}

func TestResolveTemplates_ComplexTemplate(t *testing.T) {
	env := map[string]string{
		"CMD": "--project {{ .project_root }} --key {{ .secrets.key }}",
	}
	data := &TemplateData{
		Secrets:     map[string]string{"key": "my-secret"},
		ProjectRoot: "/home/user/project",
	}
	result, err := ResolveTemplates(env, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "--project /home/user/project --key my-secret"
	if result["CMD"] != expected {
		t.Errorf("got %q, want %q", result["CMD"], expected)
	}
}

func TestResolveTemplates_EmptyEnvMap(t *testing.T) {
	data := &TemplateData{
		Secrets: map[string]string{"key": "val"},
	}
	result, err := ResolveTemplates(map[string]string{}, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty map, got %v", result)
	}
}

func TestIsTemplate(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"{{ .secrets.key }}", true},
		{"hello {{ .project_root }}", true},
		{"no template here", false},
		{"", false},
		{"{not a template}", false},
	}
	for _, tt := range tests {
		got := IsTemplate(tt.input)
		if got != tt.want {
			t.Errorf("IsTemplate(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

// helper used in TestResolveTemplates_MissingKey to verify sorted key listing
func TestResolveTemplates_MissingKey_SortedKeys(t *testing.T) {
	env := map[string]string{
		"X": "{{ .secrets.missing }}",
	}
	data := &TemplateData{
		Secrets: map[string]string{"zebra": "z", "alpha": "a", "middle": "m"},
	}
	_, err := ResolveTemplates(env, data)
	if err == nil {
		t.Fatal("expected error")
	}
	errMsg := err.Error()
	// Keys should be listed in sorted order
	keys := []string{"alpha", "middle", "zebra"}
	sort.Strings(keys)
	expectedList := strings.Join(keys, ", ")
	if !strings.Contains(errMsg, expectedList) {
		t.Errorf("expected sorted key list %q in error, got: %s", expectedList, errMsg)
	}
}
