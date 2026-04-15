// internal/capability/detect_golden_test.go
package capability

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// writeFixture materialises a map of relative path → file contents
// into a fresh t.TempDir() and returns the dir. Empty contents mean
// "create the file with zero bytes." A trailing "/" in the key means
// "create as a directory, not a file."
func writeFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for p, body := range files {
		full := filepath.Join(dir, p)
		if p[len(p)-1] == '/' {
			if err := os.MkdirAll(full, 0o700); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestDetectProject_Golden(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  []string
	}{
		// 15 minimal fixtures
		{"docker only", map[string]string{"Dockerfile": ""}, []string{"docker"}},
		{"terraform only", map[string]string{"main.tf": ""}, []string{"terraform"}},
		{"go only", map[string]string{"go.mod": "module x\n"}, []string{"go"}},
		{"rust only", map[string]string{"Cargo.toml": ""}, []string{"rust"}},
		{"python only", map[string]string{"pyproject.toml": ""}, []string{"python"}},
		{"ruby only", map[string]string{"Gemfile": ""}, []string{"ruby"}},
		{"java only", map[string]string{"pom.xml": ""}, []string{"java"}},
		{"k8s dir only", map[string]string{"k8s/": ""}, []string{"k8s"}},
		{"github only", map[string]string{".github/workflows/": ""}, []string{"github"}},
		{"helm only", map[string]string{"Chart.yaml": ""}, []string{"helm"}},
		{"npm only", map[string]string{"package.json": "{}"}, []string{"npm"}},
		{"vault only", map[string]string{".vault-token": "xxx"}, []string{"vault"}},
		{"aws via go.mod", map[string]string{
			"go.mod": "module x\nrequire github.com/aws/aws-sdk-go v1\n",
		}, []string{"go", "aws"}},
		{"gcp via requirements", map[string]string{
			"requirements.txt": "google-cloud-storage\n",
		}, []string{"python", "gcp"}},
		{"git-remote via config", map[string]string{
			".git/config": "[remote \"origin\"]\n\turl = git@github.com:x/y.git\n",
		}, []string{"git-remote"}},

		// 5 combo fixtures
		{"go service + docker + ci", map[string]string{
			"go.mod":                   "module x\n",
			"Dockerfile":               "FROM golang:1.25\n",
			".github/workflows/ci.yml": "name: ci\n",
		}, []string{"docker", "go", "github"}},
		{"python data + conda + k8s", map[string]string{
			"pyproject.toml":  "[project]\nname=\"x\"\n",
			"environment.yml": "name: env\n",
			"k8s/deploy.yaml": "apiVersion: apps/v1\n",
		}, []string{"python", "k8s"}},
		{"terraform nested", map[string]string{
			"modules/vpc/main.tf": "resource \"x\" \"y\" {}\n",
		}, nil},
		{"node monorepo with aws sdk", map[string]string{
			"package.json": "{\"dependencies\":{\"aws-sdk\":\"^2\"}}\n",
		}, []string{"aws", "npm"}},
		{"empty project", map[string]string{}, nil},

		// 5 edge / negative fixtures
		{"terraform only depth-1 (not root)", map[string]string{
			"infra/main.tf": "",
		}, []string{"terraform"}},
		{"k8s yaml at depth-1", map[string]string{
			"deploy/svc.yaml": "apiVersion: v1\nkind: Service\n",
		}, []string{"k8s"}},
		{"yaml without apiVersion", map[string]string{
			"config.yaml": "name: foo\n",
		}, nil},
		{"Dockerfile as directory", map[string]string{
			"Dockerfile/": "",
		}, nil},
		{"go.mod only in subdir (no root detection)", map[string]string{
			"submodule/go.mod": "module y\n",
		}, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeFixture(t, tc.files)
			got := DetectProject(os.DirFS(dir))

			// Golden match is by set, not order; DetectProject's output
			// order is not part of its contract.
			if !reflect.DeepEqual(sortedCopy(got), sortedCopy(tc.want)) {
				t.Fatalf("DetectProject content mismatch\n  got:  %v\n  want: %v",
					got, tc.want)
			}
		})
	}
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
