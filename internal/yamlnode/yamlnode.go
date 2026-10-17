// Package yamlnode holds the yaml.Node helpers that edit config files
// in place, keeping their comments and key order.
package yamlnode

import "gopkg.in/yaml.v3"

// Value returns the value of key in mapping node (a document is
// unwrapped to its root), or nil when node is nil or has no key.
func Value(node *yaml.Node, key string) *yaml.Node {
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

// Str is a string scalar node, tagged so values like "1" stay strings.
func Str(v string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
}
