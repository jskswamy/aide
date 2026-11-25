package doctor

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/fsutil"
	"github.com/jskswamy/aide/internal/homepath"
)

// Scope is where a sandbox block lives, which decides the command that
// edits it.
type Scope struct {
	Profile   string
	Global    bool   // the profile's own block in the global config
	Named     string // a named sandbox the profile uses
	Inherited bool   // the main checkout's .aide.yaml: no command edits it
	Source    string // the file, for the report
	// Base is the directory relative rules are read from: the
	// .aide.yaml's own directory. Empty skips relative rules.
	Base string
}

// RedundantAllows reports rules in readable_extra or writable_extra that
// an earlier rule in the same list already covers. Coverage is decided
// the way the sandbox grants: a rule grants its literal path, plus its
// symlink target only when that target is under home; the path the
// kernel checks is the child's fully resolved one. A rule is kept when
// a denied_extra path overlaps it under the covering rule, since the
// narrower allow may be what re-opens it. Rules with templates or
// relative paths are skipped.
//
// shortcut: guards (project secrets, dev credentials) let a path through
// only when it is listed exactly, and they protect dotfiles, so a rule
// with a dot-named segment below its parent is never reported; upgrade
// to asking the guards directly if a guard ever protects other paths.
func RedundantAllows(sp *config.SandboxPolicy, home string, s Scope) []Finding {
	if sp == nil {
		return nil
	}
	var denied []string
	for _, d := range sp.DeniedExtra {
		if r, ok := resolveRule(d, home, s.Base); ok {
			denied = append(denied, r.kernel)
		}
	}
	var out []Finding
	for _, list := range []struct {
		name  string
		write bool
		paths []string
	}{{"readable_extra", false, sp.ReadableExtra}, {"writable_extra", true, sp.WritableExtra}} {
		var kept []grant // what the rules before this one grant
		for _, rule := range list.paths {
			r, ok := resolveRule(rule, home, s.Base)
			if !ok {
				continue
			}
			if parent, covered := coveredBy(rule, r, kept, denied); covered {
				out = append(out, redundantFinding(rule, parent, list.name, list.write, s))
				continue
			}
			for _, g := range r.grants {
				kept = append(kept, grant{rule: rule, path: g})
			}
		}
	}
	return out
}

type grant struct{ rule, path string }

func coveredBy(rule string, r resolved, kept []grant, denied []string) (string, bool) {
	p := r.kernel
	for _, g := range kept {
		// Removal matches the text exactly, so an identical duplicate
		// cannot be removed without dropping the rule itself. Guards
		// match the rule as written, so its literal path counts too.
		if g.rule == rule || !fsutil.IsUnderDir(p, g.path) || hasDotSegment(p, g.path) || dotBelow(r.literal, kept) {
			continue
		}
		overlap := false
		for _, d := range denied {
			if fsutil.IsUnderDir(d, g.path) && (fsutil.IsUnderDir(d, p) || fsutil.IsUnderDir(p, d)) {
				overlap = true
			}
		}
		if !overlap {
			return g.rule, true
		}
	}
	return "", false
}

// hasDotSegment reports whether p has a dot-named segment below parent.
func hasDotSegment(p, parent string) bool {
	rel, err := filepath.Rel(parent, p)
	if err != nil {
		return true
	}
	for seg := range strings.SplitSeq(rel, string(filepath.Separator)) {
		if strings.HasPrefix(seg, ".") && seg != "." {
			return true
		}
	}
	return false
}

// dotBelow reports whether literal has a dot-named segment below any
// granted path it sits under.
func dotBelow(literal string, kept []grant) bool {
	for _, g := range kept {
		if fsutil.IsUnderDir(literal, g.path) && hasDotSegment(literal, g.path) {
			return true
		}
	}
	return false
}

type resolved struct {
	literal string   // the rule as an absolute path, links not followed
	kernel  string   // the path the kernel checks: fully resolved
	grants  []string // what the sandbox grants for this rule
}

// resolveRule expands ~ and works out what the sandbox grants for a
// rule and which path the kernel would check for it.
func resolveRule(rule, home, base string) (resolved, bool) {
	if strings.Contains(rule, "{{") {
		return resolved{}, false
	}
	p := homepath.Expand(rule, home)
	if !filepath.IsAbs(p) {
		if base == "" {
			return resolved{}, false
		}
		p = filepath.Join(base, p)
	}
	literal := filepath.Clean(p)
	kernel := resolveExisting(literal)
	r := resolved{literal: literal, kernel: kernel, grants: []string{literal}}
	// The sandbox widens a rule to its symlink target only under home.
	if kernel != literal && fsutil.IsUnderDir(kernel, resolveExisting(filepath.Clean(home))) {
		r.grants = append(r.grants, kernel)
	}
	return r, true
}

// resolveExisting resolves symlinks in the longest existing prefix of p,
// so a path that does not exist yet still compares with ones that do
// (macOS temp dirs sit behind /var -> /private/var).
func resolveExisting(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	parent := filepath.Dir(p)
	if parent == p {
		return p
	}
	return filepath.Join(resolveExisting(parent), filepath.Base(p))
}

func redundantFinding(rule, parent, list string, write bool, s Scope) Finding {
	f := Finding{
		ID: "sandbox.redundant-allow", Subject: rule, Profile: s.Profile, Severity: SeverityTip, Source: s.Source,
		Message: fmt.Sprintf("%s in %s is already covered by %s", rule, list, parent),
	}
	switch {
	case s.Inherited:
		f.Layer = "inherited"
		f.Fix.Steps = []string{fmt.Sprintf("Remove %s from %s in the main checkout's .aide.yaml", rule, list)}
	case s.Named != "":
		flag := "--remove-readable"
		if write {
			flag = "--remove-writable"
		}
		f.Layer = "global"
		f.Fix.Argv = []string{"aide", "sandbox", "edit", s.Named, flag, rule}
	default:
		f.Layer = "local"
		f.Fix.Argv = []string{"aide", "sandbox", "allow", "--remove"}
		if write {
			f.Fix.Argv = append(f.Fix.Argv, "--write")
		}
		f.Fix.Argv = append(f.Fix.Argv, rule)
		if s.Global {
			f.Layer = "global"
			f.Fix.Argv = append(f.Fix.Argv, "--global", "--profile", s.Profile)
		}
	}
	return f
}
