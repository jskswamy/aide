package launcher

import (
	"bytes"
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
