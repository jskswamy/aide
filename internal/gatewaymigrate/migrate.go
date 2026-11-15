// Package gatewaymigrate moves saved state from the legacy credential
// selection to per-harness gateway choices.
package gatewaymigrate

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/fsutil"
	"github.com/jskswamy/aide/internal/prefs"
)

// TranslatePreference returns rec with its version 1 credential
// selection translated into gateway choices, and a notice for each
// credential it could not translate. A selection with no gateway for
// claude or codex becomes "none" for it; a credential that is (or became, through
// renames: harness, then old name, to new name) a gateway serving a
// harness becomes that harness's choice. Anything else is dropped. A
// choice rec already holds wins. It translates in memory only; rec's
// own list is cleared in the result, never in the caller's copy.
func TranslatePreference(rec prefs.Preference, ctx config.Context, renames map[string]map[string]string) (prefs.Preference, []string) {
	if rec.LegacyCredentials == nil {
		return rec, nil
	}
	creds := *rec.LegacyCredentials
	scope := config.DefaultGatewayHarnesses
	if slices.Contains(scope, rec.Harness) {
		scope = []string{rec.Harness}
	}
	out := rec
	out.LegacyCredentials = nil
	out.Gateway = map[string]string{}
	for h, v := range rec.Gateway {
		out.Gateway[h] = v
	}
	set := func(h, v string) {
		if out.Gateway[h] == "" {
			out.Gateway[h] = v
		}
	}
	chosen := map[string]bool{}
	var notices []string
	for _, c := range creds {
		served := false
		for _, h := range scope {
			name := c
			if to, ok := renames[h][c]; ok {
				name = to
			}
			if gw, ok := ctx.ConvertedGateways[name]; ok && slices.Contains(gw.EffectiveHarnesses(), h) {
				set(h, name)
				chosen[h] = true
				served = true
			}
		}
		if _, plain := ctx.Credentials[c]; !served && !plain {
			notices = append(notices, fmt.Sprintf("saved credential %q names no gateway that serves %s; dropped "+
				"(add one with: aide gateway add %s --harnesses %s)", c, strings.Join(scope, ", "), c, scope[0]))
		}
	}
	// A selection with no gateway for a slot harness means none, as the flags do.
	for _, h := range scope {
		if (h == "claude" || h == "codex") && !chosen[h] {
			set(h, "none")
		}
	}
	if len(out.Gateway) == 0 {
		out.Gateway = nil
	}
	return out, notices
}

// CacheReader gives the model a gateway of a profile should start on:
// the one OpenCode pins to it (PinnedModel), else the cached
// catalogue's default (DefaultModel); "" when there is none.
type CacheReader interface {
	DefaultModel(profile, gateway string) string
	PinnedModel(ctx config.Context, gateway string) string
}

// Plan is a gateway migration of a loaded config: Config is the
// migrated copy (never the caller's), Notes the summary lines.
type Plan struct {
	Config *config.Config
	Notes  []string
	// legacy and renames are the profiles' contexts before migration
	// (with their converted gateways) and their renames, which
	// translate saved repository preferences.
	legacy  map[string]config.Context
	renames map[string]map[string]map[string]string
}

// Empty reports a plan that changes nothing.
func (p Plan) Empty() bool { return p.Config == nil || len(p.legacy) == 0 }

// Result is what Apply did.
type Result struct {
	// BackupDir holds the backups, "" when nothing was backed up.
	BackupDir string
	// ReadOnly is true when the config directory refused the write; the
	// caller keeps running from the in-memory translation.
	ReadOnly bool
	Summary  string
}

// PlanGatewayMigration converts every profile that has legacy gateways
// into gateways: in the one-API-per-gateway form plus an explicit
// gateway_choice: (the choice today's default rule makes, flagged in
// the notes when it was inherited from the sorted gateway order, not
// named in default_credentials:), drops the gateway names from
// default_credentials: and backfills a missing model from caches. It
// reads cfg and caches only. Plain credentials: and everything else
// stay as they are.
func PlanGatewayMigration(cfg *config.Config, caches CacheReader) (Plan, error) {
	out := *cfg
	out.Contexts = maps.Clone(cfg.Contexts)
	out.NeedsGatewayMigration = false
	out.LegacyRenames = nil
	plan := Plan{Config: &out, legacy: map[string]config.Context{}, renames: map[string]map[string]map[string]string{}}
	for _, name := range slices.Sorted(maps.Keys(cfg.Contexts)) {
		ctx := cfg.Contexts[name]
		if !slices.ContainsFunc(slices.Collect(maps.Values(ctx.Gateways)), config.GatewayConfig.IsLegacy) {
			continue
		}
		var renames map[string]map[string]string
		ctx.ConvertedGateways, renames = config.ConvertLegacyGateways(ctx)
		plan.legacy[name], plan.renames[name] = ctx, renames
		migrated := ctx
		migrated.Gateways = map[string]config.GatewayConfig{}
		for gwName, gw := range ctx.ConvertedGateways {
			gw = gw.WithBaseURL()
			if gw.Model == "" && caches != nil {
				if slices.Contains(gw.EffectiveHarnesses(), "opencode") {
					gw.Model = caches.PinnedModel(ctx, gwName)
				}
				if gw.Model == "" {
					gw.Model = caches.DefaultModel(name, gwName)
				}
			}
			migrated.Gateways[gwName] = gw
		}
		migrated.ConvertedGateways = migrated.Gateways
		migrated.GatewayChoice = maps.Clone(ctx.GatewayChoice)
		if migrated.GatewayChoice == nil {
			migrated.GatewayChoice = map[string]string{}
		}
		for _, h := range config.DefaultGatewayHarnesses {
			if !ctx.ChoiceIsUnset(h) {
				continue
			}
			v := config.LegacyGatewayChoice(ctx, h, renames)
			if v == "" {
				continue
			}
			migrated.GatewayChoice[h] = v
			if v != "none" && !named(ctx.DefaultCredentials, v, renames[h]) {
				plan.Notes = append(plan.Notes, fmt.Sprintf("profile %q: %s now uses gateway %q, inherited from the old sorted gateway order; "+
					"change it with: aide gateway use <name|none> --harness %s --profile %s", name, h, v, h, name))
			}
		}
		migrated.DefaultCredentials = slices.DeleteFunc(slices.Clone(ctx.DefaultCredentials), func(c string) bool {
			_, wasGateway := ctx.Gateways[c]
			return wasGateway
		})
		plan.Notes = append(plan.Notes, codexNotes(name, ctx, renames)...)
		out.Contexts[name] = migrated
	}
	return plan, nil
}

// Migrate plans and applies the migration of cfg and, once written,
// makes cfg the migrated config. A read-only config directory leaves
// cfg as loaded (translated in memory).
func Migrate(cfg *config.Config, caches CacheReader, configDir, stateDir string) (Result, error) {
	plan, err := PlanGatewayMigration(cfg, caches)
	if err != nil {
		return Result{}, err
	}
	res, err := Apply(plan, configDir, stateDir)
	if err == nil && !res.ReadOnly && !plan.Empty() {
		*cfg = *plan.Config
	}
	return res, err
}

// named reports whether default_credentials: lists gateway v under its
// legacy name (renames maps old names to new for the harness).
func named(defaults []string, v string, renames map[string]string) bool {
	return slices.Contains(defaults, v) || slices.ContainsFunc(defaults, func(d string) bool { return renames[d] == v })
}

// codexNotes names the legacy gateways that reached Codex only through
// the omitted harnesses: default and so lose that mark, with the
// command that adds a Codex gateway.
func codexNotes(profile string, ctx config.Context, renames map[string]map[string]string) []string {
	var notes []string
	for _, old := range slices.Sorted(maps.Keys(ctx.Gateways)) {
		gw := ctx.Gateways[old]
		if !gw.IsLegacy() || !slices.Contains(gw.EffectiveHarnesses(), "codex") || renames["codex"][old] != "" {
			continue
		}
		name := old
		for _, h := range gw.EffectiveHarnesses() {
			if to := renames[h][old]; to != "" {
				name = to
			}
		}
		if slices.Contains(ctx.ConvertedGateways[name].EffectiveHarnesses(), "codex") {
			continue
		}
		auth := "--secret-store " + gw.Store + " --key " + gw.Key
		if gw.Key == "" {
			auth = "--no-auth"
		}
		notes = append(notes, fmt.Sprintf("profile %q: gateway %q no longer serves codex; if it serves /v1/responses add one with: "+
			"aide gateway add %s-codex --url %s/v1 --api openai-responses %s --harnesses codex --profile %s",
			profile, name, name, config.NormalizeGatewayBaseURL(gw.BaseURL), auth, profile))
	}
	return notes
}

// Apply backs up the YAML files and preferences.json plan changes into
// <stateDir>/migrations/<UTC timestamp>/, writes the migrated YAML under
// configDir and the translated preferences under stateDir, and returns
// the summary to print. A config directory that refuses the write
// (read-only) is not an error: nothing is changed and Result.ReadOnly
// says so.
func Apply(plan Plan, configDir, stateDir string) (Result, error) {
	if plan.Empty() {
		return Result{}, nil
	}
	readOnly := Result{ReadOnly: true, Summary: "aide: note: " + configDir + " is read-only, so aide runs from the translated gateway config; " +
		"run `aide config migrate` to see what to change in your dotfiles\n"}
	// Check before backing up, so a read-only dir leaves no backups behind.
	probe, err := os.CreateTemp(configDir, ".aide-write-check-*")
	if err != nil {
		if errors.Is(err, syscall.EROFS) || errors.Is(err, os.ErrPermission) {
			return readOnly, nil
		}
		return Result{}, err
	}
	_ = probe.Close()
	_ = os.Remove(probe.Name())
	backup := filepath.Join(stateDir, "migrations", time.Now().UTC().Format("20060102T150405Z"))
	files := []string{filepath.Join(configDir, "config.yaml")}
	for name := range plan.legacy {
		files = append(files, config.ProfileFilePath(configDir, name))
	}
	for _, f := range files {
		rel, err := filepath.Rel(configDir, f)
		if err != nil {
			return Result{}, err
		}
		if _, err := os.Stat(f); errors.Is(err, os.ErrNotExist) {
			continue
		}
		if _, err := fsutil.BackupToDir(f, backup, rel); err != nil {
			return Result{}, err
		}
	}
	if err := config.WriteConfigTo(plan.Config, filepath.Join(configDir, "config.yaml")); err != nil {
		if errors.Is(err, syscall.EROFS) || errors.Is(err, os.ErrPermission) {
			return readOnly, nil
		}
		return Result{}, err
	}
	notes := slices.Clone(plan.Notes)
	prefsPath := filepath.Join(stateDir, "preferences.json")
	if err := translatePreferences(plan, prefsPath, backup, &notes); err != nil {
		notes = append(notes, fmt.Sprintf("could not translate %s (%v); aide still reads it", prefsPath, err))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "aide: migrated gateway config to gateways: with gateway_choice: (backups in %s)\n", backup)
	for _, n := range notes {
		fmt.Fprintf(&b, "aide: note: %s\n", n)
	}
	return Result{BackupDir: backup, Summary: b.String()}, nil
}

// translatePreferences rewrites version 1 repository preferences of the
// migrated profiles as version 2, appending what it could not
// translate to notes.
func translatePreferences(plan Plan, path, backup string, notes *[]string) error {
	f, err := prefs.Load(path)
	if err != nil {
		return err
	}
	changed := false
	for profile, byRemote := range f.Profiles {
		ctx, ok := plan.legacy[profile]
		if !ok {
			continue
		}
		for remote, rec := range byRemote {
			if rec.LegacyCredentials == nil {
				continue
			}
			translated, ns := TranslatePreference(rec, ctx, plan.renames[profile])
			byRemote[remote], changed = translated, true
			*notes = append(*notes, ns...)
		}
	}
	if !changed {
		return nil
	}
	return prefs.SaveBackingUpTo(path, f, backup)
}
