package seatbelt

import "testing"

func TestEnvLookup_Found(t *testing.T) {
	ctx := &Context{
		Env: []string{"HOME=/home/user", "AWS_CONFIG_FILE=/custom/aws"},
	}
	val, ok := ctx.EnvLookup("AWS_CONFIG_FILE")
	if !ok {
		t.Fatal("expected EnvLookup to find AWS_CONFIG_FILE")
	}
	if val != "/custom/aws" {
		t.Errorf("expected /custom/aws, got %q", val)
	}
}

func TestEnvLookup_NotFound(t *testing.T) {
	ctx := &Context{
		Env: []string{"HOME=/home/user"},
	}
	_, ok := ctx.EnvLookup("MISSING_KEY")
	if ok {
		t.Error("expected EnvLookup to return false for missing key")
	}
}

func TestEnvLookup_EmptyValue(t *testing.T) {
	ctx := &Context{
		Env: []string{"EMPTY_VAR="},
	}
	val, ok := ctx.EnvLookup("EMPTY_VAR")
	if !ok {
		t.Fatal("expected EnvLookup to find EMPTY_VAR")
	}
	if val != "" {
		t.Errorf("expected empty string, got %q", val)
	}
}

func TestEnvLookup_NilEnv(t *testing.T) {
	ctx := &Context{}
	_, ok := ctx.EnvLookup("ANY_KEY")
	if ok {
		t.Error("expected EnvLookup to return false with nil Env")
	}
}
