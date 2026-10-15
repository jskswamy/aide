package provision

import "testing"

func TestParseSourceRef(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		want  SourceRef
		aide  string
		bare  string
		clazz string
	}{
		{"bare owner/repo", "owner/repo", SourceRef{Transport: "", Repo: "owner/repo"}, "github:owner/repo", "owner/repo", "github"},
		{"github prefix", "github:owner/repo", SourceRef{Transport: "github", Repo: "owner/repo"}, "github:owner/repo", "owner/repo", "github"},
		{"git prefix", "git:host/path.git", SourceRef{Transport: "git", Repo: "host/path.git"}, "git:host/path.git", "git:host/path.git", "git"},
		{"https URL", "https://example.com/x", SourceRef{Transport: "https", Repo: "example.com/x"}, "https://example.com/x", "https://example.com/x", "url"},
		{"http URL", "http://example.com/x", SourceRef{Transport: "http", Repo: "example.com/x"}, "http://example.com/x", "http://example.com/x", "url"},
		{"local abs path", "/abs/path", SourceRef{Transport: "local", Repo: "/abs/path"}, "/abs/path", "/abs/path", "local"},
		{"bare name", "name", SourceRef{Transport: "", Repo: "name"}, "github:name", "name", "github"},
		{"npm package", "npm:@scope/pkg", SourceRef{Transport: "npm", Repo: "@scope/pkg"}, "npm:@scope/pkg", "npm:@scope/pkg", "npm"},
		{"file URL", "file:///abs/pkg", SourceRef{Transport: "file", Repo: "/abs/pkg"}, "file:///abs/pkg", "file:///abs/pkg", "file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseSourceRef(tt.in)
			if got != tt.want {
				t.Errorf("ParseSourceRef(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
			if a := got.Aide(); a != tt.aide {
				t.Errorf("Aide() = %q, want %q", a, tt.aide)
			}
			if b := got.Bare(); b != tt.bare {
				t.Errorf("Bare() = %q, want %q", b, tt.bare)
			}
			if c := got.Classify(); c != tt.clazz {
				t.Errorf("Classify() = %q, want %q", c, tt.clazz)
			}
		})
	}
}
