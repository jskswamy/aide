package guards

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jskswamy/aide/internal/homepath"
	"github.com/jskswamy/aide/pkg/seatbelt"
)

type gitIntegrationGuard struct{}

// GitIntegrationGuard returns a guard for read-only access to git configuration files.
func GitIntegrationGuard() seatbelt.Guard { return &gitIntegrationGuard{} }

func (g *gitIntegrationGuard) Name() string { return "git-integration" }
func (g *gitIntegrationGuard) Type() string { return "always" }
func (g *gitIntegrationGuard) Description() string {
	return "Git configuration files (read-only, parsed from gitconfig)"
}

func (g *gitIntegrationGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	if ctx == nil {
		return seatbelt.GuardResult{}
	}
	home := ctx.HomeDir
	if home == "" {
		return seatbelt.GuardResult{}
	}

	gcResult := ParseGitConfigWithEnv(home, ctx.ProjectRoot, ctx.EnvLookup)

	var result seatbelt.GuardResult

	if val, ok := ctx.EnvLookup("GIT_CONFIG_GLOBAL"); ok && val != "" {
		result.Overrides = append(result.Overrides, seatbelt.Override{
			EnvVar: "GIT_CONFIG_GLOBAL", Value: val, DefaultPath: home + "/.gitconfig",
		})
	}
	if val, ok := ctx.EnvLookup("GIT_CONFIG_SYSTEM"); ok && val != "" {
		result.Overrides = append(result.Overrides, seatbelt.Override{
			EnvVar: "GIT_CONFIG_SYSTEM", Value: val, DefaultPath: "/etc/gitconfig",
		})
	}

	allPaths := gcResult.AllPaths()
	if len(allPaths) == 0 {
		return result
	}

	var pathExprs []string
	for _, p := range allPaths {
		pathExprs = append(pathExprs, fmt.Sprintf(`    (literal "%s")`, p))
	}

	result.Rules = append(result.Rules,
		seatbelt.SectionAllow("Git configuration (read-only)"),
		seatbelt.AllowRule(fmt.Sprintf("(allow file-read*\n%s)", strings.Join(pathExprs, "\n"))),
	)

	// GPG signing support: if commit.gpgsign is enabled in any config,
	// allow access to ~/.gnupg for signing commits. GPG needs read+write
	// (trustdb lock files, random_seed) and agent socket access.
	if gcResult.GPGSign {
		gnupgHome := home + "/.gnupg"
		if val, ok := ctx.EnvLookup("GNUPGHOME"); ok && val != "" {
			gnupgHome = val
		}
		result.Rules = append(result.Rules,
			seatbelt.SectionAllow("GPG signing for git commits (read-write)"),
			seatbelt.AllowRule(fmt.Sprintf("(allow file-read* file-write*\n    %s)",
				seatbelt.HomeSubpath(home, ".gnupg"))),
		)
		// GPG agent unix socket for passphrase caching
		result.Rules = append(result.Rules,
			seatbelt.SectionAllow("GPG agent socket"),
			seatbelt.AllowRule(fmt.Sprintf(`(allow network-outbound
    (remote unix-socket (path-literal "%s/S.gpg-agent")))`, gnupgHome)),
		)
	}

	// SSH commit signing: when gpg.format=ssh, git invokes ssh-keygen -Y sign
	// which reads both the private signing key and its .pub sibling. Auto-grant
	// read access (and known_hosts read+write for TOFU on first push).
	if strings.EqualFold(gcResult.GPGFormat, "ssh") {
		addSSHSigningRules(&result, home, gcResult.SigningKey)
	}

	return result
}

// addSSHSigningRules appends allow rules for the SSH signing key (and its .pub
// sibling) plus known_hosts. Mitigations baked in:
//
//   - signingKey is read from the parsed global gitconfig (parser does not look
//     at <project>/.git/config), so a malicious repo cannot inject a path.
//   - Symlinks are resolved before emitting the rule.
//   - Paths escaping $HOME are rejected (defense in depth).
//   - Literal "ssh-…" pubkey strings produce no rule (no file to grant).
func addSSHSigningRules(result *seatbelt.GuardResult, home, signingKey string) {
	// known_hosts is needed for ssh host verification on `git push`. Write
	// access supports TOFU: first connect to a new host writes the host key.
	knownHosts := filepath.Join(home, ".ssh", "known_hosts")
	result.Rules = append(result.Rules,
		seatbelt.SectionAllow("SSH known_hosts for git push (read-write for TOFU)"),
		seatbelt.AllowRule(fmt.Sprintf(`(allow file-read* file-write*
    (literal "%s"))`, knownHosts)),
	)

	signingKey = strings.TrimSpace(signingKey)
	if signingKey == "" {
		result.Skipped = append(result.Skipped,
			"git-integration: gpg.format=ssh but user.signingkey is empty — no key path to grant")
		return
	}
	// Literal pubkey: nothing to grant; private key lives elsewhere
	// (agent socket, hardware token, etc.) outside this guard's concern.
	if strings.HasPrefix(signingKey, "ssh-") {
		result.Skipped = append(result.Skipped,
			"git-integration: user.signingkey is a literal pubkey string — no file rule emitted")
		return
	}

	// Resolve ~, then symlinks. Strip surrounding quotes if present.
	signingKey = strings.Trim(signingKey, `"`)
	resolved := homepath.Expand(signingKey, home)
	if !filepath.IsAbs(resolved) {
		result.Skipped = append(result.Skipped, fmt.Sprintf(
			"git-integration: user.signingkey %q is not an absolute path — skipped", signingKey))
		return
	}
	resolved = ResolveSymlink(resolved)

	// Defense in depth: refuse to grant on paths outside $HOME. The parser
	// already ignores repo-local .git/config, but if a future change ever
	// allowed it, this stops /etc/shadow-style escapes.
	if !pathUnderHome(resolved, home) {
		result.Skipped = append(result.Skipped, fmt.Sprintf(
			"git-integration: user.signingkey %q resolves outside HOME (%q) — refusing to auto-grant",
			signingKey, resolved))
		return
	}

	priv, pub := splitSigningKeyPaths(resolved)
	result.Rules = append(result.Rules,
		seatbelt.SectionAllow(fmt.Sprintf("SSH commit signing key (read-only) — auto-granted because gpg.format=ssh, user.signingkey=%s", signingKey)),
		seatbelt.AllowRule(fmt.Sprintf(`(allow file-read*
    (literal "%s")
    (literal "%s"))`, priv, pub)),
	)
}

// splitSigningKeyPaths returns (privateKeyPath, publicKeyPath). git accepts
// either the private or .pub form in user.signingkey; ssh-keygen -Y sign reads
// both. Strip/append .pub as needed so we always grant both.
func splitSigningKeyPaths(path string) (priv, pub string) {
	if priv, ok := strings.CutSuffix(path, ".pub"); ok {
		return priv, path
	}
	return path, path + ".pub"
}

// pathUnderHome reports whether resolved is the same as home or a descendant.
// Both inputs should be absolute; resolved should be symlink-resolved.
func pathUnderHome(resolved, home string) bool {
	if home == "" {
		return false
	}
	resolvedHome := ResolveSymlink(home)
	rel, err := filepath.Rel(resolvedHome, resolved)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
