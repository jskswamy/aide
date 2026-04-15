package main

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/capability"
)

// TestDetectProject_OsDirFS_RealFilesystem verifies the wrap used in
// production (os.DirFS) matches the behaviour the unit tests cover
// with fstest.MapFS. Three realistic project layouts.
func TestDetectProject_OsDirFS_RealFilesystem(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  []string
	}{
		{"go service", map[string]string{
			"go.mod":                   "module x\n",
			"Dockerfile":               "FROM golang:1.25\n",
			".github/workflows/ci.yml": "name: ci\n",
		}, []string{"docker", "go", "github"}},
		{"python k8s", map[string]string{
			"pyproject.toml":  "[project]\nname=\"x\"\n",
			"k8s/deploy.yaml": "apiVersion: apps/v1\n",
		}, []string{"python", "k8s"}},
		{"node + aws", map[string]string{
			"package.json": "{\"dependencies\":{\"aws-sdk\":\"^2\"}}\n",
		}, []string{"aws", "npm"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for p, body := range tc.files {
				full := filepath.Join(dir, p)
				if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got := capability.DetectProject(os.DirFS(dir))
			sort.Strings(got)
			sort.Strings(tc.want)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("DetectProject(os.DirFS(%s)) = %v, want %v",
					dir, got, tc.want)
			}
			_ = bytes.Buffer{} // keep "bytes" import live for potential future assertions
		})
	}
}
