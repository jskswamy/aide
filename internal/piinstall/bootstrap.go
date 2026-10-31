package piinstall

import (
	"fmt"
	"path/filepath"
	"slices"
)

// ExecutableParents validates Landlock's implicit recursive bootstrap scopes.
// They must be checked even though the Pi module grants only launcher files.
func (installation Installation) ExecutableParents(executable, home string, privateDirs []string) ([]string, error) {
	resolved, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return nil, err
	}
	parents := []string{filepath.Dir(executable), filepath.Dir(resolved)}
	slices.Sort(parents)
	parents = slices.Compact(parents)
	for _, parent := range parents {
		if err := validateGrant(parent, installation.Root, parent, home, privateDirs); err != nil {
			return nil, fmt.Errorf("managed Pi executable %q, bootstrap parent %q: %w", executable, parent, err)
		}
	}
	return parents, nil
}
