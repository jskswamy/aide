//go:build linux

package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/jskswamy/aide/internal/sandbox"
	"github.com/spf13/cobra"
)

// sandboxApplyCmd is the hidden re-exec target used by the Landlock backend.
// It handles two internal phases distinguished by the first argument:
//
// Phase 1 — Landlock apply (first re-exec from the launcher):
//
//	aide __sandbox-apply --policy-fd=<N> -- <agent> [args...]
//
// Reads the serialised policy JSON from file-descriptor N (a memfd passed by
// the parent via cmd.ExtraFiles), applies Landlock to the current process,
// then either syscall.Execs the agent directly (AllowSubprocess=true) or
// forks into a new PID namespace and runs Phase 2 (AllowSubprocess=false).
//
// Phase 2 — seccomp+exec (second re-exec, inside the PID namespace):
//
//	aide __sandbox-apply -- <agent-path> <agent-argv0> [args...]
//
// Installs the no-subprocess seccomp filter and syscall.Execs <agent-path>
// with argv = [<agent-argv0>, args...]. Splitting the binary path from the
// agent's argv preserves the original argv[0] across the re-exec so the
// agent sees the same name it sees under AllowSubprocess=true. Detected
// when args[0] == "--" (no --policy-fd prefix).
func sandboxApplyCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "__sandbox-apply [--policy-fd=<N>] -- <agent> [args...]",
		Hidden:             true,
		DisableFlagParsing: true,
		SilenceUsage:       true,
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("usage: __sandbox-apply [--policy-fd=<N>] -- <agent> [args...]")
			}

			// Phase 2: args[0] == "--" means we are inside the PID namespace.
			// Layout: ["--", <agent-path>, <agent-argv0>, <agent-args>...].
			// agent-path is the binary to exec; agent-argv0 is the name the
			// agent sees as argv[0]. Splitting them preserves argv[0] across
			// the re-exec (Greptile P2: argv[0] divergence).
			if args[0] == "--" {
				if len(args) < 3 {
					return fmt.Errorf("usage: __sandbox-apply -- <agent-path> <agent-argv0> [args...]")
				}
				agentPath := args[1]
				agentCmd := args[2:]
				if err := sandbox.RunSandboxExec(agentPath, agentCmd); err != nil {
					fmt.Fprintf(os.Stderr, "aide: sandbox-exec: %v\n", err)
					os.Exit(1)
				}
				return nil
			}

			// Phase 1: args[0] is --policy-fd=<N>.
			const fdPrefix = "--policy-fd="
			if len(args) < 3 || args[1] != "--" || !strings.HasPrefix(args[0], fdPrefix) || len(args[0]) == len(fdPrefix) {
				return fmt.Errorf("usage: __sandbox-apply --policy-fd=<N> -- <agent> [args...]")
			}
			policyFDStr := args[0][len(fdPrefix):]
			agentCmd := args[2:]
			if len(agentCmd) == 0 {
				return fmt.Errorf("no agent command after '--'")
			}
			if err := sandbox.RunSandboxApply(policyFDStr, agentCmd); err != nil {
				fmt.Fprintf(os.Stderr, "aide: sandbox-apply: %v\n", err)
				os.Exit(1)
			}
			return nil
		},
	}
}
