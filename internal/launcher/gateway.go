package launcher

import (
	"context"
	"fmt"
	"maps"
	"os"
	"slices"
	"time"

	"github.com/jskswamy/aide/internal/config"
	aidectx "github.com/jskswamy/aide/internal/context"
	"github.com/jskswamy/aide/internal/gateway"
	"github.com/jskswamy/aide/internal/harness"
	"github.com/jskswamy/aide/internal/homepath"
	"github.com/jskswamy/aide/internal/ui"
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

// EnrichBannerHarnessesAndGateways populates HarnessConfigDir,
// AvailableHarnesses, AvailableGateways, and gateway/endpoint metadata
// on data.Credentials for both Launch and `aide which`.
func EnrichBannerHarnessesAndGateways(
	data *ui.BannerData,
	cfg *config.Config,
	rc *aidectx.ResolvedContext,
	agentName string,
	homeDir string,
	lookPath LookPathFunc,
) {
	if data == nil || rc == nil {
		return
	}
	harnessName := agentName
	if cfg != nil {
		harnessName = cfg.HarnessOf(agentName)
	}
	if dir := launchConfigDir(harnessName, rc.Context, homeDir, os.Getenv); dir != "" {
		data.HarnessConfigDir = homepath.Tilde(dir, homeDir)
	}

	// Populate other installed profile-capable harnesses when the
	// launched agent is itself a known harness.
	if _, isHarness := harness.Lookup(harnessName); lookPath != nil && isHarness {
		data.AvailableHarnesses = nil
		for _, d := range harness.All() {
			if !d.Launchable() || d.ProfileEnvKey == "" || d.Name == harnessName {
				continue
			}
			bin := d.Binary
			if cfg != nil {
				if def, ok := cfg.Agents[d.Name]; ok && def.Binary != "" {
					bin = def.Binary
				}
			}
			if _, err := lookPath(bin); err != nil {
				continue
			}
			opt := ui.HarnessOption{Name: d.Name}
			if dir := launchConfigDir(d.Name, rc.Context, homeDir, os.Getenv); dir != "" {
				opt.ConfigDir = homepath.Tilde(dir, homeDir)
			}
			data.AvailableHarnesses = append(data.AvailableHarnesses, opt)
		}
	}

	// Enrich active credentials with endpoint URL and gateway cache/file status.
	selected := map[string]bool{}
	for i := range data.Credentials {
		c := &data.Credentials[i]
		selected[c.Name] = true
		if gw, ok := rc.Context.Gateways[c.Name]; ok {
			c.IsGateway = true
			c.URL = gateway.NormalizeBaseURL(gw.BaseURL)
			if c.Store == "" {
				c.Store = gw.Store
			}
			if c.Key == "" {
				c.Key = gw.Key
			}
			if homeDir != "" {
				cat, _ := gateway.ReadCache(homeDir, rc.Name, c.Name)
				if cat != nil {
					c.ModelCount = len(cat.Models)
					c.ModelAge = gateway.FormatAge(time.Since(cat.FetchedAt))
				}
				st := gateway.InspectHarness(rc.Context, c.Name, gw, cat, harnessName, homeDir)
				if st.FilePath != "" {
					c.TargetFile = homepath.Tilde(st.FilePath, homeDir)
				}
				c.TargetSync = string(st.State)
			}
			continue
		}
		if b, ok := rc.Context.Credentials[c.Name]; ok {
			c.URL = b.EndpointURL(c.Name, harnessName)
		} else {
			c.URL = config.BuiltinCredentialURL(c.Name)
		}
	}

	// Populate AvailableGateways: other credentials/gateways on the profile that serve harnessName.
	data.AvailableGateways = nil
	for _, name := range slices.Sorted(maps.Keys(rc.Context.Credentials)) {
		if selected[name] {
			continue
		}
		b := rc.Context.Credentials[name]
		if _, isGW := rc.Context.Gateways[name]; isGW {
			if !b.AllowsHarness(harnessName) {
				continue
			}
			data.AvailableGateways = append(data.AvailableGateways, ui.GatewayOption{
				Name: name,
				URL:  gateway.NormalizeBaseURL(rc.Context.Gateways[name].BaseURL),
			})
			continue
		}
		if !b.DefaultAllowsHarness(name, harnessName) {
			continue
		}
		data.AvailableGateways = append(data.AvailableGateways, ui.GatewayOption{
			Name: name,
			URL:  b.EndpointURL(name, harnessName),
		})
	}
}
