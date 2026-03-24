package seatbelt

import "testing"

func TestRuleIntent_AllowRule(t *testing.T) {
	r := AllowRule("(allow file-read* (subpath \"/usr\"))")
	if r.Intent() != Allow {
		t.Errorf("expected Allow (%d), got %d", Allow, r.Intent())
	}
	if r.String() != "(allow file-read* (subpath \"/usr\"))" {
		t.Errorf("unexpected content: %q", r.String())
	}
}

func TestRuleIntent_DenyRule(t *testing.T) {
	r := DenyRule(`(deny file-read-data (subpath "/home/.ssh"))`)
	if r.Intent() != Deny {
		t.Errorf("expected Deny (%d), got %d", Deny, r.Intent())
	}
	if r.String() != `(deny file-read-data (subpath "/home/.ssh"))` {
		t.Errorf("unexpected content: %q", r.String())
	}
}

func TestRuleIntent_SectionAllow(t *testing.T) {
	r := SectionAllow("Infrastructure")
	if r.Intent() != Allow {
		t.Errorf("expected Allow (%d), got %d", Allow, r.Intent())
	}
	if r.String() != ";; --- Infrastructure ---\n" {
		t.Errorf("unexpected content: %q", r.String())
	}
}

func TestRuleIntent_SectionDeny(t *testing.T) {
	r := SectionDeny("Credentials")
	if r.Intent() != Deny {
		t.Errorf("expected Deny (%d), got %d", Deny, r.Intent())
	}
	if r.String() != ";; --- Credentials ---\n" {
		t.Errorf("unexpected content: %q", r.String())
	}
}

func TestRuleIntent_AllowOp(t *testing.T) {
	r := AllowOp("network*")
	if r.Intent() != Allow {
		t.Errorf("expected Allow (%d), got %d", Allow, r.Intent())
	}
	if r.String() != "(allow network*)" {
		t.Errorf("unexpected content: %q", r.String())
	}
}

func TestRuleIntent_DenyOp(t *testing.T) {
	r := DenyOp("default")
	if r.Intent() != Allow {
		t.Errorf("expected Allow (%d) for infrastructure deny-op, got %d", Allow, r.Intent())
	}
	if r.String() != "(deny default)" {
		t.Errorf("unexpected content: %q", r.String())
	}
}

func TestRuleIntent_UtilityConstructors(t *testing.T) {
	tests := []struct {
		name string
		rule Rule
	}{
		{"Raw", Raw("test")},
		{"Section", Section("test")},
		{"Comment", Comment("test")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.rule.Intent() != Allow {
				t.Errorf("expected Allow (%d), got %d", Allow, tt.rule.Intent())
			}
		})
	}
}
