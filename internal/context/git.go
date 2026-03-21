// Package context provides git remote detection and context resolution for aide.
package context

import (
	"net/url"
	"os/exec"
	"strings"
)

// DetectRemote returns the raw URL of the given git remote for the repository
// at dir. If remoteName is empty, it defaults to "origin".
// Returns an empty string if the directory is not a git repo or the remote
// does not exist.
func DetectRemote(dir string, remoteName string) string {
	if remoteName == "" {
		remoteName = "origin"
	}
	cmd := exec.Command("git", "-C", dir, "remote", "get-url", remoteName)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// ParseRemoteHost normalizes a git remote URL to the form "host/org/repo".
// Supports SSH shorthand (git@host:org/repo.git), HTTPS, SSH protocol,
// and git:// URLs. Strips .git suffix and userinfo.
func ParseRemoteHost(rawURL string) string {
	if rawURL == "" {
		return ""
	}

	// Handle SSH shorthand: git@github.com:org/repo.git
	if strings.Contains(rawURL, ":") && !strings.Contains(rawURL, "://") {
		// Split on first ':'
		parts := strings.SplitN(rawURL, ":", 2)
		if len(parts) != 2 {
			return rawURL
		}
		host := parts[0]
		// Strip user@ from host
		if idx := strings.Index(host, "@"); idx >= 0 {
			host = host[idx+1:]
		}
		path := strings.TrimSuffix(parts[1], ".git")
		path = strings.TrimPrefix(path, "/")
		return host + "/" + path
	}

	// Handle standard URLs: https://, ssh://, git://
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	host := u.Hostname()
	path := strings.TrimPrefix(u.Path, "/")
	path = strings.TrimSuffix(path, ".git")

	if host == "" {
		return ""
	}

	return host + "/" + path
}

// ProjectRoot returns the root directory of the git repository containing cwd.
// If cwd is not inside a git repository, it returns cwd as a fallback.
func ProjectRoot(cwd string) string {
	cmd := exec.Command("git", "-C", cwd, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return cwd
	}
	return strings.TrimSpace(string(out))
}
