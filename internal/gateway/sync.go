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
	// Prefetch skips FetchModels and uses Models (nil is an empty catalogue).
	Prefetch bool
	Models   []Model
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
// gateway the profile's gateway_choice names for claude writes it. A fetch error leaves
// the cache and every harness file untouched. Without a deadline on
// ctx, the fetch is bounded by DefaultFetchTimeout.
func Sync(ctx context.Context, opts SyncOptions) (*SyncResult, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultFetchTimeout)
		defer cancel()
	}
	ep := Endpoint(opts.Gateway)
	if ep.Switch != nil {
		return &SyncResult{}, nil // nothing to fetch or write for a provider switch
	}
	progress := opts.Progress
	if progress == nil {
		progress = io.Discard
	}
	base := NormalizeBaseURL(ep.BaseURL)
	models := opts.Models
	if !opts.Prefetch {
		fmt.Fprintf(progress, "→ Fetching models from %s...\n", ep.ModelsURL())
		var err error
		models, err = FetchModels(ctx, opts.HTTPClient, ep, opts.Token)
		if err != nil {
			return nil, fmt.Errorf("gateway %q: %w", opts.GatewayName, err)
		}
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
	var err error
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
