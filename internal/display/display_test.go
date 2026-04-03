package display

import (
	"testing"
)

func TestNetworkDisplayName(t *testing.T) {
	tests := []struct {
		mode string
		want string
	}{
		{"outbound", "outbound only"},
		{"none", "none"},
		{"unrestricted", "unrestricted"},
		{"", ""},
		{"custom", "custom"},
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			got := NetworkDisplayName(tt.mode)
			if got != tt.want {
				t.Errorf("NetworkDisplayName(%q) = %q, want %q", tt.mode, got, tt.want)
			}
		})
	}
}

func TestClassifyEnvSource(t *testing.T) {
	tests := []struct {
		name    string
		val     string
		wantSrc string
		wantKey string
	}{
		{"secrets dot", "{{ .secrets.api_key }}", "from secrets.api_key", "api_key"},
		{"secrets index", `{{ index .secrets "api_key" }}`, "from secrets.api_key", "api_key"},
		{"project root", "{{ .project_root }}", "from project_root", ""},
		{"runtime dir", "{{ .runtime_dir }}", "from runtime_dir", ""},
		{"generic template", "{{ .other }}", "template", ""},
		{"literal", "plain-value", "literal", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src, key := ClassifyEnvSource(tt.val)
			if src != tt.wantSrc {
				t.Errorf("ClassifyEnvSource(%q) source = %q, want %q", tt.val, src, tt.wantSrc)
			}
			if key != tt.wantKey {
				t.Errorf("ClassifyEnvSource(%q) key = %q, want %q", tt.val, key, tt.wantKey)
			}
		})
	}
}

func TestResolveEnvDisplay(t *testing.T) {
	secrets := map[string]string{"api_key": "sk-ant-very-long-secret"}

	t.Run("with secret key", func(t *testing.T) {
		got := ResolveEnvDisplay("{{ .secrets.api_key }}", "", "api_key", secrets)
		if got != "sk-ant-v***" {
			t.Errorf("got %q, want %q", got, "sk-ant-v***")
		}
	})

	t.Run("missing secret key", func(t *testing.T) {
		got := ResolveEnvDisplay("{{ .secrets.missing }}", "", "missing", secrets)
		if got != "{{ .secrets.missing }}" {
			t.Errorf("got %q, want original value", got)
		}
	})

	t.Run("no secret key", func(t *testing.T) {
		got := ResolveEnvDisplay("literal", "", "", nil)
		if got != "literal" {
			t.Errorf("got %q, want %q", got, "literal")
		}
	})

	t.Run("nil secret map", func(t *testing.T) {
		got := ResolveEnvDisplay("val", "", "key", nil)
		if got != "val" {
			t.Errorf("got %q, want %q", got, "val")
		}
	})
}

func TestRedactValue(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"short", "short***"},
		{"12345678", "12345678***"},
		{"123456789", "12345678***"},
		{"sk-ant-api03-very-long-key", "sk-ant-a***"},
		{"", "***"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := RedactValue(tt.input)
			if got != tt.want {
				t.Errorf("RedactValue(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestEnvAnnotation(t *testing.T) {
	tests := []struct {
		name string
		val  string
		want string
	}{
		{"secrets", "{{ .secrets.api_key }}", "\u2190 secrets.api_key"},
		{"project root", "{{ .project_root }}/bin", "\u2190 project_root"},
		{"runtime dir", "{{ .runtime_dir }}/mcp", "\u2190 runtime_dir"},
		{"template", "{{ .other }}", "\u2190 template"},
		{"literal", "plain-value", "= plain-value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EnvAnnotation(tt.val)
			if got != tt.want {
				t.Errorf("EnvAnnotation(%q) = %q, want %q", tt.val, got, tt.want)
			}
		})
	}
}

func TestSplitCommaList(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"simple", "a,b,c", 3},
		{"spaces", " a , b , c ", 3},
		{"empty parts", "a,,b", 2},
		{"single", "a", 1},
		{"empty string", "", 0},
		{"only commas", ",,,", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SplitCommaList(tt.input)
			if len(got) != tt.want {
				t.Errorf("SplitCommaList(%q) len = %d, want %d", tt.input, len(got), tt.want)
			}
		})
	}
}

func TestRemoveFromSlice(t *testing.T) {
	tests := []struct {
		name  string
		slice []string
		item  string
		want  int
	}{
		{"remove existing", []string{"a", "b", "c"}, "b", 2},
		{"remove missing", []string{"a", "b"}, "c", 2},
		{"remove all", []string{"a", "a", "a"}, "a", 0},
		{"empty slice", nil, "a", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RemoveFromSlice(tt.slice, tt.item)
			if len(got) != tt.want {
				t.Errorf("RemoveFromSlice() len = %d, want %d", len(got), tt.want)
			}
		})
	}
}

func TestExpandHome(t *testing.T) {
	t.Run("tilde prefix", func(t *testing.T) {
		got := ExpandHome("~/Documents")
		if got == "~/Documents" {
			t.Error("ExpandHome should expand tilde prefix")
		}
	})

	t.Run("absolute path", func(t *testing.T) {
		got := ExpandHome("/usr/local/bin")
		if got != "/usr/local/bin" {
			t.Errorf("got %q, want /usr/local/bin", got)
		}
	})

	t.Run("relative path", func(t *testing.T) {
		got := ExpandHome("relative/path")
		if got != "relative/path" {
			t.Errorf("got %q, want relative/path", got)
		}
	})

	t.Run("tilde only no slash", func(t *testing.T) {
		got := ExpandHome("~")
		if got != "~" {
			t.Errorf("got %q, want ~ unchanged", got)
		}
	})
}
