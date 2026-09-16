// internal/output/output_test.go
package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestParse(t *testing.T) {
	cases := []struct {
		raw     string
		want    Format
		wantErr bool
	}{
		{"human", Human, false},
		{"json", JSON, false},
		{"", "", true},
		{"xml", "", true},
	}
	for _, c := range cases {
		got, err := Parse(c.raw)
		if c.wantErr {
			if err == nil {
				t.Errorf("Parse(%q): expected error, got nil", c.raw)
			}
			continue
		}
		if err != nil {
			t.Errorf("Parse(%q): unexpected error: %v", c.raw, err)
		}
		if got != c.want {
			t.Errorf("Parse(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
}

func TestDefaultFromEnv(t *testing.T) {
	if got := DefaultFromEnv("human"); got != "human" {
		t.Errorf("unset AIDE_FORMAT: got %q, want %q", got, "human")
	}
	t.Setenv("AIDE_FORMAT", "json")
	if got := DefaultFromEnv("human"); got != "json" {
		t.Errorf("AIDE_FORMAT=json: got %q, want %q", got, "json")
	}
}

func TestRegisterFlag_And_FromCmd(t *testing.T) {
	cmd := &cobra.Command{Use: "x", RunE: func(*cobra.Command, []string) error { return nil }}
	RegisterFlag(cmd)
	cmd.SetArgs([]string{"--format", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	got, err := FromCmd(cmd)
	if err != nil {
		t.Fatalf("FromCmd: %v", err)
	}
	if got != JSON {
		t.Errorf("FromCmd = %q, want %q", got, JSON)
	}
}

func TestRegisterFlag_DefaultsToAideFormatEnv(t *testing.T) {
	t.Setenv("AIDE_FORMAT", "json")
	cmd := &cobra.Command{Use: "x", RunE: func(*cobra.Command, []string) error { return nil }}
	RegisterFlag(cmd)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	got, err := FromCmd(cmd)
	if err != nil {
		t.Fatalf("FromCmd: %v", err)
	}
	if got != JSON {
		t.Errorf("FromCmd = %q, want %q (from AIDE_FORMAT)", got, JSON)
	}
}

func TestRegisterFlag_ExplicitFlagBeatsEnv(t *testing.T) {
	t.Setenv("AIDE_FORMAT", "json")
	cmd := &cobra.Command{Use: "x", RunE: func(*cobra.Command, []string) error { return nil }}
	RegisterFlag(cmd)
	cmd.SetArgs([]string{"--format", "human"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	got, err := FromCmd(cmd)
	if err != nil {
		t.Fatalf("FromCmd: %v", err)
	}
	if got != Human {
		t.Errorf("FromCmd = %q, want %q (explicit flag wins)", got, Human)
	}
}

func TestFromCmd_FlagNotRegistered_DefaultsHuman(t *testing.T) {
	cmd := &cobra.Command{Use: "x"}
	got, err := FromCmd(cmd)
	if err != nil {
		t.Fatalf("FromCmd: %v", err)
	}
	if got != Human {
		t.Errorf("FromCmd on unregistered flag = %q, want %q", got, Human)
	}
}

func TestEmit_JSON(t *testing.T) {
	var buf bytes.Buffer
	type payload struct {
		Name string `json:"name"`
	}
	humanCalled := false
	err := Emit(&buf, JSON, payload{Name: "x"}, func(w io.Writer) error {
		humanCalled = true
		return nil
	})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if humanCalled {
		t.Error("humanFn must not run in JSON mode")
	}
	var got payload
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, buf.String())
	}
	if got.Name != "x" {
		t.Errorf("got %+v", got)
	}
	if !strings.Contains(buf.String(), "\n  \"name\"") {
		t.Errorf("expected 2-space indent, got:\n%s", buf.String())
	}
}

func TestEmit_Human(t *testing.T) {
	var buf bytes.Buffer
	err := Emit(&buf, Human, "unused", func(w io.Writer) error {
		_, werr := w.Write([]byte("hello\n"))
		return werr
	})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if buf.String() != "hello\n" {
		t.Errorf("got %q", buf.String())
	}
}

func TestEmit_HumanError_Propagates(t *testing.T) {
	wantErr := errors.New("boom")
	err := Emit(&bytes.Buffer{}, Human, nil, func(io.Writer) error { return wantErr })
	if !errors.Is(err, wantErr) {
		t.Errorf("got %v, want %v", err, wantErr)
	}
}

func TestPrintError_JSON(t *testing.T) {
	var buf bytes.Buffer
	PrintError(&buf, JSON, errors.New("something broke"))
	var got map[string]string
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, buf.String())
	}
	if got["error"] != "something broke" {
		t.Errorf("got %+v", got)
	}
}

func TestPrintError_Human(t *testing.T) {
	var buf bytes.Buffer
	PrintError(&buf, Human, errors.New("something broke"))
	if buf.String() != "Error: something broke\n" {
		t.Errorf("got %q", buf.String())
	}
}
