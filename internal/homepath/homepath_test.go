package homepath_test

import (
	"os"
	"testing"

	"github.com/jskswamy/aide/internal/homepath"
)

func TestExpandWithExplicitHome(t *testing.T) {
	cases := []struct {
		name string
		in   string
		home string
		want string
	}{
		{"tilde slash prefix", "~/Documents", "/Users/alice", "/Users/alice/Documents"},
		{"trailing slash preserved", "~/work/", "/Users/alice", "/Users/alice/work/"},
		{"lone tilde", "~", "/Users/alice", "/Users/alice"},
		{"absolute path passthrough", "/usr/local/bin", "/Users/alice", "/usr/local/bin"},
		{"relative path passthrough", "relative/path", "/Users/alice", "relative/path"},
		{"tilde mid-path is not expanded", "/foo/~/bar", "/Users/alice", "/foo/~/bar"},
		{"empty path passthrough", "", "/Users/alice", ""},
		{"tilde no slash with name passthrough", "~user", "/Users/alice", "~user"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := homepath.Expand(tc.in, tc.home)
			if got != tc.want {
				t.Errorf("Expand(%q, %q) = %q, want %q", tc.in, tc.home, got, tc.want)
			}
		})
	}
}

func TestExpandFallsBackToUserHomeDir(t *testing.T) {
	want, err := os.UserHomeDir()
	if err != nil {
		t.Skip("os.UserHomeDir unavailable")
	}
	got := homepath.Expand("~/foo", "")
	if got != want+"/foo" {
		t.Errorf("Expand(~/foo, \"\") = %q, want %q", got, want+"/foo")
	}
	got = homepath.Expand("~", "")
	if got != want {
		t.Errorf("Expand(~, \"\") = %q, want %q", got, want)
	}
}

func TestCollapse(t *testing.T) {
	cases := []struct {
		name string
		in   string
		home string
		want string
	}{
		{"replaces home with tilde", "/Users/alice/work", "/Users/alice", "~/work"},
		{"replaces all occurrences", "see /Users/alice and /Users/alice/x", "/Users/alice", "see ~ and ~/x"},
		{"empty home is passthrough", "/Users/alice/x", "", "/Users/alice/x"},
		{"no occurrence is unchanged", "/var/log", "/Users/alice", "/var/log"},
		{"empty string", "", "/Users/alice", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := homepath.Collapse(tc.in, tc.home)
			if got != tc.want {
				t.Errorf("Collapse(%q, %q) = %q, want %q", tc.in, tc.home, got, tc.want)
			}
		})
	}
}
