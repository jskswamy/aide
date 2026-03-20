package seatbelt

import "strings"

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
func (p *Profile) Render() (string, error) {
	if len(p.modules) == 0 {
		return "", nil
	}
	var b strings.Builder
	for _, m := range p.modules {
		b.WriteString(renderModule(m, &p.ctx))
	}
	return b.String(), nil
}
