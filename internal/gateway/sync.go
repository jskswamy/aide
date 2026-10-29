package gateway

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/homepath"
)

// SyncOptions is one gateway sync: which gateway, on which profile,
// with which (already decrypted) token.
type SyncOptions struct {
	ProfileName string
	Profile     config.Context
	GatewayName string
	Gateway     config.GatewayConfig
	Token       string
	HomeDir     string
	HTTPClient  *http.Client
	// Progress receives one line per step; nil is silent.
	Progress io.Writer
}

// SyncResult is the fresh catalogue and each harness's file status.
type SyncResult struct {
	Catalogue       *CachedCatalogue
	HarnessStatuses []HarnessFileStatus
}

// Sync fetches the gateway's models, caches them, and writes them into
// the config file of every harness the gateway serves whose dir
// already exists (the others stay pending until their first launch).
// Claude Code reads one gateway file per config dir, so only the
// gateway that is the profile's default for claude writes it; a launch
// that selects another gateway writes it then. A fetch error leaves
// the cache and every harness file untouched. Without a deadline on
// ctx, the fetch is bounded by DefaultFetchTimeout.
func Sync(ctx context.Context, opts SyncOptions) (*SyncResult, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultFetchTimeout)
		defer cancel()
	}
	progress := opts.Progress
	if progress == nil {
		progress = io.Discard
	}
	base := NormalizeBaseURL(opts.Gateway.BaseURL)
	fmt.Fprintf(progress, "→ Fetching models from %s/v1/models...\n", base)
	models, err := FetchModels(ctx, opts.HTTPClient, base, opts.Token)
	if err != nil {
		return nil, fmt.Errorf("gateway %q: %w", opts.GatewayName, err)
	}
	cat := &CachedCatalogue{
		Profile:   opts.ProfileName,
		Gateway:   opts.GatewayName,
		BaseURL:   base,
		FetchedAt: time.Now().UTC(),
		Models:    models,
	}
	if err := WriteCache(opts.HomeDir, cat); err != nil {
		return nil, err
	}
	cachePath, _ := CachePath(opts.HomeDir, opts.ProfileName, opts.GatewayName)
	fmt.Fprintf(progress, "✓ Cached %d LLM models in %s\n", len(models), homepath.Tilde(cachePath, opts.HomeDir))

	res := &SyncResult{Catalogue: cat}
	for _, h := range opts.Gateway.EffectiveHarnesses() {
		var st HarnessFileStatus
		if !CanSyncHarness(opts.Profile, opts.GatewayName, h) {
			st = InspectHarness(opts.Profile, opts.GatewayName, opts.Gateway, cat, h, opts.HomeDir)
		} else if st, err = MaterializeHarness(opts.Profile, opts.GatewayName, opts.Gateway, cat, h, opts.HomeDir, false); err != nil {
			st.State, st.Detail = StateStale, err.Error()
		}
		res.HarnessStatuses = append(res.HarnessStatuses, st)
	}
	return res, nil
}
