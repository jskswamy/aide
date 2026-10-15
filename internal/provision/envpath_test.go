package provision

import (
	"path/filepath"
	"testing"
)

func TestEnvOrHomePath(t *testing.T) {
	ctx := Context{HomeDir: "/home/u", Env: map[string]string{"SET": "/elsewhere", "EMPTY": ""}}
	for _, tt := range []struct {
		key  string
		want string
	}{
		{"SET", "/elsewhere"},
		{"EMPTY", filepath.Join("/home/u", ".agent", "dir")},
		{"UNSET", filepath.Join("/home/u", ".agent", "dir")},
	} {
		if got := EnvOrHomePath(ctx, tt.key, ".agent", "dir"); got != tt.want {
			t.Errorf("EnvOrHomePath(%s) = %q, want %q", tt.key, got, tt.want)
		}
	}
}
