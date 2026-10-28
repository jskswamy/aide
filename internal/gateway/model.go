// Package gateway discovers a custom LLM gateway's models, caches the
// normalized catalogue under aide's state directory, and writes it into
// each harness's own config (Claude Code, Pi, OpenCode and Codex) so
// their model pickers list the gateway's models. Bearer tokens never
// reach any file this package writes: harness configs name the
// variable aide injects the token as.
package gateway

import "time"

// Model is one gateway model, reduced to display metadata.
type Model struct {
	ID              string `json:"id"`
	DisplayName     string `json:"display_name"`
	Provider        string `json:"provider,omitempty"`
	ContextWindow   int    `json:"context_window,omitempty"`
	MaxOutputTokens int    `json:"max_output_tokens,omitempty"`
}

// CachedCatalogue is the cache file for one gateway on one profile.
type CachedCatalogue struct {
	Profile   string    `json:"profile"`
	Gateway   string    `json:"gateway"`
	BaseURL   string    `json:"base_url"`
	FetchedAt time.Time `json:"fetched_at"`
	Models    []Model   `json:"models"`
}

// HarnessSyncState is whether a harness's config file carries the
// gateway's current catalogue.
type HarnessSyncState string

// Harness sync states.
const (
	// StateSynced: the file matches the fresh cached catalogue.
	StateSynced HarnessSyncState = "synced"
	// StateStale: the dir exists but the file is missing, unreadable,
	// different from the catalogue, or the catalogue has expired.
	StateStale HarnessSyncState = "stale"
	// StatePending: the harness's profile dir does not exist yet; the
	// file is written on its first launch.
	StatePending HarnessSyncState = "pending"
)

// HarnessFileStatus is one harness's config file and its state.
type HarnessFileStatus struct {
	Harness  string           `json:"harness"`
	FilePath string           `json:"file_path"`
	State    HarnessSyncState `json:"state"`
	Detail   string           `json:"detail,omitempty"`
}
