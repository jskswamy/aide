package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/jskswamy/aide/internal/envscrub"
	"github.com/jskswamy/aide/internal/fsutil"
	"github.com/jskswamy/aide/internal/harness"
	"github.com/jskswamy/aide/internal/statedir"
	"github.com/jskswamy/aide/internal/yamlnode"
)

// MigrateOptions tunes MigrateProfiles.
type MigrateOptions struct {
	// Home expands ~ in native config dirs for validation.
	Home string
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
	// Set are the profile's provider endpoints moved into this
	// binding's route for Harness, so the key keeps its endpoint.
	Set     map[string]string
	Harness string
}

// Migration is the result of MigrateProfiles. Nothing is written.
type Migration struct {
	// Output is the migrated config text; the input unchanged when
	// Changed is false.
	Output  []byte
	Changed bool
	// SplitLayout is true when the input config contained inline
	// profiles, sandbox settings, or provisioning settings that split
	// into modular files in SplitFiles.
	SplitLayout bool
	// SplitFiles maps relative paths ("config.yaml", "sandbox.yaml",
	// "provisioning.yaml", "profiles/<name>.yaml") to their migrated
	// YAML bytes when SplitLayout is true.
	SplitFiles map[string][]byte
	// SplitErr holds any error encountered while splitting into modular
	// files (e.g. YAML anchors/aliases across sections or invalid
	// minimal config settings).
	SplitErr error
	// Renamed lists the key renames applied, as "old -> new".
	Renamed     []string
	Credentials []MigratedCredential
	// MentionsOpenCode reports that a profile runs OpenCode or pins an
	// OpenCode config dir, so its accounts may now open another database.
	MentionsOpenCode bool
}

// NeedsWrite reports whether migrating this config produces either a schema
// rewrite (Changed) or a modular file split (SplitLayout).
func (m *Migration) NeedsWrite() bool {
	return m != nil && (m.Changed || m.SplitLayout)
}

// Preview renders the migration preview text: multi-file sections when
// SplitLayout is true, otherwise Output.
func (m *Migration) Preview() string {
	if !m.SplitLayout || len(m.SplitFiles) == 0 {
		return strings.TrimRight(string(m.Output), "\n")
	}
	var order []string
	for _, fixed := range []string{"config.yaml", "sandbox.yaml", "provisioning.yaml"} {
		if _, ok := m.SplitFiles[fixed]; ok {
			order = append(order, fixed)
		}
	}
	for _, k := range slices.Sorted(maps.Keys(m.SplitFiles)) {
		if strings.HasPrefix(k, "profiles/") {
			order = append(order, k)
		}
	}
	var sb strings.Builder
	for i, rel := range order {
		if i > 0 {
			sb.WriteString("\n")
		}
		fmt.Fprintf(&sb, "# === %s ===\n%s\n", rel, strings.TrimRight(string(m.SplitFiles[rel]), "\n"))
	}
	return strings.TrimRight(sb.String(), "\n")
}

// ApplyMigrationSplit writes m.SplitFiles atomically into configDir.
func ApplyMigrationSplit(configDir string, m *Migration) error {
	if m.SplitErr != nil {
		return m.SplitErr
	}
	if !m.SplitLayout || len(m.SplitFiles) == 0 {
		return writeIfChanged(filepath.Join(configDir, "config.yaml"), m.Output, nil)
	}
	for _, rel := range slices.Sorted(maps.Keys(m.SplitFiles)) {
		target := filepath.Join(configDir, filepath.FromSlash(rel))
		if err := writeIfChanged(target, m.SplitFiles[rel], nil); err != nil {
			return err
		}
	}
	return nil
}

// InspectLoadedMigration reports MentionsOpenCode from an already-split loaded
// Config when config.yaml itself has no inline profiles.
func InspectLoadedMigration(cfg *Config) bool {
	if cfg == nil {
		return false
	}
	return mentionsOpenCode(cfg)
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
	m := &Migration{Output: data, MentionsOpenCode: mentionsOpenCode(&cfg)}
	if len(doc.Content) == 0 {
		return m, nil
	}
	root := doc.Content[0]
	if len(cfg.Agents) == 0 && len(cfg.Contexts) == 0 {
		if cfg.hasInlineSandbox() || cfg.hasInlineProvisioning() {
			if err := refuseSharedNodes(root, "config"); err != nil {
				m.SplitErr = err
			} else if err := validateConfigStruct(&cfg, opts.Home, "migrated config"); err != nil {
				m.SplitErr = err
			} else {
				m.SplitErr = m.populateSplitFiles(&doc)
			}
		}
		return m, nil
	}

	names := sortedKeys(cfg.Contexts)
	moves := map[string][]MigratedCredential{}
	for _, name := range names {
		ctx := cfg.Contexts[name]
		_, legacy, err := TranslateLegacyCredentials(ctx, ctx.Secret)
		if err != nil {
			return nil, fmt.Errorf("profiles.%s: %w", name, err)
		}
		for _, l := range legacy {
			mc := MigratedCredential{Profile: name, Name: l.Name, EnvVar: l.EnvVar, Store: l.Binding.Store, Key: l.Binding.Key}
			if !IsBuiltinCredential(l.Name) {
				h := cfg.HarnessOf(ctx.Agent)
				if _, ok := harness.Lookup(h); !ok {
					return nil, fmt.Errorf("%w: profiles.%s: env: %s is a custom provider credential, and its binding must name the "+
						"harnesses it is for, but the profile's agent %q is not a known harness; move it to credentials: by hand "+
						"with harnesses: [<harness>] and re-run", ErrLegacyCredentialEnv, name, l.EnvVar, ctx.Agent)
				}
				mc.Harnesses = []string{h}
			}
			if len(moves[name]) == 0 {
				if err := attachEndpoints(&mc, ctx, cfg.HarnessOf(ctx.Agent)); err != nil {
					return nil, fmt.Errorf("profiles.%s: %w", name, err)
				}
			}
			moves[name] = append(moves[name], mc)
		}
	}
	if cfg.ProfileSchema && len(moves) == 0 {
		if cfg.hasInlineProfiles() || cfg.hasInlineSandbox() || cfg.hasInlineProvisioning() {
			if err := refuseSharedNodes(root, "config"); err != nil {
				m.SplitErr = err
			} else {
				m.SplitErr = m.populateSplitFiles(&doc)
			}
		}
		return m, nil
	}
	sectionKey := keyProfiles
	if !cfg.ProfileSchema {
		sectionKey = keyContexts
	}
	if err := refuseSharedNodes(yamlnode.Value(root, sectionKey), sectionKey); err != nil {
		return nil, err
	}

	if !cfg.ProfileSchema {
		m.renameTopLevel(root)
	}
	section := yamlnode.Value(root, keyProfiles)
	for _, name := range names {
		entry := yamlnode.Value(section, name)
		for _, mc := range moves[name] {
			if err := moveCredential(entry, mc); err != nil {
				return nil, fmt.Errorf("profiles.%s: %w", name, err)
			}
			m.Credentials = append(m.Credentials, mc)
		}
	}

	if len(m.Renamed) == 0 && len(m.Credentials) == 0 {
		if cfg.hasInlineProfiles() || cfg.hasInlineSandbox() || cfg.hasInlineProvisioning() {
			if err := refuseSharedNodes(root, "config"); err != nil {
				m.SplitErr = err
			} else {
				m.SplitErr = m.populateSplitFiles(&doc)
			}
		}
		return m, nil
	}
	out, err := yamlnode.Marshal(&doc)
	if err != nil {
		return nil, fmt.Errorf("encoding migrated config: %w", err)
	}
	if err := validateLoadable(out, opts.Home, "migrated config"); err != nil {
		return nil, err
	}
	if err := checkEnvKept(&cfg, out, moves); err != nil {
		return nil, err
	}
	m.Output = out
	m.Changed = true
	if err := m.populateSplitFiles(&doc); err != nil {
		return nil, err
	}
	return m, nil
}

// renameTopLevel renames the legacy top-level keys and each entry's
// native config dir keys.
func (m *Migration) renameTopLevel(root *yaml.Node) {
	for _, k := range [][2]string{{keyContexts, keyProfiles}, {keyDefaultContext, keyDefaultProfile}} {
		if yamlnode.Value(root, k[0]) != nil {
			renameKey(root, k[0], k[1])
			m.Renamed = append(m.Renamed, k[0]+": -> "+k[1]+":")
		}
	}
	section := yamlnode.Value(root, keyProfiles)
	if section == nil || section.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(section.Content); i += 2 {
		name, entry := section.Content[i].Value, section.Content[i+1]
		for _, k := range profileDirKeys {
			if yamlnode.Value(entry, k[1]) != nil {
				renameKey(entry, k[1], k[0])
				m.Renamed = append(m.Renamed, fmt.Sprintf("profiles.%s.%s: -> %s:", name, k[1], k[0]))
			}
		}
	}
}

// attachEndpoints moves ctx's provider endpoints into mc's route for h,
// where a launch requires them (they decide where the key is sent).
// shortcut: with several legacy keys the endpoints go to the first one;
// a launch then names any other key that needs them.
func attachEndpoints(mc *MigratedCredential, ctx Context, h string) error {
	for _, k := range slices.Sorted(maps.Keys(ctx.Env)) {
		if !envscrub.IsProviderEndpointName(k) {
			continue
		}
		if _, ok := harness.Lookup(h); !ok {
			return fmt.Errorf("%w: env: %s is a provider endpoint for %s, but the profile's agent %q is not a known harness; "+
				"move both to credentials.%s.for.<harness> by hand", ErrLegacyCredentialEnv, k, mc.EnvVar, ctx.Agent, mc.Name)
		}
		if strings.Contains(ctx.Env[k], "{{") {
			return fmt.Errorf("%w: env: %s is a templated provider endpoint; a route's set: holds plain values, "+
				"so move it to credentials.%s.for.%s.set by hand", ErrLegacyCredentialEnv, k, mc.Name, h)
		}
		if mc.Set == nil {
			mc.Set, mc.Harness = map[string]string{}, h
		}
		mc.Set[k] = ctx.Env[k]
	}
	return nil
}

// moveCredential removes mc's env: entry from a profile entry node and
// adds the matching credentials: binding and default_credentials:
// selection.
func moveCredential(entry *yaml.Node, mc MigratedCredential) error {
	env := yamlnode.Value(entry, "env")
	if env == nil || yamlnode.Value(env, mc.EnvVar) == nil {
		return fmt.Errorf("env: %s not found in the profile's own env: map; move it to credentials: by hand", mc.EnvVar)
	}
	deleteKey(env, mc.EnvVar)
	for k := range mc.Set {
		deleteKey(env, k)
	}
	if len(env.Content) == 0 {
		deleteKey(entry, "env")
	}
	binding := CredentialBinding{Backend: CredentialBackendAge, Store: mc.Store, Key: mc.Key}
	if !IsBuiltinCredential(mc.Name) {
		binding.Env = []string{mc.EnvVar}
		binding.Harnesses = mc.Harnesses
	}
	if len(mc.Set) > 0 {
		binding.For = map[string]CredentialRoute{mc.Harness: {Env: binding.Env, Set: mc.Set}}
		binding.Env, binding.Harnesses = nil, nil
	}
	var value yaml.Node
	if err := value.Encode(binding); err != nil {
		return err
	}
	creds := ensureValue(entry, "credentials", yaml.MappingNode)
	creds.Content = append(creds.Content, yamlnode.Str(mc.Name), &value)
	defaults := ensureValue(entry, "default_credentials", yaml.SequenceNode)
	defaults.Content = append(defaults.Content, yamlnode.Str(mc.Name))
	return nil
}

// mentionsOpenCode reports whether any profile runs OpenCode, or a
// profile pins an OpenCode config dir.
func mentionsOpenCode(cfg *Config) bool {
	for _, ctx := range cfg.Contexts {
		if _, ok := ctx.ConfigDirs["opencode"]; ok || cfg.HarnessOf(ctx.Agent) == "opencode" {
			return true
		}
	}
	return false
}

// validateLoadable checks config text (what names it in errors) the
// way Load checks profiles, so aide never writes a config it would then
// refuse to load.
func validateLoadable(data []byte, home, what string) error {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("%s does not parse: %w", what, err)
	}
	checks := []func() error{cfg.validateCredentials}
	if home != "" {
		checks = append(checks, func() error { return cfg.validateConfigDirs(home) },
			func() error { return cfg.validateProfileDirs(home) })
	}
	for _, check := range checks {
		if err := check(); err != nil {
			return fmt.Errorf("%s would not load: %w", what, err)
		}
	}
	return nil
}

// ensureValue returns the value of key in mapping, adding an empty
// node of kind when absent.
func ensureValue(mapping *yaml.Node, key string, kind yaml.Kind) *yaml.Node {
	if v := yamlnode.Value(mapping, key); v != nil {
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
	mapping.Content = append(mapping.Content, yamlnode.Str(key), v)
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

// refuseSharedNodes stops a migration over a profiles/contexts section
// that uses YAML anchors, aliases or merge keys: editing a shared node
// would change (or drop) other profiles' settings with it.
func refuseSharedNodes(section *yaml.Node, key string) error {
	if section == nil {
		return nil
	}
	var where string
	var walk func(n *yaml.Node, path string)
	walk = func(n *yaml.Node, path string) {
		if where != "" || n == nil {
			return
		}
		if n.Kind == yaml.AliasNode || n.Anchor != "" {
			where = path
			return
		}
		if n.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(n.Content); i += 2 {
				k := n.Content[i].Value
				if k == "<<" {
					where = path
					return
				}
				walk(n.Content[i+1], path+"."+k)
			}
			return
		}
		for _, c := range n.Content {
			walk(c, path)
		}
	}
	walk(section, key)
	if where == "" {
		return nil
	}
	return fmt.Errorf("%w: %s uses a YAML anchor, alias or merge key (&name, *name or <<:); "+
		"migrating would edit settings shared with other profiles. Expand it into plain values by hand and re-run",
		ErrSharedYAML, where)
}

// ErrSharedYAML reports a config migration cannot edit safely because
// profile settings are shared through YAML anchors or aliases.
var ErrSharedYAML = errors.New("cannot migrate shared YAML nodes")

// checkEnvKept fails unless every profile's effective env: in the
// migrated text equals the input's minus the credentials moved out of
// it: a safety net against any edit dropping unrelated variables.
func checkEnvKept(before *Config, migrated []byte, moves map[string][]MigratedCredential) error {
	var after Config
	if err := yaml.Unmarshal(migrated, &after); err != nil {
		return fmt.Errorf("migrated config does not parse: %w", err)
	}
	if !slices.Equal(sortedKeys(before.Contexts), sortedKeys(after.Contexts)) {
		return errors.New("migration changed the set of profiles; nothing written")
	}
	for name, ctx := range before.Contexts {
		want := maps.Clone(ctx.Env)
		for _, mc := range moves[name] {
			delete(want, mc.EnvVar)
			for k := range mc.Set {
				delete(want, k)
			}
		}
		if len(want) == 0 {
			want = nil
		}
		got := after.Contexts[name].Env
		if len(got) == 0 {
			got = nil
		}
		if !maps.Equal(want, got) {
			return fmt.Errorf("migration would change profiles.%s env: beyond the moved credentials; nothing written", name)
		}
	}
	return nil
}

func (m *Migration) populateSplitFiles(doc *yaml.Node) error {
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil
	}
	// Re-parse the current migrated YAML into a fresh document node so
	// extracting sections does not mutate doc.
	migratedBytes, err := yamlnode.Marshal(doc)
	if err != nil {
		return err
	}
	var clone yaml.Node
	if err := yaml.Unmarshal(migratedBytes, &clone); err != nil {
		return err
	}
	if len(clone.Content) == 0 || clone.Content[0].Kind != yaml.MappingNode {
		return nil
	}
	root := clone.Content[0]
	files := map[string][]byte{}
	splitHappened := false

	// 1. Extract profiles/<name>.yaml
	if profNode := yamlnode.Value(root, keyProfiles); profNode != nil && profNode.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(profNode.Content); i += 2 {
			kNode := profNode.Content[i]
			vNode := profNode.Content[i+1]
			if !gatewayIdentRE.MatchString(kNode.Value) {
				return fmt.Errorf("%w: %q must match %s", ErrInvalidProfileFilename, kNode.Value, gatewayIdentRE.String())
			}
			pDoc := yaml.Node{
				Kind:        yaml.DocumentNode,
				HeadComment: kNode.HeadComment,
				Content:     []*yaml.Node{vNode},
			}
			b, err := yamlnode.Marshal(&pDoc)
			if err != nil {
				return err
			}
			files["profiles/"+kNode.Value+".yaml"] = b
			splitHappened = true
		}
		deleteKey(root, keyProfiles)
	}

	// 2. Extract sandbox.yaml
	sbxRoot := &yaml.Node{Kind: yaml.MappingNode}
	for _, k := range validSandboxFileKeys {
		if v := yamlnode.Value(root, k); v != nil {
			sbxRoot.Content = append(sbxRoot.Content, yamlnode.Str(k), v)
			deleteKey(root, k)
		}
	}
	if len(sbxRoot.Content) > 0 {
		b, err := yamlnode.Marshal(&yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{sbxRoot}})
		if err != nil {
			return err
		}
		files["sandbox.yaml"] = b
		splitHappened = true
	}

	// 3. Extract provisioning.yaml
	provRoot := &yaml.Node{Kind: yaml.MappingNode}
	for _, k := range validProvisioningFileKeys {
		if v := yamlnode.Value(root, k); v != nil {
			provRoot.Content = append(provRoot.Content, yamlnode.Str(k), v)
			deleteKey(root, k)
		}
	}
	if len(provRoot.Content) > 0 {
		b, err := yamlnode.Marshal(&yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{provRoot}})
		if err != nil {
			return err
		}
		files["provisioning.yaml"] = b
		splitHappened = true
	}

	if !splitHappened {
		return nil
	}

	coreBytes, err := yamlnode.Marshal(&clone)
	if err != nil {
		return err
	}
	files["config.yaml"] = coreBytes
	m.SplitFiles = files
	m.SplitLayout = true
	return nil
}

const migrateNudgeCooldown = 7 * 24 * time.Hour

type migrateNudgeState struct {
	LastNudgedAt time.Time `json:"last_nudged_at"`
}

// MaybeNudgeMigration prints a brief one-line reminder to errOut at most
// once every 7 days when cfg was loaded from a legacy single-file config.yaml
// and errOut is an interactive terminal.
func MaybeNudgeMigration(cfg *Config, errOut io.Writer, isTTY bool, homeDir string, now time.Time) {
	if cfg == nil || !cfg.LegacyMonolithic || !isTTY || homeDir == "" {
		return
	}
	stateFile := statedir.Path(homeDir, "migrate-nudge.json")
	if data, err := os.ReadFile(stateFile); err == nil {
		var st migrateNudgeState
		if json.Unmarshal(data, &st) == nil && !st.LastNudgedAt.IsZero() && now.Sub(st.LastNudgedAt) < migrateNudgeCooldown {
			return
		}
	}
	fmt.Fprintln(errOut, "aide: note: config.yaml uses the legacy single-file layout; run 'aide config migrate --write' to split into modular files.")
	if data, err := json.Marshal(migrateNudgeState{LastNudgedAt: now.UTC()}); err == nil {
		_ = fsutil.AtomicWrite(stateFile, append(data, '\n'))
	}
}
