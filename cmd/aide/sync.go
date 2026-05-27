// `aide sync` — plan-then-apply reconciliation of declared plugins and
// MCP servers against the agent's installed state. See
// docs/specs/2026-05-15-declarative-agent-provisioning-design.md.
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/secrets"
	"github.com/spf13/cobra"
)

func syncCmd() *cobra.Command {
	var contextName string
	var planOnly bool
	var yes bool
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Reconcile declared plugins and MCP servers for a context",
		Long: `aide sync inspects the agent's installed state, computes the diff
against the context's declared plugins and MCP servers, and applies
the changes through the agent's CLI / config file.

Flags:
  --plan   Print the plan and exit without making changes.
  --yes    Non-interactive mode: apply with the default actions and
           skip the confirmation prompt. Unmanaged items are left
           in place. Fails fast if the agent's plugin install path
           requires a TTY.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSync(cmd.OutOrStdout(), cmd.InOrStdin(), contextName, planOnly, yes)
		},
	}
	cmd.Flags().StringVar(&contextName, "context", "", "Context name (default: matched by CWD)")
	cmd.Flags().BoolVar(&planOnly, "plan", false, "Show the plan and exit without applying")
	cmd.Flags().BoolVar(&yes, "yes", false, "Apply without prompting")
	return cmd
}

func runSync(out io.Writer, in io.Reader, contextName string, planOnly, yes bool) error {
	env, err := loadProvisionEnv(contextName)
	if err != nil {
		return err
	}
	desired, err := provision.ResolveDesired(env.cfg, env.contextName)
	if err != nil {
		return err
	}

	// Template-resolve MCP env values against context secrets so the
	// plan diff compares already-resolved values against the agent's
	// installed state. Without this, a desired `{{ .secrets.X }}` would
	// never equal the agent's "real-value" installed state and aide
	// sync would propose an unnecessary update every run. Plan output
	// itself only prints op kind + name (not env values), so resolving
	// here does not leak secrets to stdout / journal files.
	if err := resolveMCPSecretsForSync(env, &desired); err != nil {
		return err
	}

	// Capability mismatch surfaces here so we don't even build a plan
	// the engine would refuse to execute.
	if !env.prov.SupportsPlugins() && len(desired.Plugins) > 0 {
		return fmt.Errorf("agent %q does not support plugins (declared: %d)", env.prov.Name(), len(desired.Plugins))
	}
	if !env.prov.SupportsMCP() && len(desired.MCPServers) > 0 {
		return fmt.Errorf("agent %q does not support MCP servers (declared: %d)", env.prov.Name(), len(desired.MCPServers))
	}

	installed := provision.Installed{
		MCPServers:   map[string]provision.MCPServer{},
		Marketplaces: map[string]provision.Marketplace{},
	}
	if env.prov.SupportsPlugins() {
		got, err := env.prov.InstalledPlugins(env.provCtx)
		if err != nil {
			return fmt.Errorf("listing installed plugins: %w", err)
		}
		for _, p := range got {
			installed.Plugins = append(installed.Plugins, p.Key)
		}
		// Marketplaces are an attribute of plugin-supporting agents.
		mks, err := env.prov.InstalledMarketplaces(env.provCtx)
		if err != nil {
			return fmt.Errorf("listing installed marketplaces: %w", err)
		}
		for _, m := range mks {
			installed.Marketplaces[m.Key] = m
		}
	}
	var managedCtxState provision.ContextState
	if cs, ok := env.state.Contexts[env.contextName]; ok && cs != nil {
		managedCtxState = *cs
	}

	if env.prov.SupportsMCP() {
		names := provision.MCPQueryNames(desired.MCPServers, managedCtxState.MCPServers)
		got, err := provision.ReadInstalledMCP(env.prov, env.provCtx, names)
		if err != nil {
			return err
		}
		installed.MCPServers = got
	}

	plan := provision.ComputePlan(env.provCtx, desired, installed, managedCtxState)
	renderPlan(out, plan)

	if planOnly {
		return nil
	}

	if plan.HasMutations() {
		// TTY short-circuit: avoid hanging on stdin for drivers whose
		// plugin install path requires a real terminal.
		if env.prov.RequiresTTY() && yes && hasPluginMutations(plan) {
			return fmt.Errorf("agent %q plugin install requires TTY; cannot run with --yes", env.prov.Name())
		}

		if !yes {
			// Unmanaged items used to hard-bail here ("run aide adopt
			// first"), which forced an unrelated workflow whenever a
			// context had plugins/MCPs aide didn't know about. The
			// OpIgnore path is genuinely a no-op (engine.go skips it),
			// so the gate added friction without preventing harm. Now
			// we just inform and let the prompt cover consent; users
			// who want the items managed can still run `aide adopt`
			// separately.
			if n := unmanagedCount(plan); n > 0 {
				fmt.Fprintf(out, "\nNote: %d unmanaged item(s) will be left alone. Run `aide adopt` to bring them under aide.\n", n)
			}
			fmt.Fprint(out, "\nApply this plan? [y/N]: ")
			reader := bufio.NewReader(in)
			ans, _ := reader.ReadString('\n')
			ans = strings.TrimSpace(strings.ToLower(ans))
			if ans != "y" && ans != "yes" {
				fmt.Fprintln(out, "Aborted.")
				return nil
			}
		}

		if _, err := provision.Apply(env.prov, plan, provision.ApplyOptions{}); err != nil {
			return err
		}
	}

	// Persist state on success — including the no-mutations case so
	// declared-and-already-installed items get claimed as managed on
	// first sync.
	if err := updateStateAfterSync(env, desired, plan); err != nil {
		return fmt.Errorf("saving state: %w", err)
	}

	if plan.HasMutations() {
		installs, updates, uninstalls := countOps(plan)
		fmt.Fprintf(out, "\nSync complete: %d installed, %d updated, %d uninstalled.\n", installs, updates, uninstalls)
	} else {
		fmt.Fprintln(out, "\nNothing to apply. Declared items are already installed; state updated.")
	}
	return nil
}

func renderPlan(out io.Writer, plan provision.Plan) {
	if len(plan.Ops) == 0 {
		fmt.Fprintf(out, "Plan for context %s: no changes\n", plan.Context.Name)
		return
	}
	fmt.Fprintf(out, "Plan for context %s (agent: %s):\n\n", plan.Context.Name, plan.Context.Agent)
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, op := range plan.Ops {
		sym := opSymbol(op.OpKind)
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", sym, op.Kind.String(), op.Name)
	}
	_ = tw.Flush()
}

func opSymbol(k provision.OpKind) string {
	switch k {
	case provision.OpInstall:
		return "+ install"
	case provision.OpUpdate:
		return "~ update"
	case provision.OpUninstall:
		return "- uninstall"
	case provision.OpAdopt:
		return "↑ adopt"
	case provision.OpIgnore:
		return "  unmanaged"
	}
	return "?"
}

func hasPluginMutations(plan provision.Plan) bool {
	for _, op := range plan.Ops {
		if op.Kind == provision.KindPlugin && op.OpKind != provision.OpIgnore {
			return true
		}
	}
	return false
}

// unmanagedCount counts OpIgnore ops — items installed in the agent
// that aide neither declares nor manages. The engine treats them as
// no-ops; the sync prompt surfaces the count so users know aide saw
// them and chose to leave them alone.
func unmanagedCount(plan provision.Plan) int {
	n := 0
	for _, op := range plan.Ops {
		if op.OpKind == provision.OpIgnore {
			n++
		}
	}
	return n
}

func countOps(plan provision.Plan) (installs, updates, uninstalls int) {
	for _, op := range plan.Ops {
		switch op.OpKind {
		case provision.OpInstall:
			installs++
		case provision.OpUpdate:
			updates++
		case provision.OpUninstall:
			uninstalls++
		case provision.OpAdopt, provision.OpIgnore:
			// counted elsewhere; not a sync-plan op
		}
	}
	return
}

// updateStateAfterSync writes the post-sync state: the desired set
// becomes the managed set, the config-hash is refreshed, and SyncedAt
// is bumped.
func updateStateAfterSync(env *provisionEnv, desired provision.Desired, plan provision.Plan) error {
	hash, err := provision.ConfigHash(config.FilePath())
	if err != nil {
		return err
	}
	if env.state.Contexts == nil {
		env.state.Contexts = map[string]*provision.ContextState{}
	}
	cs := env.state.Contexts[env.contextName]
	if cs == nil {
		cs = &provision.ContextState{}
		env.state.Contexts[env.contextName] = cs
	}
	if cs.Plugins == nil {
		cs.Plugins = map[string]provision.ManagedItem{}
	}
	if cs.MCPServers == nil {
		cs.MCPServers = map[string]provision.ManagedItem{}
	}
	if cs.Marketplaces == nil {
		cs.Marketplaces = map[string]provision.ManagedItem{}
	}
	// Drop managed entries that were uninstalled.
	for _, op := range plan.Ops {
		if op.OpKind == provision.OpUninstall {
			switch op.Kind {
			case provision.KindPlugin:
				delete(cs.Plugins, op.Name)
			case provision.KindMCP:
				delete(cs.MCPServers, op.Name)
			case provision.KindMarketplace:
				delete(cs.Marketplaces, op.Name)
			}
		}
	}
	now := time.Now().UTC()
	for k, p := range desired.Plugins {
		mi := cs.Plugins[k]
		if mi.InstalledAt.IsZero() {
			mi.InstalledAt = now
		}
		mi.Version = pluginVersion(p.Name)
		cs.Plugins[k] = mi
	}
	for k := range desired.MCPServers {
		mi := cs.MCPServers[k]
		if mi.InstalledAt.IsZero() {
			mi.InstalledAt = now
		}
		cs.MCPServers[k] = mi
	}
	for k, m := range desired.Marketplaces {
		mi := cs.Marketplaces[k]
		if mi.InstalledAt.IsZero() {
			mi.InstalledAt = now
		}
		mi.Source = m.Source
		cs.Marketplaces[k] = mi
	}
	cs.ConfigHash = hash
	cs.SyncedAt = now
	if err := os.MkdirAll(parentDir(env.statePath), 0o750); err != nil {
		return err
	}
	return provision.SaveState(env.statePath, env.state)
}

func pluginVersion(name string) string {
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '@' {
			return name[i+1:]
		}
	}
	return ""
}

func parentDir(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[:i]
		}
	}
	return "."
}

// resolveMCPSecretsForSync mirrors the launcher's secret-decrypt +
// template-resolve dance, but applies it to the MCP server env map in
// the sync pipeline. When the context declares a secret file, decrypt
// it; build a TemplateData; pass it (or nil if no secret is configured)
// to provision.ResolveSecretsInMCPEnv. ResolveSecretsInMCPEnv's nil-td
// branch is what catches the misconfiguration where the user references
// {{ .secrets.X }} in an MCP env without supplying a secret file.
//
// RuntimeDir is left empty: sync writes to the agent's config FILE, not
// the agent's runtime, so {{ .runtime_dir }} has no meaning here.
// Referencing it in an MCP env will silently substitute an empty string
// — a known v1 quirk; the canonical use case is {{ .secrets.X }}.
func resolveMCPSecretsForSync(env *provisionEnv, desired *provision.Desired) error {
	var td *config.TemplateData
	if env.ctx.Secret != "" {
		secretsPath := config.ResolveSecretPath(env.ctx.Secret)
		identity, err := secrets.DiscoverAgeKey()
		if err != nil {
			return fmt.Errorf("discovering age key for context %q: %w", env.contextName, err)
		}
		secretsMap, err := secrets.DecryptSecretsFile(secretsPath, identity)
		if err != nil {
			return fmt.Errorf("decrypting secrets for context %q: %w", env.contextName, err)
		}
		cwd, _ := os.Getwd()
		td = &config.TemplateData{
			Secrets:     secretsMap,
			ProjectRoot: cwd,
		}
	}
	return provision.ResolveSecretsInMCPEnv(desired, td)
}
