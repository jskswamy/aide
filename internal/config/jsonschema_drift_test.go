package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/jskswamy/aide/schemas"
	"gopkg.in/yaml.v3"
)

// TestJSONSchemaStructParity ensures every yaml-tagged struct field in
// internal/config has a matching property in the corresponding embedded JSON
// Schema, and vice versa. This catches schema drift whenever a Go config
// struct changes.
func TestJSONSchemaStructParity(t *testing.T) {
	t.Parallel()

	type check struct {
		name      string
		kind      schemas.Kind
		defName   string // empty = root schema object
		goType    reflect.Type
		renames   map[string]string // Go yaml tag -> on-disk YAML key
		extraKeys []string          // polymorphic yaml:"-" fields handled in UnmarshalYAML
	}

	checks := []check{
		// config.schema.json
		{name: "config/root", kind: schemas.KindConfig, goType: reflect.TypeFor[CoreConfig]()},
		{name: "config/AgentDef", kind: schemas.KindConfig, defName: "AgentDef", goType: reflect.TypeFor[AgentDef]()},
		{name: "config/Preferences", kind: schemas.KindConfig, defName: "Preferences", goType: reflect.TypeFor[Preferences]()},
		{name: "config/StatuslineConfig", kind: schemas.KindConfig, defName: "StatuslineConfig", goType: reflect.TypeFor[StatuslineConfig]()},
		{name: "config/ModuleConfig", kind: schemas.KindConfig, defName: "ModuleConfig", goType: reflect.TypeFor[ModuleConfig]()},

		// profile.schema.json (profiles/<name>.yaml uses EncodeProfileYAML key renames)
		{
			name:      "profile/root",
			kind:      schemas.KindProfile,
			goType:    reflect.TypeFor[Context](),
			renames:   map[string]string{"profile": "config_dir_name", "profile_dir": "config_dir"},
			extraKeys: []string{"mcp_servers", "plugins", "hooks"},
		},
		{name: "profile/MatchRule", kind: schemas.KindProfile, defName: "MatchRule", goType: reflect.TypeFor[MatchRule]()},
		{name: "profile/CredentialBinding", kind: schemas.KindProfile, defName: "CredentialBinding", goType: reflect.TypeFor[CredentialBinding]()},
		{name: "profile/CredentialRoute", kind: schemas.KindProfile, defName: "CredentialRoute", goType: reflect.TypeFor[CredentialRoute]()},
		{name: "profile/GatewayConfig", kind: schemas.KindProfile, defName: "GatewayConfig", goType: reflect.TypeFor[GatewayConfig]()},
		{name: "profile/SandboxPolicy", kind: schemas.KindProfile, defName: "SandboxPolicy", goType: reflect.TypeFor[SandboxPolicy]()},
		{name: "profile/MCPServer", kind: schemas.KindProfile, defName: "MCPServer", goType: reflect.TypeFor[MCPServer]()},
		{name: "profile/MCPServerOverride", kind: schemas.KindProfile, defName: "MCPServerOverride", goType: reflect.TypeFor[ContextOverride[MCPServer]]()},
		{name: "profile/PluginOverride", kind: schemas.KindProfile, defName: "PluginOverride", goType: reflect.TypeFor[ContextOverride[PluginEntry]]()},
		{name: "profile/HookEntry", kind: schemas.KindProfile, defName: "HookEntry", goType: reflect.TypeFor[HookEntry]()},
		{name: "profile/HooksOverride", kind: schemas.KindProfile, defName: "HooksOverride", goType: reflect.TypeFor[HooksOverride]()},

		// sandbox.schema.json
		{name: "sandbox/root", kind: schemas.KindSandbox, goType: reflect.TypeFor[SandboxFile]()},
		{name: "sandbox/CapabilityDef", kind: schemas.KindSandbox, defName: "CapabilityDef", goType: reflect.TypeFor[CapabilityDef]()},
		{name: "sandbox/SandboxPolicy", kind: schemas.KindSandbox, defName: "SandboxPolicy", goType: reflect.TypeFor[SandboxPolicy]()},
		{name: "sandbox/CustomGuard", kind: schemas.KindSandbox, defName: "CustomGuard", goType: reflect.TypeFor[CustomGuard]()},
		{name: "sandbox/GuardType", kind: schemas.KindSandbox, defName: "GuardType", goType: reflect.TypeFor[GuardType]()},

		// provisioning.schema.json
		{name: "provisioning/root", kind: schemas.KindProvisioning, goType: reflect.TypeFor[ProvisioningFile]()},
		{name: "provisioning/MCPServer", kind: schemas.KindProvisioning, defName: "MCPServer", goType: reflect.TypeFor[MCPServer]()},
		{name: "provisioning/HookEntry", kind: schemas.KindProvisioning, defName: "HookEntry", goType: reflect.TypeFor[HookEntry]()},

		// project.schema.json
		{name: "project/root", kind: schemas.KindProject, goType: reflect.TypeFor[ProjectOverride]()},
		{name: "project/SandboxPolicy", kind: schemas.KindProject, defName: "SandboxPolicy", goType: reflect.TypeFor[SandboxPolicy]()},
		{name: "project/Preferences", kind: schemas.KindProject, defName: "Preferences", goType: reflect.TypeFor[Preferences]()},
		{name: "project/StatuslineConfig", kind: schemas.KindProject, defName: "StatuslineConfig", goType: reflect.TypeFor[StatuslineConfig]()},
		{name: "project/ModuleConfig", kind: schemas.KindProject, defName: "ModuleConfig", goType: reflect.TypeFor[ModuleConfig]()},
	}

	for _, tc := range checks {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			doc := loadSchemaMap(t, tc.kind)
			obj := doc
			if tc.defName != "" {
				defs, ok := doc["$defs"].(map[string]any)
				if !ok {
					t.Fatalf("%s: missing $defs", tc.kind)
				}
				sub, ok := defs[tc.defName].(map[string]any)
				if !ok {
					t.Fatalf("%s: missing $defs.%s", tc.kind, tc.defName)
				}
				obj = sub
			}
			props, ok := obj["properties"].(map[string]any)
			if !ok {
				t.Fatalf("%s (%s): missing properties map", tc.kind, tc.defName)
			}

			goKeys := structYAMLKeys(tc.goType, tc.renames, tc.extraKeys)
			schemaKeys := make([]string, 0, len(props))
			for k := range props {
				schemaKeys = append(schemaKeys, k)
			}
			slices.Sort(goKeys)
			slices.Sort(schemaKeys)

			for _, k := range goKeys {
				if _, exists := props[k]; !exists {
					t.Errorf("Go YAML field %q missing from %s (%s)", k, schemas.Filename(tc.kind), tc.defName)
				}
			}
			for _, k := range schemaKeys {
				if !slices.Contains(goKeys, k) {
					t.Errorf("schema property %q in %s (%s) does not exist on Go struct %s", k, schemas.Filename(tc.kind), tc.defName, tc.goType.Name())
				}
			}
		})
	}
}

// TestJSONSchemaNetworkPolicyParity checks the object branch of NetworkPolicy
// in all schemas that embed it.
func TestJSONSchemaNetworkPolicyParity(t *testing.T) {
	t.Parallel()

	goKeys := structYAMLKeys(reflect.TypeFor[NetworkPolicy](), nil, nil)
	for _, kind := range []schemas.Kind{schemas.KindProfile, schemas.KindSandbox, schemas.KindProject} {
		doc := loadSchemaMap(t, kind)
		defs, ok := doc["$defs"].(map[string]any)
		if !ok {
			t.Fatalf("%s: missing $defs", kind)
		}
		np, ok := defs["NetworkPolicy"].(map[string]any)
		if !ok {
			t.Fatalf("%s: missing NetworkPolicy def", kind)
		}
		branches, ok := np["oneOf"].([]any)
		if !ok {
			t.Fatalf("%s: NetworkPolicy missing oneOf", kind)
		}

		var objProps map[string]any
		for _, b := range branches {
			bm, ok := b.(map[string]any)
			if !ok {
				continue
			}
			if bm["type"] == "object" {
				props, ok := bm["properties"].(map[string]any)
				if !ok {
					t.Fatalf("%s: NetworkPolicy object branch missing properties", kind)
				}
				objProps = props
			}
		}
		if objProps == nil {
			t.Fatalf("%s: NetworkPolicy missing object branch", kind)
		}
		for _, k := range goKeys {
			if _, ok := objProps[k]; !ok {
				t.Errorf("%s: NetworkPolicy missing property %q", kind, k)
			}
		}
		for k := range objProps {
			if !slices.Contains(goKeys, k) {
				t.Errorf("%s: NetworkPolicy has unknown property %q", kind, k)
			}
		}
	}
}

func loadSchemaMap(t *testing.T, kind schemas.Kind) map[string]any {
	t.Helper()
	raw, err := schemas.Bytes(kind)
	if err != nil {
		t.Fatalf("schemas.Bytes(%q): %v", kind, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("json.Unmarshal(%q): %v", kind, err)
	}
	return doc
}

func structYAMLKeys(rt reflect.Type, renames map[string]string, extra []string) []string {
	var keys []string
	for i := range rt.NumField() {
		f := rt.Field(i)
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("yaml")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		if name == "" || name == "-" {
			continue
		}
		if renamed, ok := renames[name]; ok {
			name = renamed
		}
		keys = append(keys, name)
	}
	keys = append(keys, extra...)
	return keys
}

// TestJSONSchemaDocumentValidation validates positive and negative YAML
// documents—including writer output and polymorphic YAML shapes—against all
// five embedded Draft 2020-12 JSON Schemas.
func TestJSONSchemaDocumentValidation(t *testing.T) {
	t.Parallel()

	t.Run("writer output validates against schemas", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "config.yaml")
		yolo := false
		showInfo := true
		cfg := &Config{
			ProfileSchema:  true,
			DefaultContext: "work",
			Agents:         map[string]AgentDef{"claude": {Binary: "claude", Icon: "🤖"}},
			Preferences:    &Preferences{ShowInfo: &showInfo, InfoStyle: "compact", InfoDetail: "normal", Yolo: &yolo},
			Statusline:     &StatuslineConfig{Order: []string{"sandbox", "network"}, Sandbox: &ModuleConfig{On: "on"}},
			Capabilities:   map[string]CapabilityDef{"custom": {Extends: "go", Ports: []int{22}}},
			Sandboxes:      map[string]SandboxPolicy{"strict": {Network: &NetworkPolicy{Mode: "none"}, WritableExtra: []string{"/tmp"}}},
			CustomGuards:   map[string]CustomGuard{"corp": {Paths: []string{"~/.corp"}}},
			GuardTypes:     map[string]GuardType{"readonly": {Behavior: "deny-write", Description: "ro"}},
			NeverAllow:     []string{"~/.ssh"},
			Plugins:        PluginMap{"owner/repo": PluginEntryMarketplace([]string{"p1"}), "direct": PluginEntryURLDirect("https://example.com/p")},
			MCPServers:     MCPServerMap{"srv": {Command: "npx", Args: []string{"-y", "srv"}}},
			Hooks:          HooksMap{"PreToolUse": {{Name: "guard", Command: "echo ok", Timeout: 5}}},
			Contexts: map[string]Context{
				"work": {
					Agent:        "claude",
					Match:        []MatchRule{{Remote: "github.com/org/*"}},
					Capabilities: []string{"k8s", "network"},
					Env:          map[string]string{"FOO": "bar"},
					Profile:      "work",
					Sandbox:      &SandboxRef{Inline: &SandboxPolicy{WritableExtra: []string{"~/src"}, Network: &NetworkPolicy{Mode: "outbound", AllowPorts: []int{443}}}},
					Gateways:     map[string]GatewayConfig{"gw": {Preset: "anthropic", Store: "work", Key: "k", Harnesses: []string{"claude"}}},
					GatewayChoice: map[string]string{"claude": "gw"},
				},
			},
		}
		if err := WriteConfigTo(cfg, cfgPath); err != nil {
			t.Fatalf("WriteConfigTo: %v", err)
		}
		projPath := filepath.Join(dir, ProjectConfigFileName)
		if err := WriteProjectOverride(projPath, &ProjectOverride{
			Profile:            "work",
			Agent:              "claude",
			Capabilities:       []string{"go"},
			CapabilityVariants: map[string][]string{"python": {"uv"}},
			Sandbox:            &SandboxPolicy{Network: &NetworkPolicy{Mode: "outbound"}},
		}); err != nil {
			t.Fatalf("WriteProjectOverride: %v", err)
		}

		files := map[string]schemas.Kind{
			cfgPath:                                 schemas.KindConfig,
			ProfileFilePath(dir, "work"):            schemas.KindProfile,
			filepath.Join(dir, "sandbox.yaml"):      schemas.KindSandbox,
			filepath.Join(dir, "provisioning.yaml"): schemas.KindProvisioning,
			projPath:                                schemas.KindProject,
		}
		for p, kind := range files {
			raw, err := os.ReadFile(p)
			if err != nil {
				t.Fatalf("ReadFile(%s): %v", p, err)
			}
			if err := validateYAMLAgainstSchema(t, kind, raw); err != nil {
				t.Errorf("%s failed schema %s validation: %v\n%s", filepath.Base(p), kind, err, raw)
			}
		}
	})

	positiveCases := []struct {
		name string
		kind schemas.Kind
		yaml string
	}{
		{
			name: "profile sandbox false and legacy mcp_servers list",
			kind: schemas.KindProfile,
			yaml: "agent: claude\nsandbox: false\nmcp_servers:\n  - srv1\n",
		},
		{
			name: "profile sandbox named string and override mcp_servers",
			kind: schemas.KindProfile,
			yaml: "agent: claude\nsandbox: strict\nmcp_servers:\n  exclude:\n    - srv1\n  extra:\n    local:\n      url: https://example.com\n",
		},
		{
			name: "profile config_dir_name and config_dir",
			kind: schemas.KindProfile,
			yaml: "agent: claude\nconfig_dir_name: work\nconfig_dir: ~/.claude-work\n",
		},
		{
			name: "provisioning declare-only null plugin",
			kind: schemas.KindProvisioning,
			yaml: "plugins:\n  owner/repo: null\n  direct: https://example.com/p\n  owner/m:\n    - a\n",
		},
		{
			name: "project sandbox false and string network",
			kind: schemas.KindProject,
			yaml: "profile: work\nsandbox: false\n",
		},
	}
	for _, tc := range positiveCases {
		t.Run("valid/"+tc.name, func(t *testing.T) {
			t.Parallel()
			if err := validateYAMLAgainstSchema(t, tc.kind, []byte(tc.yaml)); err != nil {
				t.Errorf("expected valid YAML to pass %s schema, got: %v", tc.kind, err)
			}
		})
	}

	negativeCases := []struct {
		name string
		kind schemas.Kind
		yaml string
	}{
		{
			name: "config show_info wrong type string instead of bool",
			kind: schemas.KindConfig,
			yaml: "preferences:\n  show_info: \"yes\"\n",
		},
		{
			name: "config unknown root property",
			kind: schemas.KindConfig,
			yaml: "unknown_key: true\n",
		},
		{
			name: "config agent missing required binary",
			kind: schemas.KindConfig,
			yaml: "agents:\n  claude:\n    icon: x\n",
		},
		{
			name: "profile sandbox true disallowed",
			kind: schemas.KindProfile,
			yaml: "agent: claude\nsandbox: true\n",
		},
		{
			name: "profile unknown property apiVersion",
			kind: schemas.KindProfile,
			yaml: "apiVersion: v1\nagent: claude\n",
		},
		{
			name: "sandbox invalid network mode enum",
			kind: schemas.KindSandbox,
			yaml: "sandboxes:\n  s:\n    network: bogus\n",
		},
		{
			name: "sandbox custom_guard missing required paths",
			kind: schemas.KindSandbox,
			yaml: "custom_guards:\n  g:\n    description: missing paths\n",
		},
		{
			name: "provisioning hook missing required command",
			kind: schemas.KindProvisioning,
			yaml: "hooks:\n  PreToolUse:\n    - name: only-name\n",
		},
		{
			name: "provisioning plugin entry object disallowed",
			kind: schemas.KindProvisioning,
			yaml: "plugins:\n  bad:\n    nested: true\n",
		},
		{
			name: "project yolo wrong type string",
			kind: schemas.KindProject,
			yaml: "yolo: \"true\"\n",
		},
		{
			name: "project unknown property",
			kind: schemas.KindProject,
			yaml: "secret: work\n",
		},
	}
	for _, tc := range negativeCases {
		t.Run("invalid/"+tc.name, func(t *testing.T) {
			t.Parallel()
			if err := validateYAMLAgainstSchema(t, tc.kind, []byte(tc.yaml)); err == nil {
				t.Errorf("expected invalid YAML to fail %s schema, but it passed:\n%s", tc.kind, tc.yaml)
			}
		})
	}
}

func validateYAMLAgainstSchema(t *testing.T, kind schemas.Kind, yamlBytes []byte) error {
	t.Helper()
	var parsed any
	if err := yaml.Unmarshal(yamlBytes, &parsed); err != nil {
		return fmt.Errorf("yaml.Unmarshal: %w", err)
	}
	// Normalize via JSON round-trip so numbers/maps match JSON Schema types.
	jb, err := json.Marshal(parsed)
	if err != nil {
		return fmt.Errorf("json.Marshal: %w", err)
	}
	var doc any
	if err := json.Unmarshal(jb, &doc); err != nil {
		return fmt.Errorf("json.Unmarshal: %w", err)
	}
	schema := loadSchemaMap(t, kind)
	defs, _ := schema["$defs"].(map[string]any)
	return validateJSONNode(doc, schema, defs, "$")
}

// validateJSONNode checks the Draft 2020-12 subset used by aide's schemas:
// $ref, type, const, enum, required, properties, additionalProperties, items,
// minimum, maximum, oneOf, and anyOf.
func validateJSONNode(val any, schema map[string]any, defs map[string]any, path string) error {
	if ref, ok := schema["$ref"].(string); ok {
		name, cut := strings.CutPrefix(ref, "#/$defs/")
		if !cut {
			return fmt.Errorf("%s: unsupported $ref %q", path, ref)
		}
		target, ok := defs[name].(map[string]any)
		if !ok {
			return fmt.Errorf("%s: missing $defs.%s", path, name)
		}
		return validateJSONNode(val, target, defs, path)
	}

	if oneOf, ok := schema["oneOf"].([]any); ok {
		matches := 0
		for _, branch := range oneOf {
			bm, ok := branch.(map[string]any)
			if !ok {
				continue
			}
			if validateJSONNode(val, bm, defs, path) == nil {
				matches++
			}
		}
		if matches != 1 {
			return fmt.Errorf("%s: matched %d oneOf branches, want 1 (val=%#v)", path, matches, val)
		}
	}

	if anyOf, ok := schema["anyOf"].([]any); ok {
		matched := false
		for _, branch := range anyOf {
			bm, ok := branch.(map[string]any)
			if !ok {
				continue
			}
			if validateJSONNode(val, bm, defs, path) == nil {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("%s: matched 0 anyOf branches (val=%#v)", path, val)
		}
	}

	if c, hasConst := schema["const"]; hasConst {
		if !reflect.DeepEqual(val, c) {
			return fmt.Errorf("%s: got %#v, want const %#v", path, val, c)
		}
	}

	if enumVals, ok := schema["enum"].([]any); ok {
		if !slices.ContainsFunc(enumVals, func(e any) bool { return reflect.DeepEqual(val, e) }) {
			return fmt.Errorf("%s: value %#v not in enum %v", path, val, enumVals)
		}
	}

	if typ, ok := schema["type"].(string); ok {
		if err := checkJSONType(val, typ, path); err != nil {
			return err
		}
	}

	switch v := val.(type) {
	case map[string]any:
		if reqs, ok := schema["required"].([]any); ok {
			for _, r := range reqs {
				rk, _ := r.(string)
				if _, exists := v[rk]; !exists {
					return fmt.Errorf("%s: missing required property %q", path, rk)
				}
			}
		}
		props, _ := schema["properties"].(map[string]any)
		for k, child := range v {
			childPath := path + "." + k
			if propSchema, exists := props[k].(map[string]any); exists {
				if err := validateJSONNode(child, propSchema, defs, childPath); err != nil {
					return err
				}
				continue
			}
			switch addl := schema["additionalProperties"].(type) {
			case bool:
				if !addl {
					return fmt.Errorf("%s: additional property %q not allowed", path, k)
				}
			case map[string]any:
				if err := validateJSONNode(child, addl, defs, childPath); err != nil {
					return err
				}
			}
		}
	case []any:
		if itemSchema, ok := schema["items"].(map[string]any); ok {
			for i, item := range v {
				if err := validateJSONNode(item, itemSchema, defs, fmt.Sprintf("%s[%d]", path, i)); err != nil {
					return err
				}
			}
		}
	case float64:
		if minV, ok := schema["minimum"].(float64); ok && v < minV {
			return fmt.Errorf("%s: %v < minimum %v", path, v, minV)
		}
		if maxV, ok := schema["maximum"].(float64); ok && v > maxV {
			return fmt.Errorf("%s: %v > maximum %v", path, v, maxV)
		}
	}
	return nil
}

func checkJSONType(val any, typ, path string) error {
	switch typ {
	case "object":
		if _, ok := val.(map[string]any); !ok {
			return fmt.Errorf("%s: got %T, want object", path, val)
		}
	case "array":
		if _, ok := val.([]any); !ok {
			return fmt.Errorf("%s: got %T, want array", path, val)
		}
	case "string":
		if _, ok := val.(string); !ok {
			return fmt.Errorf("%s: got %T, want string", path, val)
		}
	case "boolean":
		if _, ok := val.(bool); !ok {
			return fmt.Errorf("%s: got %T, want boolean", path, val)
		}
	case "integer":
		f, ok := val.(float64)
		if !ok || f != float64(int64(f)) {
			return fmt.Errorf("%s: got %#v, want integer", path, val)
		}
	case "null":
		if val != nil {
			return fmt.Errorf("%s: got %T, want null", path, val)
		}
	}
	return nil
}
