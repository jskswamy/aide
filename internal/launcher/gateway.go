package launcher

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/jskswamy/aide/internal/config"
	aidectx "github.com/jskswamy/aide/internal/context"
	"github.com/jskswamy/aide/internal/gateway"
	"github.com/jskswamy/aide/internal/harness"
	"github.com/jskswamy/aide/internal/homepath"
	"github.com/jskswamy/aide/internal/prefs"
	"github.com/jskswamy/aide/internal/ui"
)

// applyGatewayChoice prepends the startup flags that put harnessName on
// the chosen gateway, printing why when the user's own flags win or the
// gateway has no model to start on. ctx is the profile with its
// gateways in launch form.
func (l *Launcher) applyGatewayChoice(harnessName string, ctx config.Context, choice Choice, args []string) []string {
	if choice.Value == "" || choice.Value == gatewayNone {
		return args
	}
	entry, model := gateway.ProviderID(choice.Value), ctx.Gateways[choice.Value].Model
	if d, ok := launchableHarness(harnessName); ok && model == "" && !choice.Derived && d.ChoiceArgs != nil && d.ChoiceArgs(entry, "") == nil {
		fmt.Fprintf(l.stderr(), "aide: note: gateway %q has no model, so %s starts on its own default; set one with: "+
			"aide gateway edit %s --model <id>, or switch with /model\n", choice.Value, harnessName, choice.Value)
	}
	args, _, notice := ApplyChoice(harnessName, args, entry, model)
	if notice != "" {
		fmt.Fprintf(l.stderr(), "aide: note: %s\n", notice)
	}
	return args
}

// choicePrompter is the ChoicePrompter of a launch from cwd: the
// picker on a terminal (nothing is decrypted), remembering the answer
// where the user says, and ErrNoTerminal off one.
func (l *Launcher) choicePrompter(cwd, homeDir string) ChoicePrompter {
	return func(harnessName, profile string, options []ui.PickerOption) (string, ui.Remember, error) {
		isTTY := isStdinTTY
		if l.StdinIsTTY != nil {
			isTTY = l.StdinIsTTY
		}
		if !l.Interactive || !isTTY() {
			return "", "", ErrNoTerminal
		}
		key := PreferenceKey(profile, aidectx.GitRemotes(cwd))
		choice, scope, err := ui.PickGateway(l.stdinReader(), l.stderr(), harnessName, profile, options, key.Validate() == nil)
		if err != nil {
			return "", "", err
		}
		if err := l.rememberChoice(scope, key, harnessName, choice, homeDir); err != nil {
			fmt.Fprintf(l.stderr(), "aide: warning: could not remember the choice (%v); using it for this launch only\n", err)
			scope = ui.RememberOnce
		}
		if scope == ui.RememberOnce {
			scope = ""
		}
		return choice, scope, nil
	}
}

// rememberChoice saves choice for harnessName in the repository
// preference or in the profile, as scope says. The profile is saved
// from a fresh load, so nothing a .aide.yaml merged in reaches the
// user's config.
func (l *Launcher) rememberChoice(scope ui.Remember, key prefs.Key, harnessName, choice, homeDir string) error {
	switch scope {
	case ui.RememberRepo:
		return prefs.Update(l.prefsPath(homeDir), func(f *prefs.File) error {
			p, _ := f.Get(key)
			p.Gateway = maps.Clone(p.Gateway)
			if p.Gateway == nil {
				p.Gateway = map[string]string{}
			}
			p.Gateway[harnessName] = choice
			f.Set(key, p)
			return nil
		})
	case ui.RememberProfile:
		fresh, err := config.Load(l.configDir(), "")
		if err != nil {
			return err
		}
		ctx, ok := fresh.Contexts[key.Profile]
		if !ok {
			return fmt.Errorf("profile %q is not in %s", key.Profile, l.configDir())
		}
		ctx.GatewayChoice = maps.Clone(ctx.GatewayChoice)
		if ctx.GatewayChoice == nil {
			ctx.GatewayChoice = map[string]string{}
		}
		ctx.GatewayChoice[harnessName] = choice
		fresh.Contexts[key.Profile] = ctx
		return config.WriteConfigTo(fresh, filepath.Join(l.configDir(), "config.yaml"))
	case ui.RememberOnce:
	}
	return nil
}

// ensureGatewaySynced brings the model catalogue of every gateway the
// launch injects into the launched harness's config before exec, in
// parallel. An expired or missing cache is refreshed with the token
// credEnv already holds (no second decryption); a fresh cache is
// written into a missing or stale harness file without any network
// call. Codex's entries for the marked gateways it does not use are
// written without fetching. Nothing here stops a launch: a gateway that
// cannot be reached falls back to its cached catalogue, or launches
// without one, with a warning.
func (l *Launcher) ensureGatewaySynced(rc *aidectx.ResolvedContext, harnessName, homeDir string, choice Choice, credPlan CredentialPlan, credEnv map[string]string) {
	// Only the chosen gateway is the harness's default (its Codex
	// model_provider, Claude's gateway file); pre-bound credentials keep
	// the context from synthesizing another.
	ctx := rc.Context
	ctx.DefaultCredentials = nil
	if _, ok := ctx.Gateways[choice.Value]; ok {
		ctx.DefaultCredentials = []string{choice.Value}
	}
	var injected []string
	logs := make([]bytes.Buffer, len(credPlan.Bindings))
	var wg sync.WaitGroup
	for i, b := range credPlan.Bindings {
		if _, ok := ctx.Gateways[b.Name]; !ok {
			continue
		}
		injected = append(injected, b.Name)
		token := ""
		if len(b.Env) > 0 {
			token = credEnv[b.Env[0]]
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			l.syncGateway(&logs[i], rc, ctx, b.Name, token, harnessName, homeDir)
		}()
	}
	wg.Wait()
	for i := range logs {
		_, _ = l.stderr().Write(logs[i].Bytes())
	}
	if harnessName != "codex" {
		return
	}
	for _, name := range ctx.GatewayCandidates(harnessName) {
		if slices.Contains(injected, name) {
			continue
		}
		gw, cat := ctx.Gateways[name], &gateway.CachedCatalogue{}
		if gateway.InspectHarness(ctx, name, gw, cat, harnessName, homeDir).State == gateway.StateSynced {
			continue
		}
		if _, err := gateway.MaterializeHarness(ctx, name, gw, cat, harnessName, homeDir, true); err != nil {
			fmt.Fprintf(l.stderr(), "aide: warning: could not write gateway %q for %s: %v\n", name, harnessName, err)
		}
	}
}

// syncGateway syncs one gateway, writing its progress to w.
func (l *Launcher) syncGateway(w io.Writer, rc *aidectx.ResolvedContext, ctx config.Context, name, token, harnessName, homeDir string) {
	gw := ctx.Gateways[name]
	ttl := gw.TTL()
	base := gateway.NormalizeBaseURL(gw.BaseURL)
	cat, err := gateway.ReadCache(homeDir, rc.Name, name)
	if err != nil || !gateway.MatchesBaseURL(cat, base) {
		cat = nil // unreadable, or cached for a base_url since changed
	}

	refreshed := false
	if gateway.IsExpired(cat, ttl, time.Now()) {
		timeout := gateway.DefaultFetchTimeout
		if cat != nil {
			timeout = gateway.RefreshFetchTimeout
			fmt.Fprintf(w, "aide: gateway %q cache expired (last synced %s ago, ttl %s); refreshing models from %s/v1/models...\n",
				name, gateway.FormatAge(time.Since(cat.FetchedAt)), gateway.FormatAge(ttl), base)
		} else {
			fmt.Fprintf(w, "aide: gateway %q has no cached models; fetching from %s/v1/models...\n", name, base)
		}
		fctx, cancel := context.WithTimeout(context.Background(), timeout)
		res, err := gateway.Sync(fctx, gateway.SyncOptions{
			ProfileName: rc.Name, Profile: ctx, GatewayName: name, Gateway: gw,
			Token: token, HomeDir: homeDir,
		})
		cancel()
		switch {
		case err == nil:
			cat, refreshed = res.Catalogue, true
		case cat != nil:
			fmt.Fprintf(w, "aide: warning: could not refresh gateway %q (%v); using cached catalogue from %s ago (%d models)\n",
				name, err, gateway.FormatAge(time.Since(cat.FetchedAt)), len(cat.Models))
		default:
			fmt.Fprintf(w, "aide: warning: could not fetch initial models for gateway %q (%v); launching without local model catalogue\n",
				name, err)
			return
		}
	}

	st := gateway.InspectHarness(ctx, name, gw, cat, harnessName, homeDir)
	if st.State == gateway.StateSynced && !refreshed {
		return
	}
	if st.State != gateway.StateSynced {
		if st, err = gateway.MaterializeHarness(ctx, name, gw, cat, harnessName, homeDir, true); err != nil {
			fmt.Fprintf(w, "aide: warning: could not write gateway %q models for %s: %v\n", name, harnessName, err)
			return
		}
	}
	verb := "materialized %d cached models"
	if refreshed {
		verb = "synced %d models"
	}
	fmt.Fprintf(w, "aide: "+verb+" for gateway %q -> %s\n", len(cat.Models), name, homepath.Tilde(st.FilePath, homeDir))
}

// gatewayInfo is gw's endpoint and key location for display, by name
// only: never a value.
func gatewayInfo(name, harnessName string, gw config.GatewayConfig) ui.CredentialInfo {
	kind := "gateway"
	if !gw.RequiresToken() {
		kind = "keyless"
	}
	return ui.CredentialInfo{
		Name: name, IsGateway: true, Kind: kind,
		URL:   gateway.NormalizeBaseURL(cmp.Or(gw.URL, gw.BaseURL)),
		Store: gw.Store, Key: gw.Key,
		KeyEnv: gw.TokenEnv(name, harnessName),
	}
}

// BuildGatewayViews returns one entry per gateway-capable harness of
// rc's profile: its choice, where the choice came from, and the chosen
// gateway's URL and key location. It reads config only.
func BuildGatewayViews(rc *aidectx.ResolvedContext, choices map[string]ResolvedChoice) []ui.CredentialInfo {
	var out []ui.CredentialInfo
	gws := launchGateways(rc.Context)
	for _, d := range harness.All() {
		rch, ok := choices[d.Name]
		if !ok || d.Reach == harness.ReachNone {
			continue
		}
		cands := rc.Context.GatewayCandidates(d.Name)
		v := ui.CredentialInfo{}
		switch gw, isGW := gws[rch.Value]; {
		case isGW:
			v = gatewayInfo(rch.Value, d.Name, gw)
			v.Startup = d.Reach == harness.ReachMulti
			if v.Startup {
				v.Others = len(cands) - 1
			}
		case rch.Value == "" && d.Reach == harness.ReachMulti:
			v.Others = len(cands)
		}
		v.Harness, v.Choice, v.Source, v.Warnings = d.Name, cmp.Or(rch.Value, "unset"), cmp.Or(rch.Source, SourceUnset), rch.Notices
		out = append(out, v)
	}
	return out
}

// EnrichBannerHarnessesAndGateways populates HarnessConfigDir,
// AvailableHarnesses, the gateway views, and gateway/endpoint metadata
// on data.Credentials for Launch, `aide which` and `aide status`.
func EnrichBannerHarnessesAndGateways(
	data *ui.BannerData,
	cfg *config.Config,
	rc *aidectx.ResolvedContext,
	agentName string,
	homeDir string,
	lookPath LookPathFunc,
	choices map[string]ResolvedChoice,
) {
	if data == nil || rc == nil {
		return
	}
	harnessName := agentName
	if cfg != nil {
		harnessName = cfg.HarnessOf(agentName)
	}
	data.Harness = harnessName
	data.GatewayChoices = BuildGatewayViews(rc, choices)
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

	// Enrich active credentials with endpoint URL, the harness's choice
	// and gateway cache/file status.
	gws := launchGateways(rc.Context)
	active := choices[harnessName]
	multi := isMultiHarness(harnessName)
	selected := map[string]bool{}
	for i := range data.Credentials {
		c := &data.Credentials[i]
		selected[c.Name] = true
		if gw, ok := gws[c.Name]; ok {
			info := gatewayInfo(c.Name, harnessName, gw)
			c.IsGateway, c.Kind, c.URL, c.KeyEnv = true, info.Kind, info.URL, info.KeyEnv
			c.Harness, c.Choice, c.Source = harnessName, cmp.Or(active.Value, "unset"), cmp.Or(active.Source, SourceUnset)
			c.Startup = multi && c.Name == active.Value
			if c.Store == "" {
				c.Store = gw.Store
			}
			if c.Key == "" {
				c.Key = gw.Key
			}
			if homeDir != "" {
				cat, _ := gateway.ReadCache(homeDir, rc.Name, c.Name)
				if gateway.MatchesBaseURL(cat, gw.BaseURL) {
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

	// A slot harness's other gateways are what --gateway can switch to;
	// a multi harness has them all in /model already.
	data.AvailableGateways = nil
	if multi {
		return
	}
	for _, name := range rc.Context.GatewayCandidates(harnessName) {
		if !selected[name] {
			info := gatewayInfo(name, harnessName, gws[name])
			data.AvailableGateways = append(data.AvailableGateways, ui.GatewayOption{
				Name: name, URL: info.URL, Store: info.Store, Key: info.Key, KeyEnv: info.KeyEnv})
		}
	}
}

func isMultiHarness(name string) bool {
	d, ok := harness.Lookup(name)
	return ok && d.Reach == harness.ReachMulti
}
