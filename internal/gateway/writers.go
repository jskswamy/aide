package gateway

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/fsutil"
	"github.com/jskswamy/aide/internal/harness"
	"github.com/jskswamy/aide/internal/homepath"
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
	write(path, gwName string, gw config.GatewayConfig, cat *CachedCatalogue, isDefault bool) error
	// mismatch describes how the file differs from cat, or "" when it
	// carries the catalogue.
	mismatch(path, gwName string, gw config.GatewayConfig, cat *CachedCatalogue) (string, error)
	remove(path, gwName string, gw config.GatewayConfig) (bool, error)
}

var harnessFiles = map[string]harnessFile{
	"claude":   claudeFile{},
	"pi":       piFile{},
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
// inherited environment, then the harness default.
func HarnessConfigDir(ctx config.Context, harnessName, home string) (string, error) {
	if _, err := lookupHarnessFile(harnessName); err != nil {
		return "", err
	}
	if ctx.SelectsConfigDir() {
		return ctx.NativeConfigDir(harnessName, home), nil
	}
	d, ok := harness.Lookup(harnessName)
	if !ok || d.ProfileEnvKey == "" || d.DefaultConfigDir == "" {
		return "", fmt.Errorf("gateway: %s has no known config dir", harnessName)
	}
	if v := ctx.Env[d.ProfileEnvKey]; v != "" {
		return homepath.Expand(v, home), nil
	}
	if v := os.Getenv(d.ProfileEnvKey); v != "" {
		return homepath.Expand(v, home), nil
	}
	return d.DefaultDir(home), nil
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

// MaterializeHarness writes cat into harnessName's config file. When
// the harness's dir does not exist and createDirIfMissing is false,
// nothing is written and the status is pending: aide leaves a harness
// dir to its first launch.
func MaterializeHarness(ctx config.Context, gwName string, gw config.GatewayConfig, cat *CachedCatalogue, harnessName, home string, createDirIfMissing bool) (HarnessFileStatus, error) {
	hf, dir, path, err := harnessTarget(ctx, harnessName, home)
	if err != nil {
		return HarnessFileStatus{Harness: harnessName, EnvVar: gw.TokenEnv(harnessName)}, err
	}
	st := HarnessFileStatus{Harness: harnessName, EnvVar: gw.TokenEnv(harnessName), FilePath: path}
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
	if err := hf.write(path, gwName, gw, cat, IsDefaultGateway(ctx, gwName, harnessName)); err != nil {
		return st, err
	}
	st.State, st.Detail = StateSynced, fmt.Sprintf("%d models", len(cat.Models))
	return st, nil
}

// InspectHarness reports whether harnessName's config file carries the
// fresh catalogue cat for gwName.
func InspectHarness(ctx config.Context, gwName string, gw config.GatewayConfig, cat *CachedCatalogue, harnessName, home string) HarnessFileStatus {
	hf, dir, path, err := harnessTarget(ctx, harnessName, home)
	st := HarnessFileStatus{Harness: harnessName, EnvVar: gw.TokenEnv(harnessName), FilePath: path}
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

// IsDefaultGateway reports whether gwName is a default credential of
// ctx for harnessName, synthesizing gateway credentials unless ctx
// already carries them (as a launch's profile does). A context that
// does not declare gwName, such as a single gateway passed on its own,
// counts as its default.
func IsDefaultGateway(ctx config.Context, gwName, harnessName string) bool {
	_, inGW := ctx.Gateways[gwName]
	_, inCred := ctx.Credentials[gwName]
	if !inGW && !inCred {
		return true
	}
	for g := range ctx.Gateways {
		if _, synthesized := ctx.Credentials[g]; !synthesized {
			var err error
			if ctx, err = config.SynthesizeGatewayCredentials(ctx); err != nil {
				return false
			}
			break
		}
	}
	if harnessName == "" {
		return slices.Contains(ctx.DefaultCredentials, gwName)
	}
	return slices.Contains(config.DefaultCredentialsForHarness(ctx, harnessName), gwName)
}

// CanSyncHarness reports whether background sync or gateway add may
// write gwName's catalogue to harnessName's config file. For harnesses
// with named provider entries (pi, opencode, codex), it is always true.
// For single-gateway-per-dir harnesses (claude), it is true when gwName
// is Claude's default gateway, or when no custom gateway in
// ctx.Gateways is Claude's default and gwName is the first custom
// gateway in sorted order that serves claude.
func CanSyncHarness(ctx config.Context, gwName, harnessName string) bool {
	if !SingleGatewayPerDir(harnessName) {
		return true
	}
	if IsDefaultGateway(ctx, gwName, harnessName) {
		return true
	}
	names := slices.Sorted(maps.Keys(ctx.Gateways))
	for _, name := range names {
		if IsDefaultGateway(ctx, name, harnessName) {
			return false
		}
	}
	for _, name := range names {
		if slices.Contains(ctx.Gateways[name].EffectiveHarnesses(), harnessName) {
			return name == gwName
		}
	}
	return false
}

func dirExists(dir string) bool {
	info, err := os.Stat(dir)
	return err == nil && info.IsDir()
}

func modelIDs(models []Model) []string {
	ids := make([]string, len(models))
	for i, m := range models {
		ids[i] = m.ID
	}
	return ids
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

func (claudeFile) write(path, _ string, gw config.GatewayConfig, cat *CachedCatalogue, _ bool) error {
	doc := claudeDoc{BaseURL: NormalizeBaseURL(gw.BaseURL), FetchedAt: cat.FetchedAt.UnixMilli(), Models: []claudeModel{}}
	for _, m := range cat.Models {
		doc.Models = append(doc.Models, claudeModel{ID: m.ID, DisplayName: m.DisplayName, Description: formatClaudeDesc(m)})
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

func (c claudeFile) mismatch(path, _ string, gw config.GatewayConfig, cat *CachedCatalogue) (string, error) {
	doc, err := c.read(path)
	if err != nil {
		return "", err
	}
	if NormalizeBaseURL(doc.BaseURL) != NormalizeBaseURL(gw.BaseURL) {
		return "points at another gateway", nil
	}
	ids := make([]string, len(doc.Models))
	for i, m := range doc.Models {
		ids[i] = m.ID
	}
	if !slices.Equal(ids, modelIDs(cat.Models)) {
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

// formatClaudeDesc is the /model picker's description line.
func formatClaudeDesc(m Model) string {
	switch {
	case m.Provider != "" && m.ContextWindow > 0:
		return fmt.Sprintf("%s · %dK context", m.Provider, m.ContextWindow/1000)
	case m.Provider != "":
		return m.Provider
	case m.ContextWindow > 0:
		return fmt.Sprintf("%dK context", m.ContextWindow/1000)
	default:
		return "Custom gateway model"
	}
}

// piFile is Pi's <pi-dir>/models.json; a gateway is providers.<name>.
type piFile struct{}

type piProvider struct {
	BaseURL    string    `json:"baseUrl"`
	APIKey     string    `json:"apiKey"`
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

func (piFile) write(path, gwName string, gw config.GatewayConfig, cat *CachedCatalogue, _ bool) error {
	p := piProvider{
		BaseURL:    NormalizeBaseURL(gw.BaseURL),
		APIKey:     "$" + gw.EnvVarName(),
		AuthHeader: true,
		API:        "anthropic-messages",
		Models:     []piModel{},
	}
	for _, m := range cat.Models {
		p.Models = append(p.Models, piModel{ID: m.ID, Name: m.DisplayName, ContextWindow: m.ContextWindow, MaxTokens: m.MaxOutputTokens})
	}
	_, err := updateJSONEntry(path, "providers", gwName, p)
	return err
}

func (piFile) mismatch(path, gwName string, gw config.GatewayConfig, cat *CachedCatalogue) (string, error) {
	doc, err := fsutil.ReadJSONCRawMap(path)
	if err != nil {
		return "", err
	}
	var p piProvider
	ok, err := jsonEntry(doc, "providers", gwName, &p)
	switch {
	case err != nil:
		return "", fmt.Errorf("%s: %w", path, err)
	case !ok:
		return fmt.Sprintf("provider %q missing", gwName), nil
	case p.BaseURL != NormalizeBaseURL(gw.BaseURL):
		return "baseUrl differs", nil
	case p.APIKey != "$"+gw.EnvVarName():
		return "apiKey differs", nil
	}
	ids := make([]string, len(p.Models))
	for i, m := range p.Models {
		ids[i] = m.ID
	}
	if !slices.Equal(ids, modelIDs(cat.Models)) {
		return "model list differs", nil
	}
	return "", nil
}

// remove leaves an entry pointing at another URL: it belongs to
// another profile's gateway or to the user.
func (piFile) remove(path, gwName string, gw config.GatewayConfig) (bool, error) {
	doc, err := fsutil.ReadJSONCRawMap(path)
	if err != nil {
		return false, err
	}
	var p piProvider
	if ok, err := jsonEntry(doc, "providers", gwName, &p); err != nil || !ok || p.BaseURL != NormalizeBaseURL(gw.BaseURL) {
		return false, err
	}
	return updateJSONEntry(path, "providers", gwName, nil)
}

// openCodeFile is OpenCode's <opencode-dir>/opencode.jsonc (or
// $OPENCODE_CONFIG when set); a gateway is providers.<name>. Comments
// are accepted on read but not preserved on write, as with aide's MCP
// provisioning of the same file.
type openCodeFile struct{}

type openCodeProvider struct {
	Package  string                   `json:"package"`
	Name     string                   `json:"name"`
	Env      []string                 `json:"env"`
	Settings openCodeSettings         `json:"settings"`
	Headers  map[string]string        `json:"headers"`
	Models   map[string]openCodeModel `json:"models"`
}

type openCodeSettings struct {
	BaseURL string `json:"baseURL"`
	APIKey  string `json:"apiKey"`
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
	if !ctx.SelectsConfigDir() {
		if v := os.Getenv("OPENCODE_CONFIG"); v != "" {
			return homepath.Expand(v, home)
		}
	}
	return filepath.Join(dir, "opencode.jsonc")
}

func (openCodeFile) singleGatewayPerDir() bool { return false }

func openCodeAPIKey(gw config.GatewayConfig) string { return "{env:" + gw.EnvVarName() + "}" }

func openCodeAuthHeader(gw config.GatewayConfig) string {
	return "Bearer " + openCodeAPIKey(gw)
}

func defaultOpenCodeModelID(models []Model) string {
	var best string
	for _, m := range models {
		if strings.Contains(strings.ToLower(m.ID), "sonnet") {
			best = m.ID
		}
	}
	if best != "" {
		return best
	}
	return models[0].ID
}

func (openCodeFile) write(path, gwName string, gw config.GatewayConfig, cat *CachedCatalogue, isDefault bool) error {
	doc, err := fsutil.ReadJSONCRawMap(path)
	if err != nil {
		return err
	}
	p := openCodeProvider{
		Package:  "@ai-sdk/anthropic",
		Name:     gwName,
		Env:      []string{gw.EnvVarName()},
		Settings: openCodeSettings{BaseURL: NormalizeBaseURL(gw.BaseURL) + "/v1", APIKey: openCodeAPIKey(gw)},
		Headers:  map[string]string{"Authorization": openCodeAuthHeader(gw)},
		Models:   map[string]openCodeModel{},
	}
	for _, m := range cat.Models {
		om := openCodeModel{
			Name:          m.DisplayName,
			Compatibility: &openCodeCompat{},
		}
		if m.ContextWindow > 0 || m.MaxOutputTokens > 0 {
			om.Limit = &openCodeLimit{Context: m.ContextWindow, Output: m.MaxOutputTokens}
		}
		p.Models[m.ID] = om
	}
	if _, err := setJSONEntry(doc, "providers", gwName, p); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	// Clean up any legacy V1 "provider.<gwName>" entry in the same file.
	_, _ = setJSONEntry(doc, "provider", gwName, nil)

	if isDefault && len(cat.Models) > 0 {
		var curModel string
		if raw, ok := doc["model"]; ok {
			_ = json.Unmarshal(raw, &curModel)
		}
		curProv, curModID, hasSlash := strings.Cut(curModel, "/")
		shouldSet := curModel == ""
		if !shouldSet && hasSlash {
			if curProv == gwName {
				_, exists := p.Models[curModID]
				shouldSet = !exists
			} else {
				var existing json.RawMessage
				if ok, err := jsonEntry(doc, "providers", curProv, &existing); err == nil && ok {
					shouldSet = true
				}
			}
		}
		if shouldSet {
			if raw, err := json.Marshal(gwName + "/" + defaultOpenCodeModelID(cat.Models)); err == nil {
				doc["model"] = raw
			}
		}
	}
	return fsutil.WriteJSON(path, doc)
}

func (openCodeFile) mismatch(path, gwName string, gw config.GatewayConfig, cat *CachedCatalogue) (string, error) {
	doc, err := fsutil.ReadJSONCRawMap(path)
	if err != nil {
		return "", err
	}
	var p openCodeProvider
	ok, err := jsonEntry(doc, "providers", gwName, &p)
	switch {
	case err != nil:
		return "", fmt.Errorf("%s: %w", path, err)
	case !ok:
		return fmt.Sprintf("provider %q missing", gwName), nil
	case p.Package != "@ai-sdk/anthropic":
		return "package differs", nil
	case !slices.Equal(p.Env, []string{gw.EnvVarName()}):
		return "env differs", nil
	case p.Settings.BaseURL != NormalizeBaseURL(gw.BaseURL)+"/v1":
		return "baseURL differs", nil
	case p.Settings.APIKey != openCodeAPIKey(gw):
		return "apiKey differs", nil
	case p.Headers["Authorization"] != openCodeAuthHeader(gw):
		return "authorization header differs", nil
	}
	ids := make([]string, 0, len(p.Models))
	for id, m := range p.Models {
		if m.Compatibility == nil || m.Compatibility.SupportsThinkingBlockBinding {
			return "model compatibility differs", nil
		}
		ids = append(ids, id)
	}
	slices.Sort(ids)
	if !slices.Equal(ids, modelIDs(cat.Models)) {
		return "model list differs", nil
	}
	return "", nil
}

// remove leaves an entry pointing at another URL: it belongs to
// another profile's gateway or to the user.
func (openCodeFile) remove(path, gwName string, gw config.GatewayConfig) (bool, error) {
	doc, err := fsutil.ReadJSONCRawMap(path)
	if err != nil {
		return false, err
	}
	wantURL := NormalizeBaseURL(gw.BaseURL) + "/v1"
	var p openCodeProvider
	ok, err := jsonEntry(doc, "providers", gwName, &p)
	if err != nil {
		return false, err
	}
	matched := ok && p.Settings.BaseURL == wantURL
	if !matched {
		// Also check legacy V1 "provider.<gwName>" entries.
		var legacy struct {
			Options openCodeSettings `json:"options"`
		}
		if legOK, legErr := jsonEntry(doc, "provider", gwName, &legacy); legErr != nil {
			return false, legErr
		} else if legOK && legacy.Options.BaseURL == wantURL {
			matched = true
		}
	}
	if !matched {
		return false, nil
	}
	_, _ = setJSONEntry(doc, "providers", gwName, nil)
	_, _ = setJSONEntry(doc, "provider", gwName, nil)
	var curModel string
	if raw, hasModel := doc["model"]; hasModel && json.Unmarshal(raw, &curModel) == nil {
		if strings.HasPrefix(curModel, gwName+"/") {
			delete(doc, "model")
		}
	}
	return true, fsutil.WriteJSON(path, doc)
}

// codexFile is Codex's <codex-dir>/config.toml; a gateway is
// [model_providers.<name>]. Codex speaks the OpenAI wire protocol, so
// the gateway must also serve /v1/chat/completions. Comments are not
// preserved on write, as with aide's MCP provisioning of the same
// file.
type codexFile struct{}

func (codexFile) path(_ config.Context, dir, _ string) string {
	return filepath.Join(dir, "config.toml")
}

func (codexFile) singleGatewayPerDir() bool { return false }

func codexProviders(doc map[string]any) map[string]any {
	providers, _ := doc["model_providers"].(map[string]any)
	return providers
}

func (c codexFile) write(path, gwName string, gw config.GatewayConfig, _ *CachedCatalogue, isDefault bool) error {
	doc, err := fsutil.ReadTOML(path)
	if err != nil {
		return err
	}
	providers := codexProviders(doc)
	if providers == nil {
		providers = map[string]any{}
	}
	providers[gwName] = map[string]any{
		"name":     gwName,
		"base_url": NormalizeBaseURL(gw.BaseURL) + "/v1",
		"env_key":  gw.EnvVarName(),
		"wire_api": "chat",
	}
	doc["model_providers"] = providers
	if isDefault {
		cur, _ := doc["model_provider"].(string)
		if cur == "" {
			doc["model_provider"] = gwName
		} else if _, managed := providers[cur]; managed {
			doc["model_provider"] = gwName
		}
	}
	return fsutil.WriteTOML(path, doc)
}

func (c codexFile) mismatch(path, gwName string, gw config.GatewayConfig, _ *CachedCatalogue) (string, error) {
	doc, err := fsutil.ReadTOML(path)
	if err != nil {
		return "", err
	}
	p, ok := codexProviders(doc)[gwName].(map[string]any)
	switch {
	case !ok:
		return fmt.Sprintf("model provider %q missing", gwName), nil
	case p["base_url"] != NormalizeBaseURL(gw.BaseURL)+"/v1":
		return "base_url differs", nil
	case p["env_key"] != gw.EnvVarName():
		return "env_key differs", nil
	}
	return "", nil
}

// remove leaves an entry pointing at another URL: it belongs to
// another profile's gateway or to the user.
func (c codexFile) remove(path, gwName string, gw config.GatewayConfig) (bool, error) {
	doc, err := fsutil.ReadTOML(path)
	if err != nil {
		return false, err
	}
	providers := codexProviders(doc)
	p, ok := providers[gwName].(map[string]any)
	if !ok || p["base_url"] != NormalizeBaseURL(gw.BaseURL)+"/v1" {
		return false, nil
	}
	delete(providers, gwName)
	if len(providers) == 0 {
		delete(doc, "model_providers")
	}
	if doc["model_provider"] == gwName {
		delete(doc, "model_provider")
	}
	return true, fsutil.WriteTOML(path, doc)
}
