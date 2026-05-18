// Provision-related read-only commands: `aide plugin list` and
// `aide mcp list`. Both show a three-column declared/installed/managed
// view for one context.
package main

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/homepath"
	"github.com/jskswamy/aide/internal/provision"
	"github.com/spf13/cobra"
)

// resolveContextEnv materializes the context's env map for use by
// provisioning subprocesses. Values are tilde-expanded so paths like
// `~/.claude-prod` reach the agent as absolute paths. Values still
// containing template syntax (`{{ ... }}`) are skipped — they belong
// to the launch path's template renderer (with secrets) and aren't
// needed for plugin/MCP reconciliation.
func resolveContextEnv(ctx config.Context, homeDir string) map[string]string {
	if len(ctx.Env) == 0 {
		return nil
	}
	out := make(map[string]string, len(ctx.Env))
	for k, v := range ctx.Env {
		if strings.Contains(v, "{{") {
			continue
		}
		out[k] = homepath.Expand(v, homeDir)
	}
	return out
}

// pluginCmd is the `aide plugin` parent.
func pluginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plugin",
		Short: "Manage declared agent plugins",
	}
	cmd.AddCommand(pluginListCmd())
	return cmd
}

// mcpCmd is the `aide mcp` parent.
func mcpCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Manage declared MCP servers",
	}
	cmd.AddCommand(mcpListCmd())
	return cmd
}

func pluginListCmd() *cobra.Command {
	var contextName string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Show declared, installed, and managed plugins for a context",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runPluginList(cmd.OutOrStdout(), contextName)
		},
	}
	cmd.Flags().StringVar(&contextName, "context", "", "Context name (default: matched by CWD)")
	return cmd
}

func mcpListCmd() *cobra.Command {
	var contextName string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Show declared, installed, and managed MCP servers for a context",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMCPList(cmd.OutOrStdout(), contextName)
		},
	}
	cmd.Flags().StringVar(&contextName, "context", "", "Context name (default: matched by CWD)")
	return cmd
}

// provisionEnv bundles the per-command setup shared by sync/adopt/list.
type provisionEnv struct {
	cfg         *config.Config
	contextName string
	ctx         config.Context
	prov        provision.Provisioner
	provCtx     provision.Context
	statePath   string
	state       *provision.ManagedState
	homeDir     string
}

func loadProvisionEnv(contextName string) (*provisionEnv, error) {
	cfg, name, ctx, err := resolveContextForMutation(contextName)
	if err != nil {
		return nil, err
	}
	prov, ok := provision.ProvisionerFor(ctx.Agent)
	if !ok {
		return nil, fmt.Errorf("no provisioner registered for agent %q", ctx.Agent)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolving home directory: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("resolving working directory: %w", err)
	}
	pCtx, err := provision.ResolveContext(name, ctx, home, cwd, resolveContextEnv(ctx, home))
	if err != nil {
		return nil, fmt.Errorf("resolving provision context: %w", err)
	}
	statePath := provision.DefaultStatePath(home)
	st, err := provision.LoadState(statePath)
	if err != nil {
		return nil, fmt.Errorf("loading provision state: %w", err)
	}
	return &provisionEnv{
		cfg:         cfg,
		contextName: name,
		ctx:         ctx,
		prov:        prov,
		provCtx:     pCtx,
		statePath:   statePath,
		state:       st,
		homeDir:     home,
	}, nil
}

func runPluginList(out io.Writer, contextName string) error {
	env, err := loadProvisionEnv(contextName)
	if err != nil {
		return err
	}
	desired, err := provision.ResolveDesired(env.cfg, env.contextName)
	if err != nil {
		return err
	}

	var installed []provision.Plugin
	if env.prov.SupportsPlugins() {
		// Best-effort: some drivers (e.g. Claude) cannot enumerate.
		if list, err := env.prov.InstalledPlugins(env.provCtx); err == nil {
			installed = list
		}
	}
	managed := managedPluginNames(env.state, env.contextName)

	fmt.Fprintf(out, "Context: %s (agent: %s)\n\n", env.contextName, env.ctx.Agent)

	// Marketplace section first, only for marketplace-class agents.
	// Plugins logically live "under" their marketplaces, so the section
	// order mirrors the install order (marketplaces precede plugins).
	if supportsMarketplaces(env.prov) {
		installedMarkets, _ := env.prov.InstalledMarketplaces(env.provCtx)
		managedMarkets := managedMarketplaceNames(env.state, env.contextName)
		fmt.Fprintln(out, "MARKETPLACES")
		renderMarketplaceTable(out, desired.Marketplaces, installedMarkets, managedMarkets)
		fmt.Fprintln(out)
		fmt.Fprintln(out, "PLUGINS")
	}
	renderPluginTable(out, desired.Plugins, installed, managed)
	return nil
}

// supportsMarketplaces reports whether the provisioner advertises
// ShapeMarketplace in SupportedSourceShapes.
func supportsMarketplaces(p provision.Provisioner) bool {
	for _, s := range p.SupportedSourceShapes() {
		if s == provision.ShapeMarketplace {
			return true
		}
	}
	return false
}

// managedMarketplaceNames returns the set of marketplace keys aide
// has marked managed for this context.
func managedMarketplaceNames(st *provision.ManagedState, name string) map[string]bool {
	out := map[string]bool{}
	if st == nil {
		return out
	}
	if cs, ok := st.Contexts[name]; ok && cs != nil {
		for k := range cs.Marketplaces {
			out[k] = true
		}
	}
	return out
}

// renderMarketplaceTable mirrors renderPluginTable shape:
// NAME / DECLARED / INSTALLED / MANAGED / NOTE.
func renderMarketplaceTable(out io.Writer, declared map[string]provision.Marketplace, installed []provision.Marketplace, managed map[string]bool) {
	installedSet := map[string]provision.Marketplace{}
	for _, m := range installed {
		installedSet[m.Key] = m
	}
	declaredKeys := make([]string, 0, len(declared))
	for k := range declared {
		declaredKeys = append(declaredKeys, k)
	}
	installedKeys := make([]string, 0, len(installedSet))
	for k := range installedSet {
		installedKeys = append(installedKeys, k)
	}
	names := unionNames(declaredKeys, installedKeys, keysOfBool(managed))

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "  NAME\tDECLARED\tINSTALLED\tMANAGED\tNOTE")
	for _, n := range names {
		decl := "—"
		if _, ok := declared[n]; ok {
			decl = "✓"
		}
		inst := "—"
		instLabel := ""
		if m, ok := installedSet[n]; ok {
			inst = "✓"
			if m.Name != "" {
				instLabel = "(" + m.Name + ")"
			}
		}
		mgd := "—"
		if managed[n] {
			mgd = "✓"
		}
		note := marketplaceNote(declared, installedSet, managed, n)
		if instLabel != "" && note == "" {
			note = instLabel
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n", n, decl, inst, mgd, note)
	}
	if len(names) == 0 {
		fmt.Fprintln(tw, "  (no marketplaces declared, installed, or managed)")
	}
	_ = tw.Flush()
}

func marketplaceNote(declared map[string]provision.Marketplace, installed map[string]provision.Marketplace, managed map[string]bool, key string) string {
	_, dOK := declared[key]
	_, iOK := installed[key]
	_, mOK := managed[key]
	switch {
	case iOK && !dOK && !mOK:
		return "unmanaged"
	case mOK && !iOK:
		return "stale managed; aide sync will re-add"
	case mOK && !dOK && iOK:
		return "stale managed; aide sync will uninstall"
	}
	return ""
}

func runMCPList(out io.Writer, contextName string) error {
	env, err := loadProvisionEnv(contextName)
	if err != nil {
		return err
	}
	desired, err := provision.ResolveDesired(env.cfg, env.contextName)
	if err != nil {
		return err
	}

	installed := map[string]provision.MCPServer{}
	if env.prov.SupportsMCP() {
		handler := env.prov.MCPHandler(env.provCtx)
		if handler != nil {
			if got, _, err := handler.Read(env.prov.MCPConfigPath(env.provCtx)); err == nil {
				installed = got
			}
		}
	}
	managed := managedMCPNames(env.state, env.contextName)

	fmt.Fprintf(out, "Context: %s (agent: %s)\n\n", env.contextName, env.ctx.Agent)
	renderMCPTable(out, desired.MCPServers, installed, managed)
	return nil
}

func managedPluginNames(st *provision.ManagedState, name string) map[string]bool {
	out := map[string]bool{}
	if st == nil {
		return out
	}
	if cs, ok := st.Contexts[name]; ok && cs != nil {
		for k := range cs.Plugins {
			out[k] = true
		}
	}
	return out
}

func managedMCPNames(st *provision.ManagedState, name string) map[string]bool {
	out := map[string]bool{}
	if st == nil {
		return out
	}
	if cs, ok := st.Contexts[name]; ok && cs != nil {
		for k := range cs.MCPServers {
			out[k] = true
		}
	}
	return out
}

func renderPluginTable(out io.Writer, declared map[string]provision.Plugin, installed []provision.Plugin, managed map[string]bool) {
	installedSet := map[string]bool{}
	for _, p := range installed {
		installedSet[p.Key] = true
	}
	names := unionNames(keysOfPlugins(declared), pluginKeys(installed), keysOfBool(managed))

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "  NAME\tDECLARED\tINSTALLED\tMANAGED\tNOTE")
	for _, n := range names {
		decl := "—"
		if p, ok := declared[n]; ok {
			decl = fmt.Sprintf("%s %s", p.Source, p.Name)
		}
		inst := "—"
		if installedSet[n] {
			inst = "✓"
		}
		mgd := "—"
		if managed[n] {
			mgd = "✓"
		}
		note := pluginNote(declared, installedSet, managed, n)
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n", n, decl, inst, mgd, note)
	}
	if len(names) == 0 {
		fmt.Fprintln(tw, "  (no plugins declared, installed, or managed)")
	}
	_ = tw.Flush()
}

func renderMCPTable(out io.Writer, declared map[string]provision.MCPServer, installed map[string]provision.MCPServer, managed map[string]bool) {
	names := unionNames(keysOfMCP(declared), keysOfMCP(installed), keysOfBool(managed))

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "  NAME\tDECLARED\tINSTALLED\tMANAGED\tNOTE")
	for _, n := range names {
		decl := "—"
		if m, ok := declared[n]; ok {
			decl = m.Command
			if decl == "" {
				decl = m.URL
			}
			if decl == "" {
				decl = "(declared)"
			}
		}
		inst := "—"
		if _, ok := installed[n]; ok {
			inst = "✓"
		}
		mgd := "—"
		if managed[n] {
			mgd = "✓"
		}
		_, hasDecl := declared[n]
		_, hasInst := installed[n]
		note := stateNote(hasDecl, hasInst, managed[n])
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n", n, decl, inst, mgd, note)
	}
	if len(names) == 0 {
		fmt.Fprintln(tw, "  (no MCP servers declared, installed, or managed)")
	}
	_ = tw.Flush()
}

func pluginNote(declared map[string]provision.Plugin, installed, managed map[string]bool, name string) string {
	_, hasDecl := declared[name]
	return stateNote(hasDecl, installed[name], managed[name])
}

func stateNote(declared, installed, managed bool) string {
	switch {
	case !declared && installed && !managed:
		return "unmanaged"
	case !declared && !installed && managed:
		return "stale managed; aide sync will uninstall"
	case !declared && installed && managed:
		return "stale managed"
	default:
		return ""
	}
}

func unionNames(sets ...[]string) []string {
	seen := map[string]bool{}
	for _, s := range sets {
		for _, n := range s {
			seen[n] = true
		}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func keysOfPlugins(m map[string]provision.Plugin) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func keysOfMCP(m map[string]provision.MCPServer) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func keysOfBool(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func pluginKeys(ps []provision.Plugin) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.Key)
	}
	return out
}
