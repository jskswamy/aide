package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/provision"
	claudeprov "github.com/jskswamy/aide/internal/provision/agents/claude"
	"github.com/spf13/cobra"
)

func statuslineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "statusline <agent>",
		Short:        "Render or install the aide statusline for a coding agent",
		SilenceUsage: true,
	}
	cmd.AddCommand(statuslineAgentCmd("claude"))
	return cmd
}

func statuslineAgentCmd(agent string) *cobra.Command {
	var install, remove bool
	cmd := &cobra.Command{
		Use:          agent,
		Short:        fmt.Sprintf("Render or install aide statusline for %s", agent),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("home dir: %w", err)
			}
			ctx := provision.Context{
				HomeDir: homeDir,
				Env:     envSliceToMap(os.Environ()),
			}
			switch {
			case install:
				return installStatusline(cmd, ctx, homeDir, agent)
			case remove:
				return removeStatusline(cmd, ctx, homeDir)
			}

			// Render mode: only when stdin is a pipe (invoked by Claude Code).
			fi, err := os.Stdin.Stat()
			if err != nil || (fi.Mode()&os.ModeCharDevice) != 0 {
				fmt.Fprintf(cmd.OutOrStdout(),
					"aide statusline %s\n\nRender aide session state as a statusline.\n\nFlags:\n  --install  Configure %s to run aide statusline\n  --remove   Remove the statusLine entry\n",
					agent, agent)
				return nil
			}

			io.Copy(io.Discard, os.Stdin) //nolint:errcheck

			cwd, _ := os.Getwd()
			cfg, _ := config.Load(config.Dir(), cwd)
			var global, project *config.StatuslineConfig
			if cfg != nil {
				global = cfg.Statusline
				if cfg.ProjectOverride != nil {
					project = cfg.ProjectOverride.Statusline
				}
			}
			resolved := config.ResolveStatusline(global, project)
			out := renderStatusline(resolved, envForRender())
			if out != "" {
				fmt.Fprintln(cmd.OutOrStdout(), out)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&install, "install", false, fmt.Sprintf("Install aide statusline for %s", agent))
	cmd.Flags().BoolVar(&remove, "remove", false, "Remove aide statusline")
	return cmd
}

func installStatusline(cmd *cobra.Command, ctx provision.Context, homeDir, agent string) error {
	existing, err := claudeprov.ReadStatusLine(ctx)
	if err != nil {
		return err
	}
	target := fmt.Sprintf("aide statusline %s", agent)
	switch existing {
	case "":
		if err := claudeprov.WriteStatusLine(ctx, target); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Installed: statusLine.command = %q\n", target)
	case target:
		fmt.Fprintln(cmd.OutOrStdout(), "Already installed.")
	default:
		wrapperPath, err := claudeprov.WriteWrapper(homeDir, existing)
		if err != nil {
			return err
		}
		if err := claudeprov.WriteStatusLine(ctx, wrapperPath); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Generated wrapper at %s\nstatusLine.command → %s\n", wrapperPath, wrapperPath)
	}
	return nil
}

func removeStatusline(cmd *cobra.Command, ctx provision.Context, homeDir string) error {
	prev, err := claudeprov.RemoveStatusLine(ctx)
	if err != nil {
		return err
	}
	if prev == "" {
		fmt.Fprintln(cmd.OutOrStdout(), "statusLine was not configured.")
		return nil
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Removed statusLine.")
	if prev == claudeprov.WrapperScriptPath(homeDir) {
		fmt.Fprintf(cmd.OutOrStdout(), "Wrapper at %s — delete manually if no longer needed.\n", prev)
	}
	return nil
}

// renderStatusline renders active aide modules joined by " | ".
// auto_approve is always prepended first when active, regardless of order.
func renderStatusline(cfg config.StatuslineConfig, env map[string]string) string {
	var parts []string
	if env["AIDE_AUTO_APPROVE"] == "1" && cfg.AutoApprove != nil {
		v := cfg.AutoApprove.Value
		if v == "" {
			v = "🚨"
		}
		parts = append(parts, v)
	}
	for _, mod := range cfg.Order {
		if s := renderModule(mod, cfg, env); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " | ")
}

func isModuleDisabled(m *config.ModuleConfig) bool {
	return m != nil && m.Disabled != nil && *m.Disabled
}

func renderModule(name string, cfg config.StatuslineConfig, env map[string]string) string {
	switch name {
	case "sandbox":
		if isModuleDisabled(cfg.Sandbox) || cfg.Sandbox == nil {
			return ""
		}
		if env["AIDE_SANDBOX"] == "off" {
			return cfg.Sandbox.Off
		}
		return cfg.Sandbox.On

	case "network":
		if isModuleDisabled(cfg.Network) || cfg.Network == nil {
			return ""
		}
		if env["AIDE_NETWORK_MODE"] == "unrestricted" {
			return cfg.Network.Unrestricted
		}
		return cfg.Network.Outbound

	case "caps":
		if isModuleDisabled(cfg.Caps) || cfg.Caps == nil {
			return ""
		}
		caps := env["AIDE_CAPS"]
		if caps == "" {
			return ""
		}
		icon := cfg.Caps.Icon
		if icon == "" {
			icon = "⚡"
		}
		return icon + " " + caps

	case "trust":
		if isModuleDisabled(cfg.Trust) || cfg.Trust == nil {
			return ""
		}
		if env["AIDE_TRUST"] != "untrusted" {
			return ""
		}
		return cfg.Trust.Untrusted

	case "context":
		if isModuleDisabled(cfg.Context) || cfg.Context == nil {
			return ""
		}
		ctx := env["AIDE_CONTEXT"]
		if ctx == "" {
			return ""
		}
		icon := cfg.Context.Icon
		if icon == "" {
			icon = "📁"
		}
		return icon + " " + ctx

	case "auto_approve":
		return "" // always prepended separately; ignored in order loop
	}
	return ""
}

// envForRender extracts AIDE_* vars from os.Environ for the render path.
func envForRender() map[string]string {
	return map[string]string{
		"AIDE_SANDBOX":      os.Getenv("AIDE_SANDBOX"),
		"AIDE_NETWORK_MODE": os.Getenv("AIDE_NETWORK_MODE"),
		"AIDE_CAPS":         os.Getenv("AIDE_CAPS"),
		"AIDE_TRUST":        os.Getenv("AIDE_TRUST"),
		"AIDE_AUTO_APPROVE": os.Getenv("AIDE_AUTO_APPROVE"),
		"AIDE_CONTEXT":      os.Getenv("AIDE_CONTEXT"),
	}
}

// envSliceToMap converts []string{"K=V", ...} to map[string]string{"K":"V", ...}.
func envSliceToMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, kv := range env {
		if i := strings.IndexByte(kv, '='); i >= 0 {
			m[kv[:i]] = kv[i+1:]
		}
	}
	return m
}
