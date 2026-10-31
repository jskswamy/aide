package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// piManagedSymlinkArgs reconstructs only validated bootstrap links that are not
// already visible through a mount. It never binds their containing directories.
func piManagedSymlinkArgs(policy Policy, mounted []string) ([]string, error) {
	provider, ok := policy.AgentModule.(interface{ BootstrapPaths() []string })
	if !ok {
		return nil, nil
	}
	queue := provider.BootstrapPaths()
	visited := make(map[string]bool)
	links := make(map[string]string)
	for len(queue) > 0 {
		path := filepath.Clean(queue[0])
		queue = queue[1:]
		if visited[path] {
			continue
		}
		visited[path] = true
		if len(visited) > 512 {
			return nil, fmt.Errorf("managed Pi bootstrap link chain is too large")
		}
		if !filepath.IsAbs(path) {
			return nil, fmt.Errorf("managed Pi bootstrap path is not absolute: %q", path)
		}
		parts := strings.Split(strings.TrimPrefix(path, string(filepath.Separator)), string(filepath.Separator))
		current := string(filepath.Separator)
		for i, part := range parts {
			current = filepath.Join(current, part)
			info, err := os.Lstat(current)
			if err != nil {
				return nil, fmt.Errorf("managed Pi bootstrap link %q: %w", current, err)
			}
			if info.Mode()&os.ModeSymlink == 0 {
				continue
			}
			target, err := os.Readlink(current)
			if err != nil {
				return nil, err
			}
			covered := false
			for _, mount := range mounted {
				if current == mount || strings.HasPrefix(current, strings.TrimSuffix(mount, string(filepath.Separator))+string(filepath.Separator)) {
					covered = true
					break
				}
			}
			if !covered {
				links[current] = target
			}
			next := target
			if !filepath.IsAbs(next) {
				next = filepath.Join(filepath.Dir(current), next)
			}
			if i+1 < len(parts) {
				next = filepath.Join(append([]string{next}, parts[i+1:]...)...)
			}
			queue = append(queue, next)
			break
		}
	}
	paths := make([]string, 0, len(links))
	for path := range links {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	var args []string
	for _, path := range paths {
		args = append(args, "--dir", filepath.Dir(path), "--symlink", links[path], path)
	}
	return args, nil
}
