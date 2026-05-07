package diag

import (
	"regexp"
	"strconv"
)

// denyLineRE matches: "Sandbox: <process>(<pid>) deny(<code>) <op> <path>"
// where <process> is one or more non-paren characters, <pid> is digits,
// and <op>/<path> are space-separated.
var denyLineRE = regexp.MustCompile(`Sandbox:\s+\S+?\((\d+)\)\s+deny\(\d+\)\s+(\S+)\s+(.*)`)

// parseLogShow extracts deny events for the given pid from `log show` output.
// Cross-platform so tests can run on Linux CI even though CollectDenials
// itself is Darwin-only.
func parseLogShow(out string, pid int) []Denial {
	var denials []Denial
	for _, line := range splitLines(out) {
		m := denyLineRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		gotPid, err := strconv.Atoi(m[1])
		if err != nil || gotPid != pid {
			continue
		}
		denials = append(denials, Denial{Operation: m[2], Path: m[3], PID: gotPid})
	}
	return denials
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}
