package gateway

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/fsutil"
	"github.com/jskswamy/aide/internal/gatewaypreset"
	"github.com/jskswamy/aide/internal/harness"
	"github.com/jskswamy/aide/internal/homepath"
	"github.com/jskswamy/aide/internal/yamlnode"
	"gopkg.in/yaml.v3"
)

// harnessFile is how one harness stores a gateway's provider and
// models. Every implementation edits only the gateway's own entry and
// keeps the rest of the file.
type harnessFile interface {
	// path is the config file for this harness under ctx.
	path(ctx config.Context, dir, home, gwName string) string
	write(path, gwName string, gw config.GatewayConfig, cat *CachedCatalogue) error
	// mismatch describes how the file differs from cat, or "" when it
	// carries the catalogue.
	mismatch(path, gwName string, gw config.GatewayConfig, cat *CachedCatalogue) (string, error)
	remove(path, gwName string, gw config.GatewayConfig) (bool, error)
	// prune drops the aide-<x> entries whose x is not in keep, the
	// legacy bare entry of every gateway in gws (when it points at that
	// gateway's URL), and the startup pin aide wrote (a model or
	// provider naming an aide-<x> entry or a gateway of gws).
	prune(path string, gws map[string]config.GatewayConfig, keep map[string]bool) (bool, error)
}

var harnessFiles = map[string]harnessFile{
	"claude":   claudeFile{},
	"pi":       piFile{},
	"omp":      ompFile{},
	"opencode": openCodeFile{},
	"codex":    codexFile{},
}

func lookupHarnessFile(harnessName string) (harnessFile, error) {
	hf, ok := harnessFiles[harnessName]
	if !ok {
		return nil, fmt.Errorf("gateway: %q is not a gateway harness", harnessName)
	}
	return hf, nil
}

// HarnessConfigDir is the config dir a launch of harnessName under ctx
// uses: the profile's own dir when it selects one; otherwise the
// harness's dir variable from the profile's env:, then from the
// inherited environment, then from the harness's sandbox module when
// sandboxing is enabled, then the harness default.
func HarnessConfigDir(ctx config.Context, harnessName, home string) (string, error) {
	if _, err := lookupHarnessFile(harnessName); err != nil {
		return "", err
	}
	// Launching under harnessName gives a named profile its derived dirs
	// (as the launch does after --agent), even when its own agent: has none.
	ctx.Agent = harnessName
	(*config.Config)(nil).ApplyDefaultProfileDir(ctx.Name, &ctx)
	dir := ctx.LaunchConfigDir(harnessName, home, os.Getenv)
	if dir == "" {
		return "", fmt.Errorf("gateway: %s has no known config dir", harnessName)
	}
	return dir, nil
}

// ErrSharedNativeDir reports a named profile that would use a harness's
// native dir, which other profiles share.
var ErrSharedNativeDir = errors.New("gateway: named profile would use the shared native harness dir")

// harnessTarget resolves the harness's file handler, dir and file. A
// named profile never gets the native dir: only the built-in default
// profile owns it.
func harnessTarget(ctx config.Context, harnessName, home, gwName string) (harnessFile, string, string, error) {
	hf, err := lookupHarnessFile(harnessName)
	if err != nil {
		return nil, "", "", err
	}
	dir, err := HarnessConfigDir(ctx, harnessName, home)
	if err != nil {
		return nil, "", "", err
	}
	if d, ok := harness.Lookup(harnessName); ok && ctx.Name != "" && !config.IsDefaultProfile(ctx.Name) && fsutil.SameDir(d.DefaultDir(home), dir) {
		return nil, "", "", fmt.Errorf("%w: profile %q, %s dir %s; set its dir variable aside: config_dir_name or config_dirs in the profile instead of env", ErrSharedNativeDir, ctx.Name, harnessName, homepath.Tilde(dir, home))
	}
	path := hf.path(ctx, dir, home, gwName)
	if _, ok := hf.(openCodeFile); ok {
		dir = filepath.Dir(path)
	}
	return hf, dir, path, nil
}

// ClaudeSettingsPath returns the per-gateway Claude Code --settings file
// path for gwName under ctx (<claude-dir>/cache/aide-gateway-<gw>.json).
func ClaudeSettingsPath(ctx config.Context, gwName, home string) (string, error) {
	_, _, path, err := harnessTarget(ctx, "claude", home, gwName)
	return path, err
}

// unwritable says why harnessName gets no entry for gw, or "": a
// provider switch has no URL, Claude loads only Anthropic Messages and
// Codex refuses openai-chat (wire_api = "chat").
func unwritable(harnessName string, gw config.GatewayConfig) string {
	switch ep := Endpoint(gw); {
	case ep.Switch != nil:
		return "provider switch: nothing to write"
	case harnessName == "claude" && ep.API != gatewaypreset.APIAnthropicMessages:
		return "claude loads only anthropic-messages gateways"
	case harnessName == "codex" && ep.API == gatewaypreset.APIOpenAIChat:
		return "codex loads only openai-responses gateways"
	}
	return ""
}

// withoutSwitches is gws minus the provider-switch gateways, which no
// harness file carries.
func withoutSwitches(gws map[string]config.GatewayConfig) map[string]config.GatewayConfig {
	out := make(map[string]config.GatewayConfig, len(gws))
	for n, gw := range gws {
		if Endpoint(gw).Switch == nil {
			out[n] = gw
		}
	}
	return out
}

// harnessWrites serializes writes to harness files: gateways synced in
// parallel share one file per harness, and each write reads the file
// first.
var harnessWrites sync.Mutex

// MaterializeHarness writes cat into harnessName's config file. When
// the harness's dir does not exist and createDirIfMissing is false,
// nothing is written and the status is pending: aide leaves a harness
// dir to its first launch.
func MaterializeHarness(ctx config.Context, gwName string, gw config.GatewayConfig, cat *CachedCatalogue, harnessName, home string, createDirIfMissing bool) (HarnessFileStatus, error) {
	hf, dir, path, err := harnessTarget(ctx, harnessName, home, gwName)
	if err != nil {
		return HarnessFileStatus{Harness: harnessName, EnvVar: gw.TokenEnv(gwName, harnessName)}, err
	}
	st := HarnessFileStatus{Harness: harnessName, EnvVar: gw.TokenEnv(gwName, harnessName), FilePath: path}
	if why := unwritable(harnessName, gw); why != "" {
		st.State, st.Detail = StatePending, why
		return st, nil
	}
	if cat == nil {
		return st, errors.New("gateway: no cached catalogue to write")
	}
	if !dirExists(dir) {
		if !createDirIfMissing {
			st.State, st.Detail = StatePending, "dir not created yet"
			return st, nil
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return st, fmt.Errorf("creating %s: %w", dir, err)
		}
	}
	harnessWrites.Lock()
	err = hf.write(path, gwName, gw, cat)
	harnessWrites.Unlock()
	if err != nil {
		return st, err
	}
	st.State, st.Detail = StateSynced, fmt.Sprintf("%d models", len(cat.Models))
	return st, nil
}

// InspectHarness reports whether harnessName's config file carries the
// fresh catalogue cat for gwName.
func InspectHarness(ctx config.Context, gwName string, gw config.GatewayConfig, cat *CachedCatalogue, harnessName, home string) HarnessFileStatus {
	hf, dir, path, err := harnessTarget(ctx, harnessName, home, gwName)
	st := HarnessFileStatus{Harness: harnessName, EnvVar: gw.TokenEnv(gwName, harnessName), FilePath: path}
	if err != nil {
		st.State, st.Detail = StateStale, err.Error()
		return st
	}
	if why := unwritable(harnessName, gw); why != "" {
		st.State, st.Detail = StatePending, why
		return st
	}
	if !dirExists(dir) {
		st.State, st.Detail = StatePending, "dir not provisioned"
		return st
	}
	switch {
	case cat == nil:
		st.State, st.Detail = StateStale, "no cached catalogue"
		return st
	case !MatchesBaseURL(cat, Endpoint(gw).BaseURL):
		st.State, st.Detail = StateStale, "base URL changed — run: aide gateway sync"
		return st
	case IsExpired(cat, gw.TTL(), time.Now()):
		st.State, st.Detail = StateStale, "cached catalogue expired"
		return st
	}
	if _, err := os.Stat(path); err != nil {
		st.State, st.Detail = StateStale, "not written yet"
		return st
	}
	diff, err := hf.mismatch(path, gwName, gw, cat)
	switch {
	case err != nil:
		st.State, st.Detail = StateStale, err.Error()
	case diff != "":
		st.State, st.Detail = StateStale, diff
	default:
		st.State, st.Detail = StateSynced, fmt.Sprintf("%d models", len(cat.Models))
	}
	return st
}

// RemoveFromHarness strips gwName's entry from harnessName's config
// file and reports whether there was one.
func RemoveFromHarness(ctx config.Context, gwName string, gw config.GatewayConfig, harnessName, home string) (bool, error) {
	if Endpoint(gw).Switch != nil {
		return false, nil
	}
	hf, _, path, err := harnessTarget(ctx, harnessName, home, gwName)
	if err != nil {
		return false, err
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return hf.remove(path, gwName, gw)
}

// Prune cleans harnessName's config file after a gateway change: it
// removes aide-<gw> entries of gateways not in configured, the legacy
// bare <gw> entries of ctx's gateways that point at their URL, and the
// top-level model (OpenCode) or model_provider (Codex) aide used to
// pin. A pin it did not write stays. It reports whether the file
// changed; a missing file is not an error.
func Prune(ctx config.Context, harnessName, home string, configured []string) (bool, error) {
	hf, dir, path, err := harnessTarget(ctx, harnessName, home, "")
	if err != nil {
		return false, err
	}
	if _, isClaude := hf.(claudeFile); isClaude {
		path = filepath.Join(dir, "cache")
		if !dirExists(path) {
			return false, nil
		}
	} else if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	keep := setOf(configured)
	// Only the default profile reaches the native dir (harnessTarget refuses
	// named profiles), and other profiles' older entries there are not its to drop.
	if d, ok := harness.Lookup(harnessName); ok && fsutil.SameDir(d.DefaultDir(home), dir) {
		keep = nil
	}
	harnessWrites.Lock()
	defer harnessWrites.Unlock()
	return hf.prune(path, withoutSwitches(ctx.Gateways), keep)
}

func setOf(names []string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

// staleAide reports an aide-<x> entry name whose gateway x is not kept;
// a nil keep (a dir shared with other profiles) keeps every entry.
func staleAide(name string, keep map[string]bool) bool {
	gw, ok := strings.CutPrefix(name, "aide-")
	return ok && keep != nil && !keep[gw]
}

// jsonEntryNames lists the entry names under doc[section].
func jsonEntryNames(doc map[string]json.RawMessage, section string) ([]string, error) {
	entries := map[string]json.RawMessage{}
	if raw, ok := doc[section]; ok {
		if err := json.Unmarshal(raw, &entries); err != nil {
			return nil, fmt.Errorf("%s: %w", section, err)
		}
	}
	return slices.Sorted(maps.Keys(entries)), nil
}

func dirExists(dir string) bool {
	info, err := os.Stat(dir)
	return err == nil && info.IsDir()
}

// removeBoth removes gwName's aide-<gw> entry and its legacy bare
// <gw> entry with rm, reporting whether either existed.
func removeBoth(path, gwName string, gw config.GatewayConfig, rm func(path, name string, gw config.GatewayConfig) (bool, error)) (bool, error) {
	a, err := rm(path, ProviderID(gwName), gw)
	if err != nil {
		return a, err
	}
	b, err := rm(path, gwName, gw)
	return a || b, err
}

// jsonEntry reads doc[section][name] into v, reporting whether the
// entry exists.
func jsonEntry(doc map[string]json.RawMessage, section, name string, v any) (bool, error) {
	entries := map[string]json.RawMessage{}
	if raw, ok := doc[section]; ok {
		if err := json.Unmarshal(raw, &entries); err != nil {
			return false, fmt.Errorf("%s: %w", section, err)
		}
	}
	raw, ok := entries[name]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(raw, v)
}

// setJSONEntry sets doc[section][name] to v, or deletes it when v is
// nil (dropping section once empty), and reports whether it existed.
func setJSONEntry(doc map[string]json.RawMessage, section, name string, v any) (bool, error) {
	entries := map[string]json.RawMessage{}
	if raw, ok := doc[section]; ok {
		if err := json.Unmarshal(raw, &entries); err != nil {
			return false, fmt.Errorf("%s: %w", section, err)
		}
	}
	_, existed := entries[name]
	if v == nil {
		delete(entries, name)
	} else {
		raw, err := json.Marshal(v)
		if err != nil {
			return existed, err
		}
		entries[name] = raw
	}
	if len(entries) == 0 {
		delete(doc, section)
		return existed, nil
	}
	raw, err := json.Marshal(entries)
	if err != nil {
		return existed, err
	}
	doc[section] = raw
	return existed, nil
}

// updateJSONEntry rewrites path with doc[section][name] = v (nil
// deletes) and reports whether the entry existed.
func updateJSONEntry(path, section, name string, v any) (bool, error) {
	doc, err := fsutil.ReadJSONCRawMap(path)
	if err != nil {
		return false, err
	}
	existed, err := setJSONEntry(doc, section, name, v)
	if err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	if v == nil && !existed {
		return false, nil
	}
	return existed, fsutil.WriteJSON(path, doc)
}

// claudeFile is a per-gateway Claude Code --settings file,
// <claude-dir>/cache/aide-gateway-<gw>.json, carrying modelPicker so
// Claude Code's /model picker lists every model the gateway serves
// (including non-Anthropic IDs) without colliding across concurrent
// gateway launches or being overwritten by Claude's /v1/models discovery.
type claudeFile struct{}

type claudeSettingsDoc struct {
	ModelPicker claudeModelPicker `json:"modelPicker"`
}

type claudeModelPicker struct {
	ReplaceBuiltInOptions bool                `json:"replaceBuiltInOptions"`
	Options               []claudePickerModel `json:"options"`
}

type claudePickerModel struct {
	Model       string `json:"model"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

const (
	claudeSettingsPrefix = "aide-gateway-"
	claudeSettingsSuffix = ".json"
)

func (claudeFile) path(_ config.Context, dir, _, gwName string) string {
	return filepath.Join(dir, "cache", claudeSettingsPrefix+gwName+claudeSettingsSuffix)
}

// prune removes stale aide-gateway-<x>.json files in cacheDir whose x
// is not in keep, plus any legacy cache/gateway-models.json pointing at
// one of gws's URLs.
func (claudeFile) prune(cacheDir string, gws map[string]config.GatewayConfig, keep map[string]bool) (bool, error) {
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	changed := false
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if keep != nil && strings.HasPrefix(name, claudeSettingsPrefix) && strings.HasSuffix(name, claudeSettingsSuffix) {
			gw := strings.TrimSuffix(strings.TrimPrefix(name, claudeSettingsPrefix), claudeSettingsSuffix)
			if !keep[gw] {
				if err := os.Remove(filepath.Join(cacheDir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
					return changed, err
				}
				changed = true
			}
		}
	}
	// Clean up legacy cache/gateway-models.json when it points at one of gws's URLs.
	legacyPath := filepath.Join(cacheDir, "gateway-models.json")
	if data, err := os.ReadFile(legacyPath); err == nil { // #nosec G304 -- Claude's legacy gateway cache in the profile's dir
		var legacy struct {
			BaseURL string `json:"baseUrl"`
		}
		if json.Unmarshal(data, &legacy) == nil && legacy.BaseURL != "" {
			legacyURL := NormalizeBaseURL(legacy.BaseURL)
			for _, gw := range gws {
				if NormalizeBaseURL(Endpoint(gw).BaseURL) == legacyURL {
					if err := os.Remove(legacyPath); err != nil && !errors.Is(err, os.ErrNotExist) {
						return changed, err
					}
					changed = true
					break
				}
			}
		}
	}
	return changed, nil
}

func claudePickerModels(gwName string, models []Model) []claudePickerModel {
	out := make([]claudePickerModel, len(models))
	for i, m := range models {
		label := m.DisplayName
		if label == "" {
			label = m.ID
		}
		out[i] = claudePickerModel{Model: m.ID, Label: label, Description: formatClaudeDesc(gwName, m)}
	}
	return out
}

func (claudeFile) write(path, gwName string, _ config.GatewayConfig, cat *CachedCatalogue) error {
	doc := claudeSettingsDoc{
		ModelPicker: claudeModelPicker{
			ReplaceBuiltInOptions: true,
			Options:               claudePickerModels(gwName, cat.Models),
		},
	}
	return fsutil.WriteJSON(path, doc)
}

func (claudeFile) read(path string) (claudeSettingsDoc, error) {
	var doc claudeSettingsDoc
	data, err := os.ReadFile(path) // #nosec G304 -- Claude's gateway settings in the profile's dir
	if err != nil {
		return doc, err
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return doc, fmt.Errorf("parsing %s: %w", path, err)
	}
	return doc, nil
}

func (c claudeFile) mismatch(path, gwName string, _ config.GatewayConfig, cat *CachedCatalogue) (string, error) {
	doc, err := c.read(path)
	if err != nil {
		return "", err
	}
	if !doc.ModelPicker.ReplaceBuiltInOptions {
		return "modelPicker settings differ", nil
	}
	if !slices.Equal(doc.ModelPicker.Options, claudePickerModels(gwName, cat.Models)) {
		return "model list differs", nil
	}
	return "", nil
}

func (claudeFile) remove(path, _ string, _ config.GatewayConfig) (bool, error) {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func formatContextTokens(n int) string {
	if n >= 1_000_000 {
		s := fmt.Sprintf("%.1f", float64(n)/1_000_000)
		return strings.TrimSuffix(s, ".0") + "M"
	}
	if n >= 1_000 {
		return fmt.Sprintf("%dK", n/1_000)
	}
	return fmt.Sprintf("%d", n)
}

// formatClaudeDesc is the /model picker's description line.
func formatClaudeDesc(gwName string, m Model) string {
	switch {
	case gwName != "" && m.ContextWindow > 0:
		return fmt.Sprintf("%s · %s context", gwName, formatContextTokens(m.ContextWindow))
	case gwName != "":
		return gwName
	case m.ContextWindow > 0:
		return fmt.Sprintf("%s context", formatContextTokens(m.ContextWindow))
	default:
		return "Custom gateway model"
	}
}

// piFile is Pi's <pi-dir>/models.json; a gateway is providers.<name>.
type piFile struct{}

type piProvider struct {
	BaseURL    string            `json:"baseUrl"`
	APIKey     string            `json:"apiKey,omitempty"`
	AuthHeader bool              `json:"authHeader"`
	API        string            `json:"api"`
	Headers    map[string]string `json:"headers,omitempty"`
	Models     []piModel         `json:"models"`
}

type piModel struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ContextWindow int    `json:"contextWindow,omitempty"`
	MaxTokens     int    `json:"maxTokens,omitempty"`
}

func (piFile) path(_ config.Context, dir, _, _ string) string {
	return filepath.Join(dir, "models.json")
}

func piModels(models []Model) []piModel {
	out := make([]piModel, len(models))
	for i, m := range models {
		out[i] = piModel{ID: m.ID, Name: m.DisplayName, ContextWindow: m.ContextWindow, MaxTokens: m.MaxOutputTokens}
	}
	return out
}

// isLiteralPiPlaceholder reports whether s is a non-empty literal value
// in Pi's models.json (neither a "!cmd" shell reference nor a "$ENV"
// template). Pi hides custom providers unless apiKey resolves to a
// non-empty string, even when authHeader is false.
func isLiteralPiPlaceholder(s string) bool {
	return s != "" && !strings.HasPrefix(s, "!") && !strings.Contains(s, "$")
}

// piNoAuth is the literal apiKey of a keyless Pi entry; Pi hides the
// provider without one.
const piNoAuth = "aide-no-auth"

// harnessAPI is the api string Pi and Oh My Pi use for each wire protocol.
var harnessAPI = map[gatewaypreset.API]string{
	gatewaypreset.APIAnthropicMessages: "anthropic-messages",
	gatewaypreset.APIOpenAIChat:        "openai-completions",
	gatewaypreset.APIOpenAIResponses:   "openai-responses",
}

// keyed reports whether a key is sent to the gateway.
func keyed(gw config.GatewayConfig, ep EndpointSpec) bool { return gw.RequiresToken() && !ep.Keyless }

// piEntry is gw's Pi provider without its models.
func piEntry(gwName string, gw config.GatewayConfig) piProvider {
	ep := Endpoint(gw)
	p := piProvider{BaseURL: ep.BaseURL, API: harnessAPI[ep.API], APIKey: piNoAuth}
	if keyed(gw, ep) {
		p.APIKey = "$" + config.GatewayKeyVar(gwName)
		if ep.Header == gatewaypreset.HeaderXAPIKey {
			p.Headers = map[string]string{"x-api-key": p.APIKey}
		} else {
			p.AuthHeader = true
		}
	}
	return p
}

func (piFile) write(path, gwName string, gw config.GatewayConfig, cat *CachedCatalogue) error {
	p := piEntry(gwName, gw)
	p.Models = piModels(cat.Models)
	if _, err := updateJSONEntry(path, "providers", ProviderID(gwName), p); err != nil {
		return err
	}
	_, err := piFile{}.removeEntry(path, gwName, gw)
	return err
}

func (piFile) mismatch(path, gwName string, gw config.GatewayConfig, cat *CachedCatalogue) (string, error) {
	doc, err := fsutil.ReadJSONCRawMap(path)
	if err != nil {
		return "", err
	}
	var p piProvider
	want := piEntry(gwName, gw)
	ok, err := jsonEntry(doc, "providers", ProviderID(gwName), &p)
	switch {
	case err != nil:
		return "", fmt.Errorf("%s: %w", path, err)
	case !ok:
		return fmt.Sprintf("provider %q missing", ProviderID(gwName)), nil
	case p.BaseURL != want.BaseURL:
		return "baseUrl differs", nil
	case p.API != want.API:
		return "api differs", nil
	case p.AuthHeader != want.AuthHeader || !maps.Equal(p.Headers, want.Headers):
		return "apiKey differs", nil
	case keyed(gw, Endpoint(gw)) && p.APIKey != want.APIKey, !keyed(gw, Endpoint(gw)) && !isLiteralPiPlaceholder(p.APIKey):
		return "apiKey differs", nil
	}
	if !slices.Equal(p.Models, piModels(cat.Models)) {
		return "model list differs", nil
	}
	return "", nil
}

// remove drops gwName's aide-<gw> entry and the bare <gw> entry an
// earlier aide wrote.
func (f piFile) remove(path, gwName string, gw config.GatewayConfig) (bool, error) {
	return removeBoth(path, gwName, gw, f.removeEntry)
}

// removeEntry leaves an entry pointing at another URL: it belongs to
// another profile's gateway or to the user.
func (piFile) removeEntry(path, name string, gw config.GatewayConfig) (bool, error) {
	doc, err := fsutil.ReadJSONCRawMap(path)
	if err != nil {
		return false, err
	}
	var p piProvider
	if ok, err := jsonEntry(doc, "providers", name, &p); err != nil || !ok || p.BaseURL != Endpoint(gw).BaseURL {
		return false, err
	}
	return updateJSONEntry(path, "providers", name, nil)
}

func (f piFile) prune(path string, gws map[string]config.GatewayConfig, keep map[string]bool) (bool, error) {
	changed := false
	for _, name := range slices.Sorted(maps.Keys(gws)) {
		ok, err := f.removeEntry(path, name, gws[name])
		if err != nil {
			return changed, err
		}
		changed = changed || ok
	}
	doc, err := fsutil.ReadJSONCRawMap(path)
	if err != nil {
		return changed, err
	}
	names, err := jsonEntryNames(doc, "providers")
	if err != nil {
		return changed, fmt.Errorf("%s: %w", path, err)
	}
	for _, name := range names {
		if !staleAide(name, keep) {
			continue
		}
		if _, err := updateJSONEntry(path, "providers", name, nil); err != nil {
			return changed, err
		}
		changed = true
	}
	return changed, nil
}

// ompFile is Oh My Pi's <omp-dir>/models.yml (or existing models.yaml);
// a gateway is providers.<name>.
type ompFile struct{}

type ompProvider struct {
	BaseURL    string     `yaml:"baseUrl"`
	API        string     `yaml:"api"`
	Auth       string     `yaml:"auth,omitempty"`
	AuthHeader bool       `yaml:"authHeader,omitempty"`
	APIKey     string     `yaml:"apiKey,omitempty"`
	Models     []ompModel `yaml:"models"`
}

type ompModel struct {
	ID            string `yaml:"id"`
	Name          string `yaml:"name"`
	ContextWindow int    `yaml:"contextWindow,omitempty"`
	MaxTokens     int    `yaml:"maxTokens,omitempty"`
}

func (ompFile) path(_ config.Context, dir, _, _ string) string {
	yml := filepath.Join(dir, "models.yml")
	if _, err := os.Stat(yml); errors.Is(err, os.ErrNotExist) {
		yamlAlt := filepath.Join(dir, "models.yaml")
		if _, err := os.Stat(yamlAlt); err == nil {
			return yamlAlt
		}
	}
	return yml
}

func readOMPModelsDoc(path string) ([]byte, map[string]yaml.Node, error) {
	doc := map[string]yaml.Node{}
	data, err := os.ReadFile(path) // #nosec G304 -- caller supplies a config path
	if errors.Is(err, os.ErrNotExist) {
		return nil, doc, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("reading %s: %w", path, err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return data, doc, nil
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if len(root.Content) == 0 || root.Content[0].Tag == "!!null" {
		return data, doc, nil
	}
	if err := root.Decode(&doc); err != nil {
		return nil, nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return data, doc, nil
}

func ompProviderEntry(doc map[string]yaml.Node, name string, v any) (bool, error) {
	entries := map[string]yaml.Node{}
	if raw, ok := doc["providers"]; ok && raw.Tag != "!!null" {
		if err := raw.Decode(&entries); err != nil {
			return false, fmt.Errorf("providers: %w", err)
		}
	}
	entry, ok := entries[name]
	if !ok {
		return false, nil
	}
	return true, entry.Decode(v)
}

func updateOMPProviderEntry(path, name string, v any) (bool, error) {
	existing, doc, err := readOMPModelsDoc(path)
	if err != nil {
		return false, err
	}
	entries := map[string]yaml.Node{}
	if raw, ok := doc["providers"]; ok && raw.Tag != "!!null" {
		if err := raw.Decode(&entries); err != nil {
			return false, fmt.Errorf("%s: providers: %w", path, err)
		}
	}
	_, existed := entries[name]
	if v == nil {
		if !existed {
			return false, nil
		}
		delete(entries, name)
	} else {
		var n yaml.Node
		if err := n.Encode(v); err != nil {
			return existed, err
		}
		entries[name] = n
	}
	if len(entries) == 0 {
		delete(doc, "providers")
	} else {
		var pNode yaml.Node
		if err := pNode.Encode(entries); err != nil {
			return existed, err
		}
		doc["providers"] = pNode
	}
	out, err := yamlnode.Marshal(doc)
	if err != nil {
		return existed, fmt.Errorf("encoding %s: %w", path, err)
	}
	return existed, fsutil.AtomicWrite(path, yamlnode.Merge(existing, out))
}

func ompModels(models []Model) []ompModel {
	out := make([]ompModel, len(models))
	for i, m := range models {
		out[i] = ompModel{
			ID:            m.ID,
			Name:          m.DisplayName,
			ContextWindow: m.ContextWindow,
			MaxTokens:     m.MaxOutputTokens,
		}
	}
	return out
}

// ompEntry is gw's Oh My Pi provider without its models.
func ompEntry(gwName string, gw config.GatewayConfig) ompProvider {
	ep := Endpoint(gw)
	p := ompProvider{BaseURL: ep.BaseURL, API: harnessAPI[ep.API], Auth: "none"}
	if keyed(gw, ep) {
		p.Auth, p.APIKey = "", config.GatewayKeyVar(gwName)
		p.AuthHeader = ep.Header != gatewaypreset.HeaderXAPIKey
	}
	return p
}

func (ompFile) write(path, gwName string, gw config.GatewayConfig, cat *CachedCatalogue) error {
	p := ompEntry(gwName, gw)
	p.Models = ompModels(cat.Models)
	if _, err := updateOMPProviderEntry(path, ProviderID(gwName), p); err != nil {
		return err
	}
	_, err := ompFile{}.removeEntry(path, gwName, gw)
	return err
}

func (ompFile) mismatch(path, gwName string, gw config.GatewayConfig, cat *CachedCatalogue) (string, error) {
	_, doc, err := readOMPModelsDoc(path)
	if err != nil {
		return "", err
	}
	var p ompProvider
	want := ompEntry(gwName, gw)
	ok, err := ompProviderEntry(doc, ProviderID(gwName), &p)
	switch {
	case err != nil:
		return "", fmt.Errorf("%s: %w", path, err)
	case !ok:
		return fmt.Sprintf("provider %q missing", ProviderID(gwName)), nil
	case p.BaseURL != want.BaseURL:
		return "baseUrl differs", nil
	case p.API != want.API:
		return "api differs", nil
	case p.APIKey != want.APIKey || p.AuthHeader != want.AuthHeader:
		return "apiKey differs", nil
	case want.Auth == "none" && p.Auth != "none", want.Auth == "" && p.Auth != "" && p.Auth != "apiKey":
		return "apiKey differs", nil
	}
	if !slices.Equal(p.Models, ompModels(cat.Models)) {
		return "model list differs", nil
	}
	return "", nil
}

// remove drops gwName's aide-<gw> entry and the bare <gw> entry an
// earlier aide wrote.
func (f ompFile) remove(path, gwName string, gw config.GatewayConfig) (bool, error) {
	return removeBoth(path, gwName, gw, f.removeEntry)
}

// removeEntry leaves an entry pointing at another URL: it belongs to
// another profile's gateway or to the user.
func (ompFile) removeEntry(path, name string, gw config.GatewayConfig) (bool, error) {
	_, doc, err := readOMPModelsDoc(path)
	if err != nil {
		return false, err
	}
	var p ompProvider
	if ok, err := ompProviderEntry(doc, name, &p); err != nil || !ok || p.BaseURL != Endpoint(gw).BaseURL {
		return false, err
	}
	return updateOMPProviderEntry(path, name, nil)
}

func (f ompFile) prune(path string, gws map[string]config.GatewayConfig, keep map[string]bool) (bool, error) {
	changed := false
	for _, name := range slices.Sorted(maps.Keys(gws)) {
		ok, err := f.removeEntry(path, name, gws[name])
		if err != nil {
			return changed, err
		}
		changed = changed || ok
	}
	_, doc, err := readOMPModelsDoc(path)
	if err != nil {
		return changed, err
	}
	entries := map[string]yaml.Node{}
	if raw, ok := doc["providers"]; ok && raw.Tag != "!!null" {
		if err := raw.Decode(&entries); err != nil {
			return changed, fmt.Errorf("%s: providers: %w", path, err)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(entries)) {
		if !staleAide(name, keep) {
			continue
		}
		if _, err := updateOMPProviderEntry(path, name, nil); err != nil {
			return changed, err
		}
		changed = true
	}
	return changed, nil
}

// openCodeFile is OpenCode's <opencode-dir>/opencode.jsonc (or
// $OPENCODE_CONFIG when set); a gateway is providers.<name>. Comments
// are accepted on read but not preserved on write, as with aide's MCP
// provisioning of the same file.
type openCodeFile struct{}

type openCodeProvider struct {
	Package  string                   `json:"package"`
	Name     string                   `json:"name"`
	Env      []string                 `json:"env,omitempty"`
	Settings openCodeSettings         `json:"settings"`
	Headers  map[string]string        `json:"headers,omitempty"`
	Models   map[string]openCodeModel `json:"models"`
}

type openCodeSettings struct {
	BaseURL string `json:"baseURL"`
	APIKey  string `json:"apiKey,omitempty"`
}

type openCodeModel struct {
	Name          string          `json:"name"`
	Limit         *openCodeLimit  `json:"limit,omitempty"`
	Compatibility *openCodeCompat `json:"compatibility,omitempty"`
}

type openCodeLimit struct {
	Context int `json:"context,omitempty"`
	Output  int `json:"output,omitempty"`
}

// shortcut: custom gateways reject Anthropic's thinking.block_binding beta; add a gateway option only if a custom proxy ever supports it.
type openCodeCompat struct {
	SupportsThinkingBlockBinding bool `json:"supportsThinkingBlockBinding"`
}

func (openCodeFile) path(ctx config.Context, dir, home, _ string) string {
	if v := ctx.Env["OPENCODE_CONFIG"]; v != "" {
		return homepath.Expand(v, home)
	}
	return filepath.Join(dir, "opencode.jsonc")
}

func openCodeAPIKey(gwName string) string { return "{env:" + config.GatewayKeyVar(gwName) + "}" }

// openCodeBaseURL is settings.baseURL: @ai-sdk/anthropic appends
// /messages to it, so it carries /v1; the openai-* URL is exact.
func openCodeBaseURL(ep EndpointSpec) string {
	if ep.API == gatewaypreset.APIAnthropicMessages {
		return ep.BaseURL + "/v1"
	}
	return ep.BaseURL
}

// openCodeHeaders is the entry-level key header, nil when keyless.
func openCodeHeaders(gwName string, gw config.GatewayConfig, ep EndpointSpec) map[string]string {
	switch {
	case !keyed(gw, ep):
		return nil
	case ep.Header == gatewaypreset.HeaderXAPIKey:
		return map[string]string{"x-api-key": openCodeAPIKey(gwName)}
	}
	return map[string]string{"Authorization": "Bearer " + openCodeAPIKey(gwName)}
}

func openCodePackage(ep EndpointSpec) string {
	if ep.API == gatewaypreset.APIAnthropicMessages {
		return "@ai-sdk/anthropic"
	}
	return "@ai-sdk/openai-compatible"
}

// DefaultModelID is the model a harness starts on when a gateway names
// none: the last Sonnet in models, else the first, "" when empty.
func DefaultModelID(models []Model) string {
	var best string
	for _, m := range models {
		if strings.Contains(strings.ToLower(m.ID), "sonnet") {
			best = m.ID
		}
	}
	if best == "" && len(models) > 0 {
		best = models[0].ID
	}
	return best
}

func openCodeModels(models []Model, ep EndpointSpec) map[string]openCodeModel {
	out := make(map[string]openCodeModel, len(models))
	for _, m := range models {
		om := openCodeModel{Name: m.DisplayName}
		if ep.API == gatewaypreset.APIAnthropicMessages {
			om.Compatibility = &openCodeCompat{}
		}
		if m.ContextWindow > 0 || m.MaxOutputTokens > 0 {
			om.Limit = &openCodeLimit{Context: m.ContextWindow, Output: m.MaxOutputTokens}
		}
		out[m.ID] = om
	}
	return out
}

func openCodeModelEqual(a, b openCodeModel) bool {
	if a.Name != b.Name || (a.Limit == nil) != (b.Limit == nil) {
		return false
	}
	return a.Limit == nil || *a.Limit == *b.Limit
}

func (openCodeFile) write(path, gwName string, gw config.GatewayConfig, cat *CachedCatalogue) error {
	doc, err := fsutil.ReadJSONCRawMap(path)
	if err != nil {
		return err
	}
	id := ProviderID(gwName)
	ep := Endpoint(gw)
	p := openCodeProvider{
		Package:  openCodePackage(ep),
		Name:     id,
		Settings: openCodeSettings{BaseURL: openCodeBaseURL(ep)},
		Headers:  openCodeHeaders(gwName, gw, ep),
		Models:   openCodeModels(cat.Models, ep),
	}
	if keyed(gw, ep) {
		p.Env = []string{config.GatewayKeyVar(gwName)}
		p.Settings.APIKey = openCodeAPIKey(gwName)
	}
	if _, err := setJSONEntry(doc, "providers", id, p); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	// Clean up any legacy V1 "provider.<gwName>" entry in the same file.
	_, _ = setJSONEntry(doc, "provider", gwName, nil)
	if _, err := dropOpenCodeEntry(doc, "providers", gwName, p.Settings.BaseURL); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return fsutil.WriteJSON(path, doc)
}

func (openCodeFile) mismatch(path, gwName string, gw config.GatewayConfig, cat *CachedCatalogue) (string, error) {
	doc, err := fsutil.ReadJSONCRawMap(path)
	if err != nil {
		return "", err
	}
	var p openCodeProvider
	ep := Endpoint(gw)
	ok, err := jsonEntry(doc, "providers", ProviderID(gwName), &p)
	switch {
	case err != nil:
		return "", fmt.Errorf("%s: %w", path, err)
	case !ok:
		return fmt.Sprintf("provider %q missing", ProviderID(gwName)), nil
	case p.Package != openCodePackage(ep):
		return "package differs", nil
	case keyed(gw, ep) && !slices.Equal(p.Env, []string{config.GatewayKeyVar(gwName)}):
		return "env differs", nil
	case !keyed(gw, ep) && len(p.Env) != 0:
		return "env differs", nil
	case p.Settings.BaseURL != openCodeBaseURL(ep):
		return "baseURL differs", nil
	case keyed(gw, ep) && p.Settings.APIKey != openCodeAPIKey(gwName):
		return "apiKey differs", nil
	case !keyed(gw, ep) && p.Settings.APIKey != "":
		return "apiKey differs", nil
	case !maps.Equal(p.Headers, openCodeHeaders(gwName, gw, ep)):
		return "authorization header differs", nil
	}
	for _, m := range p.Models {
		if (m.Compatibility == nil) != (ep.API != gatewaypreset.APIAnthropicMessages) || m.Compatibility != nil && m.Compatibility.SupportsThinkingBlockBinding {
			return "model compatibility differs", nil
		}
	}
	if !maps.EqualFunc(p.Models, openCodeModels(cat.Models, ep), openCodeModelEqual) {
		return "model list differs", nil
	}
	return "", nil
}

func (openCodeFile) prune(path string, gws map[string]config.GatewayConfig, keep map[string]bool) (bool, error) {
	doc, err := fsutil.ReadJSONCRawMap(path)
	if err != nil {
		return false, err
	}
	changed := false
	for _, name := range slices.Sorted(maps.Keys(gws)) {
		wantURL := openCodeBaseURL(Endpoint(gws[name]))
		for _, section := range []string{"providers", "provider"} {
			ok, err := dropOpenCodeEntry(doc, section, name, wantURL)
			if err != nil {
				return false, fmt.Errorf("%s: %w", path, err)
			}
			changed = changed || ok
		}
	}
	names, err := jsonEntryNames(doc, "providers")
	if err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	for _, name := range names {
		if staleAide(name, keep) {
			_, _ = setJSONEntry(doc, "providers", name, nil)
			changed = true
		}
	}
	var pin string
	if raw, ok := doc["model"]; ok && json.Unmarshal(raw, &pin) == nil {
		if prov, _, ok := strings.Cut(pin, "/"); ok && (strings.HasPrefix(prov, "aide-") || hasGateway(gws, prov)) {
			delete(doc, "model")
			changed = true
		}
	}
	if !changed {
		return false, nil
	}
	return true, fsutil.WriteJSON(path, doc)
}

func hasGateway(gws map[string]config.GatewayConfig, name string) bool {
	_, ok := gws[name]
	return ok
}

// dropOpenCodeEntry deletes section's name entry when it points at
// wantURL (an entry at another URL belongs to another profile's gateway
// or to the user) and reports whether it did. section is "providers",
// or the legacy V1 "provider" whose URL sits under options.
func dropOpenCodeEntry(doc map[string]json.RawMessage, section, name, wantURL string) (bool, error) {
	var p struct {
		Settings openCodeSettings `json:"settings"`
		Options  openCodeSettings `json:"options"`
	}
	if ok, err := jsonEntry(doc, section, name, &p); err != nil || !ok {
		return false, err
	}
	if p.Settings.BaseURL != wantURL && p.Options.BaseURL != wantURL {
		return false, nil
	}
	return setJSONEntry(doc, section, name, nil)
}

// remove drops gwName's aide-<gw> entry and the bare <gw> entry (or
// legacy V1 "provider.<gw>") an earlier aide wrote, with a top-level
// model naming either.
func (openCodeFile) remove(path, gwName string, gw config.GatewayConfig) (bool, error) {
	doc, err := fsutil.ReadJSONCRawMap(path)
	if err != nil {
		return false, err
	}
	wantURL := openCodeBaseURL(Endpoint(gw))
	var removed bool
	for _, e := range []struct{ section, name string }{
		{"providers", ProviderID(gwName)}, {"providers", gwName}, {"provider", gwName},
	} {
		ok, err := dropOpenCodeEntry(doc, e.section, e.name, wantURL)
		if err != nil {
			return false, err
		}
		if ok {
			removed = true
			var curModel string
			if raw, has := doc["model"]; has && json.Unmarshal(raw, &curModel) == nil && strings.HasPrefix(curModel, e.name+"/") {
				delete(doc, "model")
			}
		}
	}
	if !removed {
		return false, nil
	}
	return true, fsutil.WriteJSON(path, doc)
}

// codexFile is Codex's <codex-dir>/config.toml; a gateway is
// [model_providers.<name>]. Codex speaks only the OpenAI Responses API
// (0.167 refuses to load wire_api = "chat"), so the gateway must also
// serve /v1/responses. Comments are not
// preserved on write, as with aide's MCP provisioning of the same
// file.
type codexFile struct{}

func (codexFile) path(_ config.Context, dir, _, _ string) string {
	return filepath.Join(dir, "config.toml")
}

// codexWireAPI is the only wire_api Codex 0.167+ loads.
const codexWireAPI = "responses"

func codexProviders(doc map[string]any) map[string]any {
	providers, _ := doc["model_providers"].(map[string]any)
	return providers
}

// codexBaseURL is where an entry for gw points: the exact URL of an
// openai-responses gateway, or the /v1 URL an earlier aide wrote for an
// Anthropic one (which Codex never gets now, so only cleanup matches it).
func codexBaseURL(gw config.GatewayConfig) string {
	ep := Endpoint(gw)
	if ep.API != gatewaypreset.APIAnthropicMessages {
		return ep.BaseURL
	}
	return ep.BaseURL + "/v1"
}

func (c codexFile) write(path, gwName string, gw config.GatewayConfig, _ *CachedCatalogue) error {
	doc, err := fsutil.ReadTOML(path)
	if err != nil {
		return err
	}
	providers := codexProviders(doc)
	if providers == nil {
		providers = map[string]any{}
	}
	id := ProviderID(gwName)
	entry := map[string]any{
		"name":     id,
		"base_url": codexBaseURL(gw),
		"wire_api": codexWireAPI,
	}
	if keyed(gw, Endpoint(gw)) {
		entry["env_key"] = config.GatewayKeyVar(gwName)
	}
	providers[id] = entry
	doc["model_providers"] = providers
	for _, old := range codexLegacyNames(gwName) {
		dropCodexEntry(providers, old, entry["base_url"])
	}
	return fsutil.WriteTOML(path, doc)
}

// codexLegacyNames are the bare provider names an earlier aide may have
// written for gwName: its own, and for a split-off <gw>-codex, <gw>.
func codexLegacyNames(gwName string) []string {
	names := []string{gwName}
	if base, ok := strings.CutSuffix(gwName, "-codex"); ok {
		names = append(names, base)
	}
	return names
}

// dropCodexEntry deletes providers[name] when it points at baseURL (one
// at another URL belongs to another profile's gateway or to the user).
func dropCodexEntry(providers map[string]any, name string, baseURL any) bool {
	p, ok := providers[name].(map[string]any)
	if !ok || p["base_url"] != baseURL {
		return false
	}
	delete(providers, name)
	return true
}

func (codexFile) prune(path string, gws map[string]config.GatewayConfig, keep map[string]bool) (bool, error) {
	doc, err := fsutil.ReadTOML(path)
	if err != nil {
		return false, err
	}
	providers := codexProviders(doc)
	changed := false
	for _, name := range slices.Sorted(maps.Keys(gws)) {
		baseURL := codexBaseURL(gws[name])
		for _, old := range codexLegacyNames(name) {
			changed = dropCodexEntry(providers, old, baseURL) || changed
		}
	}
	for _, name := range slices.Sorted(maps.Keys(providers)) {
		if staleAide(name, keep) {
			delete(providers, name)
			changed = true
		}
	}
	if cur, _ := doc["model_provider"].(string); strings.HasPrefix(cur, "aide-") || slices.ContainsFunc(slices.Collect(maps.Keys(gws)), func(n string) bool {
		return slices.Contains(codexLegacyNames(n), cur)
	}) {
		delete(doc, "model_provider")
		changed = true
	}
	if !changed {
		return false, nil
	}
	if len(providers) == 0 {
		delete(doc, "model_providers")
	}
	return true, fsutil.WriteTOML(path, doc)
}

func (c codexFile) mismatch(path, gwName string, gw config.GatewayConfig, _ *CachedCatalogue) (string, error) {
	doc, err := fsutil.ReadTOML(path)
	if err != nil {
		return "", err
	}
	p, ok := codexProviders(doc)[ProviderID(gwName)].(map[string]any)
	switch {
	case !ok:
		return fmt.Sprintf("model provider %q missing", ProviderID(gwName)), nil
	case p["base_url"] != codexBaseURL(gw):
		return "base_url differs", nil
	case p["wire_api"] != codexWireAPI:
		return "wire_api differs", nil
	case keyed(gw, Endpoint(gw)) && p["env_key"] != config.GatewayKeyVar(gwName):
		return "env_key differs", nil
	case !keyed(gw, Endpoint(gw)):
		if _, ok := p["env_key"]; ok {
			return "env_key differs", nil
		}
	}
	return "", nil
}

// remove drops gwName's aide-<gw> entry and the bare entry an earlier
// aide wrote, with a model_provider naming either. An entry at another
// URL belongs to another profile's gateway or to the user.
func (c codexFile) remove(path, gwName string, gw config.GatewayConfig) (bool, error) {
	doc, err := fsutil.ReadTOML(path)
	if err != nil {
		return false, err
	}
	providers := codexProviders(doc)
	baseURL := codexBaseURL(gw)
	var removed bool
	for _, name := range append([]string{ProviderID(gwName)}, codexLegacyNames(gwName)...) {
		if dropCodexEntry(providers, name, baseURL) {
			removed = true
			if doc["model_provider"] == name {
				delete(doc, "model_provider")
			}
		}
	}
	if !removed {
		return false, nil
	}
	if len(providers) == 0 {
		delete(doc, "model_providers")
	}
	return true, fsutil.WriteTOML(path, doc)
}
