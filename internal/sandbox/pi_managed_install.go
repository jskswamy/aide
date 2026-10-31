package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jskswamy/aide/internal/piinstall"
)

// ValidatePiManagedInstall checks only the new grants; it never changes backend
// selection or turns an automatic grant into a user guard opt-out.
func ValidatePiManagedInstall(policy Policy, installation piinstall.Installation, backend string) error {
	if backend != BackendSeatbelt && backend != BackendLandlock && backend != BackendBwrap {
		return fmt.Errorf("managed Pi installation %q: sandbox backend %q cannot enforce access", installation.Root, backend)
	}
	denied := append([]string{}, policy.ExtraDenied...)
	for _, guard := range EvaluateGuards(&policy) {
		denied = append(denied, guard.Protected...)
	}
	grants := append([]string{installation.Root}, installation.Readable...)
	for _, raw := range denied {
		paths := []string{raw}
		glob := strings.ContainsAny(raw, "*?[")
		if glob {
			// An unmatched pattern must not disappear merely because its future
			// targets do not exist yet. Its fixed directory bounds possible overlap.
			matches, err := filepath.Glob(raw)
			if err != nil {
				return fmt.Errorf("managed Pi %s: invalid denied pattern %q: %w", backend, raw, err)
			}
			prefix := raw[:strings.IndexAny(raw, "*?[")]
			paths = matches
			paths = append(paths, filepath.Dir(prefix+"_"))
		}
		for _, path := range paths {
			deny, err := piinstall.ResolvePath(path)
			if err != nil {
				return fmt.Errorf("managed Pi %s: cannot resolve denied path %q: %w", backend, raw, err)
			}
			for _, grant := range grants {
				target, err := piinstall.ResolvePath(grant)
				if err != nil {
					return fmt.Errorf("managed Pi %s: resolve grant %q: %w", backend, grant, err)
				}
				inside := func(p, dir string) bool {
					return p == dir || strings.HasPrefix(p, strings.TrimSuffix(dir, string(filepath.Separator))+string(filepath.Separator))
				}
				if !inside(target, deny) && !inside(deny, target) {
					continue
				}
				_, statErr := os.Stat(path)
				if glob || inside(target, deny) || backend == BackendLandlock || statErr != nil {
					return fmt.Errorf("managed Pi %s: grant %q conflicts with denied path %q; remove the conflict or use a separate safe installation", backend, grant, raw)
				}
			}
		}
	}
	return nil
}
