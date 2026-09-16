package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/output"
	"github.com/spf13/cobra"
)

func newFailingCmd(errMsg string) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "aide",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(*cobra.Command, []string) error {
			return errors.New(errMsg)
		},
	}
	output.RegisterFlag(cmd)
	return cmd
}

func TestRunMain_JSONFormat_PrintsErrorEnvelope(t *testing.T) {
	cmd := newFailingCmd("boom")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--format", "json"})

	code := runMain(cmd)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	var got map[string]string
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, out.String())
	}
	if got["error"] != "boom" {
		t.Errorf("got %+v", got)
	}
	if strings.Contains(out.String(), "Error:") {
		t.Errorf("cobra's own 'Error:' text must be silenced, got:\n%s", out.String())
	}
}

func TestRunMain_HumanFormat_PrintsPlainError(t *testing.T) {
	cmd := newFailingCmd("boom")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	code := runMain(cmd)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if out.String() != "Error: boom\n" {
		t.Errorf("got %q", out.String())
	}
}

func TestRunMain_Success_ReturnsZero(t *testing.T) {
	cmd := &cobra.Command{
		Use: "aide",
		RunE: func(*cobra.Command, []string) error {
			return nil
		},
	}
	output.RegisterFlag(cmd)
	if code := runMain(cmd); code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}

func TestRunMain_JSONFormat_SubcommandError_RootFallback(t *testing.T) {
	sub := &cobra.Command{
		Use: "sub",
		RunE: func(*cobra.Command, []string) error {
			return errors.New("sub failed")
		},
	}
	root := &cobra.Command{
		Use:           "root",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(sub)
	output.RegisterFlag(root)

	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"sub", "--format", "json"})

	code := runMain(root)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	var got map[string]string
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, out.String())
	}
	if got["error"] != "sub failed" {
		t.Errorf("got %+v", got)
	}
}
