// Hooks-related commands: `aide hook list`, `aide hook add`, `aide hook remove`.
// Hooks are declared at the top level in config.yaml and are applied to all contexts
// (unlike plugins/MCP servers which are per-context).
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/provision"
	"github.com/spf13/cobra"
)

func hookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hook",
		Short: "Manage declared agent hooks",
	}
	cmd.AddCommand(hookListCmd())
	cmd.AddCommand(hookAddCmd())
	cmd.AddCommand(hookRemoveCmd())
	return cmd
}

func hookListCmd() *cobra.Command {
	var contextName string
	cmd := &cobra.Command{
		Use:           "list",
		Short:         "Show declared and managed hooks",
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runHookList(cmd.OutOrStdout(), contextName)
		},
	}
	cmd.Flags().StringVar(&contextName, "context", "", "Context name (default: matched by CWD)")
	return cmd
}

func hookAddCmd() *cobra.Command {
	var contextName string
	var event string
	var matcher string
	var command string

	cmd := &cobra.Command{
		Use:           "add",
		Short:         "Add a hook declaration to config.yaml",
		SilenceUsage:  true,
		Long: `aide hook add adds a hook declaration to config.yaml.

When all flags are provided, the hook is added directly.
When flags are omitted, an interactive prompt guides you through the inputs.

Valid events: pre_tool, post_tool, session_start, session_end, notification, stop
Valid matchers: shell (or omit for all tools)
Command template variables: {agent} (substituted with context's agent name)`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runHookAdd(cmd.OutOrStdout(), cmd.InOrStdin(), event, matcher, command, contextName)
		},
	}
	cmd.Flags().StringVar(&event, "event", "", "Hook event (pre_tool, post_tool, session_start, session_end, notification, stop)")
	cmd.Flags().StringVar(&matcher, "matcher", "", "Hook matcher (shell, or empty for all tools)")
	cmd.Flags().StringVar(&command, "command", "", "Command to execute")
	cmd.Flags().StringVar(&contextName, "context", "", "Context name (top-level by default; per-context support in a future release)")
	return cmd
}

func hookRemoveCmd() *cobra.Command {
	var contextName string
	var event string
	var matcher string
	var command string

	cmd := &cobra.Command{
		Use:          "remove",
		Short:        "Remove a hook declaration from config.yaml",
		SilenceUsage: true,
		Long: `aide hook remove removes a hook entry matching the given event, matcher, and command.

The matcher is optional; if omitted, the first entry matching event and command is removed.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runHookRemove(cmd.OutOrStdout(), event, matcher, command, contextName)
		},
	}
	cmd.Flags().StringVar(&event, "event", "", "Hook event")
	cmd.Flags().StringVar(&matcher, "matcher", "", "Tool matcher (optional)")
	cmd.Flags().StringVar(&command, "command", "", "Command string (must match exactly)")
	cmd.Flags().StringVar(&contextName, "context", "", "Context name (top-level by default; per-context support in a future release)")
	_ = cmd.MarkFlagRequired("event")
	_ = cmd.MarkFlagRequired("command")
	return cmd
}

func runHookList(out io.Writer, contextName string) error {
	env, err := loadProvisionEnv(contextName)
	if err != nil {
		return err
	}
	desired, err := provision.ResolveDesired(env.cfg, env.contextName)
	if err != nil {
		return err
	}

	managedHooks := []provision.ManagedHook{}
	if cs, ok := env.state.Contexts[env.contextName]; ok && cs != nil {
		managedHooks = cs.Hooks
	}

	fmt.Fprintf(out, "Context: %s (agent: %s)\n\n", env.contextName, env.ctx.Agent)
	renderHookTable(out, desired.Hooks, managedHooks)
	return nil
}

func renderHookTable(out io.Writer, desired []provision.Hook, managed []provision.ManagedHook) {
	managedSet := buildManagedHookSet(managed)

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "  EVENT\tMATCHER\tCOMMAND\tMANAGED")

	// Combine desired and managed for display
	displayHooks := make([]struct {
		event   string
		matcher string
		command string
		managed bool
	}, 0)

	// Add all desired hooks
	for _, h := range desired {
		matcher := h.Matcher
		if matcher == "" {
			matcher = "—"
		}
		key := provision.HookKey(h.Event, h.Matcher, h.Command)
		displayHooks = append(displayHooks, struct {
			event   string
			matcher string
			command string
			managed bool
		}{h.Event, matcher, h.Command, managedSet[key]})
	}

	// Add any managed hooks not in desired
	for _, m := range managed {
		key := provision.HookKey(m.Event, m.Matcher, m.Command)
		found := false
		for _, d := range desired {
			if provision.HookKey(d.Event, d.Matcher, d.Command) == key {
				found = true
				break
			}
		}
		if !found {
			matcher := m.Matcher
			if matcher == "" {
				matcher = "—"
			}
			displayHooks = append(displayHooks, struct {
				event   string
				matcher string
				command string
				managed bool
			}{m.Event, matcher, m.Command, true})
		}
	}

	if len(displayHooks) == 0 {
		fmt.Fprintln(tw, "  (no hooks declared)")
		_ = tw.Flush()
		return
	}

	for _, h := range displayHooks {
		mgd := "—"
		if h.managed {
			mgd = "✓"
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", h.event, h.matcher, h.command, mgd)
	}
	_ = tw.Flush()
}

func buildManagedHookSet(managed []provision.ManagedHook) map[string]bool {
	out := make(map[string]bool)
	for _, m := range managed {
		key := provision.HookKey(m.Event, m.Matcher, m.Command)
		out[key] = true
	}
	return out
}

func runHookAdd(out io.Writer, in io.Reader, event, matcher, command, _ string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}
	cfg, err := config.Load(config.Dir(), cwd)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// If flags are not fully provided, prompt interactively
	if event == "" || command == "" {
		reader := bufio.NewReader(in)
		if event == "" {
			event, err = promptHookEvent(out, reader)
			if err != nil {
				return err
			}
		}
		if matcher == "" {
			matcher, err = promptHookMatcher(out, reader)
			if err != nil {
				return err
			}
		}
		if command == "" {
			command, err = promptHookCommand(out, reader)
			if err != nil {
				return err
			}
		}
		// Prompt for context (all contexts only for now)
		fmt.Fprintln(out, "Apply to? [all contexts]")
		fmt.Fprint(out, "> ")
		// Read and accept the input (we always write to top-level)
		_, _ = reader.ReadString('\n')
	}

	// Normalize matcher: empty string is valid, means "all tools"
	if matcher != "shell" && matcher != "" {
		return fmt.Errorf("invalid matcher: %q (expected 'shell' or empty for all tools)", matcher)
	}

	// Validate event
	if !isValidEvent(event) {
		return fmt.Errorf("invalid event: %q (expected one of: pre_tool, post_tool, session_start, session_end, notification, stop)", event)
	}

	// Validate command safety before persisting to config.
	// Use the template-aware variant so {agent} is allowed.
	if err := provision.ValidateHookCommandTemplate(command); err != nil {
		return fmt.Errorf("invalid command: %w", err)
	}

	// Reject duplicate (event, matcher, command) triples.
	for _, existing := range cfg.Hooks[event] {
		if existing.Matcher == matcher && existing.Command == command {
			return fmt.Errorf("hook already declared: event=%s matcher=%q command=%q", event, matcher, command)
		}
	}

	// Add to config
	if cfg.Hooks == nil {
		cfg.Hooks = config.HooksMap{}
	}
	entry := config.HookEntry{
		Matcher: matcher,
		Command: command,
	}
	cfg.Hooks[event] = append(cfg.Hooks[event], entry)

	// Write config
	if err := config.WriteConfig(cfg); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	fmt.Fprintln(out, "Added hook. Run 'aide sync' to apply.")
	return nil
}

func runHookRemove(out io.Writer, event, matcher, command, contextName string) error {
	env, err := loadProvisionEnv(contextName)
	if err != nil {
		return err
	}
	cfg := env.cfg
	agentName := env.ctx.Agent

	if cfg.Hooks == nil || len(cfg.Hooks[event]) == 0 {
		fmt.Fprintf(out, "no hook found matching event=%s command=%s\n", event, command)
		return nil
	}

	entries := cfg.Hooks[event]
	found := false
	newEntries := []config.HookEntry{}
	for _, e := range entries {
		matcherMatches := matcher == "" || e.Matcher == matcher
		// Accept both the raw template form and the agent-resolved form.
		resolved := strings.NewReplacer("{agent}", agentName).Replace(e.Command)
		commandMatches := e.Command == command || resolved == command
		if commandMatches && matcherMatches && !found {
			found = true
			continue
		}
		newEntries = append(newEntries, e)
	}

	if !found {
		fmt.Fprintf(out, "no hook found matching event=%s matcher=%q command=%s\n", event, matcher, command)
		return nil
	}

	if len(newEntries) == 0 {
		delete(cfg.Hooks, event)
	} else {
		cfg.Hooks[event] = newEntries
	}

	if err := config.WriteConfig(cfg); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	fmt.Fprintln(out, "Removed hook. Run 'aide sync' to apply.")
	return nil
}

func promptHookEvent(out io.Writer, reader *bufio.Reader) (string, error) {
	fmt.Fprintln(out, "Event? [pre_tool, post_tool, session_start, session_end, notification, stop]")
	fmt.Fprint(out, "> ")
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("reading event: %w", err)
	}
	event := strings.TrimSpace(input)
	if !isValidEvent(event) {
		return "", fmt.Errorf("invalid event: %q", event)
	}
	return event, nil
}

func promptHookMatcher(out io.Writer, reader *bufio.Reader) (string, error) {
	fmt.Fprintln(out, "Matcher? [shell, or press enter for all tools]")
	fmt.Fprint(out, "> ")
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("reading matcher: %w", err)
	}
	matcher := strings.TrimSpace(input)
	if matcher != "" && matcher != "shell" {
		return "", fmt.Errorf("invalid matcher: %q (expected 'shell' or empty)", matcher)
	}
	return matcher, nil
}

func promptHookCommand(out io.Writer, reader *bufio.Reader) (string, error) {
	fmt.Fprintln(out, "Command?")
	fmt.Fprintln(out, "  Template variables (substituted automatically at sync time):")
	fmt.Fprintln(out, "    {agent}  replaced with the agent name for each context")
	fmt.Fprint(out, "> ")
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("reading command: %w", err)
	}
	command := strings.TrimSpace(input)
	if command == "" {
		return "", fmt.Errorf("command cannot be empty")
	}
	return command, nil
}

func isValidEvent(event string) bool {
	valid := []string{"pre_tool", "post_tool", "session_start", "session_end", "notification", "stop"}
	for _, v := range valid {
		if event == v {
			return true
		}
	}
	return false
}
