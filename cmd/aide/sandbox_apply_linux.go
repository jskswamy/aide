//go:build linux

package main

import (
	"fmt"
	"os"

	"github.com/jskswamy/aide/internal/sandbox"
	"github.com/spf13/cobra"
)

// sandboxApplyCmd is the hidden re-exec target used by the Landlock backend.
// It handles two internal phases distinguished by the first argument:
//
// Phase 1 — Landlock apply (first re-exec from the launcher):
//
//	aide __sandbox-apply <policy-json-path> -- <agent> [args...]
//
// Applies Landlock to the current process, then either syscall.Execs the agent
// directly (AllowSubprocess=true) or forks into a new PID namespace and runs
// Phase 2 (AllowSubprocess=false).
//
// Phase 2 — seccomp+exec (second re-exec, inside the PID namespace):
//
//	aide __sandbox-apply -- <agent> [args...]
//
// Installs the no-subprocess seccomp filter and syscall.Execs the agent.
// Detected when args[0] == "--" (no policy path prefix).
func sandboxApplyCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "__sandbox-apply [<policy-path>] -- <agent> [args...]",
		Hidden:             true,
		DisableFlagParsing: true,
		SilenceUsage:       true,
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("usage: __sandbox-apply [<policy-path>] -- <agent> [args...]")
			}

			// Phase 2: args[0] == "--" means we are inside the PID namespace.
			// Install seccomp and exec the agent.
			if args[0] == "--" {
				agentCmd := args[1:]
				if len(agentCmd) == 0 {
					return fmt.Errorf("no agent command after '--'")
				}
				if err := sandbox.RunSandboxExec(agentCmd); err != nil {
					fmt.Fprintf(os.Stderr, "aide: sandbox-exec: %v\n", err)
					os.Exit(1)
				}
				return nil
			}

			// Phase 1: args[0] is the policy path.
			if len(args) < 3 || args[1] != "--" {
				return fmt.Errorf("usage: __sandbox-apply <policy-path> -- <agent> [args...]")
			}
			agentCmd := args[2:]
			if len(agentCmd) == 0 {
				return fmt.Errorf("no agent command after '--'")
			}
			if err := sandbox.RunSandboxApply(args[0], agentCmd); err != nil {
				fmt.Fprintf(os.Stderr, "aide: sandbox-apply: %v\n", err)
				os.Exit(1)
			}
			return nil
		},
	}
}
