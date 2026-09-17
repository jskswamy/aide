package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestExplainCmd_JSONFormat(t *testing.T) {
	cmd := explainCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--format", "json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out.String(), "\"state\"") {
		t.Errorf("expected JSON with state key, got:\n%s", out.String())
	}
}

func TestExplainCmd_RejectsUnknownFormat(t *testing.T) {
	cmd := explainCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--format", "bogus"})

	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for unknown format")
	}
}

func TestExplainCmd_TopicArg_PrintsRecipeBody(t *testing.T) {
	cmd := explainCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"add-mcp-server"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	got := out.String()
	if strings.Contains(got, "aide configuration") {
		t.Error("topic arg should print only the recipe, not the full config overview")
	}
	if !strings.Contains(got, "Add an MCP server") {
		t.Errorf("expected recipe heading in output, got:\n%s", got)
	}
}

func TestExplainCmd_TopicArg_UnknownTopic(t *testing.T) {
	cmd := explainCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"no-such-topic"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for unknown topic")
	}
	if !strings.Contains(err.Error(), "no-such-topic") {
		t.Errorf("error should mention the topic name, got: %v", err)
	}
}

func TestExplainCmd_AideFormatEnvSetsDefault(t *testing.T) {
	t.Setenv("AIDE_FORMAT", "json")
	cmd := explainCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(nil)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out.String(), "\"state\"") {
		t.Errorf("expected JSON with state key from AIDE_FORMAT=json, got:\n%s", out.String())
	}
}

func TestExplainCmd_ExplicitFlagBeatsAideFormatEnv(t *testing.T) {
	t.Setenv("AIDE_FORMAT", "json")
	cmd := explainCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--format", "human"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.Contains(out.String(), "\"state\"") {
		t.Errorf("explicit --format human must override AIDE_FORMAT=json, got:\n%s", out.String())
	}
}
