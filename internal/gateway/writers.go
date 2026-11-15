package gateway

import (
	"bytes"
	"cmp"
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
	"github.com/jskswamy/aide/internal/homepath"
	"github.com/jskswamy/aide/internal/yamlnode"
	"gopkg.in/yaml.v3"
)

// harnessFile is how one harness stores a gateway's provider and
// models. Every implementation edits only the gateway's own entry and
// keeps the rest of the file.
type harnessFile interface {
	// path is the config file for this harness under ctx.
	path(ctx config.Context, dir, home string) string
	// singleGatewayPerDir reports whether the harness's config dir
	// holds only one gateway's models at a time (rather than named
	// provider entries).
	singleGatewayPerDir() bool
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

// SingleGatewayPerDir reports whether harnessName keeps one gateway
// file per config dir rather than named provider entries.
func SingleGatewayPerDir(harnessName string) bool {
	hf, ok := harnessFiles[harnessName]
	return ok && hf.singleGatewayPerDir()
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
	dir := ctx.LaunchConfigDir(harnessName, home, os.Getenv)
	if dir == "" {
		return "", fmt.Errorf("gateway: %s has no known config dir", harnessName)
	}
	return dir, nil
}

// harnessTarget resolves the harness's file handler, dir and file.
func harnessTarget(ctx config.Context, harnessName, home string) (harnessFile, string, string, error) {
	hf, err := lookupHarnessFile(harnessName)
	if err != nil {
		return nil, "", "", err
	}
	dir, err := HarnessConfigDir(ctx, harnessName, home)
	if err != nil {
		return nil, "", "", err
	}
	path := hf.path(ctx, dir, home)
	if !hf.singleGatewayPerDir() {
		dir = filepath.Dir(path)
	}
	return hf, dir, path, nil
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
	hf, dir, path, err := harnessTarget(ctx, harnessName, home)
	if err != nil {
		return HarnessFileStatus{Harness: harnessName, EnvVar: gw.TokenEnv(gwName, harnessName)}, err
	}
	st := HarnessFileStatus{Harness: harnessName, EnvVar: gw.TokenEnv(gwName, harnessName), FilePath: path}
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
	hf, dir, path, err := harnessTarget(ctx, harnessName, home)
	st := HarnessFileStatus{Harness: harnessName, EnvVar: gw.TokenEnv(gwName, harnessName), FilePath: path}
	if err != nil {
		st.State, st.Detail = StateStale, err.Error()
		return st
	}
	if !dirExists(dir) {
		st.State, st.Detail = StatePending, "dir not provisioned"
		return st
	}
	if SingleGatewayPerDir(harnessName) && !CanSyncHarness(ctx, gwName, harnessName) {
		if MatchesBaseURL(cat, gw.BaseURL) && !IsExpired(cat, gw.TTL(), time.Now()) {
			if _, err := os.Stat(path); err == nil {
				if diff, err := hf.mismatch(path, gwName, gw, cat); err == nil && diff == "" {
					st.State, st.Detail = StateSynced, fmt.Sprintf("%d models", len(cat.Models))
					return st
				}
			}
		}
		st.State, st.Detail = StatePending, fmt.Sprintf("not %s's active gateway; syncs when selected", harnessName)
		return st
	}
	switch {
	case cat == nil:
		st.State, st.Detail = StateStale, "no cached catalogue"
		return st
	case !MatchesBaseURL(cat, gw.BaseURL):
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
	hf, _, path, err := harnessTarget(ctx, harnessName, home)
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
	hf, _, path, err := harnessTarget(ctx, harnessName, home)
	if err != nil {
		return false, err
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	harnessWrites.Lock()
	defer harnessWrites.Unlock()
	return hf.prune(path, ctx.Gateways, setOf(configured))
}

func setOf(names []string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

// staleAide reports an aide-<x> entry name whose gateway x is not kept.
func staleAide(name string, keep map[string]bool) bool {
	gw, ok := strings.CutPrefix(name, "aide-")
	return ok && !keep[gw]
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

// CanSyncHarness reports whether background sync or gateway add may
// write gwName's catalogue to harnessName's config file. For harnesses
// with named provider entries (pi, omp, opencode, codex), it is always
// true. A single-gateway-per-dir harness (claude) holds only the
// profile's saved gateway_choice for it.
func CanSyncHarness(ctx config.Context, gwName, harnessName string) bool {
	return !SingleGatewayPerDir(harnessName) || ctx.GatewayChoice[harnessName] == gwName
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

// claudeFile is Claude Code's gateway model cache,
// <claude-dir>/cache/gateway-models.json: one file per config dir, so
// it holds one gateway at a time.
type claudeFile struct{}

type claudeDoc struct {
	BaseURL   string        `json:"baseUrl"`
	FetchedAt int64         `json:"fetchedAt"`
	Models    []claudeModel `json:"models"`
}

type claudeModel struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
}

func (claudeFile) path(_ config.Context, dir, _ string) string {
	return filepath.Join(dir, "cache", "gateway-models.json")
}

func (claudeFile) singleGatewayPerDir() bool { return true }

// prune has nothing to do: the cache file holds one gateway's models.
func (claudeFile) prune(string, map[string]config.GatewayConfig, map[string]bool) (bool, error) {
	return false, nil
}

func claudeModels(gwName string, models []Model) []claudeModel {
	out := make([]claudeModel, len(models))
	for i, m := range models {
		out[i] = claudeModel{ID: m.ID, DisplayName: m.DisplayName, Description: formatClaudeDesc(gwName, m)}
	}
	return out
}

func (claudeFile) write(path, gwName string, gw config.GatewayConfig, cat *CachedCatalogue) error {
	doc := claudeDoc{
		BaseURL:   NormalizeBaseURL(gw.BaseURL),
		FetchedAt: cat.FetchedAt.UnixMilli(),
		Models:    claudeModels(gwName, cat.Models),
	}
	return fsutil.WriteJSON(path, doc)
}

func (claudeFile) read(path string) (claudeDoc, error) {
	var doc claudeDoc
	data, err := os.ReadFile(path) // #nosec G304 -- Claude's gateway cache in the profile's dir
	if err != nil {
		return doc, err
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return doc, fmt.Errorf("parsing %s: %w", path, err)
	}
	return doc, nil
}

func (c claudeFile) mismatch(path, gwName string, gw config.GatewayConfig, cat *CachedCatalogue) (string, error) {
	doc, err := c.read(path)
	if err != nil {
		return "", err
	}
	if NormalizeBaseURL(doc.BaseURL) != NormalizeBaseURL(gw.BaseURL) {
		return "points at another gateway", nil
	}
	if !slices.Equal(doc.Models, claudeModels(gwName, cat.Models)) {
		return "model list differs", nil
	}
	return "", nil
}

func (c claudeFile) remove(path, _ string, gw config.GatewayConfig) (bool, error) {
	doc, err := c.read(path)
	if err != nil {
		return false, err
	}
	if NormalizeBaseURL(doc.BaseURL) != NormalizeBaseURL(gw.BaseURL) {
		return false, nil
	}
	return true, os.Remove(path)
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
	BaseURL    string    `json:"baseUrl"`
	APIKey     string    `json:"apiKey,omitempty"`
	AuthHeader bool      `json:"authHeader"`
	API        string    `json:"api"`
	Models     []piModel `json:"models"`
}

type piModel struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ContextWindow int    `json:"contextWindow,omitempty"`
	MaxTokens     int    `json:"maxTokens,omitempty"`
}

func (piFile) path(_ config.Context, dir, _ string) string { return filepath.Join(dir, "models.json") }

func (piFile) singleGatewayPerDir() bool { return false }

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

func (piFile) write(path, gwName string, gw config.GatewayConfig, cat *CachedCatalogue) error {
	p := piProvider{
		BaseURL:    NormalizeBaseURL(gw.BaseURL),
		AuthHeader: gw.RequiresToken(),
		API:        "anthropic-messages",
		Models:     piModels(cat.Models),
	}
	if gw.RequiresToken() {
		p.APIKey = "$" + config.GatewayKeyVar(gwName)
	} else {
		p.APIKey = cmp.Or(gwName, "none")
	}
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
	ok, err := jsonEntry(doc, "providers", ProviderID(gwName), &p)
	switch {
	case err != nil:
		return "", fmt.Errorf("%s: %w", path, err)
	case !ok:
		return fmt.Sprintf("provider %q missing", ProviderID(gwName)), nil
	case p.BaseURL != NormalizeBaseURL(gw.BaseURL):
		return "baseUrl differs", nil
	case gw.RequiresToken() && (p.APIKey != "$"+config.GatewayKeyVar(gwName) || !p.AuthHeader):
		return "apiKey differs", nil
	case !gw.RequiresToken() && (!isLiteralPiPlaceholder(p.APIKey) || p.AuthHeader):
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
	if ok, err := jsonEntry(doc, "providers", name, &p); err != nil || !ok || p.BaseURL != NormalizeBaseURL(gw.BaseURL) {
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

func (ompFile) path(_ config.Context, dir, _ string) string {
	yml := filepath.Join(dir, "models.yml")
	if _, err := os.Stat(yml); errors.Is(err, os.ErrNotExist) {
		yamlAlt := filepath.Join(dir, "models.yaml")
		if _, err := os.Stat(yamlAlt); err == nil {
			return yamlAlt
		}
	}
	return yml
}

func (ompFile) singleGatewayPerDir() bool { return false }

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

func (ompFile) write(path, gwName string, gw config.GatewayConfig, cat *CachedCatalogue) error {
	p := ompProvider{
		BaseURL: NormalizeBaseURL(gw.BaseURL),
		API:     "anthropic-messages",
		Models:  ompModels(cat.Models),
	}
	if gw.RequiresToken() {
		p.AuthHeader = true
		p.APIKey = config.GatewayKeyVar(gwName)
	} else {
		p.Auth = "none"
	}
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
	ok, err := ompProviderEntry(doc, ProviderID(gwName), &p)
	switch {
	case err != nil:
		return "", fmt.Errorf("%s: %w", path, err)
	case !ok:
		return fmt.Sprintf("provider %q missing", ProviderID(gwName)), nil
	case p.BaseURL != NormalizeBaseURL(gw.BaseURL):
		return "baseUrl differs", nil
	case p.API != "anthropic-messages":
		return "api differs", nil
	case gw.RequiresToken() && (p.APIKey != config.GatewayKeyVar(gwName) || !p.AuthHeader || (p.Auth != "" && p.Auth != "apiKey")):
		return "apiKey differs", nil
	case !gw.RequiresToken() && (p.APIKey != "" || p.AuthHeader || p.Auth != "none"):
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
	if ok, err := ompProviderEntry(doc, name, &p); err != nil || !ok || p.BaseURL != NormalizeBaseURL(gw.BaseURL) {
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

func (openCodeFile) path(ctx config.Context, dir, home string) string {
	if v := ctx.Env["OPENCODE_CONFIG"]; v != "" {
		return homepath.Expand(v, home)
	}
	return filepath.Join(dir, "opencode.jsonc")
}

func (openCodeFile) singleGatewayPerDir() bool { return false }

func openCodeAPIKey(gwName string) string { return "{env:" + config.GatewayKeyVar(gwName) + "}" }

func openCodeAuthHeader(gwName string) string {
	return "Bearer " + openCodeAPIKey(gwName)
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

func openCodeModels(models []Model) map[string]openCodeModel {
	out := make(map[string]openCodeModel, len(models))
	for _, m := range models {
		om := openCodeModel{
			Name:          m.DisplayName,
			Compatibility: &openCodeCompat{},
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
	p := openCodeProvider{
		Package:  "@ai-sdk/anthropic",
		Name:     id,
		Settings: openCodeSettings{BaseURL: NormalizeBaseURL(gw.BaseURL) + "/v1"},
		Models:   openCodeModels(cat.Models),
	}
	if gw.RequiresToken() {
		p.Env = []string{config.GatewayKeyVar(gwName)}
		p.Settings.APIKey = openCodeAPIKey(gwName)
		p.Headers = map[string]string{"Authorization": openCodeAuthHeader(gwName)}
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
	ok, err := jsonEntry(doc, "providers", ProviderID(gwName), &p)
	switch {
	case err != nil:
		return "", fmt.Errorf("%s: %w", path, err)
	case !ok:
		return fmt.Sprintf("provider %q missing", ProviderID(gwName)), nil
	case p.Package != "@ai-sdk/anthropic":
		return "package differs", nil
	case gw.RequiresToken() && !slices.Equal(p.Env, []string{config.GatewayKeyVar(gwName)}):
		return "env differs", nil
	case !gw.RequiresToken() && len(p.Env) != 0:
		return "env differs", nil
	case p.Settings.BaseURL != NormalizeBaseURL(gw.BaseURL)+"/v1":
		return "baseURL differs", nil
	case gw.RequiresToken() && p.Settings.APIKey != openCodeAPIKey(gwName):
		return "apiKey differs", nil
	case !gw.RequiresToken() && p.Settings.APIKey != "":
		return "apiKey differs", nil
	case gw.RequiresToken() && p.Headers["Authorization"] != openCodeAuthHeader(gwName):
		return "authorization header differs", nil
	case !gw.RequiresToken() && p.Headers["Authorization"] != "":
		return "authorization header differs", nil
	}
	for _, m := range p.Models {
		if m.Compatibility == nil || m.Compatibility.SupportsThinkingBlockBinding {
			return "model compatibility differs", nil
		}
	}
	if !maps.EqualFunc(p.Models, openCodeModels(cat.Models), openCodeModelEqual) {
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
		wantURL := NormalizeBaseURL(gws[name].BaseURL) + "/v1"
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
	wantURL := NormalizeBaseURL(gw.BaseURL) + "/v1"
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

func (codexFile) path(_ config.Context, dir, _ string) string {
	return filepath.Join(dir, "config.toml")
}

func (codexFile) singleGatewayPerDir() bool { return false }

// codexWireAPI is the only wire_api Codex 0.167+ loads.
const codexWireAPI = "responses"

func codexProviders(doc map[string]any) map[string]any {
	providers, _ := doc["model_providers"].(map[string]any)
	return providers
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
		"base_url": NormalizeBaseURL(gw.BaseURL) + "/v1",
		"wire_api": codexWireAPI,
	}
	if gw.RequiresToken() {
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
		baseURL := NormalizeBaseURL(gws[name].BaseURL) + "/v1"
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
	case p["base_url"] != NormalizeBaseURL(gw.BaseURL)+"/v1":
		return "base_url differs", nil
	case p["wire_api"] != codexWireAPI:
		return "wire_api differs", nil
	case gw.RequiresToken() && p["env_key"] != config.GatewayKeyVar(gwName):
		return "env_key differs", nil
	case !gw.RequiresToken():
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
	baseURL := NormalizeBaseURL(gw.BaseURL) + "/v1"
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
