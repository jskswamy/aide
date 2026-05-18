//go:build !linux

package main

import (
	"github.com/spf13/cobra"
)

// sandboxApplyCmd is a no-op on non-Linux platforms — Landlock re-exec is Linux-only.
func sandboxApplyCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "__sandbox-apply",
		Hidden: true,
		Short:  "Linux Landlock sandbox apply (not applicable on this platform)",
	}
}
