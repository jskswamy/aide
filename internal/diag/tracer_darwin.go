//go:build darwin

package diag

import (
	"context"
	"os/exec"
	"strconv"
	"time"
)

// CollectDenials runs `log show` for the recent past and returns denials
// matching the given pid. Returns ("", denials, nil) on success;
// (reason, nil, err) when the system call fails or the user lacks
// permission to read the unified log.
func CollectDenials(pid int, since time.Duration) (string, []Denial, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	args := []string{
		"show",
		"--last", strconv.Itoa(int(since.Seconds())) + "s",
		"--predicate", `sender == "Sandbox"`,
		"--style", "compact",
	}
	cmd := exec.CommandContext(ctx, "log", args...)
	out, err := cmd.Output()
	if err != nil {
		return "log show failed: " + err.Error(), nil, err
	}
	return "", parseLogShow(string(out), pid), nil
}
