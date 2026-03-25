package guards

import (
	"fmt"
	"path/filepath"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

type shellHistoryGuard struct{}

// ShellHistoryGuard returns a Guard that denies access to shell and REPL history files.
func ShellHistoryGuard() seatbelt.Guard { return &shellHistoryGuard{} }

func (g *shellHistoryGuard) Name() string { return "shell-history" }
func (g *shellHistoryGuard) Type() string { return "default" }
func (g *shellHistoryGuard) Description() string {
	return "Blocks access to shell and REPL history files containing inline secrets"
}

var historyFiles = []string{
	".bash_history",
	".zsh_history",
	".local/share/fish/fish_history",
	".python_history",
	".node_repl_history",
	".irb_history",
	".psql_history",
	".mysql_history",
	".sqlite_history",
	".lesshst",
}

func (g *shellHistoryGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	result := seatbelt.GuardResult{}

	// Build opt-out set from ExtraReadable
	optOut := make(map[string]bool)
	for _, p := range ctx.ExtraReadable {
		optOut[p] = true
	}

	for _, rel := range historyFiles {
		fullPath := filepath.Join(ctx.HomeDir, rel)
		if optOut[fullPath] {
			result.Allowed = append(result.Allowed, fullPath)
			continue
		}
		if pathExists(fullPath) {
			result.Rules = append(result.Rules, DenyFile(fullPath)...)
			result.Protected = append(result.Protected, fullPath)
		} else {
			result.Skipped = append(result.Skipped,
				fmt.Sprintf("%s not found", fullPath))
		}
	}

	return result
}
