// Package main provides the aide CLI commands.
package main

import (
	"fmt"
	"io"
	"os"

	aidectx "github.com/jskswamy/aide/internal/context"
	"github.com/jskswamy/aide/internal/provision"
)

// resolveAgentGrantContext resolves the target context's agent driver and
// the provision.Context to call it with. It reuses resolveContextForMutation
// — the same helper runScopedMutation's global branch uses — so this
// resolves the identical context the aide-side sandbox mutation just wrote
// to, respecting --context. The provision.Context is then built with
// provision.ResolveContext, the same helper other cmd/aide call sites
// (provision_list.go, statusline.go) use, so env resolution (expansion,
// template-dropping) and profile-env injection stay consistent. ok=false
// with a nil error means "nothing to do" (agent unknown to aide, or it
// doesn't implement DirectoryGranter) — not a failure the caller should
// report.
func resolveAgentGrantContext(global bool, contextName string) (granter provision.DirectoryGranter, agentName string, pctx provision.Context, ok bool, err error) {
	_, ctxName, ctx, err := resolveContextForMutation(contextName)
	if err != nil {
		return nil, "", provision.Context{}, false, err
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, "", provision.Context{}, false, fmt.Errorf("resolving home directory: %w", err)
	}
	prov, found := provision.ProvisionerFor(ctx.Agent)
	if !found {
		return nil, ctx.Agent, provision.Context{}, false, nil
	}
	g, ok := prov.(provision.DirectoryGranter)
	if !ok {
		return nil, ctx.Agent, provision.Context{}, false, nil
	}
	projectRoot := ""
	if !global {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, ctx.Agent, provision.Context{}, false, fmt.Errorf("getting working directory: %w", err)
		}
		projectRoot = aidectx.ProjectRoot(cwd)
	}
	pctx, err = provision.ResolveContext(ctxName, ctx, homeDir, projectRoot, resolveContextEnv(ctx, homeDir))
	if err != nil {
		return nil, ctx.Agent, provision.Context{}, false, fmt.Errorf("context %q: %w", ctxName, err)
	}
	return g, ctx.Agent, pctx, true, nil
}

// grantAgentDirectory best-effort grants path in the target context's
// agent's own permission store, on top of the aide-side sandbox grant
// that has already succeeded by the time this runs. Errors are
// printed as warnings, never returned — the OS-level sandbox grant is
// the security-relevant part and is unaffected by this failing.
func grantAgentDirectory(stdout, stderr io.Writer, global bool, contextName, path string, write bool) {
	granter, agentName, pctx, ok, err := resolveAgentGrantContext(global, contextName)
	if err != nil {
		fmt.Fprintf(stderr, "warning: could not resolve agent for directory grant: %v\n", err)
		return
	}
	if !ok {
		return
	}
	if err := granter.GrantDirectory(pctx, path, write); err != nil {
		fmt.Fprintf(stderr, "warning: could not add %s to %s's settings: %v\n", path, agentName, err)
		return
	}
	fmt.Fprintf(stdout, "Added %s to %s's additionalDirectories\n", path, agentName)
}

// revokeAgentDirectory is the deny-side counterpart to grantAgentDirectory.
func revokeAgentDirectory(stdout, stderr io.Writer, global bool, contextName, path string) {
	granter, agentName, pctx, ok, err := resolveAgentGrantContext(global, contextName)
	if err != nil {
		fmt.Fprintf(stderr, "warning: could not resolve agent for directory revoke: %v\n", err)
		return
	}
	if !ok {
		return
	}
	if err := granter.RevokeDirectory(pctx, path); err != nil {
		fmt.Fprintf(stderr, "warning: could not remove %s from %s's settings: %v\n", path, agentName, err)
		return
	}
	fmt.Fprintf(stdout, "Removed %s from %s's additionalDirectories\n", path, agentName)
}
