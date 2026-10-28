package gateway

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jskswamy/aide/internal/config"
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

// NormalizeBaseURL trims trailing slashes and one trailing /v1 so
// endpoints are built the same way whatever the user typed.
func NormalizeBaseURL(raw string) string {
	return config.NormalizeGatewayBaseURL(raw)
}

// FetchModels lists the gateway's LLM models: GET
// <base>/v1/models?limit=1000 with the token as a Bearer credential.
// Redirects to another host or from https to http are refused, so the
// token only ever goes where base_url says, even with a caller-supplied
// client. The result is deduplicated, sanitized and sorted by ID.
func FetchModels(ctx context.Context, client *http.Client, baseURL, token string) ([]Model, error) {
	c := http.Client{}
	if client != nil {
		c = *client
	}
	c.CheckRedirect = checkRedirect

	endpoint := NormalizeBaseURL(baseURL) + "/v1/models?limit=1000"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building request for %s: %w", endpoint, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Accept", "application/json")

	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned HTTP %d", endpoint, resp.StatusCode)
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
