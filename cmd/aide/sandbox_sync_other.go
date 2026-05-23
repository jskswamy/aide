//go:build !linux

package main

import "github.com/spf13/cobra"

// sandboxSyncCmd is a no-op on non-Linux platforms — the overlayfs
// atomic-write sync is Linux-only.
func sandboxSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "__sandbox-sync",
		Hidden: true,
		Short:  "Linux overlay sync (not applicable on this platform)",
	}
}
