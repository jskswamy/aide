package seatbelt

import "testing"

func TestRuleIntent_SetupRule(t *testing.T) {
	r := SetupRule("(deny default)")
	if r.Intent() != Setup {
		t.Errorf("expected Setup (%d), got %d", Setup, r.Intent())
	}
	if r.String() != "(deny default)" {
		t.Errorf("unexpected content: %q", r.String())
	}
}

func TestRuleIntent_RestrictRule(t *testing.T) {
	r := RestrictRule(`(deny file-read-data (subpath "/home/.ssh"))`)
	if r.Intent() != Restrict {
		t.Errorf("expected Restrict (%d), got %d", Restrict, r.Intent())
	}
}

func TestRuleIntent_GrantRule(t *testing.T) {
	r := GrantRule(`(allow file-read* (literal "/home/.ssh/known_hosts"))`)
	if r.Intent() != Grant {
		t.Errorf("expected Grant (%d), got %d", Grant, r.Intent())
	}
}

func TestRuleIntent_SectionConstructors(t *testing.T) {
	tests := []struct {
		name string
		rule Rule
		want RuleIntent
	}{
		{"SectionSetup", SectionSetup("test"), Setup},
		{"SectionRestrict", SectionRestrict("test"), Restrict},
		{"SectionGrant", SectionGrant("test"), Grant},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.rule.Intent() != tt.want {
				t.Errorf("expected %d, got %d", tt.want, tt.rule.Intent())
			}
		})
	}
}

func TestRuleIntent_BackwardCompat(t *testing.T) {
	tests := []struct {
		name string
		rule Rule
	}{
		{"Raw", Raw("test")},
		{"Allow", Allow("network*")},
		{"Deny", Deny("default")},
		{"Section", Section("test")},
		{"Comment", Comment("test")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.rule.Intent() != Setup {
				t.Errorf("expected Setup (%d) for backward compat, got %d", Setup, tt.rule.Intent())
			}
		})
	}
}
