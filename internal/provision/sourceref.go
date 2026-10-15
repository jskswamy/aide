package provision

import "strings"

// SourceRef is the canonical parsed form of a marketplace / plugin
// source ref. Aide's internal vocabulary covers seven transport
// prefixes — `github:`, `git:`, `https://`, `http://`, `npm:`,
// `file://`, and a leading `/` for local paths — plus the bare
// `owner/repo` shape that defaults to GitHub. `npm:` and `file://`
// only appear as URL-direct plugin sources (OpenCode, Pi).
//
// This file is the single place new transports get added. Callers
// (`ResolveDesired` and per-agent drivers) must route through
// ParseSourceRef so the prefix vocabulary cannot drift across the
// codebase.
type SourceRef struct {
	// Transport is the scheme: "github", "git", "https", "http",
	// "npm", "file", "local", or "" for bare repo refs (which default
	// to github).
	Transport string
	// Repo is the part after the transport — e.g. "owner/repo",
	// "/abs/path", or "host/path".
	Repo string
}

// ParseSourceRef classifies s by recognised prefix and returns the
// split form. Bare strings parse as a GitHub repo with Transport="".
func ParseSourceRef(s string) SourceRef {
	if rest, ok := strings.CutPrefix(s, "github:"); ok {
		return SourceRef{Transport: "github", Repo: rest}
	}
	if rest, ok := strings.CutPrefix(s, "git:"); ok {
		return SourceRef{Transport: "git", Repo: rest}
	}
	if rest, ok := strings.CutPrefix(s, "https://"); ok {
		return SourceRef{Transport: "https", Repo: rest}
	}
	if rest, ok := strings.CutPrefix(s, "http://"); ok {
		return SourceRef{Transport: "http", Repo: rest}
	}
	if rest, ok := strings.CutPrefix(s, "npm:"); ok {
		return SourceRef{Transport: "npm", Repo: rest}
	}
	if rest, ok := strings.CutPrefix(s, "file://"); ok {
		return SourceRef{Transport: "file", Repo: rest}
	}
	if strings.HasPrefix(s, "/") {
		return SourceRef{Transport: "local", Repo: s}
	}
	return SourceRef{Transport: "", Repo: s}
}

// Aide returns the canonical aide-internal form. Bare repos get a
// `github:` prefix; everything else round-trips unchanged.
func (r SourceRef) Aide() string {
	switch r.Transport {
	case "":
		return "github:" + r.Repo
	case "github":
		return "github:" + r.Repo
	case "git":
		return "git:" + r.Repo
	case "https":
		return "https://" + r.Repo
	case "http":
		return "http://" + r.Repo
	case "npm":
		return "npm:" + r.Repo
	case "file":
		return "file://" + r.Repo
	case "local":
		return r.Repo
	}
	return r.Repo
}

// Bare returns the form CLIs that reject the `github:` prefix expect
// (e.g. claude's `plugin marketplace add`). Non-github transports
// pass through with their scheme intact.
func (r SourceRef) Bare() string {
	switch r.Transport {
	case "", "github":
		return r.Repo
	case "https":
		return "https://" + r.Repo
	case "http":
		return "http://" + r.Repo
	case "git":
		return "git:" + r.Repo
	case "npm":
		return "npm:" + r.Repo
	case "file":
		return "file://" + r.Repo
	case "local":
		return r.Repo
	}
	return r.Repo
}

// Classify returns the Plugin.Source label for the URLDirect shape:
// "github" (including bare owner/repo), "git", "url" (http/https),
// "npm", "file" or "local". URL-direct sources never go through a
// marketplace, so none map to "marketplace".
func (r SourceRef) Classify() string {
	switch r.Transport {
	case "", "github":
		return "github"
	case "https", "http":
		return "url"
	default:
		return r.Transport
	}
}
