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
	"time"

	"github.com/jskswamy/aide/internal/fsutil"
	"github.com/jskswamy/aide/internal/statedir"
)

// Version is the preferences file format this aide reads and writes.
const Version = 1

// ErrUnsupportedVersion reports a preferences file written by a newer
// aide. It is never overwritten.
var ErrUnsupportedVersion = errors.New("unsupported preferences file version")

// ErrCorrupt reports a preferences file aide cannot read. It is never
// overwritten; `aide project clear --reset` moves it aside.
var ErrCorrupt = errors.New("unreadable preferences file")

// resetHint is the recovery a corrupt or foreign file's error names.
const resetHint = "run `aide project clear --reset` to move it aside and start over"

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
// beside provisioning state (see statedir.Path).
func DefaultPath(homeDir string) string {
	return statedir.Path(homeDir, "preferences.json")
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
		return nil, fmt.Errorf("%w: %s: %v; %s", ErrCorrupt, path, err, resetHint)
	}
	if f.Version < 1 {
		return nil, fmt.Errorf("%w: %s has no version, so it is not an aide preferences file; %s", ErrCorrupt, path, resetHint)
	}
	if f.Version > Version {
		return nil, fmt.Errorf("%w: %s has version %d, this aide supports %d; upgrade aide, or %s",
			ErrUnsupportedVersion, path, f.Version, Version, resetHint)
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

// ErrUnchanged is returned by an Update callback to skip the write.
var ErrUnchanged = errors.New("preferences unchanged")

// Update loads path, applies fn and saves the result. Nothing is
// written when loading fails, fn fails, or fn returns ErrUnchanged
// (then Update returns nil).
func Update(path string, fn func(*File) error) error {
	f, err := Load(path)
	if err != nil {
		return err
	}
	if err := fn(f); err != nil {
		if errors.Is(err, ErrUnchanged) {
			return nil
		}
		return err
	}
	return Save(path, f)
}

// Reset moves the file at path aside to <path>.corrupt-<timestamp>, so
// the next Load starts empty, and returns where it went ("" when there
// was no file). Nothing is deleted.
func Reset(path string) (string, error) {
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	moved := path + ".corrupt-" + time.Now().UTC().Format("20060102T150405.000000000Z")
	if err := os.Rename(path, moved); err != nil {
		return "", fmt.Errorf("moving preferences %s aside: %w", path, err)
	}
	return moved, nil
}
