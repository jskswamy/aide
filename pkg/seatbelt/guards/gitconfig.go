package guards

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"

	gitconfig "github.com/go-git/go-git/v5/plumbing/format/config"
)

// GitConfigResult holds all file paths discovered from git configuration.
type GitConfigResult struct {
	ConfigFiles    []string
	ExcludesFile   string
	AttributesFile string
	GPGSign        bool   // commit.gpgsign = true found in any config
	GPGProgram     string // gpg.program override (empty = default gpg)
	Err            error
	Warnings       []string
}

// AllPaths returns a deduplicated list of all file paths from git configuration.
func (r *GitConfigResult) AllPaths() []string {
	seen := make(map[string]bool)
	var out []string
	add := func(p string) {
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, f := range r.ConfigFiles {
		add(f)
	}
	add(r.ExcludesFile)
	add(r.AttributesFile)
	return out
}

const maxIncludeDepth = 10

// ParseGitConfig parses git configuration files using standard well-known paths,
// without any environment variable overrides.
func ParseGitConfig(homeDir, projectRoot string, envLookup func(string) (string, bool)) *GitConfigResult {
	if envLookup == nil {
		envLookup = func(_ string) (string, bool) { return "", false }
	}
	return ParseGitConfigWithEnv(homeDir, projectRoot, envLookup)
}

// ParseGitConfigWithEnv parses git configuration files, honouring GIT_CONFIG_GLOBAL
// and GIT_CONFIG_SYSTEM environment variables when provided via envLookup.
func ParseGitConfigWithEnv(homeDir, projectRoot string, envLookup func(string) (string, bool)) *GitConfigResult {
	result := &GitConfigResult{}
	globalConfig := ResolveSymlink(filepath.Join(homeDir, ".gitconfig"))
	xdgConfig := ResolveSymlink(xdgGitConfigPath(homeDir, envLookup))
	systemConfig := ResolveSymlink("/etc/gitconfig")

	if val, ok := envLookup("GIT_CONFIG_GLOBAL"); ok && val != "" {
		globalConfig = ResolveSymlink(ExpandTilde(val, homeDir))
	}
	if val, ok := envLookup("GIT_CONFIG_SYSTEM"); ok && val != "" {
		systemConfig = ResolveSymlink(ExpandTilde(val, homeDir))
	}

	result.ConfigFiles = []string{
		globalConfig, xdgConfig, systemConfig,
		ResolveSymlink(filepath.Join(homeDir, ".config", "git", "ignore")),
		ResolveSymlink(filepath.Join(homeDir, ".config", "git", "attributes")),
	}

	result.ExcludesFile = ResolveSymlink(filepath.Join(homeDir, ".gitignore"))
	result.AttributesFile = ResolveSymlink(filepath.Join(homeDir, ".config", "git", "attributes"))

	parsed := parseConfigFile(globalConfig)
	if parsed == nil {
		parsed = parseConfigFile(xdgConfig)
	}
	if parsed == nil {
		result.Warnings = append(result.Warnings, "no global gitconfig found, using defaults")
		return result
	}

	if val := configValue(parsed, "core", "", "excludesFile"); val != "" {
		result.ExcludesFile = ResolveSymlink(ExpandTilde(val, homeDir))
	}
	if val := configValue(parsed, "core", "", "attributesFile"); val != "" {
		result.AttributesFile = ResolveSymlink(ExpandTilde(val, homeDir))
	}
	extractGPGConfig(parsed, result)

	resolveIncludes(parsed, homeDir, projectRoot, result, 0)
	return result
}

func parseConfigFile(path string) *gitconfig.Config {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	cfg := gitconfig.New()
	decoder := gitconfig.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(cfg); err != nil {
		return nil
	}
	return cfg
}

func configValue(cfg *gitconfig.Config, section, subsection, key string) string {
	s := cfg.Section(section)
	if s == nil {
		return ""
	}
	if subsection != "" {
		ss := s.Subsection(subsection)
		if ss == nil {
			return ""
		}
		return ss.Option(key)
	}
	return s.Option(key)
}

func resolveIncludes(cfg *gitconfig.Config, homeDir, projectRoot string, result *GitConfigResult, depth int) {
	if depth >= maxIncludeDepth {
		result.Warnings = append(result.Warnings, "max include depth reached")
		return
	}

	includeSection := cfg.Section("include")
	if includeSection != nil {
		for _, opt := range includeSection.Options {
			if opt.Key == "path" {
				path := ExpandTilde(opt.Value, homeDir)
				if !filepath.IsAbs(path) {
					path = filepath.Join(homeDir, path)
				}
				resolved := ResolveSymlink(path)
				result.ConfigFiles = append(result.ConfigFiles, resolved)
				if parsed := parseConfigFile(resolved); parsed != nil {
					extractCoreOverrides(parsed, homeDir, result)
					resolveIncludes(parsed, homeDir, projectRoot, result, depth+1)
				}
			}
		}
	}

	for _, ss := range cfg.Section("includeIf").Subsections {
		if !evaluateIncludeCondition(ss.Name, projectRoot, homeDir) {
			continue
		}
		path := ss.Option("path")
		if path == "" {
			continue
		}
		path = ExpandTilde(path, homeDir)
		if !filepath.IsAbs(path) {
			path = filepath.Join(homeDir, path)
		}
		resolved := ResolveSymlink(path)
		result.ConfigFiles = append(result.ConfigFiles, resolved)
		if parsed := parseConfigFile(resolved); parsed != nil {
			extractCoreOverrides(parsed, homeDir, result)
			resolveIncludes(parsed, homeDir, projectRoot, result, depth+1)
		}
	}
}

// extractCoreOverrides checks an included config for core.excludesFile,
// core.attributesFile, commit.gpgsign, and gpg.program, updating the
// result with last-one-wins semantics matching git's actual behavior.
// Without this, values set in included configs (via [include] or
// [includeIf]) would be missed.
func extractCoreOverrides(cfg *gitconfig.Config, homeDir string, result *GitConfigResult) {
	if val := configValue(cfg, "core", "", "excludesFile"); val != "" {
		result.ExcludesFile = ResolveSymlink(ExpandTilde(val, homeDir))
	}
	if val := configValue(cfg, "core", "", "attributesFile"); val != "" {
		result.AttributesFile = ResolveSymlink(ExpandTilde(val, homeDir))
	}
	extractGPGConfig(cfg, result)
}

// extractGPGConfig checks for commit.gpgsign and gpg.program.
func extractGPGConfig(cfg *gitconfig.Config, result *GitConfigResult) {
	if val := configValue(cfg, "commit", "", "gpgsign"); strings.EqualFold(val, "true") {
		result.GPGSign = true
	}
	if val := configValue(cfg, "gpg", "", "program"); val != "" {
		result.GPGProgram = val
	}
}

func evaluateIncludeCondition(condition, projectRoot, homeDir string) bool {
	if projectRoot == "" {
		return false
	}
	if pattern, ok := strings.CutPrefix(condition, "gitdir:"); ok {
		return matchGitDir(pattern, projectRoot, homeDir, false)
	}
	if pattern, ok := strings.CutPrefix(condition, "gitdir/i:"); ok {
		return matchGitDir(pattern, projectRoot, homeDir, true)
	}
	return false
}

func matchGitDir(pattern, projectRoot, homeDir string, caseInsensitive bool) bool {
	gitDir := filepath.Join(projectRoot, ".git")
	pattern = ExpandTilde(pattern, homeDir)

	// Trailing / in gitdir: patterns means "match as prefix" (any repo
	// under this directory). expandTilde uses string concatenation
	// instead of filepath.Join to preserve the trailing slash.
	if strings.HasSuffix(pattern, "/") {
		prefix := pattern
		target := gitDir + "/"
		if caseInsensitive {
			prefix = strings.ToLower(prefix)
			target = strings.ToLower(target)
		}
		return strings.HasPrefix(target, prefix)
	}

	if caseInsensitive {
		return strings.EqualFold(gitDir, pattern)
	}
	return gitDir == pattern
}

func xdgGitConfigPath(homeDir string, envLookup func(string) (string, bool)) string {
	if envLookup != nil {
		if xdg, ok := envLookup("XDG_CONFIG_HOME"); ok && xdg != "" {
			return filepath.Join(xdg, "git", "config")
		}
	}
	return filepath.Join(homeDir, ".config", "git", "config")
}
