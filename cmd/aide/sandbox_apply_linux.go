//go:build linux

package main

import (
	"fmt"
	"os"

	"github.com/jskswamy/aide/internal/sandbox"
	"github.com/spf13/cobra"
)

// sandboxApplyCmd is the hidden re-exec target used by the Landlock backend:
//
//	aide __sandbox-apply <policy-json-path> -- <agent> [args...]
//
// It applies Landlock to the current process then syscall.Execs the agent.
func sandboxApplyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                "__sandbox-apply <policy-path> -- <agent> [args...]",
		Hidden:             true,
		DisableFlagParsing: true,
		SilenceUsage:       true,
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) < 3 {
				return fmt.Errorf("usage: __sandbox-apply <policy-path> -- <agent> [args...]")
			}

			policyPath := args[0]
			if args[1] != "--" {
				return fmt.Errorf("expected '--' separator after policy path, got %q", args[1])
			}
			agentCmd := args[2:]
			if len(agentCmd) == 0 {
				return fmt.Errorf("no agent command specified after '--'")
			}

			if err := sandbox.RunSandboxApply(policyPath, agentCmd); err != nil {
				fmt.Fprintf(os.Stderr, "aide: sandbox-apply: %v\n", err)
				os.Exit(1)
			}
			return nil
		},
	}
	return cmd
}
