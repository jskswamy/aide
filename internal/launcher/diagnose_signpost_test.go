package launcher

import (
	"bytes"
	"strings"
	"testing"
)

func TestShouldShowSignpost(t *testing.T) {
	cases := []struct {
		name   string
		exit   int
		signal string
		want   bool
	}{
		{"clean exit", 0, "", false},
		{"sigint signal name", 130, "SIGINT", false},
		{"sigterm signal name", 143, "SIGTERM", false},
		{"sighup signal name", 129, "SIGHUP", false},
		{"sigquit signal name", 131, "SIGQUIT", false},
		{"sigint exit code without signal name", 130, "", false},
		{"sigterm exit code without signal name", 143, "", false},
		{"sighup exit code without signal name", 129, "", false},
		{"sigquit exit code without signal name", 131, "", false},
		{"plain non-zero", 1, "", true},
		{"non-zero 7", 7, "", true},
		{"non-zero 137 (sigkill via signal)", 137, "SIGKILL", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ShouldShowSignpost(c.exit, c.signal)
			if got != c.want {
				t.Errorf("ShouldShowSignpost(%d, %q) = %v, want %v", c.exit, c.signal, got, c.want)
			}
		})
	}
}

func TestEmitSignpost(t *testing.T) {
	var buf bytes.Buffer
	EmitSignpost(&buf)
	if !strings.Contains(buf.String(), "--diagnose") {
		t.Errorf("EmitSignpost output missing --diagnose hint: %q", buf.String())
	}
	if !strings.Contains(buf.String(), "hint:") {
		t.Errorf("EmitSignpost output should be prefixed 'hint:': %q", buf.String())
	}
}
