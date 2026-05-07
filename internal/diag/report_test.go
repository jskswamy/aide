package diag

import (
	"reflect"
	"strings"
	"testing"
)

func TestReportHasNoSecretValueFields(t *testing.T) {
	suspiciousSubstrings := []string{
		"apikey",
		"api_key",
		"token",
		"password",
		"passwd",
		"credential",
		"bearer",
		"passphrase",
		"privatekey",
		"private_key",
		"secret",
		"value",
	}
	allowedSecretLikeFields := map[string]bool{
		"SecretSourcePaths": true, // documented exception: file paths only, no values
	}
	r := Report{}
	v := reflect.ValueOf(r)
	for i := 0; i < v.NumField(); i++ {
		fieldName := v.Type().Field(i).Name
		lowered := strings.ToLower(fieldName)
		for _, bad := range suspiciousSubstrings {
			if strings.Contains(lowered, bad) {
				if !allowedSecretLikeFields[fieldName] {
					t.Errorf("Report has suspicious field %q (matched %q) — secret values must not be storable", fieldName, bad)
				}
				break
			}
		}
	}
}

func TestEnvKeyOnlyHoldsKeyAndLen(t *testing.T) {
	fields := []struct {
		name string
		kind reflect.Kind
	}{
		{"Name", reflect.String},
		{"Length", reflect.Int},
	}
	typ := reflect.TypeOf(EnvKey{})
	if typ.NumField() != len(fields) {
		t.Fatalf("EnvKey must have exactly %d fields, got %d", len(fields), typ.NumField())
	}
	for i, want := range fields {
		got := typ.Field(i)
		if got.Name != want.name {
			t.Errorf("EnvKey field %d: name = %q, want %q", i, got.Name, want.name)
		}
		if got.Type.Kind() != want.kind {
			t.Errorf("EnvKey field %d (%s): kind = %s, want %s", i, got.Name, got.Type.Kind(), want.kind)
		}
	}
}
