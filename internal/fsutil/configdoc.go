package fsutil

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/pelletier/go-toml/v2"
	"github.com/tailscale/hujson"
)

// ReadTOML reads a TOML document into a map; a missing file returns an
// empty map.
func ReadTOML(path string) (map[string]any, error) {
	doc := map[string]any{}
	data, err := os.ReadFile(path) // #nosec G304 -- caller supplies a config path
	if errors.Is(err, os.ErrNotExist) {
		return doc, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	if err := toml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return doc, nil
}

// WriteTOML encodes doc as TOML and writes it atomically to path.
func WriteTOML(path string, doc map[string]any) error {
	out, err := toml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("encoding %s: %w", path, err)
	}
	return AtomicWrite(path, out)
}

// ReadJSONC reads a JSON or JSONC file into v, returning false when the
// file does not exist.
func ReadJSONC(path string, v any) (bool, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- caller supplies a config path
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reading %s: %w", path, err)
	}
	std, err := hujson.Standardize(data)
	if err != nil {
		return false, fmt.Errorf("parsing %s: %w", path, err)
	}
	if err := json.Unmarshal(std, v); err != nil {
		return false, fmt.Errorf("parsing %s: %w", path, err)
	}
	return true, nil
}

// ReadJSONCMap reads a JSON or JSONC object into a generic map; a
// missing file returns an empty map.
func ReadJSONCMap(path string) (map[string]any, error) {
	doc := map[string]any{}
	if _, err := ReadJSONC(path, &doc); err != nil {
		return nil, err
	}
	return doc, nil
}

// ReadJSONCRawMap reads a JSON or JSONC object keeping top-level values
// as raw JSON so untouched keys round-trip as written; a missing file
// returns an empty map.
func ReadJSONCRawMap(path string) (map[string]json.RawMessage, error) {
	doc := map[string]json.RawMessage{}
	if _, err := ReadJSONC(path, &doc); err != nil {
		return nil, err
	}
	return doc, nil
}

// WriteJSON encodes doc as indented JSON with a trailing newline and
// writes it atomically to path.
func WriteJSON(path string, doc any) error {
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding %s: %w", path, err)
	}
	return AtomicWrite(path, append(out, '\n'))
}
