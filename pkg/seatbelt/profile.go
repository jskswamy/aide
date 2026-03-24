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

// ProfileResult holds the rendered profile and per-guard diagnostics.
type ProfileResult struct {
	Profile string        // rendered seatbelt profile text
	Guards  []GuardResult // per-guard diagnostics for banner display
}

// Render generates the Seatbelt .sb profile string.
// Rules from all modules are collected, stable-sorted by intent, then rendered.
// Allow(100) rules appear first, then Deny(200). The sort order is for
// readability — Seatbelt uses deny-wins-over-allow semantics.
func (p *Profile) Render() (ProfileResult, error) {
	if len(p.modules) == 0 {
		return ProfileResult{}, nil
	}
	var allRules []taggedRule
	var guardResults []GuardResult
	for _, m := range p.modules {
		result := m.Rules(&p.ctx)
		result.Name = m.Name()
		guardResults = append(guardResults, result)
		for _, r := range result.Rules {
			allRules = append(allRules, taggedRule{module: m.Name(), rule: r})
		}
	}
	sort.SliceStable(allRules, func(i, j int) bool {
		return allRules[i].rule.intent < allRules[j].rule.intent
	})
	return ProfileResult{
		Profile: renderTaggedRules(allRules),
		Guards:  guardResults,
	}, nil
}
