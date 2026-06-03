package provision_test

import (
	"testing"

	"github.com/jskswamy/aide/internal/provision"
)

func TestValidateHookCommandAcceptsValid(t *testing.T) {
	valid := []string{
		"rtk hook gemini",
		"aide-hook",
		"/usr/local/bin/aide-hook",
		"bd prime",
		"helper-script arg1 arg2",
		"cmd --flag value",
		"cmd:colon",
	}
	for _, cmd := range valid {
		if err := provision.ValidateHookCommand(cmd); err != nil {
			t.Errorf("ValidateHookCommand(%q) = %v, want nil", cmd, err)
		}
	}
}

func TestValidateHookCommandRejectsMetacharacters(t *testing.T) {
	cases := []struct {
		cmd  string
		desc string
	}{
		{"rtk hook; rm -rf ~", "semicolon"},
		{"cmd | other", "pipe"},
		{"cmd && other", "ampersand"},
		{"cmd `evil`", "backtick"},
		{"echo $HOME", "dollar"},
		{"cmd > /dev/null", "greater-than"},
		{"cmd < input", "less-than"},
		{"a\nb", "newline"},
		{`cmd "arg"`, "double quote"},
		{`cmd 'arg'`, "single quote"},
		{"cmd\\arg", "backslash"},
		{"cmd$(evil)", "dollar-paren"},
	}
	for _, tc := range cases {
		err := provision.ValidateHookCommand(tc.cmd)
		if err == nil {
			t.Errorf("ValidateHookCommand(%q) should reject %s, got nil", tc.cmd, tc.desc)
		}
	}
}

func TestValidateHookCommandRejectsEmpty(t *testing.T) {
	if err := provision.ValidateHookCommand(""); err == nil {
		t.Error("ValidateHookCommand(\"\") should return error")
	}
}

func TestValidateHookCommandTemplateAllowsAgentVar(t *testing.T) {
	valid := []string{
		"rtk hook {agent}",
		"notify-send {agent} started",
		"{agent}-hook",
	}
	for _, cmd := range valid {
		if err := provision.ValidateHookCommandTemplate(cmd); err != nil {
			t.Errorf("ValidateHookCommandTemplate(%q) = %v, want nil", cmd, err)
		}
	}
}

func TestValidateHookCommandTemplateStillRejectsDangerous(t *testing.T) {
	cases := []string{
		"rtk hook {agent}; rm -rf ~",
		"cmd {agent} | evil",
		"cmd {a,b}",
	}
	for _, cmd := range cases {
		if err := provision.ValidateHookCommandTemplate(cmd); err == nil {
			t.Errorf("ValidateHookCommandTemplate(%q) should reject dangerous command", cmd)
		}
	}
}

func TestHookKey(t *testing.T) {
	k1 := provision.HookKey("pre_tool", "shell", "rtk hook")
	k2 := provision.HookKey("pre_tool", "shell", "rtk hook")
	k3 := provision.HookKey("pre_tool", "", "rtk hook")
	if k1 != k2 {
		t.Error("same inputs should produce same key")
	}
	if k1 == k3 {
		t.Error("different matcher should produce different key")
	}
	// Verify pipe in event doesn't collide with separator.
	k4 := provision.HookKey("pre_tool|shell", "", "cmd")
	k5 := provision.HookKey("pre_tool", "shell", "cmd")
	if k4 == k5 {
		t.Error("pipe in event should not collide with separator")
	}
}
