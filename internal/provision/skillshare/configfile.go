package skillshare

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/jskswamy/aide/internal/fsutil"
	"github.com/jskswamy/aide/internal/homepath"
)

// ConfigPath is the Skillshare config file: $SKILLSHARE_CONFIG when
// set (https://skillshare.runkids.cc/docs/reference/targets/configuration),
// else $XDG_CONFIG_HOME/skillshare/config.yaml, else
// ~/.config/skillshare/config.yaml.
func ConfigPath(home string, getenv func(string) string) string {
	if p := getenv("SKILLSHARE_CONFIG"); p != "" {
		return expand(p, home)
	}
	return filepath.Join(ConfigDir(home, getenv), "config.yaml")
}

// Result is the outcome of merging a Plan into a Skillshare config.
type Result struct {
	// Output is the merged config; the input bytes when nothing changed.
	Output  []byte
	Changed bool
	// Written are the plan's targets the merged config holds.
	Written                            []Target
	Added, Updated, Removed, Unchanged []string
	// Conflicts are plan targets left out because a target of the
	// user's already owns their name or dir.
	Conflicts []Skip
	// References name removed targets still listed elsewhere in the
	// config, as "name (path)", for the user to clean up.
	References []string
}

// Merge writes p's targets into the Skillshare config existing (empty
// for none), keeping everything else: comments, other keys, the user's
// own targets, and extra keys on aide's targets. aide's targets not in
// p are removed. Running it again on its output changes nothing.
func Merge(existing []byte, p Plan, home string) (*Result, error) {
	doc, err := parseDoc(existing)
	if err != nil {
		return nil, err
	}
	root := doc.Content[0]
	targets, err := targetsMapping(root)
	if err != nil {
		return nil, err
	}

	res := &Result{}
	userDirs := map[string]string{} // user target name -> expanded config_dir
	taken := map[string]bool{}      // names of the user's own targets
	for i := 0; i+1 < len(targets.Content); i += 2 {
		name, val := targets.Content[i].Value, targets.Content[i+1]
		if owned(name, val) {
			continue
		}
		taken[name] = true
		if dir := scalarValue(val, "config_dir"); dir != "" {
			userDirs[name] = expand(dir, home)
		}
	}
	want := map[string]Target{}
	for _, t := range p.Targets {
		if skip := conflict(t, taken, userDirs, home); skip != nil {
			res.Conflicts = append(res.Conflicts, *skip)
			continue
		}
		want[t.Name] = t
		res.Written = append(res.Written, t)
	}

	kept := targets.Content[:0:0]
	present := map[string]bool{}
	for i := 0; i+1 < len(targets.Content); i += 2 {
		key, val := targets.Content[i], targets.Content[i+1]
		if !owned(key.Value, val) {
			kept = append(kept, key, val)
			continue
		}
		t, ok := want[key.Value]
		if !ok {
			res.Removed = append(res.Removed, key.Value)
			continue
		}
		present[key.Value] = true
		agentChanged := setScalar(val, "agent", t.Agent)
		dirChanged := setScalar(val, "config_dir", t.ConfigDir)
		if agentChanged || dirChanged {
			res.Updated = append(res.Updated, key.Value)
		} else {
			res.Unchanged = append(res.Unchanged, key.Value)
		}
		kept = append(kept, key, val)
	}
	for _, t := range res.Written {
		if present[t.Name] {
			continue
		}
		kept = append(kept, strNode(t.Name), targetNode(t))
		res.Added = append(res.Added, t.Name)
	}
	targets.Content = kept

	res.Changed = len(res.Added)+len(res.Updated)+len(res.Removed) > 0
	if !res.Changed {
		res.Output = existing
		return res, nil
	}
	if len(res.Added) > 0 {
		targets.Style &^= yaml.FlowStyle
	}
	for _, name := range res.Removed {
		for _, at := range references(root, name, "") {
			res.References = append(res.References, fmt.Sprintf("%s (%s)", name, at))
		}
	}
	res.Output, err = encode(doc)
	return res, err
}

// RenderTargets is targets as a Skillshare `targets:` block.
func RenderTargets(targets []Target) ([]byte, error) {
	m := &yaml.Node{Kind: yaml.MappingNode}
	for _, t := range targets {
		m.Content = append(m.Content, strNode(t.Name), targetNode(t))
	}
	root := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{strNode("targets"), m}}
	return encode(&yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}})
}

func conflict(t Target, taken map[string]bool, userDirs map[string]string, home string) *Skip {
	if taken[t.Name] {
		return &Skip{Profile: t.Profile, Agent: t.Agent, Reason: fmt.Sprintf(
			"your Skillshare target %s is not an account target, so aide leaves it alone; rename it to let aide write this one", t.Name)}
	}
	dir := expand(t.ConfigDir, home)
	names := make([]string, 0, len(userDirs))
	for name := range userDirs {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if fsutil.SameDir(userDirs[name], dir) {
			return &Skip{Profile: t.Profile, Agent: t.Agent, Reason: fmt.Sprintf(
				"%s is already the config_dir of your Skillshare target %s", t.ConfigDir, name)}
		}
	}
	return nil
}

// owned reports whether the target name: val is one aide wrote.
func owned(name string, val *yaml.Node) bool {
	return strings.HasPrefix(name, NamePrefix) && slices.Contains(accountAgents, scalarValue(val, "agent"))
}

func parseDoc(data []byte) (*yaml.Node, error) {
	doc := &yaml.Node{}
	if len(bytes.TrimSpace(data)) > 0 {
		if err := yaml.Unmarshal(data, doc); err != nil {
			return nil, fmt.Errorf("parsing Skillshare config: %w", err)
		}
	}
	if doc.Kind == 0 || len(doc.Content) == 0 {
		return &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}, nil
	}
	if doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("skillshare config is not a YAML mapping")
	}
	return doc, nil
}

// targetsMapping returns root's targets: mapping, adding it if absent.
func targetsMapping(root *yaml.Node) (*yaml.Node, error) {
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "targets" {
			continue
		}
		v := root.Content[i+1]
		switch {
		case v.Kind == yaml.MappingNode:
			return v, nil
		case v.Kind == yaml.ScalarNode && v.Tag == "!!null":
			*v = yaml.Node{Kind: yaml.MappingNode}
			return v, nil
		default:
			return nil, errors.New("skillshare config: targets: is not a mapping")
		}
	}
	v := &yaml.Node{Kind: yaml.MappingNode}
	root.Content = append(root.Content, strNode("targets"), v)
	return v, nil
}

func targetNode(t Target) *yaml.Node {
	return &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		strNode("agent"), strNode(t.Agent),
		strNode("config_dir"), strNode(t.ConfigDir),
	}}
}

func strNode(v string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v} }

func scalarValue(m *yaml.Node, key string) string {
	if m == nil || m.Kind != yaml.MappingNode {
		return ""
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key && m.Content[i+1].Kind == yaml.ScalarNode {
			return m.Content[i+1].Value
		}
	}
	return ""
}

// setScalar sets key in mapping m to v, reporting whether it changed.
func setScalar(m *yaml.Node, key, v string) bool {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			if n := m.Content[i+1]; n.Kind == yaml.ScalarNode && n.Value == v {
				return false
			}
			m.Content[i+1] = strNode(v)
			return true
		}
	}
	m.Content = append(m.Content, strNode(key), strNode(v))
	return true
}

// references returns the paths of sequence items under n equal to name.
func references(n *yaml.Node, name, path string) []string {
	var out []string
	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			p := n.Content[i].Value
			if path != "" {
				p = path + "." + p
			}
			if path == "" && p == "targets" {
				continue
			}
			out = append(out, references(n.Content[i+1], name, p)...)
		}
	case yaml.SequenceNode:
		for _, item := range n.Content {
			if item.Kind == yaml.ScalarNode && item.Value == name {
				out = append(out, path)
			} else {
				out = append(out, references(item, name, path)...)
			}
		}
	case yaml.DocumentNode, yaml.ScalarNode, yaml.AliasNode:
	}
	return out
}

func encode(doc *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("encoding Skillshare config: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("encoding Skillshare config: %w", err)
	}
	return buf.Bytes(), nil
}

func expand(p, home string) string { return homepath.Expand(p, home) }
