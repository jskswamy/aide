package launcher

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jskswamy/aide/internal/config"
	aidectx "github.com/jskswamy/aide/internal/context"
	"github.com/jskswamy/aide/internal/envscrub"
	"github.com/jskswamy/aide/internal/harness"
	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/secrets"
)

// detectAmbientProviderKeys inspects environ for non-empty provider secret
// variables that are not already configured in ctx.Env, permitted by
// capabilities, or covered by ctx.Credentials / ctx.Gateways. When
// requireHarnessMatch is true, built-in provider credentials that do not
// target activeHarness by default are skipped.
func detectAmbientProviderKeys(
	environ []string,
	ctx config.Context,
	permitted []string,
	activeHarness string,
	requireHarnessMatch bool,
) []string {
	excluded := make(map[string]bool)
	for k := range ctx.Env {
		excluded[k] = true
	}
	for _, p := range permitted {
		excluded[p] = true
	}
	for name, b := range ctx.Credentials {
		for _, e := range b.EnvNames(name) {
			excluded[e] = true
		}
	}
	for _, gw := range ctx.Gateways {
		excluded[gw.EnvVarName()] = true
		for _, h := range gw.EffectiveHarnesses() {
			excluded[gw.TokenEnv(h)] = true
		}
	}

	var keys []string
	for _, entry := range environ {
		k, v, ok := strings.Cut(entry, "=")
		if !ok || v == "" {
			continue
		}
		if !envscrub.IsProviderSecretName(k) || isExcludedAmbientKey(k, excluded, permitted) {
			continue
		}
		if requireHarnessMatch && activeHarness != "" {
			bindingName := config.LegacyBindingName(k)
			if config.IsBuiltinCredential(bindingName) && !(config.CredentialBinding{}).DefaultAllowsHarness(bindingName, activeHarness) {
				continue
			}
		}
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return slices.Compact(keys)
}

func isExcludedAmbientKey(k string, excluded map[string]bool, permitted []string) bool {
	if excluded[k] {
		return true
	}
	for _, p := range permitted {
		if prefix, ok := strings.CutSuffix(p, "*"); ok && prefix != "" && strings.HasPrefix(k, prefix) {
			return true
		}
	}
	return false
}

func readAmbientLine(in *bufio.Reader) (string, error) {
	line, err := in.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// loadStoreFrom returns a StoreLoader that checks secretsDir first for
// relative store names (or stores resolved against default SecretsDir)
// before falling back to loadSecretStore.
func loadStoreFrom(secretsDir string) StoreLoader {
	return func(path string) (map[string]string, error) {
		if secretsDir != "" {
			base := filepath.Base(path)
			if !strings.HasSuffix(base, ".enc.yaml") {
				base += ".enc.yaml"
			}
			candidate := filepath.Join(secretsDir, base)
			if _, err := os.Stat(candidate); err == nil {
				return loadSecretStore(candidate)
			}
		}
		return loadSecretStore(config.ResolveSecretPath(path))
	}
}

func bindAmbientKeysToProfile(
	cfg *config.Config,
	configDir, secretsDir, profileName, storeName, activeHarness string,
	ambientKeys []string,
	in *bufio.Reader,
	out io.Writer,
) error {
	storePath := filepath.Join(secretsDir, storeName+".enc.yaml")
	var recipient string
	if _, err := os.Stat(storePath); errors.Is(err, os.ErrNotExist) {
		var rerr error
		recipient, rerr = secrets.ResolveRecipient("", in, out, true)
		if rerr != nil {
			return rerr
		}
	} else if err != nil {
		return err
	}

	kv := make(map[string]string, len(ambientKeys))
	for _, envVar := range ambientKeys {
		kv[strings.ToLower(envVar)] = os.Getenv(envVar)
	}
	if err := secrets.NewManager(secretsDir, "").SetKeys(storeName, secretsDir, kv, recipient); err != nil {
		return err
	}

	if cfg.Contexts == nil {
		cfg.Contexts = make(map[string]config.Context)
	}
	ctx := cfg.Contexts[profileName]
	if ctx.Credentials == nil {
		ctx.Credentials = make(map[string]config.CredentialBinding)
	}
	for _, envVar := range ambientKeys {
		bindingName := config.LegacyBindingName(envVar)
		secretKey := strings.ToLower(envVar)
		cb := config.CredentialBinding{
			Backend: config.CredentialBackendAge,
			Store:   storeName,
			Key:     secretKey,
		}
		if !config.IsBuiltinCredential(bindingName) {
			cb.Env = []string{envVar}
			if _, ok := harness.Lookup(activeHarness); ok {
				cb.Harnesses = []string{activeHarness}
			}
		}
		ctx.Credentials[bindingName] = cb
		if !slices.Contains(ctx.DefaultCredentials, bindingName) {
			ctx.DefaultCredentials = append(ctx.DefaultCredentials, bindingName)
		}
	}
	cfg.Contexts[profileName] = ctx
	cfg.LegacyMonolithic = false
	cfg.ProfileSchema = true
	return config.WriteConfigTo(cfg, filepath.Join(configDir, "config.yaml"))
}

func resolveAmbientBoundCredentials(
	stderr io.Writer,
	cfg *config.Config,
	rc *aidectx.ResolvedContext,
	inputs SelectionInputs,
	sel *Selection,
	credPlan *CredentialPlan,
	credEnv map[string]string,
	secretsDir string,
	ambientKeys []string,
) error {
	profileCtx, _, plan, err := PlanProfileCredentials(cfg, rc, inputs, sel)
	if err != nil {
		return err
	}
	rc.Context = profileCtx
	*credPlan = plan

	resolved, err := ResolveCredentialEnv(*credPlan, loadStoreFrom(secretsDir))
	if err != nil {
		fmt.Fprintf(stderr, "aide: note: could not decrypt newly saved store with local key (%v); using ambient %s for this launch\n",
			err, strings.Join(ambientKeys, ", "))
		for _, envVar := range ambientKeys {
			credEnv[envVar] = os.Getenv(envVar)
		}
		// Prevent step 8 template decryption from failing when encrypted to an external key.
		rc.Context.Secret = ""
		return nil
	}
	maps.Copy(credEnv, resolved)
	return nil
}

// handleAmbientProviderKeys detects ambient provider API keys when a profile
// has no active credential binding. In non-interactive mode it prints a scrub
// notice to stderr; in interactive TTY mode it offers a 4-option onboarding menu.
func (l *Launcher) handleAmbientProviderKeys(
	cfg *config.Config,
	rc *aidectx.ResolvedContext,
	cwd, homeDir string,
	inputs SelectionInputs,
	sel *Selection,
	credPlan *CredentialPlan,
	credEnv map[string]string,
	permitted []string,
) (*aidectx.ResolvedContext, bool, error) {
	tty := l.Interactive || (l.StdinIsTTY != nil && l.StdinIsTTY())
	activeHarness := cfg.HarnessOf(rc.Context.Agent)

	if !tty {
		ambientKeys := detectAmbientProviderKeys(os.Environ(), rc.Context, permitted, activeHarness, false)
		if len(ambientKeys) == 0 {
			return rc, false, nil
		}
		fmt.Fprintf(l.stderr(),
			"aide: note: scrubbed ambient %s from the environment (%s has no credential binding; configure credentials: in your profile, run interactively to save, or pass --auth login to silence)\n",
			strings.Join(ambientKeys, ", "), profileLabel(cfg, rc.Name))
		return rc, false, nil
	}

	ambientKeys := detectAmbientProviderKeys(os.Environ(), rc.Context, permitted, activeHarness, true)
	if len(ambientKeys) == 0 {
		return rc, false, nil
	}

	fmt.Fprintf(l.stderr(), "[aide] Ambient provider key(s) detected in shell environment: %s\n", strings.Join(ambientKeys, ", "))
	fmt.Fprintf(l.stderr(), "  Profile %q has no active credential binding, so aide strips ambient keys by default.\n\n", rc.Name)
	fmt.Fprintf(l.stderr(), "  [1] Save to encrypted secret store & bind to profile %q\n", rc.Name)
	fmt.Fprintln(l.stderr(), "  [2] Create a new profile for this folder with this key")
	fmt.Fprintln(l.stderr(), "  [3] Use for this launch only (one-time pass-through)")
	fmt.Fprintln(l.stderr(), "  [4] Strip and continue (default)")
	fmt.Fprint(l.stderr(), "Select [4]: ")

	choice, err := readAmbientLine(l.stdinReader())
	if err != nil {
		return rc, false, err
	}
	if choice == "" {
		choice = "4"
	}

	switch choice {
	case "1":
		storeName := rc.Context.Secret
		if storeName == "" {
			storeName = rc.Name
		}
		secretsDir := filepath.Join(l.configDir(), "secrets")
		if cfg.Contexts == nil {
			cfg.Contexts = make(map[string]config.Context)
		}
		if existing, ok := cfg.Contexts[rc.Name]; !ok || existing.Agent == "" {
			existing.Agent = rc.Context.Agent
			cfg.Contexts[rc.Name] = existing
		}
		if err := bindAmbientKeysToProfile(cfg, l.configDir(), secretsDir, rc.Name, storeName, activeHarness, ambientKeys, l.stdinReader(), l.stderr()); err != nil {
			return rc, false, err
		}
		savedCtx := cfg.Contexts[rc.Name]
		rc.Context.Credentials = savedCtx.Credentials
		rc.Context.DefaultCredentials = savedCtx.DefaultCredentials
		rc.Context.Secret = savedCtx.Secret
		if err := resolveAmbientBoundCredentials(l.stderr(), cfg, rc, inputs, sel, credPlan, credEnv, secretsDir, ambientKeys); err != nil {
			return rc, false, err
		}
		return rc, true, nil

	case "2":
		defaultName := filepath.Base(cwd)
		fmt.Fprintf(l.stderr(), "New profile name [%s]: ", defaultName)
		newName, err := readAmbientLine(l.stdinReader())
		if err != nil {
			return rc, false, err
		}
		if newName == "" {
			newName = defaultName
		}
		if _, exists := cfg.Contexts[newName]; exists {
			return rc, false, fmt.Errorf("profile %q already exists", newName)
		}
		var matchRule config.MatchRule
		if remote := aidectx.CurrentRemote(cwd); remote != "" {
			matchRule = config.MatchRule{Remote: remote}
		} else {
			matchRule = config.MatchRule{Path: cwd}
		}
		if cfg.Agents == nil {
			cfg.Agents = make(map[string]config.AgentDef)
		}
		if rc.Context.Agent != "" {
			if _, ok := cfg.Agents[rc.Context.Agent]; !ok {
				cfg.Agents[rc.Context.Agent] = config.AgentDef{Binary: activeHarness}
			}
		}
		if cfg.Contexts == nil {
			cfg.Contexts = make(map[string]config.Context)
		}
		cfg.Contexts[newName] = config.Context{
			Agent:  rc.Context.Agent,
			Secret: newName,
			Match:  []config.MatchRule{matchRule},
		}
		secretsDir := filepath.Join(l.configDir(), "secrets")
		if err := bindAmbientKeysToProfile(cfg, l.configDir(), secretsDir, newName, newName, activeHarness, ambientKeys, l.stdinReader(), l.stderr()); err != nil {
			return rc, false, err
		}
		newRC, err := aidectx.ResolveSelected(cfg, cwd, aidectx.GitRemotes(cwd), newName)
		if err != nil {
			return rc, false, err
		}
		if sel != nil {
			sel.ProfileSource = newRC.Source
		}
		cfg.ApplyDefaultProfileDir(newRC.Name, &newRC.Context)
		env, err := provision.InjectProfileEnv(cfg.HarnessOf(newRC.Context.Agent), newRC.Context, newRC.Context.Env, homeDir)
		if err != nil {
			return rc, false, fmt.Errorf("profile %q: %w", newRC.Name, err)
		}
		newRC.Context.Env = env
		inputs.Saved.Credentials = nil
		if err := resolveAmbientBoundCredentials(l.stderr(), cfg, newRC, inputs, sel, credPlan, credEnv, secretsDir, ambientKeys); err != nil {
			return rc, false, err
		}
		return newRC, true, nil

	case "3":
		for _, envVar := range ambientKeys {
			credEnv[envVar] = os.Getenv(envVar)
		}
		fmt.Fprintf(l.stderr(), "aide: note: passing ambient %s for this launch only\n", strings.Join(ambientKeys, ", "))
		return rc, true, nil

	case "4":
		return rc, false, nil

	default:
		return rc, false, fmt.Errorf("invalid selection: %q", choice)
	}
}
