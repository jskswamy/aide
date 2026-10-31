// Package piinstall discovers Pi's supported installer layout without executing it.
package piinstall

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Installation separates the writable release tree from read-only bootstrap paths.
type Installation struct {
	Root     string
	Readable []string
}

// This identifies the inspected installer format, not trusted authorship of code.
const launcherDigest = "da0c6a49fe94aadc9765cc7afc6ba6b17e553eed0247d0385c160bc188f5cfbb"

var releaseVersion = regexp.MustCompile(`^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)

// Discover returns nil for an unrelated non-managed executable. A recognized
// launcher or sibling marker makes validation failures fatal, never extra grants.
func Discover(executable, home string, env, privateDirs []string) (installation *Installation, err error) {
	candidate := filepath.Join(filepath.Dir(filepath.Dir(executable)), "install")
	defer func() {
		if err != nil {
			err = fmt.Errorf("managed Pi executable %q, candidate %q: %w", executable, candidate, err)
		}
	}()
	resolvedLauncher, err := filepath.EvalSymlinks(executable)
	if err != nil {
		// Old non-managed installs are not subject to managed-layout validation.
		if _, markerErr := os.Lstat(filepath.Join(candidate, "managed-install.json")); os.IsNotExist(markerErr) {
			return nil, nil
		}
		return nil, fmt.Errorf("resolve launcher: %w", err)
	}
	candidate = filepath.Join(filepath.Dir(filepath.Dir(resolvedLauncher)), "install")
	_, markerErr := os.Lstat(filepath.Join(candidate, "managed-install.json"))
	if info, statErr := os.Stat(resolvedLauncher); os.IsNotExist(markerErr) && (statErr != nil || !info.Mode().IsRegular() || info.Size() > 64<<10) {
		return nil, nil
	}
	content, err := readRegular(resolvedLauncher, 64<<10)
	if err != nil {
		return nil, err
	}
	recognized := fmt.Sprintf("%x", sha256.Sum256(content)) == launcherDigest
	if !recognized && os.IsNotExist(markerErr) {
		return nil, nil
	}
	if !recognized {
		return nil, fmt.Errorf("unsupported launcher format; reinstall with the supported Pi installer")
	}
	if filepath.Base(resolvedLauncher) != "pi" || filepath.Base(filepath.Dir(resolvedLauncher)) != "bin" {
		return nil, fmt.Errorf("launcher must have the supported bin/pi layout")
	}
	root, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return nil, fmt.Errorf("resolve installation: %w", err)
	}
	if info, e := os.Stat(root); e != nil || !info.IsDir() {
		return nil, fmt.Errorf("installation root is not a directory")
	}
	if err := validateGrant(root, root, resolvedLauncher, home, privateDirs); err != nil {
		return nil, err
	}
	readInside := func(name string) ([]byte, error) {
		path := filepath.Join(root, name)
		target, e := filepath.EvalSymlinks(path)
		if e != nil {
			return nil, e
		}
		if !under(target, root) {
			return nil, fmt.Errorf("%s escapes installation root", name)
		}
		return readRegular(target, 64<<10)
	}
	raw, err := readInside("managed-install.json")
	if err != nil {
		return nil, fmt.Errorf("read marker: %w", err)
	}
	var marker struct {
		Kind          string `json:"kind"`
		SchemaVersion int    `json:"schemaVersion"`
		Layout        string `json:"layout"`
		Entrypoint    struct {
			Type string `json:"type"`
			Path string `json:"path"`
		} `json:"entrypoint"`
	}
	if err = json.Unmarshal(bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf}), &marker); err != nil {
		return nil, fmt.Errorf("invalid marker: %w", err)
	}
	if marker.Kind != "pi-managed-install" || marker.SchemaVersion != 1 || marker.Layout != "releases-v1" {
		return nil, fmt.Errorf("unsupported managed marker kind, schema or layout")
	}
	if (marker.Entrypoint.Type != "script" && marker.Entrypoint.Type != "symlink") || !filepath.IsAbs(marker.Entrypoint.Path) || filepath.Base(marker.Entrypoint.Path) != "pi" {
		return nil, fmt.Errorf("invalid marker entrypoint")
	}
	raw, err = readInside("current-version")
	if err != nil {
		return nil, fmt.Errorf("read current version: %w", err)
	}
	version := strings.TrimSuffix(string(raw), "\n")
	if !releaseVersion.MatchString(version) {
		return nil, fmt.Errorf("invalid current version")
	}
	release := filepath.Join(root, "releases", version)
	releaseReal, err := filepath.EvalSymlinks(release)
	if err != nil {
		return nil, fmt.Errorf("resolve release: %w", err)
	}
	if !under(releaseReal, root) {
		return nil, fmt.Errorf("release escapes installation")
	}
	active, err := filepath.EvalSymlinks(filepath.Join(release, "node_modules", ".bin", "pi"))
	if err != nil {
		return nil, fmt.Errorf("resolve active executable: %w", err)
	}
	if !under(active, releaseReal) {
		return nil, fmt.Errorf("active executable escapes release")
	}
	if info, e := os.Stat(active); e != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
		return nil, fmt.Errorf("active executable is not executable")
	}
	installation = &Installation{Root: root, Readable: []string{resolvedLauncher, executable}}
	if candidate != root {
		installation.Readable = append(installation.Readable, candidate)
	}
	lookup := func(key, fallback string) string {
		for i := len(env) - 1; i >= 0; i-- {
			if v, ok := strings.CutPrefix(env[i], key+"="); ok && v != "" {
				return v
			}
		}
		return fallback
	}
	dataHome := lookup("XDG_DATA_HOME", filepath.Join(lookup("HOME", home), ".local", "share"))
	nodeCurrent := filepath.Join(dataHome, "pi-node", "current")
	node := filepath.Join(nodeCurrent, "bin", "node")
	if info, e := os.Stat(node); e == nil && info.Mode().IsRegular() && info.Mode()&0111 != 0 {
		runtimeRoot, e := filepath.EvalSymlinks(nodeCurrent)
		if e != nil {
			return nil, fmt.Errorf("resolve Node runtime: %w", e)
		}
		nodeReal, e := filepath.EvalSymlinks(node)
		if e != nil || !under(nodeReal, runtimeRoot) {
			return nil, fmt.Errorf("node executable escapes runtime")
		}
		installation.Readable = append(installation.Readable, nodeCurrent, runtimeRoot)
	}
	for _, grant := range installation.Readable {
		if err := validateGrant(grant, root, resolvedLauncher, home, privateDirs); err != nil {
			return nil, err
		}
	}
	slices.Sort(installation.Readable)
	installation.Readable = slices.Compact(installation.Readable)
	return installation, nil
}

func validateGrant(grant, root, launcher, home string, privateDirs []string) error {
	defaultAgent, err := ResolvePath(filepath.Join(home, ".pi", "agent"))
	if err != nil {
		return err
	}
	if under(root, defaultAgent) && root != filepath.Join(defaultAgent, "install") {
		return fmt.Errorf("installation resolves into default private state")
	}
	if !filepath.IsAbs(grant) || strings.ContainsAny(grant, "\x00\r\n\"\\") {
		return fmt.Errorf("unsafe grant path %q", grant)
	}
	canonical, err := ResolvePath(grant)
	if err != nil {
		return err
	}
	if under(canonical, defaultAgent) && canonical != launcher && !under(canonical, root) {
		return fmt.Errorf("bootstrap grant %q includes default private state", grant)
	}
	protected := append(slices.Clone(privateDirs), home, filepath.Join(home, ".pi", "agent"))
	for _, path := range protected {
		if path == "" {
			continue
		}
		private, err := ResolvePath(path)
		if err != nil {
			return fmt.Errorf("resolve private directory %q: %w", path, err)
		}
		if under(private, canonical) || under(path, grant) {
			return fmt.Errorf("grant %q includes private state %q", grant, path)
		}
		if path != home && path != filepath.Join(home, ".pi", "agent") && under(canonical, private) {
			return fmt.Errorf("grant %q lies inside private state %q", grant, path)
		}
	}
	return nil
}

func under(path, dir string) bool {
	return path == dir || strings.HasPrefix(path, strings.TrimSuffix(dir, string(filepath.Separator))+string(filepath.Separator))
}

// ResolvePath resolves existing ancestors but refuses dangling links and errors
// other than missing descendants, unlike best-effort grant-generation helpers.
func ResolvePath(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("path is not absolute: %q", path)
	}
	path = filepath.Clean(path)
	tail := ""
	for cur := path; ; cur = filepath.Dir(cur) {
		if _, err := os.Lstat(cur); err == nil {
			resolved, err := filepath.EvalSymlinks(cur)
			if err != nil {
				return "", err
			}
			return filepath.Join(resolved, tail), nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		if filepath.Dir(cur) == cur {
			return "", fmt.Errorf("cannot resolve %q", path)
		}
		tail = filepath.Join(filepath.Base(cur), tail)
	}
}

func readRegular(path string, limit int64) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file: %q", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err = f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file: %q", path)
	}
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err == nil && int64(len(b)) > limit {
		return nil, fmt.Errorf("managed file too large: %q", path)
	}
	return b, err
}
