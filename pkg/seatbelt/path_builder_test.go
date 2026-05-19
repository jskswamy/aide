package seatbelt_test

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

func TestAllowSubpath_SinglePerm(t *testing.T) {
	rule := seatbelt.AllowSubpath("/Users/x/.claude", "file-read*")
	got := rule.String()
	want := `(allow file-read* (subpath "/Users/x/.claude"))`
	if !strings.Contains(got, want) {
		t.Errorf("rule body = %q, want substring %q", got, want)
	}
	if rule.Intent() != seatbelt.Allow {
		t.Errorf("intent = %d, want Allow", rule.Intent())
	}
}

func TestAllowSubpath_MultiplePerms(t *testing.T) {
	rule := seatbelt.AllowSubpath("/Users/x/.claude", "file-read*", "file-write*")
	got := rule.String()
	want := `(allow file-read* file-write* (subpath "/Users/x/.claude"))`
	if !strings.Contains(got, want) {
		t.Errorf("rule body = %q, want substring %q", got, want)
	}
}

func TestDenySubpath_BasicDeny(t *testing.T) {
	rule := seatbelt.DenySubpath("/Users/x/.ssh", "file-read-data", "file-write*")
	got := rule.String()
	want := `(deny file-read-data file-write* (subpath "/Users/x/.ssh"))`
	if !strings.Contains(got, want) {
		t.Errorf("rule body = %q, want substring %q", got, want)
	}
	if rule.Intent() != seatbelt.Deny {
		t.Errorf("intent = %d, want Deny", rule.Intent())
	}
}

func TestAllowLiteral_SinglePath(t *testing.T) {
	rule := seatbelt.AllowLiteral("/etc/gitconfig", "file-read*")
	got := rule.String()
	want := `(allow file-read* (literal "/etc/gitconfig"))`
	if !strings.Contains(got, want) {
		t.Errorf("rule body = %q, want substring %q", got, want)
	}
}

func TestDenyLiteral_BasicDeny(t *testing.T) {
	rule := seatbelt.DenyLiteral("/Users/x/.netrc", "file-read-data")
	got := rule.String()
	want := `(deny file-read-data (literal "/Users/x/.netrc"))`
	if !strings.Contains(got, want) {
		t.Errorf("rule body = %q, want substring %q", got, want)
	}
}

// TestAllowSubpath_PathWithQuoteIsEscaped pins the security-relevant
// invariant: the builder must produce a properly-escaped sexp string
// even when the input path contains characters that would break
// the surrounding double-quote delimiter. The previous ad-hoc
// fmt.Sprintf("...%s...") sites mis-handled this; the typed builder
// must use Go's %q (or equivalent) which escapes quotes and
// backslashes into valid sexp literal syntax.
func TestAllowSubpath_PathWithQuoteIsEscaped(t *testing.T) {
	weird := `/tmp/foo"bar`
	rule := seatbelt.AllowSubpath(weird, "file-read*")
	got := rule.String()
	// The literal `"` must appear escaped as `\"`, not bare.
	if strings.Contains(got, `foo"bar`) && !strings.Contains(got, `foo\"bar`) {
		t.Errorf("path with embedded quote not escaped; rule body = %q", got)
	}
}

// TestAllowSubpath_PathWithBackslashIsEscaped — backslashes must be
// doubled in the emitted sexp string, or downstream parsing breaks.
func TestAllowSubpath_PathWithBackslashIsEscaped(t *testing.T) {
	weird := `/tmp/foo\bar`
	rule := seatbelt.AllowSubpath(weird, "file-read*")
	got := rule.String()
	if strings.Contains(got, `foo\bar`) && !strings.Contains(got, `foo\\bar`) {
		t.Errorf("path with embedded backslash not escaped; rule body = %q", got)
	}
}
