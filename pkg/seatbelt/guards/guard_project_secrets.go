// Project secrets guard for macOS Seatbelt profiles.
//
// Blocks access to .env files within the project tree and denies writes to
// .git/hooks to prevent hook injection attacks.

package guards

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

// Directories to always skip during project scanning.
var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true,
	"__pycache__": true, ".venv": true, "venv": true,
	".tox": true, ".eggs": true, "dist": true, "build": true,
}

type projectSecretsGuard struct{}

// ProjectSecretsGuard returns a Guard that denies access to .env files and
// denies writes to .git/hooks within the project root.
func ProjectSecretsGuard() seatbelt.Guard { return &projectSecretsGuard{} }

func (g *projectSecretsGuard) Name() string { return "project-secrets" }
func (g *projectSecretsGuard) Type() string { return "default" }
func (g *projectSecretsGuard) Description() string {
	return "Blocks access to .env files and denies writes to .git/hooks"
}

func (g *projectSecretsGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	result := seatbelt.GuardResult{}
	if ctx.ProjectRoot == "" {
		return result
	}

	// Build opt-out set from ExtraReadable + ExtraWritable
	optOut := make(map[string]bool)
	for _, p := range ctx.ExtraReadable {
		optOut[p] = true
	}
	for _, p := range ctx.ExtraWritable {
		optOut[p] = true
	}

	// Scan for .env* and .envrc files
	envFiles := scanEnvFiles(ctx.ProjectRoot)
	for _, f := range envFiles {
		if optOut[f] {
			result.Allowed = append(result.Allowed, f)
			continue
		}
		result.Rules = append(result.Rules, DenyFile(f)...)
		result.Protected = append(result.Protected, f)
	}

	// Deny writes to .git/hooks/
	hooksDir := filepath.Join(ctx.ProjectRoot, ".git", "hooks")
	if dirExists(hooksDir) {
		result.Rules = append(result.Rules,
			seatbelt.DenyRule(fmt.Sprintf(`(deny file-write* (subpath "%s"))`, hooksDir)))
		result.Protected = append(result.Protected, hooksDir)
	}

	return result
}

// scanEnvFiles walks the project root for .env* and .envrc files,
// skipping known non-project directories.
func scanEnvFiles(root string) []string {
	var found []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if isEnvFile(name) {
			found = append(found, path)
		}
		return nil
	})
	return found
}

func isEnvFile(name string) bool {
	if name == ".envrc" {
		return true
	}
	if name == ".env" || strings.HasPrefix(name, ".env.") {
		return true
	}
	return false
}
