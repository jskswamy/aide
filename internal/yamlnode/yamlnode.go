// Package yamlnode holds the yaml.Node helpers that edit config files
// in place, keeping their comments and key order.
package yamlnode

import (
	"bytes"
	"errors"
	"io"

	"gopkg.in/yaml.v3"
)

// Marshal encodes v as YAML with 2-space indentation.
func Marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Merge applies the semantic structure of updated onto existing, preserving
// existing comments, document-start marker (---), and mapping key order for
// retained keys while encoding with 2-space indentation. If existing cannot
// be parsed as a single YAML document, updated is returned unchanged.
func Merge(existing, updated []byte) []byte {
	if len(bytes.TrimSpace(existing)) == 0 {
		return updated
	}
	dec := yaml.NewDecoder(bytes.NewReader(existing))
	var oldDoc yaml.Node
	if dec.Decode(&oldDoc) != nil {
		return updated
	}
	var extra yaml.Node
	if !errors.Is(dec.Decode(&extra), io.EOF) {
		return updated
	}
	var newDoc yaml.Node
	if yaml.Unmarshal(updated, &newDoc) != nil {
		return updated
	}
	if oldDoc.Kind != yaml.DocumentNode || len(oldDoc.Content) != 1 ||
		newDoc.Kind != yaml.DocumentNode || len(newDoc.Content) != 1 {
		return updated
	}

	mergeNode(oldDoc.Content[0], newDoc.Content[0])
	out, err := Marshal(&oldDoc)
	if err != nil {
		return updated
	}
	if bytes.HasPrefix(existing, []byte("---\n")) && !bytes.HasPrefix(out, []byte("---\n")) {
		out = append([]byte("---\n"), out...)
	}
	return out
}

func mergeNode(dst, src *yaml.Node) {
	switch {
	case dst.Kind == yaml.MappingNode && src.Kind == yaml.MappingNode:
		srcIdx := make(map[string]int, len(src.Content)/2)
		for i := 0; i+1 < len(src.Content); i += 2 {
			srcIdx[src.Content[i].Value] = i
		}
		seen := make(map[string]bool, len(src.Content)/2)
		merged := make([]*yaml.Node, 0, len(src.Content))
		for i := 0; i+1 < len(dst.Content); i += 2 {
			oldK, oldV := dst.Content[i], dst.Content[i+1]
			j, ok := srcIdx[oldK.Value]
			if !ok {
				continue
			}
			seen[oldK.Value] = true
			mergeNode(oldV, src.Content[j+1])
			merged = append(merged, oldK, oldV)
		}
		for i := 0; i+1 < len(src.Content); i += 2 {
			newK, newV := src.Content[i], src.Content[i+1]
			if !seen[newK.Value] {
				merged = append(merged, newK, newV)
			}
		}
		dst.Style = src.Style
		dst.Content = merged
		dst.Tag = src.Tag

	case dst.Kind == yaml.SequenceNode && src.Kind == yaml.SequenceNode:
		oldItems := dst.Content
		newItems := make([]*yaml.Node, len(src.Content))
		for i, item := range src.Content {
			cp := *item
			if item.Kind == yaml.ScalarNode {
				for _, old := range oldItems {
					if old.Kind == yaml.ScalarNode && old.Value == item.Value {
						keepComments(&cp, old)
						break
					}
				}
			} else if item.Kind == yaml.MappingNode && i < len(oldItems) && oldItems[i].Kind == yaml.MappingNode {
				oldCopy := *oldItems[i]
				oldCopy.Content = append([]*yaml.Node(nil), oldItems[i].Content...)
				mergeNode(&oldCopy, &cp)
				cp = oldCopy
			}
			newItems[i] = &cp
		}
		dst.Style = src.Style
		dst.Content = newItems
		dst.Tag = src.Tag

	default:
		old := *dst
		*dst = *src
		keepComments(dst, &old)
	}
}

func keepComments(dst, old *yaml.Node) {
	if dst.HeadComment == "" {
		dst.HeadComment = old.HeadComment
	}
	if dst.LineComment == "" {
		dst.LineComment = old.LineComment
	}
	if dst.FootComment == "" {
		dst.FootComment = old.FootComment
	}
}

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
