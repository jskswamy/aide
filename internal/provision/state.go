package provision

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/jskswamy/aide/internal/fsutil"
)

// StateVersion is the current managed.json schema version. Bump on
// breaking changes and add a migration in LoadState.
const StateVersion = 1

// ManagedItem records that aide installed a plugin or MCP server.
// Version is empty for MCP entries (they have no version concept).
// Source is set for marketplaces to cache the install ref so future
// reads can correlate repo→marketplace-name without re-querying the
// agent CLI.
type ManagedItem struct {
	InstalledAt time.Time `json:"installed_at,omitempty"`
	Version     string    `json:"version,omitempty"`
	Source      string    `json:"source,omitempty"`
}

// ManagedHook records the identity of one aide-managed hook entry.
type ManagedHook struct {
	Event   string `json:"event"`
	Matcher string `json:"matcher,omitempty"`
	Command string `json:"command"`
}

// ContextState is per-context managed item tracking. ConfigHash and
// SyncedAt are per-context so each context's drift signal is
// independent — a successful sync in one context does not silence
// the drift banner for another.
type ContextState struct {
	ConfigHash     string                 `json:"config_hash,omitempty"`
	HookConfigHash string                 `json:"hook_config_hash,omitempty"`
	SyncedAt       time.Time              `json:"synced_at,omitempty"`
	Plugins        map[string]ManagedItem `json:"plugins,omitempty"`
	MCPServers     map[string]ManagedItem `json:"mcp_servers,omitempty"`
	Marketplaces   map[string]ManagedItem `json:"marketplaces,omitempty"`
	Hooks          []ManagedHook          `json:"hooks,omitempty"`
}

// ManagedState is the on-disk shape of ~/.local/state/aide/managed.json.
// Only updated when a sync run completes successfully end-to-end.
type ManagedState struct {
	Version  int                      `json:"version"`
	Contexts map[string]*ContextState `json:"contexts,omitempty"`
}

// LoadState reads the state file at path. If the file is missing,
// returns an empty state (not an error) so first-time callers get a
// blank slate.
func LoadState(path string) (*ManagedState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &ManagedState{Version: StateVersion, Contexts: map[string]*ContextState{}}, nil
		}
		return nil, fmt.Errorf("provision: reading state %s: %w", path, err)
	}
	var st ManagedState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("provision: parsing state %s: %w", path, err)
	}
	if st.Contexts == nil {
		st.Contexts = map[string]*ContextState{}
	}
	return &st, nil
}

// SaveState atomically writes st to path. Parents are created with
// 0o750, the file ends at 0o600 (via fsutil.AtomicWrite).
func SaveState(path string, st *ManagedState) error {
	if st.Version == 0 {
		st.Version = StateVersion
	}
	if st.Contexts == nil {
		st.Contexts = map[string]*ContextState{}
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("provision: marshalling state: %w", err)
	}
	return fsutil.AtomicWrite(path, data)
}

// DefaultStatePath returns ~/.local/state/aide/managed.json given a
// home directory. Caller is responsible for HOME resolution.
func DefaultStatePath(homeDir string) string {
	return homeDir + "/.local/state/aide/managed.json"
}
