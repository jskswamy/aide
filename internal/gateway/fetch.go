package gateway

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/gatewaypreset"
)

// Discovery limits.
const (
	// DefaultFetchTimeout bounds an explicit sync and a launch with no
	// cached catalogue to fall back on.
	DefaultFetchTimeout = 10 * time.Second
	// RefreshFetchTimeout bounds a launch-time refresh that can fall
	// back on an existing cache.
	RefreshFetchTimeout = 5 * time.Second
	// MaxModelsResponseBytes caps how much of a /v1/models response is
	// read.
	MaxModelsResponseBytes = 4 << 20
	// MaxNormalizedModels caps the catalogue size.
	MaxNormalizedModels = 1000

	maxIDBytes       = 256
	maxDisplayBytes  = 256
	maxProviderBytes = 128
)

// Fetch auth classification. Other non-200 statuses are not these sentinels.
var (
	ErrUnauthorized = errors.New("gateway: unauthorized")
	ErrForbidden    = errors.New("gateway: forbidden")
)

// NormalizeBaseURL trims trailing slashes and one trailing /v1 so
// endpoints are built the same way whatever the user typed.
func NormalizeBaseURL(raw string) string {
	return config.NormalizeGatewayBaseURL(raw)
}

// FetchModels lists the gateway's LLM models: GET ep.ModelsURL()
// ?limit=1000, sending token as ep.Header says (no auth header when
// token is empty).
// Redirects to another host or from https to http are refused, so the
// token only ever goes where base_url says, even with a caller-supplied
// client. The result is deduplicated, sanitized and sorted by ID.
func FetchModels(ctx context.Context, client *http.Client, ep EndpointSpec, token string) ([]Model, error) {
	if ep.Switch != nil {
		return nil, errors.New("a provider-switch gateway has no model endpoint")
	}
	c := http.Client{}
	if client != nil {
		c = *client
	}
	c.CheckRedirect = checkRedirect

	endpoint := ep.ModelsURL() + "?limit=1000"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building request for %s: %w", endpoint, err)
	}
	switch {
	case token == "" || ep.Keyless:
	case ep.Header == gatewaypreset.HeaderXAPIKey:
		req.Header.Set("x-api-key", token)
	default:
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if ep.API == gatewaypreset.APIAnthropicMessages {
		req.Header.Set("anthropic-version", "2023-06-01")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("%s returned HTTP %d", endpoint, resp.StatusCode)
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			return nil, fmt.Errorf("%w: %w", err, ErrUnauthorized)
		case http.StatusForbidden:
			return nil, fmt.Errorf("%w: %w", err, ErrForbidden)
		default:
			return nil, err
		}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxModelsResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", endpoint, err)
	}
	if len(body) > MaxModelsResponseBytes {
		return nil, fmt.Errorf("%s response exceeds %d MiB", endpoint, MaxModelsResponseBytes>>20)
	}
	return parseModels(body)
}

// checkRedirect keeps a redirect on the original host and scheme
// family: no other host, no https -> http downgrade.
func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("gateway: stopped after 10 redirects")
	}
	first := via[0].URL
	if req.URL.Host != first.Host {
		return fmt.Errorf("gateway: refusing redirect from %s to another host (%s)", first.Host, req.URL.Host)
	}
	if first.Scheme == "https" && req.URL.Scheme != "https" {
		return fmt.Errorf("gateway: refusing redirect from https to %s", req.URL.Scheme)
	}
	return nil
}

// parseModels accepts {"data": [...]} or a bare array, keeps enabled LLM
// entries, and projects each onto Model. Every other field (including
// any upstream credential a gateway returns) is dropped.
func parseModels(body []byte) ([]Model, error) {
	var entries []map[string]any
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		if err := json.Unmarshal(trimmed, &entries); err != nil {
			return nil, fmt.Errorf("parsing model list: %w", err)
		}
	} else {
		var wrapped struct {
			Data []map[string]any `json:"data"`
		}
		if err := json.Unmarshal(trimmed, &wrapped); err != nil {
			return nil, fmt.Errorf("parsing model list: %w", err)
		}
		entries = wrapped.Data
	}

	seen := map[string]bool{}
	var models []Model
	for _, e := range entries {
		if t, ok := e["model_type"].(string); ok && t != "llm" {
			continue
		}
		if enabled, ok := e["is_enabled"].(bool); ok && !enabled {
			continue
		}
		id := Sanitize(firstString(e, "slug", "id", "remote_model"))
		if id == "" || len(id) > maxIDBytes || seen[id] {
			continue
		}
		seen[id] = true
		models = append(models, Model{
			ID:              id,
			DisplayName:     truncate(Sanitize(cmp.Or(firstString(e, "name", "display_name"), id)), maxDisplayBytes),
			Provider:        truncate(Sanitize(firstString(e, "provider", "owned_by")), maxProviderBytes),
			ContextWindow:   firstInt(e, "max_context_tokens", "context_window", "context_length"),
			MaxOutputTokens: firstInt(e, "max_output_tokens", "max_tokens"),
			Cost:            parseCost(e),
		})
	}
	slices.SortFunc(models, func(a, b Model) int { return strings.Compare(a.ID, b.ID) })
	if len(models) > MaxNormalizedModels {
		models = models[:MaxNormalizedModels]
	}
	return models, nil
}

func firstString(e map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := e[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// firstInt returns the first positive-or-zero number under keys;
// negative values clamp to 0.
func firstInt(e map[string]any, keys ...string) int {
	for _, k := range keys {
		if f, ok := e[k].(float64); ok {
			return int(max(f, 0))
		}
	}
	return 0
}

// parseCost extracts optional input/output per-million-token USD pricing
// from common gateway /v1/models shapes:
//   - OpenRouter: pricing.{prompt,completion} (or input/output), USD/token
//   - LiteLLM: input_cost_per_token + output_cost_per_token (top-level or
//     under model_info), USD/token
//   - models.dev / Pi / OpenCode: cost.{input,output}, USD/Mtok
//
// Missing, non-finite, or negative values (such as OpenRouter's "-1"
// dynamic-pricing sentinel) return nil.
func parseCost(e map[string]any) *ModelCost {
	if p, ok := e["pricing"].(map[string]any); ok {
		in, okIn := firstNonNegFloat(p, "prompt", "input")
		out, okOut := firstNonNegFloat(p, "completion", "output")
		if okIn && okOut {
			return newModelCost(in*1e6, out*1e6)
		}
	}
	if in, okIn := firstNonNegFloat(e, "input_cost_per_token"); okIn {
		if out, okOut := firstNonNegFloat(e, "output_cost_per_token"); okOut {
			return newModelCost(in*1e6, out*1e6)
		}
	}
	if info, ok := e["model_info"].(map[string]any); ok {
		if in, okIn := firstNonNegFloat(info, "input_cost_per_token"); okIn {
			if out, okOut := firstNonNegFloat(info, "output_cost_per_token"); okOut {
				return newModelCost(in*1e6, out*1e6)
			}
		}
	}
	if c, ok := e["cost"].(map[string]any); ok {
		in, okIn := firstNonNegFloat(c, "input")
		out, okOut := firstNonNegFloat(c, "output")
		if okIn && okOut {
			return newModelCost(in, out)
		}
	}
	return nil
}

func newModelCost(inPerMtok, outPerMtok float64) *ModelCost {
	in := roundPrice(inPerMtok)
	out := roundPrice(outPerMtok)
	if math.IsNaN(in) || math.IsInf(in, 0) || math.IsNaN(out) || math.IsInf(out, 0) {
		return nil
	}
	return &ModelCost{
		InputPerMtok:  in,
		OutputPerMtok: out,
	}
}

func roundPrice(v float64) float64 {
	return math.Round(v*1e4) / 1e4
}

// firstNonNegFloat parses the first finite, non-negative float (JSON
// number or numeric string) under keys.
func firstNonNegFloat(m map[string]any, keys ...string) (float64, bool) {
	for _, k := range keys {
		raw, ok := m[k]
		if !ok {
			continue
		}
		var v float64
		switch t := raw.(type) {
		case float64:
			v = t
		case string:
			parsed, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
			if err != nil {
				continue
			}
			v = parsed
		default:
			continue
		}
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			continue
		}
		return v, true
	}
	return 0, false
}

// Sanitize drops invalid UTF-8 and C0/C1 control characters, which
// would otherwise let a gateway drive the user's terminal.
func Sanitize(s string) string {
	s = strings.ToValidUTF8(s, "")
	return strings.Map(func(r rune) rune {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return -1
		}
		return r
	}, s)
}

// truncate cuts s to at most n bytes on a rune boundary.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
