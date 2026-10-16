package launcher

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/jskswamy/aide/internal/config"
)

// codexEnvNameRE matches the variable names Codex reads; anything else
// in env_key is ignored rather than scrubbed.
var codexEnvNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// codexNeverScrubbed are process essentials an env_key must not remove
// from the launch environment.
var codexNeverScrubbed = []string{
	"PATH", "HOME", "USER", "SHELL", "TERM", "LANG", "TMPDIR",
	"XDG_RUNTIME_DIR", "XDG_CONFIG_HOME",
}

// CodexProviderEnvNames returns, sorted, the custom model provider key
// and header variables named in every Codex home aide can derive from
// cfg: the default ~/.codex, an exported CODEX_HOME (getenv reads the
// inherited environment), and each profile's own Codex dir (any
// profile can launch codex with --agent). Like credential binding
// names, they are configured custom credentials, so every configured
// launch and provisioning subprocess scrubs them from the inherited
// environment. Unreadable or invalid files contribute nothing here; the
// launch notes one only for the home it launches.
func CodexProviderEnvNames(cfg *config.Config, home string, getenv func(string) string) []string {
	seen := map[string]bool{}
	for _, dir := range codexHomes(cfg, home, getenv) {
		names, _ := codexProviderEnvKeys(dir)
		for _, n := range names {
			seen[n] = true
		}
	}
	return slices.Sorted(maps.Keys(seen))
}

// codexHomes lists, without duplicates, the Codex homes of cfg.
func codexHomes(cfg *config.Config, home string, getenv func(string) string) []string {
	var dirs []string
	add := func(dir string) {
		if dir != "" && !slices.Contains(dirs, dir) {
			dirs = append(dirs, dir)
		}
	}
	add(launchConfigDir("codex", config.Context{}, home, func(string) string { return "" }))
	add(launchConfigDir("codex", config.Context{}, home, getenv))
	for _, name := range slices.Sorted(maps.Keys(cfg.Contexts)) {
		if ctx := cfg.Contexts[name]; ctx.SelectsConfigDir() {
			add(ctx.NativeConfigDir("codex", home))
		}
	}
	return dirs
}

// codexConfigNoticeFor is the note for the Codex home a launch of
// harnessName uses, when its config cannot be read; "" otherwise.
func codexConfigNoticeFor(harnessName string, ctx config.Context, home string, getenv func(string) string) string {
	if harnessName != "codex" {
		return ""
	}
	_, notice := codexProviderEnvKeys(launchConfigDir(harnessName, ctx, home, getenv))
	return notice
}

// codexConfig is the part of a Codex config.toml that names provider
// credential variables (https://learn.chatgpt.com/docs/config-file/config-reference):
// model_providers.<id>.env_key and the variables behind
// model_providers.<id>.env_http_headers. Values are never read.
type codexConfig struct {
	ModelProviders map[string]struct {
		EnvKey         string            `toml:"env_key"`
		EnvHTTPHeaders map[string]string `toml:"env_http_headers"`
	} `toml:"model_providers"`
}

// codexProviderEnvKeys returns, sorted, the variable names the custom
// model providers in dir/config.toml and its profile files
// (dir/<name>.config.toml) read their keys and headers from. A missing
// file declares nothing. An unreadable or invalid file contributes no
// names and yields a notice naming the file, never its content.
func codexProviderEnvKeys(dir string) (names []string, notice string) {
	files := []string{filepath.Join(dir, "config.toml")}
	if profiles, err := filepath.Glob(filepath.Join(dir, "*.config.toml")); err == nil {
		files = append(files, profiles...)
	}
	seen := map[string]bool{}
	for _, path := range files {
		data, err := os.ReadFile(path) // #nosec G304 -- the profile's own Codex config
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		var cfg codexConfig
		if err == nil {
			err = toml.Unmarshal(data, &cfg)
		}
		if err != nil {
			if notice == "" {
				notice = codexConfigNotice(path, err)
			}
			continue
		}
		for _, p := range cfg.ModelProviders {
			for _, n := range append([]string{p.EnvKey}, mapValues(p.EnvHTTPHeaders)...) {
				if codexEnvNameRE.MatchString(n) && !slices.Contains(codexNeverScrubbed, n) && !strings.HasPrefix(n, "AIDE_") {
					seen[n] = true
				}
			}
		}
	}
	for n := range seen {
		names = append(names, n)
	}
	slices.Sort(names)
	return names, notice
}

// codexConfigNotice explains that path's provider keys are not
// scrubbed. A TOML error is reduced to its line so no content leaks.
func codexConfigNotice(path string, err error) string {
	reason := "cannot be read"
	var decodeErr *toml.DecodeError
	if errors.As(err, &decodeErr) {
		row, _ := decodeErr.Position()
		reason = fmt.Sprintf("is not valid TOML (line %d)", row)
	}
	return fmt.Sprintf("%s %s: custom model provider keys it names are not scrubbed from the shell", path, reason)
}

func mapValues(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}
