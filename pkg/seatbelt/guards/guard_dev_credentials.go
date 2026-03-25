package guards

import (
	"fmt"
	"path/filepath"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

type devCredentialsGuard struct{}

// DevCredentialsGuard returns a Guard that denies access to development tool credential files.
func DevCredentialsGuard() seatbelt.Guard { return &devCredentialsGuard{} }

func (g *devCredentialsGuard) Name() string        { return "dev-credentials" }
func (g *devCredentialsGuard) Type() string        { return "default" }
func (g *devCredentialsGuard) Description() string {
	return "Blocks access to development tool credential files within allowed directories"
}

// credentialPaths lists relative paths under $HOME that contain auth tokens.
// These live inside allowed directories (~/.config/, ~/.cargo/, etc.) so they
// need explicit deny rules.
var credentialPaths = []struct {
	rel   string
	isDir bool
}{
	{".config/gh", true},                  // GitHub CLI OAuth tokens
	{".cargo/credentials.toml", false},    // crates.io publish token
	{".gradle/gradle.properties", false},  // Maven/Artifactory tokens
	{".m2/settings.xml", false},           // Maven repository credentials
	{".config/hub", false},                // Hub CLI GitHub token
	{".config/glab-cli", true},            // GitLab CLI token
	{".pypirc", false},                    // PyPI upload credentials
	{".gem/credentials", false},           // RubyGems push token
}

func (g *devCredentialsGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	result := seatbelt.GuardResult{}

	// Build opt-out set from ExtraReadable
	optOut := make(map[string]bool)
	for _, p := range ctx.ExtraReadable {
		optOut[p] = true
	}

	for _, cred := range credentialPaths {
		fullPath := filepath.Join(ctx.HomeDir, cred.rel)

		if optOut[fullPath] {
			result.Allowed = append(result.Allowed, fullPath)
			continue
		}

		if cred.isDir {
			if !dirExists(fullPath) {
				result.Skipped = append(result.Skipped,
					fmt.Sprintf("%s not found", fullPath))
				continue
			}
			result.Rules = append(result.Rules, DenyDir(fullPath)...)
		} else {
			if !pathExists(fullPath) {
				result.Skipped = append(result.Skipped,
					fmt.Sprintf("%s not found", fullPath))
				continue
			}
			result.Rules = append(result.Rules, DenyFile(fullPath)...)
		}
		result.Protected = append(result.Protected, fullPath)
	}

	return result
}
