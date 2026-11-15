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

	"github.com/jskswamy/aide/internal/fsutil"
	"github.com/jskswamy/aide/internal/statedir"
	"github.com/jskswamy/aide/internal/yamlnode"
)

// MigrateOptions tunes MigrateProfiles.
type MigrateOptions struct {
	// Home expands ~ in native config dirs for validation.
	Home string
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
	Renamed []string
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
// and profile_dir: -> config_dir:. Provider credentials and endpoints
// are not touched here: the gateway migration converts them.
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

	if cfg.ProfileSchema {
		if cfg.hasInlineProfiles() || cfg.hasInlineSandbox() || cfg.hasInlineProvisioning() {
			if err := refuseSharedNodes(root, "config"); err != nil {
				m.SplitErr = err
			} else {
				m.SplitErr = m.populateSplitFiles(&doc)
			}
		}
		return m, nil
	}
	if err := refuseSharedNodes(yamlnode.Value(root, keyContexts), keyContexts); err != nil {
		return nil, err
	}
	m.renameTopLevel(root)
	if len(m.Renamed) == 0 {
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
	if err := checkEnvKept(&cfg, out); err != nil {
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
// migrated text equals the input's: a safety net against any edit
// dropping variables.
func checkEnvKept(before *Config, migrated []byte) error {
	var after Config
	if err := yaml.Unmarshal(migrated, &after); err != nil {
		return fmt.Errorf("migrated config does not parse: %w", err)
	}
	if !slices.Equal(sortedKeys(before.Contexts), sortedKeys(after.Contexts)) {
		return errors.New("migration changed the set of profiles; nothing written")
	}
	for name, ctx := range before.Contexts {
		want := ctx.Env
		if len(want) == 0 {
			want = nil
		}
		got := after.Contexts[name].Env
		if len(got) == 0 {
			got = nil
		}
		if !maps.Equal(want, got) {
			return fmt.Errorf("migration would change profiles.%s env:; nothing written", name)
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
