package usage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// IsolatePiStores passes ccusage a private copy of its selected config without
// additive named pi stores. The default pi store remains selected by PI_AGENT_DIR.
func IsolatePiStores(args, scopeEnv []string) ([]string, func(), error) {
	var paths []string
	index := -1
	inline := false
	for i, arg := range args {
		if arg == "--config" && i+1 < len(args) {
			index = i + 1
			paths = []string{args[index]}
			break
		}
		if strings.HasPrefix(arg, "--config=") && len(arg) > len("--config=") {
			index, inline = i, true
			paths = []string{strings.TrimPrefix(arg, "--config=")}
			break
		}
	}
	if index < 0 {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, nil, err
		}
		paths = append(paths, filepath.Join(cwd, ".ccusage", "ccusage.json"))
		for _, e := range scopeEnv {
			if strings.HasPrefix(e, "CLAUDE_CONFIG_DIR=") {
				for _, dir := range strings.Split(strings.TrimPrefix(e, "CLAUDE_CONFIG_DIR="), ",") {
					if dir = strings.TrimSpace(dir); dir != "" {
						paths = append(paths, filepath.Join(dir, "ccusage.json"))
					}
				}
			}
		}
	}
	root := map[string]json.RawMessage{}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, nil, fmt.Errorf("reading ccusage config %s: %w", path, err)
		}
		if jsonObject(data) && json.Unmarshal(data, &root) == nil {
			break
		}
		root = map[string]json.RawMessage{}
	}
	stripped := false
	if raw, ok := root["pi"]; ok {
		var pi map[string]json.RawMessage
		if jsonObject(raw) && json.Unmarshal(raw, &pi) == nil {
			if _, ok := pi["stores"]; ok {
				delete(pi, "stores")
				updated, err := json.Marshal(pi)
				if err != nil {
					return nil, nil, err
				}
				root["pi"] = updated
				stripped = true
			}
		}
	}
	if !stripped {
		return args, func() {}, nil
	}
	data, err := json.Marshal(root)
	if err != nil {
		return nil, nil, err
	}
	f, err := os.CreateTemp("", "aide-ccusage-*.json")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { _ = os.Remove(f.Name()) }
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		cleanup()
		return nil, nil, err
	}
	if err = f.Close(); err != nil {
		cleanup()
		return nil, nil, err
	}
	args = append([]string(nil), args...)
	switch {
	case index < 0:
		args = append(args, "--config", f.Name())
	case inline:
		args[index] = "--config=" + f.Name()
	default:
		args[index] = f.Name()
	}
	return args, cleanup, nil
}
