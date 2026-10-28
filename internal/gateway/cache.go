package gateway

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/fsutil"
	"github.com/jskswamy/aide/internal/statedir"
)

// CachePath is the catalogue file for gatewayName on profile:
// <state>/gateways/<profile>/<gateway>.json. Both names become path
// segments, so anything that could leave that directory is refused.
func CachePath(homeDir, profile, gatewayName string) (string, error) {
	if profile == "" || profile == "." || profile == ".." || strings.ContainsAny(profile, `/\`) {
		return "", fmt.Errorf("gateway cache: profile name %q is not a safe path segment", profile)
	}
	if !config.IsGatewayName(gatewayName) {
		return "", fmt.Errorf("gateway cache: gateway name %q is not a safe path segment", gatewayName)
	}
	return statedir.Path(homeDir, filepath.Join("gateways", profile, gatewayName+".json")), nil
}

// ReadCache returns the cached catalogue, or nil and no error when
// there is none yet.
func ReadCache(homeDir, profile, gatewayName string) (*CachedCatalogue, error) {
	path, err := CachePath(homeDir, profile, gatewayName)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path) // #nosec G304 -- path built from validated names under aide's state dir
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading gateway cache %s: %w", path, err)
	}
	var cat CachedCatalogue
	if err := json.Unmarshal(data, &cat); err != nil {
		return nil, fmt.Errorf("parsing gateway cache %s: %w", path, err)
	}
	return &cat, nil
}

// WriteCache stores cat atomically (0600, in a 0700 directory).
func WriteCache(homeDir string, cat *CachedCatalogue) error {
	path, err := CachePath(homeDir, cat.Profile, cat.Gateway)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating gateway cache dir: %w", err)
	}
	data, err := json.MarshalIndent(cat, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.AtomicWrite(path, append(data, '\n'))
}

// RemoveCache deletes the catalogue; a missing one is not an error.
func RemoveCache(homeDir, profile, gatewayName string) error {
	path, err := CachePath(homeDir, profile, gatewayName)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// FormatAge renders a cache age or TTL to the minute: 30h, 2h15m, 45m,
// <1m.
func FormatAge(d time.Duration) string {
	d = d.Round(time.Minute)
	if d < time.Minute {
		return "<1m"
	}
	s := strings.TrimSuffix(d.String(), "0s")
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}

// IsExpired reports whether cat is missing or older than ttl at now.
func IsExpired(cat *CachedCatalogue, ttl time.Duration, now time.Time) bool {
	return cat == nil || now.Sub(cat.FetchedAt) > ttl
}
