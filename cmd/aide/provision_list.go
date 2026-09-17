// Provision-related read-only commands: `aide plugin list` and
// `aide mcp list`. Both show a three-column declared/installed/managed
// view for one context.
package main

import (
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/homepath"
	"github.com/jskswamy/aide/internal/output"
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
			return runPluginList(cmd, contextName)
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
			return runMCPList(cmd, contextName)
		},
	}
	cmd.Flags().StringVar(&contextName, "context", "", "Context name (default: matched by CWD)")
	return cmd
}

// provisionRow is one declared/installed/managed row shared by the
// plugin, marketplace, and MCP list tables — both the human table
// renderers and the JSON encoders consume the same row shape.
type provisionRow struct {
	Name           string `json:"name"`
	Declared       bool   `json:"declared"`
	DeclaredLabel  string `json:"declared_label,omitempty"`
	Installed      bool   `json:"installed"`
	InstalledLabel string `json:"installed_label,omitempty"`
	Managed        bool   `json:"managed"`
	Note           string `json:"note,omitempty"`
}

// pluginListResult is the JSON shape for `aide plugin list`.
type pluginListResult struct {
	Context      string         `json:"context"`
	Agent        string         `json:"agent"`
	Marketplaces []provisionRow `json:"marketplaces,omitempty"`
	Plugins      []provisionRow `json:"plugins"`
}

// mcpListResult is the JSON shape for `aide mcp list`.
type mcpListResult struct {
	Context string         `json:"context"`
	Agent   string         `json:"agent"`
	Servers []provisionRow `json:"servers"`
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

func runPluginList(cmd *cobra.Command, contextName string) error {
	env, err := loadProvisionEnv(contextName)
	if err != nil {
		return err
	}
	desired, err := provision.ResolveDesired(env.cfg, env.contextName, "", "")
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

	result := pluginListResult{
		Context: env.contextName,
		Agent:   env.ctx.Agent,
		Plugins: buildPluginRows(desired.Plugins, installed, managed),
	}
	// Marketplace section first, only for marketplace-class agents.
	// Plugins logically live "under" their marketplaces, so the section
	// order mirrors the install order (marketplaces precede plugins).
	if supportsMarketplaces(env.prov) {
		installedMarkets, _ := env.prov.InstalledMarketplaces(env.provCtx)
		managedMarkets := managedMarketplaceNames(env.state, env.contextName)
		result.Marketplaces = buildMarketplaceRows(desired.Marketplaces, installedMarkets, managedMarkets)
	}

	out := cmd.OutOrStdout()
	format, ferr := output.FromCmd(cmd)
	if ferr != nil {
		return ferr
	}
	return output.Emit(out, format, result, func(w io.Writer) error {
		fmt.Fprintf(w, "Context: %s (agent: %s)\n\n", result.Context, result.Agent)
		if result.Marketplaces != nil {
			fmt.Fprintln(w, "MARKETPLACES")
			renderMarketplaceTable(w, result.Marketplaces)
			fmt.Fprintln(w)
			fmt.Fprintln(w, "PLUGINS")
		}
		renderPluginTable(w, result.Plugins)
		return nil
	})
}

// supportsMarketplaces reports whether the provisioner advertises
// ShapeMarketplace in SupportedSourceShapes.
func supportsMarketplaces(p provision.Provisioner) bool {
	return slices.Contains(p.SupportedSourceShapes(), provision.ShapeMarketplace)
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

// buildMarketplaceRows computes the declared/installed/managed row set
// for the marketplaces table. Pure: no I/O.
func buildMarketplaceRows(declared map[string]provision.Marketplace, installed []provision.Marketplace, managed map[string]bool) []provisionRow {
	installedSet := map[string]provision.Marketplace{}
	for _, m := range installed {
		installedSet[m.Key] = m
	}
	names := unionNames(keysOfMarketplaces(declared), keysOfInstalledMarketplaces(installedSet), keysOfBool(managed))

	rows := make([]provisionRow, 0, len(names))
	for _, n := range names {
		row := provisionRow{Name: n, Managed: managed[n]}
		if _, ok := declared[n]; ok {
			row.Declared = true
		}
		if m, ok := installedSet[n]; ok {
			row.Installed = true
			row.InstalledLabel = m.Name
		}
		row.Note = marketplaceNote(declared, installedSet, managed, n)
		if row.InstalledLabel != "" && row.Note == "" {
			row.Note = "(" + row.InstalledLabel + ")"
		}
		rows = append(rows, row)
	}
	return rows
}

// keysOfMarketplaces / keysOfInstalledMarketplaces mirror keysOfPlugins
// et al: plain key extraction for unionNames.
func keysOfMarketplaces(m map[string]provision.Marketplace) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func keysOfInstalledMarketplaces(m map[string]provision.Marketplace) []string {
	return keysOfMarketplaces(m)
}

// renderMarketplaceTable mirrors renderPluginTable shape:
// NAME / DECLARED / INSTALLED / MANAGED / NOTE.
func renderMarketplaceTable(out io.Writer, rows []provisionRow) {
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "  NAME\tDECLARED\tINSTALLED\tMANAGED\tNOTE")
	for _, r := range rows {
		decl, inst, mgd := "—", "—", "—"
		if r.Declared {
			decl = "✓"
		}
		if r.Installed {
			inst = "✓"
		}
		if r.Managed {
			mgd = "✓"
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n", r.Name, decl, inst, mgd, r.Note)
	}
	if len(rows) == 0 {
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

func runMCPList(cmd *cobra.Command, contextName string) error {
	env, err := loadProvisionEnv(contextName)
	if err != nil {
		return err
	}
	desired, err := provision.ResolveDesired(env.cfg, env.contextName, "", "")
	if err != nil {
		return err
	}

	managedItems := map[string]provision.ManagedItem{}
	if cs, ok := env.state.Contexts[env.contextName]; ok && cs != nil {
		managedItems = cs.MCPServers
	}
	installed := map[string]provision.MCPServer{}
	if env.prov.SupportsMCP() {
		names := provision.MCPQueryNames(desired.MCPServers, managedItems)
		got, err := provision.ReadInstalledMCP(env.prov, env.provCtx, names)
		if err == nil {
			installed = got
		}
	}
	managed := managedMCPNames(env.state, env.contextName)

	result := mcpListResult{
		Context: env.contextName,
		Agent:   env.ctx.Agent,
		Servers: buildMCPRows(desired.MCPServers, installed, managed),
	}

	out := cmd.OutOrStdout()
	format, ferr := output.FromCmd(cmd)
	if ferr != nil {
		return ferr
	}
	return output.Emit(out, format, result, func(w io.Writer) error {
		fmt.Fprintf(w, "Context: %s (agent: %s)\n\n", result.Context, result.Agent)
		renderMCPTable(w, result.Servers)
		return nil
	})
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

// buildPluginRows computes the declared/installed/managed row set for
// the plugins table. Pure: no I/O.
func buildPluginRows(declared map[string]provision.Plugin, installed []provision.Plugin, managed map[string]bool) []provisionRow {
	installedSet := map[string]bool{}
	for _, p := range installed {
		installedSet[p.Key] = true
	}
	names := unionNames(keysOfPlugins(declared), pluginKeys(installed), keysOfBool(managed))

	rows := make([]provisionRow, 0, len(names))
	for _, n := range names {
		row := provisionRow{Name: n, Installed: installedSet[n], Managed: managed[n]}
		if p, ok := declared[n]; ok {
			row.Declared = true
			row.DeclaredLabel = fmt.Sprintf("%s %s", p.Source, p.Name)
		}
		row.Note = pluginNote(declared, installedSet, managed, n)
		rows = append(rows, row)
	}
	return rows
}

func renderPluginTable(out io.Writer, rows []provisionRow) {
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "  NAME\tDECLARED\tINSTALLED\tMANAGED\tNOTE")
	for _, r := range rows {
		decl := "—"
		if r.Declared {
			decl = r.DeclaredLabel
		}
		inst, mgd := "—", "—"
		if r.Installed {
			inst = "✓"
		}
		if r.Managed {
			mgd = "✓"
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n", r.Name, decl, inst, mgd, r.Note)
	}
	if len(rows) == 0 {
		fmt.Fprintln(tw, "  (no plugins declared, installed, or managed)")
	}
	_ = tw.Flush()
}

// buildMCPRows computes the declared/installed/managed row set for the
// MCP servers table. Pure: no I/O.
func buildMCPRows(declared map[string]provision.MCPServer, installed map[string]provision.MCPServer, managed map[string]bool) []provisionRow {
	names := unionNames(keysOfMCP(declared), keysOfMCP(installed), keysOfBool(managed))

	rows := make([]provisionRow, 0, len(names))
	for _, n := range names {
		row := provisionRow{Name: n, Managed: managed[n]}
		if m, ok := declared[n]; ok {
			row.Declared = true
			label := m.Command
			if label == "" {
				label = m.URL
			}
			row.DeclaredLabel = label
		}
		if _, ok := installed[n]; ok {
			row.Installed = true
		}
		row.Note = stateNote(row.Declared, row.Installed, managed[n])
		rows = append(rows, row)
	}
	return rows
}

func renderMCPTable(out io.Writer, rows []provisionRow) {
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "  NAME\tDECLARED\tINSTALLED\tMANAGED\tNOTE")
	for _, r := range rows {
		decl := "—"
		if r.Declared {
			decl = r.DeclaredLabel
			if decl == "" {
				decl = "(declared)"
			}
		}
		inst, mgd := "—", "—"
		if r.Installed {
			inst = "✓"
		}
		if r.Managed {
			mgd = "✓"
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n", r.Name, decl, inst, mgd, r.Note)
	}
	if len(rows) == 0 {
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
