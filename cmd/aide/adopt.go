// `aide adopt` — promote agent-installed but undeclared plugins, MCP
// servers, marketplaces, and hooks into config.yaml so subsequent
// `aide sync` runs treat them as managed.
package main

import (
	"bufio"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/provision"
	"github.com/spf13/cobra"
)

func adoptCmd() *cobra.Command {
	var contextName string
	var yes bool
	cmd := &cobra.Command{
		Use:   "adopt",
		Short: "Promote unmanaged plugins/MCP servers into config.yaml",
		Long: `aide adopt walks plugins and MCP servers that are installed in the
agent but not declared in config.yaml, prompts to adopt each, and
rewrites config.yaml so they become part of the context's declared
state. After adoption the items are also recorded as managed in the
state file so future syncs reconcile them.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAdopt(cmd.OutOrStdout(), cmd.InOrStdin(), contextName, yes)
		},
	}
	cmd.Flags().StringVar(&contextName, "context", "", "Context name (default: matched by CWD)")
	cmd.Flags().BoolVar(&yes, "yes", false, "Adopt all unmanaged items without prompting")
	return cmd
}

func runAdopt(out io.Writer, in io.Reader, contextName string, yes bool) error {
	env, err := loadProvisionEnv(contextName)
	if err != nil {
		return err
	}
	agentDir := provision.ResolveAgentDir(env.prov, env.provCtx)
	desired, err := provision.ResolveDesired(env.cfg, env.contextName, agentDir, env.provCtx.HomeDir)
	if err != nil {
		return err
	}

	var installedPlugins []provision.Plugin
	if env.prov.SupportsPlugins() {
		got, err := env.prov.InstalledPlugins(env.provCtx)
		if err != nil {
			return fmt.Errorf("listing installed plugins: %w", err)
		}
		installedPlugins = got
	}

	managedMCPItems := map[string]provision.ManagedItem{}
	if cs, ok := env.state.Contexts[env.contextName]; ok && cs != nil {
		managedMCPItems = cs.MCPServers
	}
	installedMCP := map[string]provision.MCPServer{}
	if env.prov.SupportsMCP() {
		// Adopt's MCP discovery is bounded for CLI-driven drivers
		// (claude/gemini/codex/copilot) — there's no enumerate-all over
		// the agent's CLI surface that distinguishes aide-relevant
		// entries from plugin-bundled or built-in ones. For those
		// drivers, adopt won't surface manually-added MCP servers; the
		// user can add them to config.yaml directly. File-based handlers
		// (none today, but reserved for future agents without a CLI)
		// keep their full enumeration via ReadInstalledMCP's handler
		// branch.
		names := provision.MCPQueryNames(desired.MCPServers, managedMCPItems)
		got, err := provision.ReadInstalledMCP(env.prov, env.provCtx, names)
		if err != nil {
			return err
		}
		installedMCP = got
	}

	managedPlugins := managedPluginNames(env.state, env.contextName)
	managedMCP := managedMCPNames(env.state, env.contextName)

	var unmanagedPlugins []provision.Plugin
	for _, p := range installedPlugins {
		if _, isDesired := desired.Plugins[p.Key]; isDesired {
			continue
		}
		if managedPlugins[p.Key] {
			continue
		}
		unmanagedPlugins = append(unmanagedPlugins, p)
	}
	var unmanagedMCP []string
	for k := range installedMCP {
		if _, isDesired := desired.MCPServers[k]; isDesired {
			continue
		}
		if managedMCP[k] {
			continue
		}
		unmanagedMCP = append(unmanagedMCP, k)
	}
	// Marketplaces: ask the driver if it has any (only marketplace-class
	// drivers do). An installed marketplace that's neither declared nor
	// previously managed by aide is "unmanaged" — adopt records it as a
	// declare-only entry so the user can subsequently install plugins
	// from it via the regular declared workflow.
	managedMarkets := managedMarketplaceNames(env.state, env.contextName)
	var unmanagedMarketplaces []provision.Marketplace
	if supportsMarketplaces(env.prov) {
		if mks, err := env.prov.InstalledMarketplaces(env.provCtx); err == nil {
			for _, m := range mks {
				if _, isDesired := desired.Marketplaces[m.Key]; isDesired {
					continue
				}
				if managedMarkets[m.Key] {
					continue
				}
				unmanagedMarketplaces = append(unmanagedMarketplaces, m)
			}
		}
	}
	// Hooks: discover installed hooks not already in desired or managed.
	var unmanagedHooks []provision.Hook
	if hi, ok := env.prov.(provision.HookInstaller); ok {
		installedHooks, err := hi.ReadHooks(env.provCtx)
		if err != nil {
			return fmt.Errorf("listing installed hooks: %w", err)
		}
		managedHookSet := map[string]bool{}
		if cs, ok := env.state.Contexts[env.contextName]; ok && cs != nil {
			for _, mh := range cs.Hooks {
				managedHookSet[provision.HookKey(mh.Event, mh.Matcher, mh.Command)] = true
			}
		}
		desiredHookSet := map[string]bool{}
		for _, h := range desired.Hooks {
			desiredHookSet[provision.HookKey(h.Event, h.Matcher, h.Command)] = true
		}
		for _, h := range installedHooks {
			key := provision.HookKey(h.Event, h.Matcher, h.Command)
			if desiredHookSet[key] || managedHookSet[key] {
				continue
			}
			unmanagedHooks = append(unmanagedHooks, h)
		}
		sort.Slice(unmanagedHooks, func(i, j int) bool {
			return provision.HookKey(unmanagedHooks[i].Event, unmanagedHooks[i].Matcher, unmanagedHooks[i].Command) <
				provision.HookKey(unmanagedHooks[j].Event, unmanagedHooks[j].Matcher, unmanagedHooks[j].Command)
		})
	}

	sort.Slice(unmanagedPlugins, func(i, j int) bool { return unmanagedPlugins[i].Key < unmanagedPlugins[j].Key })
	sort.Strings(unmanagedMCP)
	sort.Slice(unmanagedMarketplaces, func(i, j int) bool { return unmanagedMarketplaces[i].Key < unmanagedMarketplaces[j].Key })

	if len(unmanagedPlugins) == 0 && len(unmanagedMCP) == 0 && len(unmanagedMarketplaces) == 0 && len(unmanagedHooks) == 0 {
		fmt.Fprintln(out, "No unmanaged plugins, MCP servers, marketplaces, or hooks to adopt.")
		return nil
	}

	reader := bufio.NewReader(in)
	adoptedPlugins := []provision.Plugin{}
	for _, p := range unmanagedPlugins {
		if yes || promptAdopt(out, reader, "plugin "+p.Key) {
			adoptedPlugins = append(adoptedPlugins, p)
		}
	}
	adoptedMCP := []string{}
	for _, k := range unmanagedMCP {
		if yes || promptAdopt(out, reader, "mcp "+k) {
			adoptedMCP = append(adoptedMCP, k)
		}
	}
	adoptedMarkets := []provision.Marketplace{}
	for _, m := range unmanagedMarketplaces {
		if yes || promptAdopt(out, reader, "marketplace "+m.Key) {
			adoptedMarkets = append(adoptedMarkets, m)
		}
	}
	adoptedHooks := []provision.Hook{}
	for _, h := range unmanagedHooks {
		label := "hook " + hookOpName(h.Event, h.Matcher, h.Command)
		if yes || promptAdopt(out, reader, label) {
			adoptedHooks = append(adoptedHooks, h)
		}
	}

	if len(adoptedPlugins) == 0 && len(adoptedMCP) == 0 && len(adoptedMarkets) == 0 && len(adoptedHooks) == 0 {
		fmt.Fprintln(out, "Nothing adopted.")
		return nil
	}

	// Rewrite config.yaml with the adopted entries.
	ctx := env.cfg.Contexts[env.contextName]
	if env.cfg.Plugins == nil && (len(adoptedPlugins) > 0 || len(adoptedMarkets) > 0) {
		env.cfg.Plugins = config.PluginMap{}
	}

	// Adopted marketplaces become declare-only (null-valued) entries —
	// the user has explicitly claimed the marketplace but hasn't yet
	// declared which plugins they want from it. If a subsequent adopt
	// brings plugin entries under the same key, the marketplace-shape
	// merge code below will upgrade the entry from declare-only to
	// a list-valued marketplace entry.
	for _, m := range adoptedMarkets {
		// Skip local/name-only marketplaces: config keys must look like
		// "owner/repo" or a URL; bare names like "rfctl-local" fail
		// ValidatePlugins and would corrupt the config.
		if !config.LooksLikeRepo(m.Key) {
			fmt.Fprintf(out, "Note: marketplace %q has no repo path — skipped from config (declare manually if needed).\n", m.Key)
			continue
		}
		if existing, ok := env.cfg.Plugins[m.Key]; ok {
			// Keep whatever shape already exists; declare-only would
			// otherwise overwrite a user-set entry.
			_ = existing
			continue
		}
		env.cfg.Plugins[m.Key] = config.PluginEntryDeclareOnly()
	}

	// For marketplace agents, look up the marketplace-name → repo
	// mapping once. Each adopted plugin's Name is of the form
	// `<plugin>@<marketplace-name>` (per the driver's installed-plugins
	// surface). We group plugins by marketplace and write list-valued
	// entries under the repo key.
	var marketplaceByName map[string]provision.Marketplace
	if supportsMarketplaces(env.prov) {
		if mks, err := env.prov.InstalledMarketplaces(env.provCtx); err == nil {
			marketplaceByName = map[string]provision.Marketplace{}
			for _, m := range mks {
				if m.Name != "" {
					marketplaceByName[m.Name] = m
				}
			}
		}
	}

	for _, p := range adoptedPlugins {
		// Try marketplace shape first: parse `<plugin>@<marketplace-name>`
		// from Plugin.Name and look up the marketplace's repo key.
		if marketplaceByName != nil {
			if plugin, marketName, ok := splitPluginRef(p.Name); ok {
				if mk, found := marketplaceByName[marketName]; found && mk.Key != "" {
					existing := env.cfg.Plugins[mk.Key]
					merged := appendPluginToMarketplace(existing, plugin)
					env.cfg.Plugins[mk.Key] = merged
					continue
				}
			}
		}
		// Fallback: URL-direct entry under the bare plugin name.
		// Used when the agent isn't marketplace-class OR when we
		// couldn't resolve the marketplace (e.g. agent reported a
		// plugin from a marketplace not currently listed).
		src := p.Name
		if src == "" {
			src = p.Key
		}
		env.cfg.Plugins[p.Key] = config.PluginEntryURLDirect(src)
	}
	if len(adoptedMCP) > 0 && env.cfg.MCPServers == nil {
		env.cfg.MCPServers = config.MCPServerMap{}
	}
	for _, k := range adoptedMCP {
		src := installedMCP[k]
		env.cfg.MCPServers[k] = config.MCPServer{
			Command: src.Command,
			URL:     src.URL,
			Args:    src.Args,
			Env:     src.Env,
		}
		if !slices.Contains(ctx.MCPServers, k) {
			ctx.MCPServers = append(ctx.MCPServers, k)
		}
	}
	env.cfg.Contexts[env.contextName] = ctx

	// Adopted hooks are written to the top-level hooks map so they apply
	// across contexts (matching how top-level plugins work). Duplicate
	// guard: skip entries already present (event+matcher+command match).
	if len(adoptedHooks) > 0 && env.cfg.Hooks == nil {
		env.cfg.Hooks = config.HooksMap{}
	}
	for _, h := range adoptedHooks {
		existing := env.cfg.Hooks[h.Event]
		alreadyPresent := false
		for _, e := range existing {
			if e.Matcher == h.Matcher && e.Command == h.Command {
				alreadyPresent = true
				break
			}
		}
		if !alreadyPresent {
			cmd := h.Command
			if agentDir != "" && strings.HasPrefix(cmd, agentDir+"/") {
				cmd = "{agent_dir}" + cmd[len(agentDir):]
			}
			env.cfg.Hooks[h.Event] = append(existing, config.HookEntry{
				Name:    hookCommandBasename(h.Command),
				Matcher: h.Matcher,
				Command: cmd,
				Timeout: h.Timeout,
			})
		}
	}

	if err := config.WriteConfig(env.cfg); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	// Mark the adopted items as managed in state so the next sync
	// keeps them aligned.
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
	now := time.Now().UTC()
	for _, p := range adoptedPlugins {
		cs.Plugins[p.Key] = provision.ManagedItem{InstalledAt: now, Version: pluginVersion(p.Name)}
	}
	for _, k := range adoptedMCP {
		cs.MCPServers[k] = provision.ManagedItem{InstalledAt: now}
	}
	for _, m := range adoptedMarkets {
		cs.Marketplaces[m.Key] = provision.ManagedItem{InstalledAt: now}
	}
	for _, h := range adoptedHooks {
		cs.Hooks = append(cs.Hooks, provision.ManagedHook{
			Event:   h.Event,
			Matcher: h.Matcher,
			Command: h.Command,
		})
	}
	if err := provision.SaveState(env.statePath, env.state); err != nil {
		return fmt.Errorf("saving state: %w", err)
	}

	fmt.Fprintf(out, "Adopted: %d plugin(s), %d mcp server(s), %d marketplace(s), %d hook(s).\n",
		len(adoptedPlugins), len(adoptedMCP), len(adoptedMarkets), len(adoptedHooks))
	return nil
}

// hookOpName builds the display label for a hook Op, including the matcher
// when set so that hooks sharing event+command but with different matchers
// appear distinct in adopt prompts.
func hookOpName(event, matcher, command string) string {
	if matcher == "" {
		return event + ":" + command
	}
	return event + ":" + matcher + ":" + command
}

// hookCommandBasename returns the last path component of a hook command,
// used as the Name field when adopting a hook into config.
func hookCommandBasename(command string) string {
	if i := strings.LastIndexByte(command, '/'); i >= 0 {
		return command[i+1:]
	}
	return command
}

func promptAdopt(out io.Writer, reader *bufio.Reader, label string) bool {
	fmt.Fprintf(out, "Adopt %s? [a]dopt / [s]kip: ", label)
	ans, _ := reader.ReadString('\n')
	ans = strings.TrimSpace(strings.ToLower(ans))
	return ans == "a" || ans == "adopt"
}

// splitPluginRef splits a `<plugin>@<marketplace-name>` ref into its
// components. Returns ok=false when the ref doesn't contain an "@" or
// either side is empty.
func splitPluginRef(ref string) (plugin, marketplace string, ok bool) {
	at := strings.IndexByte(ref, '@')
	if at <= 0 || at == len(ref)-1 {
		return "", "", false
	}
	return ref[:at], ref[at+1:], true
}

// appendPluginToMarketplace returns a marketplace-shape PluginEntry
// with the new plugin appended to existing.Plugins. If existing is a
// non-marketplace shape (or zero), the result is a new marketplace
// entry containing just the new plugin. Idempotent: appending the
// same plugin twice leaves the list unchanged.
func appendPluginToMarketplace(existing config.PluginEntry, plugin string) config.PluginEntry {
	plugins := []string{}
	if existing.Shape() == config.PluginShapeMarketplace {
		plugins = append(plugins, existing.Plugins...)
	}
	if slices.Contains(plugins, plugin) {
		return existing
	}
	plugins = append(plugins, plugin)
	sort.Strings(plugins)
	return config.PluginEntryMarketplace(plugins)
}
