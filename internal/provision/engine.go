package provision

import (
	"fmt"
	"strings"
)

// ApplyOptions tweaks Apply's behavior.
type ApplyOptions struct {
	// MCPHandler overrides the driver's default handler (mainly for
	// tests). When nil, prov.MCPHandler(plan.Context) is used.
	MCPHandler MCPHandler
}

// ApplyResult summarises what an Apply run did.
type ApplyResult struct {
	Performed  int    // successful mutating ops (excludes OpIgnore)
	Skipped    int    // OpIgnore ops
	Failed     string // empty if success; otherwise the failing op description
	RolledBack int    // inverse ops run during rollback
}

// Apply walks the plan and executes each op against prov. On any
// failure the engine rolls back via the journal and returns the error.
// On success it returns the result; the caller persists state.
func Apply(prov Provisioner, plan Plan, opts ApplyOptions) (ApplyResult, error) {
	var handler MCPHandler
	if opts.MCPHandler != nil {
		handler = opts.MCPHandler
	} else if prov.SupportsMCP() {
		handler = prov.MCPHandler(plan.Context)
	}

	var res ApplyResult
	j := &Journal{}

	// canonical caches the driver's repo → marketplace-name mapping.
	// Lazily populated on first plugin install (after any preceding
	// marketplace adds). Invalidated whenever a marketplace is added
	// during this apply run so subsequent plugin installs pick up the
	// new canonical name. The map keys are the desired-side repo keys
	// (matching what desired.Marketplaces[].Key holds); values are the
	// agent's canonical marketplace name (e.g. "beads-marketplace").
	var canonical map[string]string
	resolvePluginRef := func(p Plugin) Plugin {
		if canonical == nil {
			mks, _ := prov.InstalledMarketplaces(plan.Context)
			canonical = map[string]string{}
			for _, m := range mks {
				if m.Name != "" {
					canonical[m.Key] = m.Name
				}
			}
		}
		at := strings.IndexByte(p.Name, '@')
		if at <= 0 || at == len(p.Name)-1 {
			return p
		}
		repo := p.Name[at+1:]
		if cn, ok := canonical[repo]; ok {
			p.Name = p.Name[:at+1] + cn
		}
		return p
	}

	for _, op := range plan.Ops {
		switch op.OpKind {
		case OpIgnore:
			res.Skipped++
			continue
		case OpAdopt:
			// Adoption mutates config.yaml — handled outside the engine.
			res.Skipped++
			continue
		case OpInstall, OpUpdate, OpUninstall:
			// dispatched to the per-Kind switch below
		}

		switch op.Kind {
		case KindPlugin:
			if !prov.SupportsPlugins() {
				if err := j.Rollback(); err != nil {
					return res, fmt.Errorf("capability mismatch: agent %q does not support plugins; rollback: %w", prov.Name(), err)
				}
				return res, fmt.Errorf("capability mismatch: agent %q does not support plugins (declared plugin: %q)", prov.Name(), op.Name)
			}
			switch op.OpKind {
			case OpInstall, OpUpdate:
				resolved := resolvePluginRef(*op.Plugin)
				if err := prov.InstallPlugin(plan.Context, resolved); err != nil {
					_ = j.Rollback()
					return res, fmt.Errorf("install plugin %q: %w", op.Name, err)
				}
				name := op.Name
				j.Record(func() error { return prov.UninstallPlugin(plan.Context, name) })
				res.Performed++
			case OpUninstall:
				if err := prov.UninstallPlugin(plan.Context, op.Name); err != nil {
					_ = j.Rollback()
					return res, fmt.Errorf("uninstall plugin %q: %w", op.Name, err)
				}
				res.Performed++
			case OpAdopt, OpIgnore:
				// handled by the outer switch; unreachable here
			}

		case KindMarketplace:
			// Marketplace adds yield a canonical name the agent uses
			// (e.g. claude names "steveyegge/beads" as "beads-
			// marketplace"). Invalidate the canonical cache so the
			// next plugin install refreshes it from the driver.
			switch op.OpKind {
			case OpInstall:
				if err := prov.AddMarketplace(plan.Context, *op.Marketplace); err != nil {
					_ = j.Rollback()
					return res, fmt.Errorf("add marketplace %q: %w", op.Name, err)
				}
				canonical = nil
				mName := op.Name
				j.Record(func() error { return prov.RemoveMarketplace(plan.Context, mName) })
				res.Performed++
			case OpUninstall:
				if err := prov.RemoveMarketplace(plan.Context, op.Name); err != nil {
					_ = j.Rollback()
					return res, fmt.Errorf("remove marketplace %q: %w", op.Name, err)
				}
				canonical = nil
				res.Performed++
			case OpUpdate, OpAdopt, OpIgnore:
				// marketplaces don't support in-place update; adopt/ignore handled above
			}

		case KindMCP:
			if !prov.SupportsMCP() {
				_ = j.Rollback()
				return res, fmt.Errorf("capability mismatch: agent %q does not support MCP (declared server: %q)", prov.Name(), op.Name)
			}
			if handler == nil {
				_ = j.Rollback()
				return res, fmt.Errorf("provision: driver %q returned nil MCPHandler", prov.Name())
			}
			path := prov.MCPConfigPath(plan.Context)
			prev, _, err := handler.Read(path)
			if err != nil {
				_ = j.Rollback()
				return res, fmt.Errorf("read MCP file: %w", err)
			}
			snapshot := copyMCPMap(prev)
			j.Record(func() error { return handler.Write(path, snapshot) })

			switch op.OpKind {
			case OpInstall, OpUpdate:
				prev[op.Name] = *op.MCP
			case OpUninstall:
				delete(prev, op.Name)
			case OpAdopt, OpIgnore:
				// handled by the outer switch; unreachable here
			}
			if err := handler.Write(path, prev); err != nil {
				_ = j.Rollback()
				return res, fmt.Errorf("%s MCP %q: %w", op.OpKind, op.Name, err)
			}
			res.Performed++
		}
	}

	return res, nil
}

func copyMCPMap(in map[string]MCPServer) map[string]MCPServer {
	out := make(map[string]MCPServer, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
