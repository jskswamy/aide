// Package context provides git remote detection and context resolution for aide.
package context

import (
	"net/url"
	"os/exec"
	"strings"
)

// DefaultRemoteName is the git remote that identifies a repository: it is
// what match rules without remote_name are scored against, and the remote
// whose normalized URL CurrentRemote reports. This is the only place the
// name is spelled out; callers use CurrentRemote or GitRemotes.
const DefaultRemoteName = "origin"

// RemoteLookup returns the raw URL of the named git remote, or "" if there
// is no such remote. Resolve always passes a non-empty name. Raw URLs may
// embed credentials: compare them, but log or persist only ParseRemoteHost
// of them.
type RemoteLookup func(name string) string

// GitRemotes returns a RemoteLookup for the repository at dir. It runs git
// lazily, at most once per remote name, so resolving a configuration
// without remote rules never shells out. It is not safe for concurrent use.
func GitRemotes(dir string) RemoteLookup {
	cache := map[string]string{}
	return func(name string) string {
		if name == "" {
			name = DefaultRemoteName
		}
		if u, ok := cache[name]; ok {
			return u
		}
		u := DetectRemote(dir, name)
		cache[name] = u
		return u
	}
}

// CurrentRemote returns the normalized identity (see ParseRemoteHost) of
// the default remote of the repository at dir, or "" if dir is not in a
// git repository, has no such remote, or the remote has no host (a local
// path). The result never contains credentials.
func CurrentRemote(dir string) string {
	return ParseRemoteHost(DetectRemote(dir, DefaultRemoteName))
}

// DetectRemote returns the raw URL of the given git remote for the repository
// at dir. If remoteName is empty, it defaults to DefaultRemoteName.
// Returns an empty string if the directory is not a git repo or the remote
// does not exist. The raw URL may embed credentials; see RemoteLookup.
func DetectRemote(dir string, remoteName string) string {
	if remoteName == "" {
		remoteName = DefaultRemoteName
	}
	cmd := exec.Command("git", "-C", dir, "remote", "get-url", remoteName)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// ParseRemoteHost normalizes a git remote URL to the form "host[:port]/org/repo".
// It accepts scp-style (git@host:org/repo.git), ssh://, https://, git:// and
// already-normalized forms. The host is lowercased, a port is kept unless
// it is the scheme's default (ssh 22, https 443, http 80, git 9418), a trailing ".git" and surrounding slashes are trimmed, and any
// userinfo (user names, passwords, tokens) is dropped. Path case is kept,
// since some hosts treat repository paths case-sensitively.
//
// The result never contains userinfo; inputs that cannot be parsed, and
// local paths or file:// URLs (which have no host), yield "". It is
// idempotent: ParseRemoteHost(ParseRemoteHost(x)) == ParseRemoteHost(x).
// Anything aide logs or persists about a remote uses this form only.
//
// An scp-style remote whose path starts with digits and no user
// ("host:2222/org/repo") is read as host:port, the normalized form,
// rather than as a relative path.
func ParseRemoteHost(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	if strings.Contains(rawURL, "://") {
		return normalizeURLRemote(rawURL)
	}
	return normalizeSCPRemote(rawURL)
}

// normalizeURLRemote handles scheme URLs (https://, ssh://, git://, file://).
func normalizeURLRemote(rawURL string) string {
	if strayURLUserinfo(rawURL) {
		return ""
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return ""
	}
	port := u.Port()
	if port == schemeDefaultPorts[strings.ToLower(u.Scheme)] {
		port = "" // one identity for ssh://host:22/x and git@host:x
	}
	return joinRemote(host, port, u.Path)
}

// schemeDefaultPorts maps git transport schemes to the port they use when
// none is given; an explicit default port is dropped from the identity.
var schemeDefaultPorts = map[string]string{
	"ssh":     "22",
	"git+ssh": "22",
	"ssh+git": "22",
	"https":   "443",
	"http":    "80",
	"git":     "9418",
}

// stripRemoteUserinfo removes userinfo (user names, passwords, tokens)
// from a remote URL or remote rule without otherwise normalizing it, so a
// glob such as "https://tok@github.com/org/*" can be shown safely. It errs
// toward removing too much: text that only looks like userinfo goes too.
func stripRemoteUserinfo(s string) string {
	scheme := ""
	if i := strings.Index(s, "://"); i >= 0 {
		scheme, s = s[:i+3], s[i+3:]
	}
	hostEnds := ":" // scp-style: userinfo@host:path
	if scheme != "" {
		hostEnds = ":/"
	}
	if i := userinfoEnd(s, hostEnds); i >= 0 {
		return scheme + s[i+1:]
	}
	if i := strayUserinfoEnd(s); i >= 0 {
		return scheme + s[i+1:]
	}
	return scheme + s
}

// userinfoEnd returns the index of the '@' that ends the userinfo of a
// remote without its scheme, or -1. It is the last '@' followed by a host
// segment ended by one of hostEnds, so a password containing '/' or '@' is
// skipped over. Scp-style remotes pass ":" so that an '@' inside the path
// ("git@host:org/a@b/c") is not taken for the userinfo end.
func userinfoEnd(s, hostEnds string) int {
	for i := strings.LastIndex(s, "@"); i >= 0; i = strings.LastIndex(s[:i], "@") {
		rest := s[i+1:]
		j := strings.IndexAny(rest, hostEnds+"@")
		if j > 0 && rest[j] != '@' {
			return i
		}
	}
	return -1
}

// strayURLUserinfo reports whether a scheme URL has an '@' that url.Parse
// would not treat as userinfo but that follows a ':' after the scheme,
// e.g. "https://user:12/ss@host/x", where an unescaped '/' in the password
// makes "user:12" parse as host:port. Such URLs are refused rather than
// risk emitting part of a password.
func strayURLUserinfo(rawURL string) bool {
	_, after, _ := strings.Cut(rawURL, "://")
	authority, _, _ := strings.Cut(after, "/")
	if i := strings.LastIndex(authority, "@"); i >= 0 {
		after = after[i+1:] // well-formed userinfo; url.Parse strips it
	}
	return strayUserinfoEnd(after) >= 0
}

// strayUserinfoEnd is the fallback when userinfoEnd finds no host-shaped
// '@': it returns the last '@' if one appears before the first '/' or
// after a ':' (where only userinfo puts it, e.g. "user:pa/ss@host"), or
// -1 when every '@' looks like part of a path.
func strayUserinfoEnd(s string) int {
	at := strings.LastIndex(s, "@")
	if at < 0 {
		return -1
	}
	head, _, _ := strings.Cut(s, "/")
	if strings.Contains(head, "@") || strings.Contains(s[:at], ":") {
		return at
	}
	return -1
}

// normalizeSCPRemote handles scp-style remotes ([user@]host:path) and the
// normalized form itself (host[:port]/path). Local paths yield "".
func normalizeSCPRemote(rawURL string) string {
	if strings.HasPrefix(rawURL, "/") || strings.HasPrefix(rawURL, ".") {
		return ""
	}
	hadUser := false
	if i := userinfoEnd(rawURL, ":"); i >= 0 {
		rawURL = rawURL[i+1:]
		hadUser = true
	} else if strayUserinfoEnd(rawURL) >= 0 {
		return "" // userinfo with no recognizable host after it
	}

	host, rest := rawURL, ""
	if strings.HasPrefix(rawURL, "[") {
		end := strings.Index(rawURL, "]")
		if end < 0 {
			return ""
		}
		host, rest = rawURL[1:end], rawURL[end+1:]
		if rest != "" && rest[0] != ':' && rest[0] != '/' {
			return ""
		}
	} else if i := strings.IndexAny(rawURL, ":/"); i >= 0 {
		host, rest = rawURL[:i], rawURL[i:]
	}
	host = strings.ToLower(host)
	if host == "" {
		return ""
	}

	port := ""
	if strings.HasPrefix(rest, ":") {
		rest = rest[1:]
		if !hadUser {
			digits, after, _ := strings.Cut(rest, "/")
			if isPort(digits) {
				port, rest = digits, after
			}
		}
	}
	return joinRemote(host, port, rest)
}

// joinRemote assembles the normalized "host[:port]/path" form.
func joinRemote(host, port, path string) string {
	path = strings.Trim(path, "/")
	path = strings.TrimSuffix(path, ".git")
	path = strings.TrimRight(path, "/")
	if strings.Contains(host, ":") {
		host = "[" + host + "]" // IPv6 literal
	}
	if port != "" {
		host += ":" + port
	}
	if path == "" {
		return host
	}
	return host + "/" + path
}

// isPort reports whether s is a non-empty string of ASCII digits.
func isPort(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
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
