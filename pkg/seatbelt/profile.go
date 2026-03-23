package seatbelt

import "sort"

// Profile composes Seatbelt modules into a complete .sb profile.
type Profile struct {
	modules []Module
	ctx     Context
}

// New creates a profile builder for the given home directory.
func New(homeDir string) *Profile {
	return &Profile{
		ctx: Context{HomeDir: homeDir},
	}
}

// Use adds modules to the profile. Modules render in the order added.
func (p *Profile) Use(modules ...Module) *Profile {
	p.modules = append(p.modules, modules...)
	return p
}

// WithContext sets additional context fields.
func (p *Profile) WithContext(fn func(*Context)) *Profile {
	fn(&p.ctx)
	return p
}

// Render generates the Seatbelt .sb profile string.
// Rules from all modules are collected, stable-sorted by intent, then rendered.
// This ensures Setup(100) rules appear first, Restrict(200) second, Grant(300) last,
// leveraging Seatbelt's last-rule-wins semantics.
func (p *Profile) Render() (string, error) {
	if len(p.modules) == 0 {
		return "", nil
	}
	var allRules []taggedRule
	for _, m := range p.modules {
		rules := m.Rules(&p.ctx)
		for _, r := range rules {
			allRules = append(allRules, taggedRule{module: m.Name(), rule: r})
		}
	}
	sort.SliceStable(allRules, func(i, j int) bool {
		return allRules[i].rule.intent < allRules[j].rule.intent
	})
	return renderTaggedRules(allRules), nil
}
