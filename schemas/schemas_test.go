package schemas_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jskswamy/aide/schemas"
)

func TestSchemaEmbeddedValidJSONAndRefs(t *testing.T) {
	t.Parallel()

	kinds := schemas.AllKinds()
	if len(kinds) != 5 {
		t.Fatalf("AllKinds() len = %d, want 5", len(kinds))
	}

	for _, kind := range kinds {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()

			raw, err := schemas.Bytes(kind)
			if err != nil {
				t.Fatalf("Bytes(%q) error = %v", kind, err)
			}
			var doc map[string]any
			if err := json.Unmarshal(raw, &doc); err != nil {
				t.Fatalf("invalid JSON in %s: %v", schemas.Filename(kind), err)
			}
			if got, _ := doc["$schema"].(string); got != "https://json-schema.org/draft/2020-12/schema" {
				t.Errorf("$schema = %q, want Draft 2020-12", got)
			}
			if got, _ := doc["$id"].(string); got != schemas.DefaultURL(kind) {
				t.Errorf("$id = %q, want %q", got, schemas.DefaultURL(kind))
			}
			if title, _ := doc["title"].(string); title == "" {
				t.Error("schema missing non-empty title")
			}

			defs, _ := doc["$defs"].(map[string]any)
			var refs []string
			collectRefs(doc, &refs)
			for _, ref := range refs {
				name, ok := strings.CutPrefix(ref, "#/$defs/")
				if !ok {
					t.Errorf("unexpected $ref %q (expected #/$defs/...)", ref)
					continue
				}
				if _, exists := defs[name]; !exists {
					t.Errorf("broken $ref %q: %q not in $defs", ref, name)
				}
			}
		})
	}
}

func TestSchemaParseKindAndUnknown(t *testing.T) {
	t.Parallel()

	for _, k := range schemas.AllKinds() {
		got, ok := schemas.ParseKind(" " + strings.ToUpper(string(k)) + " ")
		if !ok || got != k {
			t.Errorf("ParseKind(%q) = (%q, %v), want (%q, true)", k, got, ok, k)
		}
	}
	if _, ok := schemas.ParseKind("unknown"); ok {
		t.Error("ParseKind(unknown) = true, want false")
	}
	if _, err := schemas.Bytes(schemas.Kind("unknown")); err == nil {
		t.Error("Bytes(unknown) = nil, want error")
	}
}

func TestSchemaKindForSplitRel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		rel  string
		want schemas.Kind
	}{
		{"config.yaml", schemas.KindConfig},
		{"sandbox.yaml", schemas.KindSandbox},
		{"provisioning.yaml", schemas.KindProvisioning},
		{"profiles/work.yaml", schemas.KindProfile},
		{"profiles/default.yaml", schemas.KindProfile},
	}
	for _, tc := range tests {
		if got := schemas.KindForSplitRel(tc.rel); got != tc.want {
			t.Errorf("KindForSplitRel(%q) = %q, want %q", tc.rel, got, tc.want)
		}
	}
}

func TestSchemaFileURI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{"/Users/alice/.config/aide/schemas/profile.schema.json", "file:///Users/alice/.config/aide/schemas/profile.schema.json"},
		{"/Users/alice smith/my config/profile.schema.json", "file:///Users/alice%20smith/my%20config/profile.schema.json"},
	}
	for _, tc := range tests {
		if got := schemas.FileURI(tc.in); got != tc.want {
			t.Errorf("FileURI(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestModelineHelpers(t *testing.T) {
	t.Parallel()

	target := schemas.DefaultURL(schemas.KindProfile)
	wantLine := schemas.Modeline(target)

	t.Run("prepends when absent", func(t *testing.T) {
		in := []byte("agent: claude\n")
		if schemas.HasModeline(in) {
			t.Fatal("HasModeline = true, want false")
		}
		got := string(schemas.EnsureModeline(in, target))
		want := wantLine + "\nagent: claude\n"
		if got != want {
			t.Errorf("EnsureModeline =\n%q\nwant:\n%q", got, want)
		}
		// Idempotent on second call.
		if again := string(schemas.EnsureModeline([]byte(got), target)); again != want {
			t.Errorf("EnsureModeline not idempotent:\n%q", again)
		}
	})

	t.Run("prepends above document start marker", func(t *testing.T) {
		in := []byte("---\nagent: claude\n")
		got := string(schemas.EnsureModeline(in, target))
		want := wantLine + "\n---\nagent: claude\n"
		if got != want {
			t.Errorf("EnsureModeline with --- =\n%q\nwant:\n%q", got, want)
		}
	})

	t.Run("preserves custom modeline in EnsureModeline", func(t *testing.T) {
		in := []byte("# file header\n# yaml-language-server: $schema=file:///custom.json\nagent: claude\n")
		if !schemas.HasModeline(in) {
			t.Fatal("HasModeline = false, want true")
		}
		line, ok := schemas.ExtractModeline(in)
		if !ok || line != "# yaml-language-server: $schema=file:///custom.json" {
			t.Fatalf("ExtractModeline = (%q, %v)", line, ok)
		}
		if got := string(schemas.EnsureModeline(in, target)); got != string(in) {
			t.Errorf("EnsureModeline modified custom modeline:\n%q", got)
		}
	})

	t.Run("ignores comment after first YAML key", func(t *testing.T) {
		in := []byte("agent: claude\n# yaml-language-server: $schema=file:///ignored.json\n")
		if schemas.HasModeline(in) {
			t.Fatal("HasModeline = true for non-leading comment, want false")
		}
	})

	t.Run("SetModeline replaces existing and preserves surrounding comments", func(t *testing.T) {
		in := []byte("# my profile\n# yaml-language-server: $schema=file:///old.json\nagent: claude\n")
		got := string(schemas.SetModeline(in, target))
		want := "# my profile\n" + wantLine + "\nagent: claude\n"
		if got != want {
			t.Errorf("SetModeline =\n%q\nwant:\n%q", got, want)
		}
	})

	t.Run("SetModeline prepends when absent", func(t *testing.T) {
		in := []byte("agent: claude\n")
		got := string(schemas.SetModeline(in, target))
		want := wantLine + "\nagent: claude\n"
		if got != want {
			t.Errorf("SetModeline =\n%q\nwant:\n%q", got, want)
		}
	})

	t.Run("EnsureModelineLine restores extracted line", func(t *testing.T) {
		in := []byte("env:\n  FOO: bar\n")
		custom := "# yaml-language-server: $schema=file:///local.json"
		got := string(schemas.EnsureModelineLine(in, custom))
		want := custom + "\nenv:\n  FOO: bar\n"
		if got != want {
			t.Errorf("EnsureModelineLine =\n%q\nwant:\n%q", got, want)
		}
	})

	t.Run("UTF-8 BOM keeps BOM at byte zero and detects existing modeline", func(t *testing.T) {
		bom := "\xef\xbb\xbf"
		withHeader := []byte(bom + "# yaml-language-server: $schema=file:///custom.json\nagent: claude\n")
		if !schemas.HasModeline(withHeader) {
			t.Fatal("HasModeline = false with UTF-8 BOM, want true")
		}
		if got := string(schemas.EnsureModeline(withHeader, target)); got != string(withHeader) {
			t.Errorf("EnsureModeline duplicated header with UTF-8 BOM:\n%q", got)
		}
		if got := string(schemas.SetModeline(withHeader, target)); got != bom+wantLine+"\nagent: claude\n" {
			t.Errorf("SetModeline with UTF-8 BOM =\n%q", got)
		}

		noHeader := []byte(bom + "agent: claude\n")
		if got := string(schemas.EnsureModeline(noHeader, target)); got != bom+wantLine+"\nagent: claude\n" {
			t.Errorf("EnsureModeline did not keep UTF-8 BOM at byte 0:\n%q", got)
		}
	})
}

func collectRefs(v any, out *[]string) {
	switch x := v.(type) {
	case map[string]any:
		if ref, ok := x["$ref"].(string); ok {
			*out = append(*out, ref)
		}
		for _, val := range x {
			collectRefs(val, out)
		}
	case []any:
		for _, item := range x {
			collectRefs(item, out)
		}
	}
}
