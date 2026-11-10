package usage

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/fsutil"
)

type piStore struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

var invalidStoreName = regexp.MustCompile(`[^a-z0-9_-]+`)

func derivePiStores(cfg *config.Config, home string) ([]piStore, error) {
	names := make([]string, 0, len(cfg.Contexts))
	for name := range cfg.Contexts {
		names = append(names, name)
	}
	sort.Strings(names)
	defaultPath := filepath.Join(home, ".pi", "agent", "sessions")
	stores := make([]piStore, 0, len(names)+1)
	used := make(map[string]string)
	for _, profile := range names {
		ctx := cfg.Contexts[profile]
		if !ctx.SelectsConfigDir() {
			continue
		}
		path := filepath.Join(ctx.NativeConfigDir("pi", home), "sessions")
		if !filepath.IsAbs(path) {
			return nil, fmt.Errorf("pi store for profile %q has non-absolute path %q", profile, path)
		}
		if pathsOverlap(path, defaultPath) {
			if sameStorePath(path, defaultPath) {
				continue
			}
			return nil, fmt.Errorf("pi store for profile %q (%s) overlaps default pi store %s", profile, path, defaultPath)
		}
		name := "pi-" + invalidStoreName.ReplaceAllString(strings.ToLower(profile), "-")
		if len(name) > 32 {
			name = name[:32]
		}
		if prev, ok := used[name]; ok {
			return nil, fmt.Errorf("pi store name %q collides for profiles %q and %q", name, prev, profile)
		}
		used[name] = profile
		for _, prev := range stores {
			if pathsOverlap(path, prev.Path) {
				return nil, fmt.Errorf("pi stores %q (%s) and %q (%s) overlap", prev.Name, prev.Path, name, path)
			}
		}
		stores = append(stores, piStore{Name: name, Path: path})
	}
	omp := filepath.Join(home, ".omp", "agent", "sessions")
	if info, err := os.Stat(omp); err == nil && info.IsDir() {
		covered := false
		for _, store := range stores {
			if pathsOverlap(store.Path, omp) {
				if !pathWithin(omp, store.Path) {
					return nil, fmt.Errorf("pi store %q (%s) overlaps omp store %s", store.Name, store.Path, omp)
				}
				covered = true
				break
			}
		}
		if !covered {
			if pathsOverlap(omp, defaultPath) {
				return nil, fmt.Errorf("omp store %s overlaps default pi store %s", omp, defaultPath)
			}
			stores = append(stores, piStore{Name: "omp", Path: omp})
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("checking omp sessions: %w", err)
	}
	return stores, nil
}

func sameStorePath(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b) || fsutil.SameDir(a, b) || fsutil.ResolveNearest(a) == fsutil.ResolveNearest(b)
}

func pathsOverlap(a, b string) bool {
	for _, pair := range [][2]string{{filepath.Clean(a), filepath.Clean(b)}, {fsutil.ResolveNearest(a), fsutil.ResolveNearest(b)}} {
		if withinPath(pair[0], pair[1]) || withinPath(pair[1], pair[0]) {
			return true
		}
	}
	return fsutil.SameDir(a, b)
}

func pathWithin(path, dir string) bool {
	return withinPath(fsutil.ResolveNearest(path), fsutil.ResolveNearest(dir))
}

func withinPath(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && (rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// SetupPiStores safely merges derived pi stores into ccusage's home config.
// The caller supplies whether stdin is interactive; this function never probes a TTY.
func SetupPiStores(cfg *config.Config, home string, in io.Reader, out io.Writer, force, interactive bool) error {
	stores, err := derivePiStores(cfg, home)
	if err != nil {
		return err
	}
	path := filepath.Join(home, ".claude", "ccusage.json")
	old, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	root := make(map[string]json.RawMessage)
	if err == nil {
		if !jsonObject(old) || json.Unmarshal(old, &root) != nil {
			return fmt.Errorf("%s: expected a JSON object", path)
		}
	}
	pi := make(map[string]json.RawMessage)
	if raw, ok := root["pi"]; ok {
		if !jsonObject(raw) || json.Unmarshal(raw, &pi) != nil {
			return fmt.Errorf("%s: pi must be a JSON object", path)
		}
		if rawStores, ok := pi["stores"]; ok {
			var existing []piStore
			trimmed := bytes.TrimSpace(rawStores)
			if len(trimmed) == 0 || trimmed[0] != '[' || json.Unmarshal(rawStores, &existing) != nil {
				return fmt.Errorf("%s: pi.stores must be an array of stores", path)
			}
			for _, store := range existing {
				if store.Name == "" || store.Path == "" {
					return fmt.Errorf("%s: pi.stores contains a store without name or path", path)
				}
			}
		}
	}
	pi["stores"], err = json.Marshal(stores)
	if err != nil {
		return err
	}
	root["pi"], err = json.Marshal(pi)
	if err != nil {
		return err
	}
	updated, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	updated = append(updated, '\n')
	if bytes.Equal(old, updated) {
		fmt.Fprintln(out, "ccusage pi stores unchanged")
		return nil
	}
	if !force {
		if !interactive {
			return fmt.Errorf("setup requires a TTY for confirmation or --force")
		}
		showSetupDiff(out, path, old, updated)
		fmt.Fprint(out, "Write ccusage config? [y/N] ")
		answer, _ := bufio.NewReader(in).ReadString('\n')
		if strings.ToLower(strings.TrimSpace(answer)) != "y" {
			fmt.Fprintln(out, "No changes written")
			return nil
		}
	}
	if err := fsutil.AtomicWrite(path, updated); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	fmt.Fprintf(out, "Updated %s\n", path)
	return nil
}

func jsonObject(raw []byte) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && trimmed[0] == '{'
}

func showSetupDiff(out io.Writer, path string, old, updated []byte) {
	fmt.Fprintf(out, "--- %s (current)\n+++ %s (proposed)\n", path, path)
	for _, line := range strings.Split(strings.TrimSuffix(string(old), "\n"), "\n") {
		if line != "" {
			fmt.Fprintln(out, "-"+line)
		}
	}
	for _, line := range strings.Split(strings.TrimSuffix(string(updated), "\n"), "\n") {
		fmt.Fprintln(out, "+"+line)
	}
}
