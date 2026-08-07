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
	var agent string
	var modules []string
	var install, remove bool
	var contextName string
	cmd := &cobra.Command{
		Use:          "statusline [agent]",
		Short:        "Render or install the aide statusline for a coding agent",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if install || remove {
				if agent == "" {
					return fmt.Errorf(`--agent is required with --install/--remove (or use "aide statusline <agent> --install")`)
				}
				return runStatuslineInstallRemove(cmd, agent, install, remove, contextName)
			}
			return runStatuslineRender(cmd, agent, modules)
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "", "Coding agent (default: auto-detected)")
	cmd.Flags().StringSliceVar(&modules, "module", nil, "Render only these modules (repeatable)")
	cmd.Flags().BoolVar(&install, "install", false, "Install aide statusline")
	cmd.Flags().BoolVar(&remove, "remove", false, "Remove aide statusline")
	cmd.Flags().StringVar(&contextName, "context", "", "Context name (default: matched by CWD)")
	cmd.AddCommand(statuslineAgentCmd("claude"))
	return cmd
}

func statuslineAgentCmd(agent string) *cobra.Command {
	var install, remove bool
	var contextName string
	var modules []string
	cmd := &cobra.Command{
		Use:          agent,
		Short:        fmt.Sprintf("Render or install aide statusline for %s", agent),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if install || remove {
				return runStatuslineInstallRemove(cmd, agent, install, remove, contextName)
			}
			return runStatuslineRender(cmd, agent, modules)
		},
	}
	cmd.Flags().BoolVar(&install, "install", false, fmt.Sprintf("Install aide statusline for %s", agent))
	cmd.Flags().BoolVar(&remove, "remove", false, "Remove aide statusline")
	cmd.Flags().StringVar(&contextName, "context", "", "Context name (default: matched by CWD)")
	cmd.Flags().StringSliceVar(&modules, "module", nil, "Render only these modules (repeatable)")
	return cmd
}

// runStatuslineInstallRemove resolves the target agent's context and
// dispatches to installStatusline/removeStatusline. Shared by the bare
// `aide statusline --agent X --install` form and the explicit
// `aide statusline X --install` subcommand form.
func runStatuslineInstallRemove(cmd *cobra.Command, agent string, install, remove bool, contextName string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("home dir: %w", err)
	}
	cfg, name, cfgCtx, err := resolveContextForMutation(contextName)
	_ = cfg
	if err != nil {
		return err
	}
	cwd, _ := os.Getwd()
	pCtx, err := provision.ResolveContext(name, cfgCtx, homeDir, cwd, resolveContextEnv(cfgCtx, homeDir))
	if err != nil {
		return err
	}
	if install {
		return installStatusline(cmd, pCtx, homeDir, agent)
	}
	return removeStatusline(cmd, pCtx, homeDir)
}

// runStatuslineRender resolves the agent and renders the requested modules
// (or the full combined output when modules is empty) to stdout. Runs
// identically whether stdin is piped (Claude Code invoking it on every
// update) or a TTY (a human previewing the statusline directly) — the only
// difference is how the agent gets resolved (see resolveStatuslineAgent).
func runStatuslineRender(cmd *cobra.Command, explicitAgent string, modules []string) error {
	fi, statErr := os.Stdin.Stat()
	isTTY := statErr != nil || (fi.Mode()&os.ModeCharDevice) != 0

	var stdinData []byte
	if !isTTY {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("reading stdin: %w", err)
		}
		stdinData = data
	}

	agent := resolveStatuslineAgent(explicitAgent, isTTY, stdinData)
	if agent != "claude" {
		return fmt.Errorf("statusline rendering not yet supported for agent %q (only claude is supported)", agent)
	}

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
	out := renderStatuslineModules(resolved, envForRender(), modules)
	if out != "" {
		fmt.Fprintln(cmd.OutOrStdout(), out)
	}
	return nil
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

// renderStatuslineModules renders only the requested modules, in cfg.Order
// (not the order modules were requested in), joined by " | ". Empty
// modules renders the full combined output, identical to renderStatusline.
func renderStatuslineModules(cfg config.StatuslineConfig, env map[string]string, modules []string) string {
	if len(modules) == 0 {
		return renderStatusline(cfg, env)
	}
	want := make(map[string]bool, len(modules))
	for _, m := range modules {
		want[m] = true
	}
	var parts []string
	if want["auto_approve"] && env["AIDE_AUTO_APPROVE"] == "1" && cfg.AutoApprove != nil {
		v := cfg.AutoApprove.Value
		if v == "" {
			v = "🚨"
		}
		parts = append(parts, v)
	}
	for _, mod := range cfg.Order {
		if !want[mod] {
			continue
		}
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
		v, ok := env["AIDE_SANDBOX"]
		switch {
		case !ok:
			return cfg.Sandbox.Unmanaged
		case v == "off":
			return cfg.Sandbox.Off
		default:
			return cfg.Sandbox.On
		}

	case "network":
		if isModuleDisabled(cfg.Network) || cfg.Network == nil {
			return ""
		}
		v, ok := env["AIDE_NETWORK_MODE"]
		switch {
		case !ok:
			return cfg.Network.Unmanaged
		case v == "unrestricted":
			return cfg.Network.Unrestricted
		default:
			return cfg.Network.Outbound
		}

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

// envForRender extracts AIDE_* vars from the process environment for the
// render path. Keys are omitted entirely when the underlying env var is
// unset, distinguishing "absent" (aide didn't launch this session) from
// "present but empty" — renderModule relies on this distinction for the
// sandbox/network unmanaged state.
func envForRender() map[string]string {
	env := map[string]string{}
	for _, k := range []string{
		"AIDE_SANDBOX", "AIDE_NETWORK_MODE", "AIDE_CAPS",
		"AIDE_TRUST", "AIDE_AUTO_APPROVE", "AIDE_CONTEXT",
	} {
		if v, ok := os.LookupEnv(k); ok {
			env[k] = v
		}
	}
	return env
}
