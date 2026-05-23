//go:build linux

package main

import (
	"fmt"
	"os"

	"github.com/jskswamy/aide/internal/sandbox"
	"github.com/spf13/cobra"
)

// sandboxSyncCmd is the hidden re-exec target that wraps the bwrap+Landlock
// chain so the agent's writes to overlay-backed atomic files can be synced
// back to the host after the agent exits. Argument shape:
//
//	aide __sandbox-sync --upper UPPER --home HOMEDIR
//	                    --overlay-root ROOT
//	                    --sync-file PATH [--sync-file PATH...]
//	                    -- <child-cmd> [args...]
func sandboxSyncCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                "__sandbox-sync",
		Hidden:             true,
		DisableFlagParsing: true,
		SilenceUsage:       true,
		RunE: func(_ *cobra.Command, args []string) error {
			if err := sandbox.RunSandboxSync(args); err != nil {
				fmt.Fprintf(os.Stderr, "aide: sandbox-sync: %v\n", err)
				os.Exit(1)
			}
			return nil
		},
	}
	return cmd
}
