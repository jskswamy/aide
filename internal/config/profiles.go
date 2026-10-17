package config

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Profile schema errors. Callers branch with errors.Is.
var (
	// ErrMixedProfileDeclarations: one config file uses both the
	// profiles:/default_profile: keys and the legacy
	// contexts:/default_context: keys.
	ErrMixedProfileDeclarations = errors.New("config mixes profiles: and contexts: declarations")

	// ErrProfileNotProjectScoped: .aide.yaml tried to define a profile
	// or its native config dir. Project scope may only reference an
	// existing profile by name.
	ErrProfileNotProjectScoped = errors.New("profile not allowed in project override")
)

// Top-level keys of the two profile schemas. Both normalize into
// Config.Contexts / Config.DefaultContext.
const (
	keyProfiles       = "profiles"
	keyDefaultProfile = "default_profile"
	keyContexts       = "contexts"
	keyDefaultContext = "default_context"
)

// profileDirKeys maps each native config dir key a profiles: entry
// uses to the legacy contexts: key carrying the same value
// (Context.Profile / Context.ProfileDir).
var profileDirKeys = [][2]string{
	{"config_dir_name", "profile"},
	{"config_dir", "profile_dir"},
}

// projectDefiningKeys are .aide.yaml keys that would define a profile
// (or pick its secret store) rather than reference one.
var projectDefiningKeys = []string{
	keyProfiles, keyDefaultProfile, keyContexts, keyDefaultContext,
	"config_dir_name", "config_dir", "config_dirs", "profile_dir",
	"match", "credentials", "default_credentials", "secret",
}

// UnmarshalYAML decodes either profile schema. A profiles: file sets
// ProfileSchema so writers keep that shape; mixing schemas fails.
func (c *Config) UnmarshalYAML(node *yaml.Node) error {
	if err := rejectProfileDirKeysInContexts(mappingValue(node, keyContexts)); err != nil {
		return err
	}
	type plain Config
	if err := node.Decode((*plain)(c)); err != nil {
		return err
	}
	keys := mappingKeys(node)
	newKeys := presentKeys(keys, keyProfiles, keyDefaultProfile)
	legacyKeys := presentKeys(keys, keyContexts, keyDefaultContext)
	if len(newKeys) > 0 && len(legacyKeys) > 0 {
		return fmt.Errorf("%w: found %s together with %s; use one schema: "+
			"rename contexts: to profiles: and default_context: to default_profile: "+
			"(inside each profile, profile: becomes config_dir_name: and profile_dir: becomes config_dir:), "+
			"or keep only the legacy keys",
			ErrMixedProfileDeclarations, strings.Join(legacyKeys, " and "), strings.Join(newKeys, " and "))
	}
	if len(newKeys) == 0 {
		return nil
	}
	var raw struct {
		Profiles       yaml.Node `yaml:"profiles"`
		DefaultProfile string    `yaml:"default_profile"`
	}
	if err := node.Decode(&raw); err != nil {
		return err
	}
	profiles, err := decodeProfiles(&raw.Profiles)
	if err != nil {
		return err
	}
	c.Contexts = profiles
	c.DefaultContext = raw.DefaultProfile
	c.ProfileSchema = true
	return nil
}

// MarshalYAML writes the schema the config was loaded in.
func (c Config) MarshalYAML() (interface{}, error) {
	type plain Config
	if !c.ProfileSchema {
		return plain(c), nil
	}
	var node yaml.Node
	if err := node.Encode(plain(c)); err != nil {
		return nil, err
	}
	renameKey(&node, keyContexts, keyProfiles)
	renameKey(&node, keyDefaultContext, keyDefaultProfile)
	if profiles := mappingValue(&node, keyProfiles); profiles != nil {
		for i := 1; i < len(profiles.Content); i += 2 {
			for _, k := range profileDirKeys {
				renameKey(profiles.Content[i], k[1], k[0])
			}
		}
	}
	return &node, nil
}

// decodeProfiles decodes a profiles: mapping into Contexts, rejecting
// the legacy nested profile:/profile_dir: keys and mapping
// config_dir_name:/config_dir: onto Context.Profile/ProfileDir.
func decodeProfiles(node *yaml.Node) (map[string]Context, error) {
	if node.Kind == 0 || node.Tag == "!!null" {
		return nil, nil
	}
	if node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("profiles: expected a mapping (line %d)", node.Line)
	}
	out := make(map[string]Context, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		name := node.Content[i].Value
		entry := *node.Content[i+1]
		entry.Content = slices.Clone(entry.Content)
		for j := 0; j+1 < len(entry.Content); j += 2 {
			key := entry.Content[j].Value
			for _, k := range profileDirKeys {
				switch key {
				case k[1]:
					return nil, fmt.Errorf("profiles.%s: %s: is a contexts: key; under profiles: use %s: (line %d)",
						name, k[1], k[0], entry.Content[j].Line)
				case k[0]:
					renamed := *entry.Content[j]
					renamed.Value = k[1]
					entry.Content[j] = &renamed
				}
			}
		}
		var ctx Context
		if err := entry.Decode(&ctx); err != nil {
			return nil, fmt.Errorf("profiles.%s: %w", name, err)
		}
		out[name] = ctx
	}
	return out, nil
}

// rejectProfileDirKeysInContexts fails when a contexts: entry uses a
// key that only profiles: entries accept.
func rejectProfileDirKeysInContexts(contexts *yaml.Node) error {
	if contexts == nil {
		return nil
	}
	for i := 0; i+1 < len(contexts.Content); i += 2 {
		entry := contexts.Content[i+1]
		for j := 0; j+1 < len(entry.Content); j += 2 {
			key := entry.Content[j]
			for _, k := range profileDirKeys {
				if key.Value == k[0] {
					return fmt.Errorf("contexts.%s: %s: belongs to profiles: entries; under contexts: use %s: (line %d)",
						contexts.Content[i].Value, k[0], k[1], key.Line)
				}
			}
		}
	}
	return nil
}

// UnmarshalYAML decodes .aide.yaml, rejecting keys that would define
// a profile. This is structural and independent of trust.
func (p *ProjectOverride) UnmarshalYAML(node *yaml.Node) error {
	for _, key := range mappingKeys(node) {
		if slices.Contains(projectDefiningKeys, key) {
			return fmt.Errorf("%w: %s: belongs to a profile; set it in your profile in the user config "+
				"(.aide.yaml may only name an existing profile with profile: <name>)", ErrProfileNotProjectScoped, key)
		}
	}
	type plain ProjectOverride
	return node.Decode((*plain)(p))
}

func mappingKeys(node *yaml.Node) []string {
	if node.Kind == yaml.DocumentNode && len(node.Content) == 1 {
		node = node.Content[0]
	}
	if node.Kind != yaml.MappingNode {
		return nil
	}
	keys := make([]string, 0, len(node.Content)/2)
	for i := 0; i < len(node.Content); i += 2 {
		keys = append(keys, node.Content[i].Value)
	}
	return keys
}

// presentKeys returns, in argument order, which of want appear in keys
// rendered as "key:".
func presentKeys(keys []string, want ...string) []string {
	var out []string
	for _, w := range want {
		if slices.Contains(keys, w) {
			out = append(out, w+":")
		}
	}
	return out
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.DocumentNode && len(node.Content) == 1 {
		node = node.Content[0]
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func renameKey(node *yaml.Node, from, to string) {
	if node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == from {
			node.Content[i].Value = to
		}
	}
}

func sortedKeys(m map[string]Context) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
