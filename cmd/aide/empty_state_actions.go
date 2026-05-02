// cmd/aide/empty_state_actions.go
package main

import (
	"github.com/spf13/cobra"

	"github.com/jskswamy/aide/internal/launcher"
)

// emptyStateAdapter implements launcher.EmptyStateActions by routing
// to the same code paths as the standalone `aide context bind` /
// `aide context create` commands.
type emptyStateAdapter struct {
	cmd *cobra.Command
}

func newEmptyStateAdapter(cmd *cobra.Command) launcher.EmptyStateActions {
	return &emptyStateAdapter{cmd: cmd}
}

func (a *emptyStateAdapter) Bind(name string) error {
	bind := contextBindCmd()
	bind.SetOut(a.cmd.OutOrStdout())
	bind.SetErr(a.cmd.ErrOrStderr())
	if name != "" {
		bind.SetArgs([]string{name})
	} else {
		bind.SetArgs(nil)
	}
	return bind.Execute()
}

func (a *emptyStateAdapter) Create(name string) error {
	return runCreateWizard(a.cmd, name, createOptions{})
}
