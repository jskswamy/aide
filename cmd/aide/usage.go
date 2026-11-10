// cmd/aide/usage.go
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/jskswamy/aide/internal/usage"
)

// usageCmd returns the `aide usage` command: a thin passthrough to
// the ccusage binary, scoped to the profile aide would launch under
// for the current directory. It is always registered, regardless of
// PATH — the binary lookup happens only on the exec path, so a later
// setup story works without ccusage installed.
func usageCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "usage [ccusage-args...]",
		Short:              "Run ccusage scoped to this profile's data",
		DisableFlagParsing: true, // ccusage flags pass through
		SilenceErrors:      true, // RunE owns all error output
		RunE: func(cmd *cobra.Command, args []string) error {
			err := runUsage(cmd, args)
			var nf *usage.NotFoundError
			var ee *usage.ExitError
			switch {
			case err == nil:
			case errors.As(err, &nf):
				// Print the install hint here; SilenceErrors keeps
				// cobra from printing it again, and main.go's
				// ExitCode convention turns the return into exit 1.
				fmt.Fprintln(cmd.ErrOrStderr(), err.Error())
			case errors.As(err, &ee):
				// Print nothing: the child process's own stderr
				// already reached the user; only its exit code
				// passes through.
			default:
				// main.go prints nothing for this silenced leaf, so
				// RunE must report every other failure itself —
				// config-load or profile-resolve errors.
				fmt.Fprintf(cmd.ErrOrStderr(), "aide usage: %v\n", err)
			}
			return err
		},
	}
}

// runUsage resolves the CWD profile and execs the backend scoped to
// it, returning the failure (typed or wrapped) for RunE to report.
func runUsage(cmd *cobra.Command, args []string) error {
	env, err := cmdEnv(cmd)
	if err != nil {
		return err
	}
	_, ctx, err := usage.ResolveCWDProfile(env.Config(), env.CWD())
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	b := usage.DefaultBackend()
	childEnv := append(usage.ScrubEnv(os.Environ(), b), usage.ProfileEnv(b, ctx, home)...)
	return usage.Run(b, passthroughArgs(args), childEnv,
		cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr())
}

// passthroughArgs normalizes the ccusage arg vector: one leading "--"
// (the conventional end-of-flags separator) is stripped, and an empty
// result becomes ["daily"] so bare `aide usage` runs the daily
// report. Single-purpose by design: a later story extends this seam
// with --all/--profile extraction.
func passthroughArgs(args []string) []string {
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	if len(args) == 0 {
		return []string{"daily"}
	}
	return args
}
