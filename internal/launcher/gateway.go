package launcher

import (
	"context"
	"fmt"
	"time"

	aidectx "github.com/jskswamy/aide/internal/context"
	"github.com/jskswamy/aide/internal/gateway"
	"github.com/jskswamy/aide/internal/homepath"
)

// ensureGatewaySynced brings the model catalogue of every gateway the
// launch selected into the launched harness's config before exec. An
// expired or missing cache is refreshed with the token credEnv already
// holds (no second decryption); a fresh cache is written into a
// missing or stale harness file without any network call. Nothing here
// stops a launch: a gateway that cannot be reached falls back to its
// cached catalogue, or launches without one, with a warning.
func (l *Launcher) ensureGatewaySynced(rc *aidectx.ResolvedContext, harnessName, homeDir string, credPlan CredentialPlan, credEnv map[string]string) {
	// The launch's selection decides which gateway is "the" gateway for
	// this harness (Codex's model_provider), so present it as the
	// profile's defaults.
	ctx := rc.Context
	ctx.DefaultCredentials = nil
	for _, b := range credPlan.Bindings {
		ctx.DefaultCredentials = append(ctx.DefaultCredentials, b.Name)
	}
	for _, b := range credPlan.Bindings {
		gw, ok := rc.Context.Gateways[b.Name]
		if !ok || len(b.Env) == 0 {
			continue
		}
		name := b.Name
		ttl := gw.TTL()
		base := gateway.NormalizeBaseURL(gw.BaseURL)
		cat, err := gateway.ReadCache(homeDir, rc.Name, name)
		if err != nil || (cat != nil && cat.BaseURL != base) {
			cat = nil // unreadable, or cached for a base_url since changed
		}

		refreshed := false
		if gateway.IsExpired(cat, ttl, time.Now()) {
			timeout := gateway.DefaultFetchTimeout
			if cat != nil {
				timeout = gateway.RefreshFetchTimeout
				fmt.Fprintf(l.stderr(), "aide: gateway %q cache expired (last synced %s ago, ttl %s); refreshing models from %s/v1/models...\n",
					name, gateway.FormatAge(time.Since(cat.FetchedAt)), gateway.FormatAge(ttl), base)
			} else {
				fmt.Fprintf(l.stderr(), "aide: gateway %q has no cached models; fetching from %s/v1/models...\n", name, base)
			}
			fctx, cancel := context.WithTimeout(context.Background(), timeout)
			res, err := gateway.Sync(fctx, gateway.SyncOptions{
				ProfileName: rc.Name, Profile: ctx, GatewayName: name, Gateway: gw,
				Token: credEnv[b.Env[0]], HomeDir: homeDir,
			})
			cancel()
			switch {
			case err == nil:
				cat, refreshed = res.Catalogue, true
			case cat != nil:
				fmt.Fprintf(l.stderr(), "aide: warning: could not refresh gateway %q (%v); using cached catalogue from %s ago (%d models)\n",
					name, err, gateway.FormatAge(time.Since(cat.FetchedAt)), len(cat.Models))
			default:
				fmt.Fprintf(l.stderr(), "aide: warning: could not fetch initial models for gateway %q (%v); launching without local model catalogue\n",
					name, err)
				continue
			}
		}

		st := gateway.InspectHarness(ctx, name, gw, cat, harnessName, homeDir)
		if st.State == gateway.StateSynced && !refreshed {
			continue
		}
		if st.State != gateway.StateSynced {
			if st, err = gateway.MaterializeHarness(ctx, name, gw, cat, harnessName, homeDir, true); err != nil {
				fmt.Fprintf(l.stderr(), "aide: warning: could not write gateway %q models for %s: %v\n", name, harnessName, err)
				continue
			}
		}
		verb := "materialized %d cached models"
		if refreshed {
			verb = "synced %d models"
		}
		fmt.Fprintf(l.stderr(), "aide: "+verb+" for gateway %q -> %s\n", len(cat.Models), name, homepath.Tilde(st.FilePath, homeDir))
	}
}
