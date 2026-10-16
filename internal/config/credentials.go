package config

import (
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/jskswamy/aide/internal/envscrub"
	"github.com/jskswamy/aide/internal/harness"
)

// CredentialBackendAge is the only credential backend: an entry in an
// age/sops secret store (the same stores secret: names).
const CredentialBackendAge = "age"

// CredentialBinding is a profile-local named provider credential: the
// key Key in secret store Store, injected as Env.
type CredentialBinding struct {
	Backend string `yaml:"backend"`
	// Store names a secret store the way secret: does (see
	// ResolveSecretPath).
	Store string `yaml:"store"`
	Key   string `yaml:"key"`
	// Env overrides the built-in env names; required for custom names.
	Env []string `yaml:"env,omitempty"`
	// Harnesses restricts the binding to these harnesses; required for
	// custom names. Empty on a built-in means every harness.
	Harnesses []string `yaml:"harnesses,omitempty"`
}

// builtinCredentialEnv maps built-in binding names to the env vars
// their harnesses conventionally read.
var builtinCredentialEnv = map[string][]string{
	"anthropic":  {"ANTHROPIC_API_KEY"},
	"codex":      {"CODEX_API_KEY"},
	"gemini":     {"GEMINI_API_KEY"},
	"openai":     {"OPENAI_API_KEY"},
	"openrouter": {"OPENROUTER_API_KEY"},
}

// IsBuiltinCredential reports whether name has built-in env names.
func IsBuiltinCredential(name string) bool {
	_, ok := builtinCredentialEnv[name]
	return ok
}

// EnvNames returns the env vars binding name injects: Env when set,
// else the built-in names.
func (b CredentialBinding) EnvNames(name string) []string {
	if len(b.Env) > 0 {
		return slices.Clone(b.Env)
	}
	return slices.Clone(builtinCredentialEnv[name])
}

// AllowsHarness reports whether the binding may be injected into agent
// (a harness name or a path to its binary; resolve agents: aliases with
// Config.HarnessOf first).
func (b CredentialBinding) AllowsHarness(agent string) bool {
	if len(b.Harnesses) == 0 {
		return true
	}
	h := canonicalHarness(filepath.Base(agent))
	for _, allowed := range b.Harnesses {
		if canonicalHarness(allowed) == h {
			return true
		}
	}
	return false
}

// HarnessOf returns the harness agent runs: for an agents: entry, the
// harness its binary names (so an alias such as my-claude with binary
// claude is claude), otherwise the harness agent itself names. Names
// aide does not know are returned as given.
func (c *Config) HarnessOf(agent string) string {
	if def, ok := c.Agents[agent]; ok && def.Binary != "" {
		if d, ok := harness.Lookup(filepath.Base(def.Binary)); ok {
			return d.Name
		}
	}
	return canonicalHarness(filepath.Base(agent))
}

func canonicalHarness(name string) string {
	if d, ok := harness.Lookup(name); ok {
		return d.Name
	}
	return name
}

// ErrInvalidCredentials reports a malformed credentials: or
// default_credentials: entry.
var ErrInvalidCredentials = errors.New("invalid credentials")

var envNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// validateCredentials checks every profile's credential bindings and
// default selection.
func (c *Config) validateCredentials() error {
	section := "profiles"
	if !c.ProfileSchema {
		section = "contexts"
	}
	for _, name := range sortedKeys(c.Contexts) {
		ctx := c.Contexts[name]
		at := fmt.Sprintf("%s.%s", section, name)
		for _, cred := range slices.Sorted(maps.Keys(ctx.Credentials)) {
			if err := validateBinding(cred, ctx.Credentials[cred]); err != nil {
				return fmt.Errorf("%w: %s: credentials.%s: %s", ErrInvalidCredentials, at, cred, err)
			}
		}
		for _, sel := range ctx.DefaultCredentials {
			if _, ok := ctx.Credentials[sel]; !ok {
				return fmt.Errorf("%w: %s: default_credentials names %q, which is not under credentials: (defined: %s)",
					ErrInvalidCredentials, at, sel, definedBindings(ctx))
			}
		}
	}
	return nil
}

func validateBinding(name string, b CredentialBinding) error {
	if b.Backend != CredentialBackendAge {
		return fmt.Errorf("backend: must be %q (got %q)", CredentialBackendAge, b.Backend)
	}
	if b.Store == "" {
		return errors.New("store: is required (the secret store holding the key, as named by secret:)")
	}
	if b.Key == "" {
		return errors.New("key: is required (the key inside the secret store)")
	}
	if !IsBuiltinCredential(name) {
		builtins := strings.Join(slices.Sorted(maps.Keys(builtinCredentialEnv)), ", ")
		if len(b.Env) == 0 {
			return fmt.Errorf("env: is required for a custom credential (built-in names: %s)", builtins)
		}
		if len(b.Harnesses) == 0 {
			return fmt.Errorf("harnesses: is required for a custom credential (built-in names: %s)", builtins)
		}
	}
	for _, e := range b.Env {
		if !envNameRE.MatchString(e) {
			return fmt.Errorf("env: %q is not a valid variable name", e)
		}
		if reason := reservedEnvReason(e); reason != "" {
			return fmt.Errorf("env: %s is %s and cannot be set by a credential", e, reason)
		}
	}
	for _, h := range b.Harnesses {
		if _, ok := harness.Lookup(h); !ok {
			return fmt.Errorf("harnesses: %q is not a known harness", h)
		}
	}
	return nil
}

// reservedEssentialEnv are process essentials a binding must not
// replace.
var reservedEssentialEnv = []string{"PATH", "HOME", "USER", "SHELL", "TERM", "LANG", "TMPDIR"}

// reservedEnvReason says why a binding may not set e, or "".
func reservedEnvReason(e string) string {
	switch {
	case envscrub.IsSecretMaterialName(e):
		return "aide's own secret material"
	case strings.HasPrefix(e, "AIDE_"):
		return "reserved for aide"
	case slices.Contains(reservedEssentialEnv, e):
		return "a process essential"
	}
	for _, d := range harness.All() {
		if slices.Contains(d.ConfigEnvKeys, e) {
			return "a harness config-dir variable (set by the profile's config dir)"
		}
	}
	return ""
}

func definedBindings(ctx Context) string {
	if len(ctx.Credentials) == 0 {
		return "none"
	}
	return strings.Join(slices.Sorted(maps.Keys(ctx.Credentials)), ", ")
}

// CredentialScrubNames returns, sorted, the names to scrub from an
// inherited environment on top of the built-in registry: scrub_env:
// plus every env name any profile's credential binding injects, so a
// custom credential exported in the shell cannot reach an agent
// unselected.
func (c *Config) CredentialScrubNames() []string {
	seen := map[string]bool{}
	for _, n := range c.ScrubEnv {
		seen[n] = true
	}
	for _, ctx := range c.Contexts {
		for name, b := range ctx.Credentials {
			for _, e := range b.EnvNames(name) {
				seen[e] = true
			}
		}
	}
	return slices.Sorted(maps.Keys(seen))
}

// ErrLegacyCredentialEnv reports a provider credential in a profile's
// env: that cannot be translated into a credential binding.
var ErrLegacyCredentialEnv = errors.New("provider credential in env")

// LegacyCredential is a provider credential env: entry translated in
// memory into an implicit binding.
type LegacyCredential struct {
	Name    string // implicit binding name
	EnvVar  string
	Binding CredentialBinding
	Agent   string // harness, for the suggested harnesses: of a custom name
}

// Hint is a migration note naming the env var and the credentials:
// block to replace it with. It never contains the value.
func (l LegacyCredential) Hint(profileLabel string) string {
	return fmt.Sprintf("%s: env: %s is a provider credential; aide injects it as credential binding %q for now. "+
		"Replace the env: entry with:\n%s",
		profileLabel, l.EnvVar, l.Name, suggestedBinding(l.Name, l.EnvVar, l.Binding.Store, l.Binding.Key, l.Agent))
}

// legacySecretRef matches a value that is exactly one secret reference.
var legacySecretRef = regexp.MustCompile(`^\s*\{\{\s*(?:\.secrets\.(\w+)|index\s+\.secrets\s+"(\w+)")\s*\}\}\s*$`)

// TranslateLegacyCredentials moves ctx's provider credential env:
// entries into implicit credential bindings that are selected by
// default. An entry translates only when its value is a single
// {{ .secrets.<key> }} reference and store (the profile's own secret:
// store) is set; anything else fails with ErrLegacyCredentialEnv and
// migration guidance. ctx is not modified and nothing is written.
func TranslateLegacyCredentials(ctx Context, store string) (Context, []LegacyCredential, error) {
	var legacy []LegacyCredential
	for _, k := range slices.Sorted(maps.Keys(ctx.Env)) {
		if !envscrub.IsProviderSecretName(k) {
			continue
		}
		name := legacyBindingName(k)
		if owner := bindingFor(ctx, k); owner != "" {
			return ctx, nil, fmt.Errorf("%w: %s duplicates credentials.%s, which already injects it; remove %s from the env: map",
				ErrLegacyCredentialEnv, k, owner, k)
		}
		if _, taken := ctx.Credentials[name]; taken {
			return ctx, nil, fmt.Errorf("%w: %s cannot become credential binding %q, which credentials: already defines; remove %s from the env: map",
				ErrLegacyCredentialEnv, k, name, k)
		}
		m := legacySecretRef.FindStringSubmatch(ctx.Env[k])
		if m == nil || store == "" {
			reason := "its value is not a single {{ .secrets.<key> }} reference"
			if m != nil {
				reason = "the profile has no secret: store"
			}
			return ctx, nil, fmt.Errorf("%w: %s holds a provider credential and aide no longer passes those through env: "+
				"(%s). Put the key in an age secret store (aide secrets edit <store>) and replace the env: entry with:\n%s",
				ErrLegacyCredentialEnv, k, reason, suggestedBinding(name, k, "<store>", "<key>", ctx.Agent))
		}
		key := m[1] + m[2]
		legacy = append(legacy, LegacyCredential{
			Name:    name,
			EnvVar:  k,
			Binding: CredentialBinding{Backend: CredentialBackendAge, Store: store, Key: key, Env: []string{k}},
			Agent:   ctx.Agent,
		})
	}
	if len(legacy) == 0 {
		return ctx, nil, nil
	}
	ctx.Env = maps.Clone(ctx.Env)
	ctx.Credentials = maps.Clone(ctx.Credentials)
	if ctx.Credentials == nil {
		ctx.Credentials = map[string]CredentialBinding{}
	}
	ctx.DefaultCredentials = slices.Clone(ctx.DefaultCredentials)
	for _, l := range legacy {
		delete(ctx.Env, l.EnvVar)
		ctx.Credentials[l.Name] = l.Binding
		ctx.DefaultCredentials = append(ctx.DefaultCredentials, l.Name)
	}
	return ctx, legacy, nil
}

// bindingFor returns the name of ctx's binding that injects envVar.
func bindingFor(ctx Context, envVar string) string {
	for _, name := range slices.Sorted(maps.Keys(ctx.Credentials)) {
		if slices.Contains(ctx.Credentials[name].EnvNames(name), envVar) {
			return name
		}
	}
	return ""
}

// legacyBindingName is the built-in name injecting exactly envVar, or
// envVar lowercased.
func legacyBindingName(envVar string) string {
	for _, name := range slices.Sorted(maps.Keys(builtinCredentialEnv)) {
		if slices.Equal(builtinCredentialEnv[name], []string{envVar}) {
			return name
		}
	}
	return strings.ToLower(envVar)
}

// suggestedBinding renders the credentials: block replacing a legacy
// env: entry.
func suggestedBinding(name, envVar, store, key, agent string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "    credentials:\n      %s:\n        backend: age\n        store: %s\n        key: %s\n", name, store, key)
	if !IsBuiltinCredential(name) {
		fmt.Fprintf(&b, "        env: [%s]\n        harnesses: [%s]\n", envVar, canonicalHarness(filepath.Base(agent)))
	}
	fmt.Fprintf(&b, "    default_credentials: [%s]", name)
	return b.String()
}
