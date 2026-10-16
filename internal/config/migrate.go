package config

import (
	"bytes"
	"errors"
	"fmt"
	"slices"

	"gopkg.in/yaml.v3"

	"github.com/jskswamy/aide/internal/harness"
)

// ErrAdopt reports an --adopt choice migration cannot apply.
var ErrAdopt = errors.New("cannot adopt global login")

// MigrateOptions tunes MigrateProfiles.
type MigrateOptions struct {
	// Home expands ~ in native config dirs for validation.
	Home string
	// Adopt names profiles that keep their harness's global login:
	// each gets config_dir_name: <profile> and config_dirs pinning its
	// harness to the harness's default dir.
	Adopt []string
}

// MigratedCredential is a legacy env: provider credential rewritten
// as a credentials: binding. It names the store and key, never the
// value.
type MigratedCredential struct {
	Profile   string
	Name      string
	EnvVar    string
	Store     string
	Key       string
	Harnesses []string
}

// GlobalLogin is a profile that selects no native config dir, so its
// harness runs from the harness's default dir and shares that login
// with every other such profile.
type GlobalLogin struct {
	Profile string
	Harness string
	Dir     string // ~-relative default dir
	Adopted bool
}

// Migration is the result of MigrateProfiles. Nothing is written.
type Migration struct {
	// Output is the migrated config text; the input unchanged when
	// Changed is false.
	Output  []byte
	Changed bool
	// Renamed lists the key renames applied, as "old -> new".
	Renamed      []string
	Credentials  []MigratedCredential
	GlobalLogins []GlobalLogin
}

// MigrateProfiles converts a config.yaml from the contexts: schema to
// the profiles: schema: contexts: -> profiles:, default_context: ->
// default_profile:, and in each entry profile: -> config_dir_name:
// and profile_dir: -> config_dir:. Provider credentials templated
// into env: from the profile's secret: store become credentials:
// bindings selected by default_credentials:. Profiles on a harness's
// global login are reported and changed only when opts.Adopt names
// them.
//
// The YAML node tree is edited in place, so comments and every
// unrelated key survive; blank lines and indentation are normalized
// by the encoder. The result is validated as a config before it is
// returned. A config already in the profiles: schema with nothing to
// translate or adopt, or a minimal (flat) config, is returned as is.
func MigrateProfiles(data []byte, opts MigrateOptions) (*Migration, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	var cfg Config
	if err := doc.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	m := &Migration{Output: data}
	if len(doc.Content) == 0 || cfg.IsMinimal() {
		return m, nil
	}
	root := doc.Content[0]

	if !cfg.ProfileSchema {
		m.renameTopLevel(root)
	}
	section := mappingValue(root, keyProfiles)
	names := sortedKeys(cfg.Contexts)

	for _, name := range names {
		ctx := cfg.Contexts[name]
		_, legacy, err := TranslateLegacyCredentials(ctx, ctx.Secret)
		if err != nil {
			return nil, fmt.Errorf("profiles.%s: %w", name, err)
		}
		entry := mappingValue(section, name)
		for _, l := range legacy {
			mc := MigratedCredential{Profile: name, Name: l.Name, EnvVar: l.EnvVar, Store: l.Binding.Store, Key: l.Binding.Key}
			if !IsBuiltinCredential(l.Name) {
				mc.Harnesses = []string{cfg.HarnessOf(ctx.Agent)}
			}
			if err := moveCredential(entry, mc); err != nil {
				return nil, fmt.Errorf("profiles.%s: %w", name, err)
			}
			m.Credentials = append(m.Credentials, mc)
		}
	}

	m.GlobalLogins = globalLogins(&cfg, names)
	adopted, err := m.adopt(section, opts.Adopt)
	if err != nil {
		return nil, err
	}

	if len(m.Renamed) == 0 && len(m.Credentials) == 0 && !adopted {
		return m, nil
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return nil, fmt.Errorf("encoding migrated config: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("encoding migrated config: %w", err)
	}
	if err := validateMigrated(buf.Bytes(), opts.Home); err != nil {
		return nil, err
	}
	m.Output = buf.Bytes()
	m.Changed = true
	return m, nil
}

// renameTopLevel renames the legacy top-level keys and each entry's
// native config dir keys.
func (m *Migration) renameTopLevel(root *yaml.Node) {
	for _, k := range [][2]string{{keyContexts, keyProfiles}, {keyDefaultContext, keyDefaultProfile}} {
		if mappingValue(root, k[0]) != nil {
			renameKey(root, k[0], k[1])
			m.Renamed = append(m.Renamed, k[0]+": -> "+k[1]+":")
		}
	}
	section := mappingValue(root, keyProfiles)
	if section == nil || section.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(section.Content); i += 2 {
		name, entry := section.Content[i].Value, section.Content[i+1]
		for _, k := range profileDirKeys {
			if mappingValue(entry, k[1]) != nil {
				renameKey(entry, k[1], k[0])
				m.Renamed = append(m.Renamed, fmt.Sprintf("profiles.%s.%s: -> %s:", name, k[1], k[0]))
			}
		}
	}
}

// moveCredential removes mc's env: entry from a profile entry node and
// adds the matching credentials: binding and default_credentials:
// selection.
func moveCredential(entry *yaml.Node, mc MigratedCredential) error {
	env := mappingValue(entry, "env")
	deleteKey(env, mc.EnvVar)
	if len(env.Content) == 0 {
		deleteKey(entry, "env")
	}
	binding := CredentialBinding{Backend: CredentialBackendAge, Store: mc.Store, Key: mc.Key}
	if !IsBuiltinCredential(mc.Name) {
		binding.Env = []string{mc.EnvVar}
		binding.Harnesses = mc.Harnesses
	}
	var value yaml.Node
	if err := value.Encode(binding); err != nil {
		return err
	}
	creds := ensureValue(entry, "credentials", yaml.MappingNode)
	creds.Content = append(creds.Content, scalar(mc.Name), &value)
	defaults := ensureValue(entry, "default_credentials", yaml.SequenceNode)
	defaults.Content = append(defaults.Content, scalar(mc.Name))
	return nil
}

// globalLogins lists the profiles whose harness supports profiles but
// which select no native config dir.
func globalLogins(cfg *Config, names []string) []GlobalLogin {
	var out []GlobalLogin
	for _, name := range names {
		ctx := cfg.Contexts[name]
		if ctx.SelectsConfigDir() {
			continue
		}
		h := cfg.HarnessOf(ctx.Agent)
		d, ok := harness.Lookup(h)
		if !ok || !d.SupportsProfiles() || d.DefaultConfigDir == "" {
			continue
		}
		out = append(out, GlobalLogin{Profile: name, Harness: d.Name, Dir: "~/" + d.DefaultConfigDir})
	}
	return out
}

// adopt pins each named profile to its harness's global dir. Two
// profiles cannot both be pinned to the same dir: they would share one
// login while appearing isolated.
func (m *Migration) adopt(section *yaml.Node, names []string) (bool, error) {
	pinned := map[string]string{} // dir -> profile
	for _, name := range names {
		i := slices.IndexFunc(m.GlobalLogins, func(g GlobalLogin) bool { return g.Profile == name })
		if i < 0 {
			if mappingValue(section, name) == nil {
				return false, fmt.Errorf("%w: no profile named %q", ErrAdopt, name)
			}
			return false, fmt.Errorf("%w: profile %q already selects its own config dir; only profiles on a harness's global login can be adopted", ErrAdopt, name)
		}
		gl := &m.GlobalLogins[i]
		if prev, ok := pinned[gl.Dir]; ok && prev != name {
			return false, fmt.Errorf("%w: profiles %q and %q would both use %s, one %s login for two identities; adopt one of them "+
				"and give the other its own config_dir_name, then log in there", ErrAdopt, prev, name, gl.Dir, gl.Harness)
		}
		pinned[gl.Dir] = name
		if gl.Adopted {
			continue
		}
		entry := mappingValue(section, name)
		ensureValue(entry, "config_dir_name", yaml.ScalarNode).Value = name
		dirs := ensureValue(entry, "config_dirs", yaml.MappingNode)
		dirs.Content = append(dirs.Content, scalar(gl.Harness), scalar(gl.Dir))
		gl.Adopted = true
	}
	return len(pinned) > 0, nil
}

// validateMigrated loads the migrated text the way Load would check
// profiles, so a migration never writes a config aide then refuses.
func validateMigrated(data []byte, home string) error {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("migrated config does not parse: %w", err)
	}
	checks := []func() error{cfg.validateCredentials}
	if home != "" {
		checks = append(checks, func() error { return cfg.validateConfigDirs(home) },
			func() error { return cfg.validateProfileDirs(home) })
	}
	for _, check := range checks {
		if err := check(); err != nil {
			return fmt.Errorf("migrated config would not load: %w", err)
		}
	}
	return nil
}

// ensureValue returns the value of key in mapping, adding an empty
// node of kind when absent.
func ensureValue(mapping *yaml.Node, key string, kind yaml.Kind) *yaml.Node {
	if v := mappingValue(mapping, key); v != nil {
		if v.Kind == kind || kind == yaml.ScalarNode {
			return v
		}
		// An empty value (key:) decodes as a null scalar; replace it.
		*v = yaml.Node{Kind: kind}
		return v
	}
	v := &yaml.Node{Kind: kind}
	if kind == yaml.ScalarNode {
		v.Tag = "!!str"
	}
	mapping.Content = append(mapping.Content, scalar(key), v)
	return v
}

func deleteKey(mapping *yaml.Node, key string) {
	if mapping == nil {
		return
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content = slices.Delete(mapping.Content, i, i+2)
			return
		}
	}
}

func scalar(v string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
}
