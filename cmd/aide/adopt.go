// `aide adopt` — promote agent-installed but undeclared plugins and
// MCP servers into config.yaml so subsequent `aide sync` runs treat
// them as managed.
package main

import (
	"bufio"
	"fmt"
	"io"
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
	desired, err := provision.ResolveDesired(env.cfg, env.contextName)
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

	installedMCP := map[string]provision.MCPServer{}
	if env.prov.SupportsMCP() {
		handler := env.prov.MCPHandler(env.provCtx)
		if handler != nil {
			got, _, err := handler.Read(env.prov.MCPConfigPath(env.provCtx))
			if err != nil {
				return fmt.Errorf("reading MCP config: %w", err)
			}
			installedMCP = got
		}
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
	sort.Slice(unmanagedPlugins, func(i, j int) bool { return unmanagedPlugins[i].Key < unmanagedPlugins[j].Key })
	sort.Strings(unmanagedMCP)
	sort.Slice(unmanagedMarketplaces, func(i, j int) bool { return unmanagedMarketplaces[i].Key < unmanagedMarketplaces[j].Key })

	if len(unmanagedPlugins) == 0 && len(unmanagedMCP) == 0 && len(unmanagedMarketplaces) == 0 {
		fmt.Fprintln(out, "No unmanaged plugins, MCP servers, or marketplaces to adopt.")
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

	if len(adoptedPlugins) == 0 && len(adoptedMCP) == 0 && len(adoptedMarkets) == 0 {
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
		if !containsString(ctx.MCPServers, k) {
			ctx.MCPServers = append(ctx.MCPServers, k)
		}
	}
	env.cfg.Contexts[env.contextName] = ctx

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
	if err := provision.SaveState(env.statePath, env.state); err != nil {
		return fmt.Errorf("saving state: %w", err)
	}

	fmt.Fprintf(out, "Adopted: %d plugin(s), %d mcp server(s), %d marketplace(s).\n", len(adoptedPlugins), len(adoptedMCP), len(adoptedMarkets))
	return nil
}

func promptAdopt(out io.Writer, reader *bufio.Reader, label string) bool {
	fmt.Fprintf(out, "Adopt %s? [a]dopt / [s]kip: ", label)
	ans, _ := reader.ReadString('\n')
	ans = strings.TrimSpace(strings.ToLower(ans))
	return ans == "a" || ans == "adopt"
}

func containsString(xs []string, target string) bool {
	for _, x := range xs {
		if x == target {
			return true
		}
	}
	return false
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
	for _, p := range plugins {
		if p == plugin {
			return existing
		}
	}
	plugins = append(plugins, plugin)
	sort.Strings(plugins)
	return config.PluginEntryMarketplace(plugins)
}
