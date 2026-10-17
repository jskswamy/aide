package launcher

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/jskswamy/aide/internal/config"
	aidectx "github.com/jskswamy/aide/internal/context"
	"github.com/jskswamy/aide/internal/envscrub"
	"github.com/jskswamy/aide/internal/secrets"
	"github.com/jskswamy/aide/internal/sliceutil"
	"github.com/jskswamy/aide/internal/ui"
)

// AuthMode is how a launch authenticates the harness.
type AuthMode string

const (
	// AuthAPI injects the selected credential bindings. It is the
	// default.
	AuthAPI AuthMode = "api"
	// AuthLogin uses the harness's native login state and injects no
	// provider credentials; a credential selection is ignored.
	AuthLogin AuthMode = "login"
)

// Credential resolution errors. Callers branch with errors.Is.
var (
	ErrCredentialNotFound     = errors.New("credential binding not found")
	ErrCredentialIncompatible = errors.New("credential binding not usable with harness")
	ErrCredentialKeyMissing   = errors.New("credential key missing from secret store")
	ErrEndpointOutsideRoute   = errors.New("provider endpoint outside the credential route")
)

// CredentialPlan is what a launch injects: binding and env var names,
// never values, so it is safe to display.
type CredentialPlan struct {
	Auth     AuthMode
	Bindings []PlannedCredential
	// Ignored is the selection a login launch did not inject.
	Ignored []string
	// Skipped is the saved or default selection that has no route for
	// the launched harness.
	Skipped []string
}

// PlannedCredential is one selected binding's route for the launched
// harness. Store is empty for a keyless route.
type PlannedCredential struct {
	Name         string
	Env          []string
	Set          map[string]string
	Store        string
	Key          string
	Capabilities []string
}

// PlanCredentials checks selection (binding names, from whatever
// source chose them) against the profile ctx and the final harness
// (Config.HarnessOf the launching agent), without decrypting anything. Bindings resolve only from
// ctx: a name another profile defines is not found. An empty auth means
// AuthAPI.
func PlanCredentials(profile string, ctx config.Context, harnessName string, selection []string, auth AuthMode) (CredentialPlan, error) {
	switch auth {
	case "", AuthAPI:
		auth = AuthAPI
	case AuthLogin:
		return CredentialPlan{Auth: AuthLogin, Ignored: sliceutil.Dedup(selection)}, nil
	default:
		return CredentialPlan{}, fmt.Errorf("unknown auth mode %q (use %s or %s)", auth, AuthAPI, AuthLogin)
	}
	plan := CredentialPlan{Auth: auth}
	injectedBy := map[string]string{}
	for _, name := range sliceutil.Dedup(selection) {
		b, ok := ctx.Credentials[name]
		if !ok {
			defined := "none"
			if len(ctx.Credentials) > 0 {
				defined = strings.Join(slices.Sorted(maps.Keys(ctx.Credentials)), ", ")
			}
			return CredentialPlan{}, fmt.Errorf("%w: profile %q has no credential %q (defined: %s); "+
				"credentials resolve only from the selected profile, so define it in that profile's credentials map",
				ErrCredentialNotFound, profile, name, defined)
		}
		route, ok := b.Route(name, harnessName)
		if !ok {
			return CredentialPlan{}, fmt.Errorf("%w: profile %q credential %q is for harnesses %s, not %s; "+
				"select another credential or launch a listed harness",
				ErrCredentialIncompatible, profile, name, strings.Join(servedHarnesses(b), ", "), harnessName)
		}
		for _, e := range append(slices.Clone(route.Env), slices.Sorted(maps.Keys(route.Set))...) {
			if other, dup := injectedBy[e]; dup {
				return CredentialPlan{}, fmt.Errorf("profile %q credentials %q and %q both set %s; select only one",
					profile, other, name, e)
			}
			injectedBy[e] = name
		}
		plan.Bindings = append(plan.Bindings, PlannedCredential{
			Name: name, Env: route.Env, Set: route.Set, Store: b.Store, Key: b.Key, Capabilities: slices.Clone(b.Capabilities),
		})
	}
	return plan, nil
}

// servedHarnesses names the harnesses b has a route for.
func servedHarnesses(b config.CredentialBinding) []string {
	if len(b.For) > 0 {
		return slices.Sorted(maps.Keys(b.For))
	}
	return b.Harnesses
}

// StoreLoader returns the decrypted key/value map of the secret store
// at path.
type StoreLoader func(path string) (map[string]string, error)

// ResolveCredentialEnv decrypts the stores of plan's bindings, and only
// those, one at a time (decryption mutates the process env), and
// returns the env vars to inject. A missing store or key fails without
// returning any value.
func ResolveCredentialEnv(plan CredentialPlan, load StoreLoader) (map[string]string, error) {
	env := map[string]string{}
	stores := map[string]map[string]string{}
	for _, b := range plan.Bindings {
		maps.Copy(env, b.Set)
		if b.Store == "" {
			continue
		}
		data, ok := stores[b.Store]
		if !ok {
			var err error
			data, err = load(config.ResolveSecretPath(b.Store))
			if err != nil {
				return nil, fmt.Errorf("credential %q: secret store %q: %w", b.Name, b.Store, err)
			}
			stores[b.Store] = data
		}
		v, ok := data[b.Key]
		if !ok {
			return nil, fmt.Errorf("%w: credential %q: key %q is not in secret store %q (list keys with: aide secrets keys %s)",
				ErrCredentialKeyMissing, b.Name, b.Key, b.Store, b.Store)
		}
		for _, e := range b.Env {
			env[e] = v
		}
	}
	return env, nil
}

// cachingLoader decrypts each store at most once per launch.
func cachingLoader(load StoreLoader) StoreLoader {
	cache := map[string]map[string]string{}
	return func(path string) (map[string]string, error) {
		if m, ok := cache[path]; ok {
			return m, nil
		}
		m, err := load(path)
		if err != nil {
			return nil, err
		}
		cache[path] = m
		return m, nil
	}
}

// loadSecretStore decrypts a secret store; a var so tests can count
// decryptions.
var loadSecretStore StoreLoader = secrets.LoadSecretsMap

// profileCredentials returns rc's profile with legacy provider
// credential env: entries translated into implicit bindings. Legacy
// entries use the profile's own secret: store (.aide.yaml cannot set
// one; the loader rejects it), and .aide.yaml may not set provider credentials,
// selectors or endpoints at all (an endpoint could redirect the
// profile's key to a repository-chosen host), nor a harness config-dir
// variable (that picks the login the profile runs under).
func profileCredentials(cfg *config.Config, rc *aidectx.ResolvedContext) (config.Context, []config.LegacyCredential, error) {
	label := profileLabel(cfg, rc.Name)
	if po := cfg.ProjectOverride; po != nil {
		for _, k := range slices.Sorted(maps.Keys(po.Env)) {
			if envscrub.IsProviderName(k) {
				return config.Context{}, nil, fmt.Errorf("%w: %s sets %s; repository config cannot set provider credentials, "+
					"selectors or endpoints. Remove it there and set it for %s in your aide config "+
					"(credentials: for keys, env: for selectors and endpoints)",
					config.ErrLegacyCredentialEnv, projectFileSettingEnv(cfg, k), k, label)
			}
			if slices.Contains(envscrub.ConfigDirRedirects(), k) {
				return config.Context{}, nil, fmt.Errorf("%s sets %s; repository config cannot choose a harness's config dir, "+
					"which holds its login. Remove it there and give %s its dir in your aide config "+
					"(config_dir_name:, config_dir: or config_dirs:)",
					projectFileSettingEnv(cfg, k), k, label)
			}
		}
	}
	ctx, legacy, err := config.TranslateLegacyCredentials(rc.Context, rc.Context.Secret)
	if err != nil {
		return config.Context{}, nil, fmt.Errorf("%s: %w", label, err)
	}
	return ctx, legacy, nil
}

// projectFileSettingEnv names the .aide.yaml whose env: sets key.
func projectFileSettingEnv(cfg *config.Config, key string) string {
	return cfg.ProjectFileSetting(func(po *config.ProjectOverride) bool {
		_, ok := po.Env[key]
		return ok
	})
}

// printLegacyCredentialHints prints each legacy credential's migration
// hint once per Launcher.
func (l *Launcher) printLegacyCredentialHints(label string, legacy []config.LegacyCredential) {
	for _, c := range legacy {
		key := label + "\x00" + c.EnvVar
		if l.hintedLegacy[key] {
			continue
		}
		if l.hintedLegacy == nil {
			l.hintedLegacy = map[string]bool{}
		}
		l.hintedLegacy[key] = true
		fmt.Fprintf(l.stderr(), "aide: note: %s\n", c.Hint(label))
	}
}

// storePaths returns the secret store paths plan decrypts, in order,
// without duplicates.
func (p CredentialPlan) storePaths() []string {
	var out []string
	for _, b := range p.Bindings {
		if b.Store == "" {
			continue
		}
		if path := config.ResolveSecretPath(b.Store); !slices.Contains(out, path) {
			out = append(out, path)
		}
	}
	return out
}

// PlanProfileCredentials plans rc's credentials without decrypting
// anything, for a launch and for which/status: it applies in's
// credential precedence to sel. It returns the profile with legacy
// env: credentials translated (so their values are not displayed as
// env), the legacy credentials translated, and the plan.
func PlanProfileCredentials(cfg *config.Config, rc *aidectx.ResolvedContext, in SelectionInputs, sel *Selection) (config.Context, []config.LegacyCredential, CredentialPlan, error) {
	ctx, legacy, err := profileCredentials(cfg, rc)
	if err != nil {
		return rc.Context, nil, CredentialPlan{}, err
	}
	sel.SelectCredentials(in, ctx.DefaultCredentials)
	harnessName := cfg.HarnessOf(ctx.Agent)
	plan, err := sel.PlanCredentials(rc.Name, ctx, harnessName, in.Auth)
	if err == nil {
		err = checkEndpointsRouted(profileLabel(cfg, rc.Name), ctx, harnessName, plan, legacy)
	}
	if err != nil {
		return ctx, legacy, plan, err
	}
	for _, b := range plan.Bindings {
		for _, c := range b.Capabilities {
			if !slices.Contains(ctx.Capabilities, c) {
				ctx.Capabilities = append(slices.Clip(ctx.Capabilities), c)
			}
		}
	}
	return ctx, legacy, plan, nil
}

// checkEndpointsRouted stops a launch that would send a selected key to
// a provider endpoint set in the profile's general env: rather than in
// a selected route's set:. Keys translated from legacy env: entries
// keep the endpoint the user paired them with there.
func checkEndpointsRouted(label string, ctx config.Context, harnessName string, plan CredentialPlan, legacy []config.LegacyCredential) error {
	keyed := ""
	routed := map[string]bool{}
	for _, b := range plan.Bindings {
		for k := range b.Set {
			routed[k] = true
		}
		isLegacy := slices.ContainsFunc(legacy, func(l config.LegacyCredential) bool { return l.Name == b.Name })
		if keyed == "" && b.Store != "" && !isLegacy {
			keyed = b.Name
		}
	}
	if keyed == "" {
		return nil
	}
	for _, k := range slices.Sorted(maps.Keys(ctx.Env)) {
		if envscrub.IsProviderEndpointName(k) && !routed[k] {
			return fmt.Errorf("%w: %s: env: %s is a provider endpoint and credential %q would send its key there. "+
				"Move it into the route that uses it: credentials.%s.for.%s.set.%s",
				ErrEndpointOutsideRoute, label, k, keyed, keyed, harnessName, k)
		}
	}
	return nil
}

// SetBannerCredentials records plan on data by name only. A launch
// without bindings in API mode shows nothing.
func SetBannerCredentials(data *ui.BannerData, plan CredentialPlan) {
	if plan.Auth == AuthLogin || len(plan.Bindings) > 0 {
		data.AuthMode = string(plan.Auth)
	}
	data.Credentials = nil
	for _, b := range plan.Bindings {
		names := append(slices.Clone(b.Env), slices.Sorted(maps.Keys(b.Set))...)
		data.Credentials = append(data.Credentials, ui.CredentialInfo{Name: b.Name, Env: names})
	}
}
