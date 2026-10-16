package provision

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/jskswamy/aide/internal/fsutil"
	"github.com/jskswamy/aide/internal/statedir"
)

// StateVersion is the current managed.json schema version. Bump on
// breaking changes and add a migration in LoadState.
//
// Version 2 keys ownership under "profiles" (version 1 used
// "contexts"; LoadState still reads it).
const StateVersion = 2

// ErrStateVersion reports a state file written by a newer aide. It is
// never overwritten.
var ErrStateVersion = errors.New("unsupported provisioning state version")

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
	ConfigHash     string `json:"config_hash,omitempty"`
	HookConfigHash string `json:"hook_config_hash,omitempty"`
	// SecretsHash is sha256 of the context's encrypted secrets file
	// bytes (ciphertext only, never plaintext) — lets aide sync skip
	// decryption when neither this nor ConfigHash changed since the
	// last successful sync. Empty when the context has no secret file.
	SecretsHash string    `json:"secrets_hash,omitempty"`
	SyncedAt    time.Time `json:"synced_at,omitempty"`
	// AgentDir is the agent config dir the last successful sync
	// provisioned into (empty for agents without one). Managed MCP
	// entries only vouch for that dir; see runSync.
	AgentDir     string                 `json:"agent_dir,omitempty"`
	Plugins      map[string]ManagedItem `json:"plugins,omitempty"`
	MCPServers   map[string]ManagedItem `json:"mcp_servers,omitempty"`
	Marketplaces map[string]ManagedItem `json:"marketplaces,omitempty"`
	Hooks        []ManagedHook          `json:"hooks,omitempty"`
}

// ManagedState is the on-disk shape of managed.json (DefaultStatePath).
// Only updated when a sync run completes successfully end-to-end.
// Contexts is keyed by profile name and written as "profiles".
type ManagedState struct {
	Version  int                      `json:"version"`
	Contexts map[string]*ContextState `json:"profiles,omitempty"`
}

// UnmarshalJSON reads both the "profiles" key and the version 1
// "contexts" key, so ownership, drift hashes and agent dirs recorded
// before the rename are kept. An entry under "profiles" wins over one
// of the same name under "contexts".
func (s *ManagedState) UnmarshalJSON(data []byte) error {
	var raw struct {
		Version  int                      `json:"version"`
		Profiles map[string]*ContextState `json:"profiles"`
		Contexts map[string]*ContextState `json:"contexts"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	s.Version = raw.Version
	s.Contexts = raw.Profiles
	if s.Contexts == nil {
		s.Contexts = map[string]*ContextState{}
	}
	for name, cs := range raw.Contexts {
		if _, ok := s.Contexts[name]; !ok {
			s.Contexts[name] = cs
		}
	}
	return nil
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
	if st.Version > StateVersion {
		return nil, fmt.Errorf("%w: %s has version %d, but this aide reads up to version %d; upgrade aide "+
			"(the file is left unchanged)", ErrStateVersion, path, st.Version, StateVersion)
	}
	if st.Contexts == nil {
		st.Contexts = map[string]*ContextState{}
	}
	return &st, nil
}

// SaveState atomically writes st to path in the current format
// (StateVersion, "profiles" key). Parents are created with 0o750, the
// file ends at 0o600 (via fsutil.AtomicWrite).
func SaveState(path string, st *ManagedState) error {
	st.Version = StateVersion
	if st.Contexts == nil {
		st.Contexts = map[string]*ContextState{}
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("provision: marshalling state: %w", err)
	}
	return fsutil.AtomicWrite(path, data)
}

// DefaultStatePath returns managed.json in aide's state directory
// ($XDG_STATE_HOME/aide or ~/.local/state/aide; see statedir.Path)
// given a home directory. Caller is responsible for HOME resolution.
func DefaultStatePath(homeDir string) string {
	return statedir.Path(homeDir, "managed.json")
}

// RehashConfig moves every profile whose recorded config_hash is
// oldHash to newHash and saves the state, returning how many moved.
// Used after aide rewrites config.yaml without changing what it
// declares (profile migration), so the launch drift check does not
// report a change the user never made. Profiles recorded against
// other bytes have drifted already and keep their hash. A missing
// state file is left missing.
func RehashConfig(path, oldHash, newHash string) (int, error) {
	if oldHash == "" || oldHash == newHash {
		return 0, nil
	}
	st, err := LoadState(path)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, cs := range st.Contexts {
		if cs != nil && cs.ConfigHash == oldHash {
			cs.ConfigHash = newHash
			n++
		}
	}
	if n == 0 {
		return 0, nil
	}
	return n, SaveState(path, st)
}
