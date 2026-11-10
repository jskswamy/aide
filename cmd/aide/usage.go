// cmd/aide/usage.go
package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jskswamy/aide/internal/usage"
)

// usageCmd returns the `aide usage` command: a thin passthrough to
// the ccusage binary, scoped to the profile aide would launch under
// for the current directory. It is always registered, regardless of
// PATH — the binary lookup happens only on the exec path, so a later
// setup story works without ccusage installed.
func usageCmd() *cobra.Command {
	cmd := &cobra.Command{
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
	cmd.Flags().Bool("all", false, "Report across all existing profile data")
	cmd.Flags().String("profile", "", "Report for a named profile")
	return cmd
}

type usageOptions struct {
	all     bool
	profile string
}

// parseUsageArgs consumes aide scope flags only before the first ccusage
// argument. It leaves the separator for passthroughArgs to remove once.
func parseUsageArgs(args []string) (usageOptions, []string, error) {
	var opts usageOptions
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--all":
			opts.all = true
		case arg == "--profile" || strings.HasPrefix(arg, "--profile="):
			if arg == "--profile" {
				i++
				if i == len(args) || strings.HasPrefix(args[i], "-") {
					return opts, nil, fmt.Errorf("--profile requires a name")
				}
				opts.profile = args[i]
			} else {
				opts.profile = strings.TrimPrefix(arg, "--profile=")
			}
			if opts.profile == "" {
				return opts, nil, fmt.Errorf("--profile requires a name")
			}
		default:
			return opts, args[i:], nil
		}
		if opts.all && opts.profile != "" {
			return opts, nil, fmt.Errorf("--all and --profile are mutually exclusive")
		}
	}
	return opts, nil, nil
}

// runUsage resolves the requested scope and execs the backend.
func runUsage(cmd *cobra.Command, args []string) error {
	opts, reportArgs, err := parseUsageArgs(args)
	if err != nil {
		return err
	}
	env, err := cmdEnv(cmd)
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	b := usage.DefaultBackend()
	var scopeEnv []string
	switch {
	case opts.all:
		scopeEnv = usage.AllEnv(b, env.Config(), home, cmd.ErrOrStderr())
	case opts.profile != "":
		ctx, err := usage.ResolveProfile(env.Config(), opts.profile)
		if err != nil {
			return err
		}
		scopeEnv = usage.ProfileEnv(b, ctx, home)
	default:
		_, ctx, err := usage.ResolveCWDProfile(env.Config(), env.CWD())
		if err != nil {
			return err
		}
		scopeEnv = usage.ProfileEnv(b, ctx, home)
	}
	childEnv := append(usage.ScrubEnv(os.Environ(), b), scopeEnv...)
	return usage.Run(b, passthroughArgs(reportArgs), childEnv,
		cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr())
}

// passthroughArgs normalizes the ccusage arg vector: one leading "--"
// (the conventional end-of-flags separator) is stripped, and an empty
// result becomes ["daily"] so bare `aide usage` runs the daily
// report.
func passthroughArgs(args []string) []string {
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	if len(args) == 0 {
		return []string{"daily"}
	}
	return args
}
