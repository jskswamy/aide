package diag

import (
	"strings"
	"testing"
)

const sampleLogOutput = `2026-05-07 14:23:01.123 Sandbox: claude(1234) deny(1) file-read-data /Users/alice/Library/Keychains/login.keychain-db
2026-05-07 14:23:01.124 Sandbox: claude(1234) deny(1) mach-lookup com.apple.SecurityServer
2026-05-07 14:23:02.000 Sandbox: somethingelse(9999) deny(1) file-read-data /etc/hosts
`

func TestParseLogShow_FiltersByPID(t *testing.T) {
	got := parseLogShow(sampleLogOutput, 1234)
	if len(got) != 2 {
		t.Fatalf("expected 2 denials for pid 1234, got %d: %+v", len(got), got)
	}
	if got[0].Operation != "file-read-data" {
		t.Errorf("denial[0].Operation = %q, want %q", got[0].Operation, "file-read-data")
	}
	if !strings.Contains(got[0].Path, "Keychains") {
		t.Errorf("denial[0].Path = %q, expected Keychains", got[0].Path)
	}
	if got[1].Operation != "mach-lookup" {
		t.Errorf("denial[1].Operation = %q, want %q", got[1].Operation, "mach-lookup")
	}
	if got[1].PID != 1234 {
		t.Errorf("denial[1].PID = %d, want 1234", got[1].PID)
	}
}

func TestParseLogShow_NoMatchesReturnsEmpty(t *testing.T) {
	got := parseLogShow(sampleLogOutput, 5555)
	if len(got) != 0 {
		t.Errorf("expected no denials for pid 5555, got %d: %+v", len(got), got)
	}
}

func TestParseLogShow_HandlesEmptyInput(t *testing.T) {
	got := parseLogShow("", 1234)
	if len(got) != 0 {
		t.Errorf("expected empty result for empty input, got %d", len(got))
	}
}

func TestParseLogShow_IgnoresMalformedLines(t *testing.T) {
	input := `not a sandbox line
Sandbox: weird format with no parens deny(1) file-read /tmp
2026-05-07 14:23:01.123 Sandbox: claude(1234) deny(1) file-read-data /tmp/x
`
	got := parseLogShow(input, 1234)
	if len(got) != 1 {
		t.Fatalf("expected 1 valid denial, got %d: %+v", len(got), got)
	}
}
