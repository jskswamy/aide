// Package prefs stores user-local repository preferences: the default
// harness and the selected credential binding names for a repository,
// keyed by profile and normalized origin remote. It never stores
// credential values or model choices, and lives in aide's state
// directory, never in the repository or the user config.
package prefs

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jskswamy/aide/internal/fsutil"
)

// Version is the preferences file format this aide reads and writes.
const Version = 1

// ErrUnsupportedVersion reports a preferences file written by a newer
// aide. It is never overwritten.
var ErrUnsupportedVersion = errors.New("unsupported preferences file version")

// ErrNoRepositoryIdentity reports a directory without an origin remote,
// which preferences cannot be keyed on.
var ErrNoRepositoryIdentity = errors.New("no repository identity")

// Key identifies a repository under one profile. Remote is the
// normalized origin remote (aidectx.CurrentRemote), which never holds
// credentials.
type Key struct {
	Profile string
	Remote  string
}

// Validate reports a key that cannot hold preferences.
func (k Key) Validate() error {
	if k.Profile == "" {
		return errors.New("no profile selected")
	}
	if k.Remote == "" {
		return fmt.Errorf("%w: this directory is not a git repository with an origin remote, "+
			"so there is nothing to save repository preferences under", ErrNoRepositoryIdentity)
	}
	return nil
}

// Preference is what is saved for one repository. A nil Credentials is
// unset (the profile default applies); a non-nil empty one is an
// explicit selection of no credentials, as for native-login launches.
type Preference struct {
	Harness     string    `json:"harness,omitempty"`
	Credentials *[]string `json:"credentials,omitempty"`
}

// Selection returns names as an explicit credential selection, empty
// when names is empty.
func Selection(names ...string) *[]string {
	s := append([]string{}, names...)
	return &s
}

// IsZero reports a preference with nothing set.
func (p Preference) IsZero() bool {
	return p.Harness == "" && p.Credentials == nil
}

// File is the on-disk preferences document: profile name, then
// normalized remote.
type File struct {
	Version  int                              `json:"version"`
	Profiles map[string]map[string]Preference `json:"profiles,omitempty"`
}

// Get returns the preference saved for k.
func (f *File) Get(k Key) (Preference, bool) {
	p, ok := f.Profiles[k.Profile][k.Remote]
	return p, ok
}

// Set saves p for k; a zero p removes the entry.
func (f *File) Set(k Key, p Preference) {
	if p.IsZero() {
		delete(f.Profiles[k.Profile], k.Remote)
		if len(f.Profiles[k.Profile]) == 0 {
			delete(f.Profiles, k.Profile)
		}
		return
	}
	if f.Profiles == nil {
		f.Profiles = map[string]map[string]Preference{}
	}
	if f.Profiles[k.Profile] == nil {
		f.Profiles[k.Profile] = map[string]Preference{}
	}
	f.Profiles[k.Profile][k.Remote] = p
}

// DefaultPath is the preferences file under aide's state directory,
// beside provisioning state.
func DefaultPath(homeDir string) string {
	return filepath.Join(homeDir, ".local", "state", "aide", "preferences.json")
}

// Load reads the preferences file at path. A missing file is an empty
// document. A file from a newer aide fails with ErrUnsupportedVersion.
func Load(path string) (*File, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is aide's own state file
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &File{Version: Version}, nil
		}
		return nil, fmt.Errorf("reading preferences %s: %w", path, err)
	}
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parsing preferences %s: %w", path, err)
	}
	if f.Version > Version {
		return nil, fmt.Errorf("%w: %s has version %d, this aide supports %d; upgrade aide or move the file aside",
			ErrUnsupportedVersion, path, f.Version, Version)
	}
	f.Version = Version
	return &f, nil
}

// Save writes f to path atomically.
func Save(path string, f *File) error {
	f.Version = Version
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding preferences: %w", err)
	}
	if err := fsutil.AtomicWrite(path, append(data, '\n')); err != nil {
		return fmt.Errorf("writing preferences %s: %w", path, err)
	}
	return nil
}

// Update loads path, applies fn and saves the result. Nothing is
// written when loading or fn fails.
func Update(path string, fn func(*File) error) error {
	f, err := Load(path)
	if err != nil {
		return err
	}
	if err := fn(f); err != nil {
		return err
	}
	return Save(path, f)
}
