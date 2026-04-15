// Package main provides the aide CLI commands.
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/jskswamy/aide/internal/capability"
	"github.com/jskswamy/aide/internal/config"
	aidectx "github.com/jskswamy/aide/internal/context"
	"github.com/spf13/cobra"
)

func registerCommands(rootCmd *cobra.Command) {
	rootCmd.AddCommand(initCmd())
	rootCmd.AddCommand(whichCmd())
	rootCmd.AddCommand(validateCmd())
	rootCmd.AddCommand(secretsCmd())
	rootCmd.AddCommand(configCmd())
	rootCmd.AddCommand(agentsCmd())
	rootCmd.AddCommand(useCmd())
	rootCmd.AddCommand(contextCmd())
	rootCmd.AddCommand(envCmd())
	rootCmd.AddCommand(setupCmd())
	rootCmd.AddCommand(sandboxCmd())
	rootCmd.AddCommand(capCmd())
	rootCmd.AddCommand(statusCmd())
	rootCmd.AddCommand(trustCmd())
	rootCmd.AddCommand(denyCmd())
	rootCmd.AddCommand(untrustCmd())
}

// askMatchRule prompts the user with human-friendly questions to build a match rule.
// cwd is used as the default for "this folder" option.
func askMatchRule(out io.Writer, reader *bufio.Reader, cwd string) (config.MatchRule, error) {
	fmt.Fprintln(out, "  How should aide recognize this context?")
	fmt.Fprintf(out, "    [1] This folder (%s)\n", cwd)
	fmt.Fprintln(out, "    [2] A folder path or pattern")
	fmt.Fprintln(out, "    [3] By git repository URL")
	fmt.Fprint(out, "  Select [1]: ")

	input, err := reader.ReadString('\n')
	if err != nil {
		return config.MatchRule{}, fmt.Errorf("reading selection: %w", err)
	}
	input = strings.TrimSpace(input)

	choice := 1
	if input != "" {
		choice, err = strconv.Atoi(input)
		if err != nil || choice < 1 || choice > 3 {
			return config.MatchRule{}, fmt.Errorf("invalid selection: %q", input)
		}
	}

	switch choice {
	case 1:
		path := cwd
		fmt.Fprint(out, "  Include all subfolders? (Y/n): ")
		answer, err := reader.ReadString('\n')
		if err != nil {
			return config.MatchRule{}, fmt.Errorf("reading response: %w", err)
		}
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer == "" || answer == "y" || answer == "yes" {
			path = cwd + "/**"
		}
		return config.MatchRule{Path: path}, nil

	case 2:
		fmt.Fprint(out, "  Folder path: ")
		pathInput, err := reader.ReadString('\n')
		if err != nil {
			return config.MatchRule{}, fmt.Errorf("reading path: %w", err)
		}
		path := strings.TrimSpace(pathInput)
		if path == "" {
			return config.MatchRule{}, fmt.Errorf("path cannot be empty")
		}
		fmt.Fprint(out, "  Include all subfolders? (Y/n): ")
		answer, err := reader.ReadString('\n')
		if err != nil {
			return config.MatchRule{}, fmt.Errorf("reading response: %w", err)
		}
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer == "" || answer == "y" || answer == "yes" {
			path = strings.TrimRight(path, "/") + "/**"
		}
		return config.MatchRule{Path: path}, nil

	case 3:
		fmt.Fprintln(out, "  Examples: github.com/company/*, gitlab.com/team/project")
		fmt.Fprint(out, "  Git remote URL pattern: ")
		urlInput, err := reader.ReadString('\n')
		if err != nil {
			return config.MatchRule{}, fmt.Errorf("reading URL: %w", err)
		}
		url := strings.TrimSpace(urlInput)
		if url == "" {
			return config.MatchRule{}, fmt.Errorf("URL pattern cannot be empty")
		}
		return config.MatchRule{Remote: url}, nil
	}

	return config.MatchRule{}, fmt.Errorf("invalid selection")
}

// resolveContextForMutation loads config, resolves the context name, and returns
// the config, context name, and context for modification.
//
// This helper does not use cmdEnv because it has no *cobra.Command in scope —
// callers invoke it from several RunE bodies with arguments other than cmd.
// The preamble it performs is identical in shape to cmdEnv but is duplicated
// here intentionally to keep the signature *cobra.Command-free.
func resolveContextForMutation(contextName string) (*config.Config, string, config.Context, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, "", config.Context{}, fmt.Errorf("getting working directory: %w", err)
	}
	cfg, err := config.Load(config.Dir(), cwd)
	if err != nil {
		return nil, "", config.Context{}, fmt.Errorf("loading config: %w", err)
	}
	if contextName == "" {
		remoteURL := aidectx.DetectRemote(cwd, "origin")
		rc, err := aidectx.Resolve(cfg, cwd, remoteURL)
		if err != nil {
			return nil, "", config.Context{}, fmt.Errorf("resolving context: %w", err)
		}
		contextName = rc.Name
	}
	ctx, ok := cfg.Contexts[contextName]
	if !ok {
		return nil, "", config.Context{}, fmt.Errorf("context %q not found", contextName)
	}
	return cfg, contextName, ctx, nil
}

// resolveProjectOverrideForMutation loads the global config and project override
// for mutation. Returns the global config (for validation), the project override
// (empty if .aide.yaml doesn't exist), and the path to write .aide.yaml to.
//
// Like resolveContextForMutation, this helper has no *cobra.Command in scope
// and so performs its own preamble rather than using cmdEnv.
func resolveProjectOverrideForMutation() (*config.Config, *config.ProjectOverride, string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, nil, "", fmt.Errorf("getting working directory: %w", err)
	}
	cfg, err := config.Load(config.Dir(), cwd)
	if err != nil {
		return nil, nil, "", fmt.Errorf("loading config: %w", err)
	}
	poPath := config.FindProjectConfigForWrite(cwd)
	po := cfg.ProjectOverride
	if po == nil {
		po = &config.ProjectOverride{}
	}
	return cfg, po, poPath, nil
}

// capabilityNamesForCompletion returns a sorted list of all capability names
// (built-in + user-defined from config) for shell tab completion.
//
// This helper is called from the cobra flag completion callback in main.go,
// which receives (*cobra.Command, []string, string) — the *cobra.Command it
// gets is the one being completed against, and wiring cmdEnv through the
// completion path would push Env construction into every tab-completion call.
// The direct config.Load here is both best-effort (errors swallowed so a
// partially loadable config still yields the built-in completions) and
// performance-sensitive, so it stays inline.
func capabilityNamesForCompletion() []string {
	builtins := capability.Builtins()
	names := make([]string, 0, len(builtins))
	for name := range builtins {
		names = append(names, name)
	}

	// Try to load config for user-defined capabilities.
	cwd, err := os.Getwd()
	if err == nil {
		if cfg, err := config.Load(config.Dir(), cwd); err == nil {
			userCaps := capability.FromConfigDefs(cfg.Capabilities)
			for name := range userCaps {
				// Only add if not already present from builtins.
				if _, exists := builtins[name]; !exists {
					names = append(names, name)
				}
			}
		}
	}

	sort.Strings(names)
	return names
}
