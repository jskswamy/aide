package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/jskswamy/aide/internal/capability"
	"github.com/jskswamy/aide/internal/config"
)

func TestResolveTerminalBrowserGrants_DefaultXDG(t *testing.T) {
	home := t.TempDir()
	dataDir := filepath.Join(home, ".local", "share", "terminal-browser-58a67c5b")
	stateDir := filepath.Join(home, ".local", "state", "terminal-browser-58a67c5b")
	devDir := filepath.Join(home, ".local", "state", "terminal-browser-dev-abcdef01")
	interopState := filepath.Join(home, ".local", "state", "terminal-browser-interop")
	interopShare := filepath.Join(home, ".local", "share", "terminal-browser-interop")
	cfgDir := filepath.Join(home, ".config", "terminal-browser")
	sibling := filepath.Join(home, ".local", "share", "other-app")

	for _, d := range []string{dataDir, stateDir, devDir, interopState, interopShare, cfgDir, sibling} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	noEnv := func(string) string { return "" }
	g := resolveTerminalBrowserGrants(nil, home, noEnv, "darwin")

	mustContain := func(list []string, raw, label string) {
		t.Helper()
		resolved, _ := filepath.EvalSymlinks(raw)
		if !slices.Contains(list, resolved) {
			t.Errorf("%s missing %q; got %v", label, resolved, list)
		}
	}

	mustContain(g.Writable, dataDir, "Writable")
	mustContain(g.Writable, stateDir, "Writable")
	mustContain(g.Writable, devDir, "Writable")
	mustContain(g.Writable, interopState, "Writable")
	mustContain(g.Writable, interopShare, "Writable")
	mustContain(g.Readable, cfgDir, "Readable")

	resolvedSibling, _ := filepath.EvalSymlinks(sibling)
	if slices.Contains(g.Writable, resolvedSibling) || slices.Contains(g.Readable, resolvedSibling) {
		t.Errorf("sibling dir %q must not be granted", resolvedSibling)
	}
	resolvedCfg, _ := filepath.EvalSymlinks(cfgDir)
	if slices.Contains(g.Writable, resolvedCfg) {
		t.Errorf("config dir %q must be read-only, not writable", resolvedCfg)
	}

	// macOS emits scoped unix-socket outbound + bind Allow rules for state, runtime & state interop dirs.
	for _, sd := range []string{stateDir, devDir, interopState} {
		r, _ := filepath.EvalSymlinks(sd)
		for _, wantRule := range []string{
			fmt.Sprintf(`network-outbound (remote unix-socket (subpath "%s"))`, r),
			fmt.Sprintf(`network-bind (local unix-socket (subpath "%s"))`, r),
		} {
			if !slices.Contains(g.Allow, wantRule) {
				t.Errorf("Allow missing %q; got %v", wantRule, g.Allow)
			}
		}
	}

	// Linux must not emit macOS Seatbelt Allow rules.
	gLinux := resolveTerminalBrowserGrants(nil, home, noEnv, "linux")
	if len(gLinux.Allow) != 0 {
		t.Errorf("linux Allow = %v, want empty", gLinux.Allow)
	}
	mustContain(gLinux.Writable, dataDir, "linux Writable")
}

func TestResolveTerminalBrowserGrants_EnvOverridesAndSafety(t *testing.T) {
	home := t.TempDir()
	customInterop := filepath.Join(home, "custom-interop")
	customCfg := filepath.Join(home, "custom-tb-cfg")
	sshDir := filepath.Join(home, ".ssh")
	for _, d := range []string{customInterop, customCfg, sshDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	env := func(k string) string {
		switch k {
		case "TERMINAL_BROWSER_INTEROP_DIR":
			return customInterop
		case "TERMINAL_BROWSER_CONFIG_DIR":
			return customCfg
		}
		return ""
	}
	g := resolveTerminalBrowserGrants(nil, home, env, "darwin")
	rInterop, _ := filepath.EvalSymlinks(customInterop)
	rCfg, _ := filepath.EvalSymlinks(customCfg)
	if !slices.Contains(g.Writable, rInterop) {
		t.Errorf("custom interop %q not in Writable: %v", rInterop, g.Writable)
	}
	wantRule := fmt.Sprintf(`network-outbound (remote unix-socket (subpath "%s"))`, rInterop)
	if !slices.Contains(g.Allow, wantRule) {
		t.Errorf("custom interop socket Allow missing %q; got %v", wantRule, g.Allow)
	}
	if !slices.Contains(g.Readable, rCfg) {
		t.Errorf("custom config %q not in Readable: %v", rCfg, g.Readable)
	}

	// Unsafe overrides (~/.ssh, /etc, $HOME) must be rejected.
	for _, bad := range []string{sshDir, "/etc", home} {
		badEnv := func(k string) string {
			if k == "TERMINAL_BROWSER_INTEROP_DIR" || k == "TERMINAL_BROWSER_CONFIG_DIR" {
				return bad
			}
			return ""
		}
		bg := resolveTerminalBrowserGrants(nil, home, badEnv, "darwin")
		if len(bg.Writable) != 0 || len(bg.Readable) != 0 || len(bg.Allow) != 0 {
			t.Errorf("unsafe override %q must produce empty grants, got %+v", bad, bg)
		}
	}

	// Path containing quote must not produce Writable, Readable, or Allow rules on macOS.
	quoteDir := filepath.Join(home, `tb"inject`)
	if err := os.MkdirAll(quoteDir, 0o755); err != nil {
		t.Fatal(err)
	}
	quoteEnv := func(k string) string {
		if k == "TERMINAL_BROWSER_INTEROP_DIR" || k == "TERMINAL_BROWSER_CONFIG_DIR" {
			return quoteDir
		}
		return ""
	}
	qg := resolveTerminalBrowserGrants(nil, home, quoteEnv, "darwin")
	if len(qg.Writable) != 0 || len(qg.Readable) != 0 || len(qg.Allow) != 0 {
		t.Errorf("path with quote must not emit macOS Writable/Readable/Allow rules, got %+v", qg)
	}
}

func TestAddDynamicPathsWithEnv_UsesProfileEnv(t *testing.T) {
	home := t.TempDir()
	profileInterop := filepath.Join(home, "profile-browser-interop")
	if err := os.MkdirAll(profileInterop, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("TERMINAL_BROWSER_INTEROP_DIR", "")
	t.Setenv("TERMINAL_BROWSER_CONFIG_DIR", "")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	profileEnv := func(k string) string {
		if k == "TERMINAL_BROWSER_INTEROP_DIR" {
			return profileInterop
		}
		return ""
	}
	_, overrides, _, err := ResolveCapabilitiesWithVariants(
		[]string{"terminal-browser"},
		&config.Config{},
		VariantSelectionOptions{EnvLookup: profileEnv},
	)
	if err != nil {
		t.Fatalf("ResolveCapabilitiesWithVariants: %v", err)
	}
	rInterop, _ := filepath.EvalSymlinks(profileInterop)
	if !slices.Contains(overrides.WritableExtra, rInterop) {
		t.Errorf("profile interop %q not in WritableExtra: %v", rInterop, overrides.WritableExtra)
	}
}

func TestAddDynamicPaths_InheritsViaSources(t *testing.T) {
	home := t.TempDir()
	dataDir := filepath.Join(home, ".local", "share", "terminal-browser-58a67c5b")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("TERMINAL_BROWSER_INTEROP_DIR", "")
	t.Setenv("TERMINAL_BROWSER_CONFIG_DIR", "")

	caps := []capability.ResolvedCapability{
		{Name: "my-custom-tb", Sources: []string{"my-custom-tb", "terminal-browser"}},
		{Name: "unrelated", Sources: []string{"unrelated"}},
	}
	AddDynamicPaths(caps, &config.Config{})

	rData, _ := filepath.EvalSymlinks(dataDir)
	if !slices.Contains(caps[0].Writable, rData) {
		t.Errorf("inherited capability missing %q in Writable: %v", rData, caps[0].Writable)
	}
	if len(caps[1].Writable) != 0 || len(caps[1].Readable) != 0 || len(caps[1].Allow) != 0 {
		t.Errorf("unrelated capability should not receive dynamic paths: %+v", caps[1])
	}
}

func TestResolveTerminalBrowserGrants_DistinctRuntimeAndStateSocketRules(t *testing.T) {
	home := t.TempDir()
	stateDir := filepath.Join(home, ".local", "state", "terminal-browser-58a67c5b")
	rtRoot := filepath.Join(home, "custom-runtime")
	rtDir := filepath.Join(rtRoot, "terminal-browser-58a67c5b")
	for _, d := range []string{stateDir, rtDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	env := func(k string) string {
		if k == "XDG_RUNTIME_DIR" {
			return rtRoot
		}
		return ""
	}
	g := resolveTerminalBrowserGrants(nil, home, env, "darwin")
	for _, sd := range []string{stateDir, rtDir} {
		r, _ := filepath.EvalSymlinks(sd)
		for _, wantRule := range []string{
			fmt.Sprintf(`network-outbound (remote unix-socket (subpath "%s"))`, r),
			fmt.Sprintf(`network-bind (local unix-socket (subpath "%s"))`, r),
		} {
			if !slices.Contains(g.Allow, wantRule) {
				t.Errorf("Allow missing %q when XDG_RUNTIME_DIR differs from state dir; got %v", wantRule, g.Allow)
			}
		}
	}
}
