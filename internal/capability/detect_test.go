package capability

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"
)

func mustWriteFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
}

func TestDetectProject_Dockerfile(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "Dockerfile"), []byte("FROM alpine"))

	suggestions := DetectProject(dir)
	assertContains(t, suggestions, "docker", "expected docker suggestion for project with Dockerfile")
}

func TestDetectProject_DockerCompose(t *testing.T) {
	for _, name := range []string{"docker-compose.yaml", "docker-compose.yml"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			mustWriteFile(t, filepath.Join(dir, name), []byte("version: '3'"))

			suggestions := DetectProject(dir)
			assertContains(t, suggestions, "docker", "expected docker suggestion for "+name)
		})
	}
}

func TestDetectProject_Terraform(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "main.tf"), []byte("resource \"aws_instance\" {}"))

	suggestions := DetectProject(dir)
	assertContains(t, suggestions, "terraform", "expected terraform suggestion for .tf files")
}

func TestDetectProject_TerraformOneLevelDeep(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "infra")
	mustMkdirAll(t, subdir)
	mustWriteFile(t, filepath.Join(subdir, "main.tf"), []byte("resource \"aws_instance\" {}"))

	suggestions := DetectProject(dir)
	assertContains(t, suggestions, "terraform", "expected terraform suggestion for .tf files one level deep")
}

func TestDetectProject_K8sDirectory(t *testing.T) {
	dir := t.TempDir()
	k8sDir := filepath.Join(dir, "k8s")
	mustMkdirAll(t, k8sDir)

	suggestions := DetectProject(dir)
	assertContains(t, suggestions, "k8s", "expected k8s suggestion for k8s/ directory")
}

func TestDetectProject_ManifestsDirectory(t *testing.T) {
	dir := t.TempDir()
	manifestsDir := filepath.Join(dir, "manifests")
	mustMkdirAll(t, manifestsDir)

	suggestions := DetectProject(dir)
	assertContains(t, suggestions, "k8s", "expected k8s suggestion for manifests/ directory")
}

func TestDetectProject_K8sManifests(t *testing.T) {
	dir := t.TempDir()
	k8sDir := filepath.Join(dir, "k8s")
	mustMkdirAll(t, k8sDir)
	mustWriteFile(t, filepath.Join(k8sDir, "deployment.yaml"),
		[]byte("apiVersion: apps/v1\nkind: Deployment"))

	suggestions := DetectProject(dir)
	assertContains(t, suggestions, "k8s", "expected k8s suggestion for project with k8s manifests")
}

func TestDetectProject_YAMLWithAPIVersion(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "deploy")
	mustMkdirAll(t, subdir)
	mustWriteFile(t, filepath.Join(subdir, "service.yaml"),
		[]byte("apiVersion: v1\nkind: Service"))

	suggestions := DetectProject(dir)
	assertContains(t, suggestions, "k8s", "expected k8s suggestion for YAML with apiVersion one level deep")
}

func TestDetectProject_YAMLWithAPIVersionTopLevel(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "deployment.yaml"),
		[]byte("apiVersion: apps/v1\nkind: Deployment"))

	suggestions := DetectProject(dir)
	assertContains(t, suggestions, "k8s", "expected k8s suggestion for YAML with apiVersion at top level")
}

func TestDetectProject_AWSGoMod(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "go.mod"), []byte("module example\nrequire github.com/aws/aws-sdk-go v1.0.0"))

	suggestions := DetectProject(dir)
	assertContains(t, suggestions, "aws", "expected aws suggestion for go.mod with aws-sdk-go")
}

func TestDetectProject_AWSPython(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "requirements.txt"), []byte("boto3==1.26.0\nrequests"))

	suggestions := DetectProject(dir)
	assertContains(t, suggestions, "aws", "expected aws suggestion for requirements.txt with boto3")
}

func TestDetectProject_AWSNode(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "package.json"), []byte(`{"dependencies":{"aws-sdk":"^2.0.0"}}`))

	suggestions := DetectProject(dir)
	assertContains(t, suggestions, "aws", "expected aws suggestion for package.json with aws-sdk")
}

func TestDetectProject_GCPGoMod(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "go.mod"), []byte("module example\nrequire cloud.google.com/go v0.100.0"))

	suggestions := DetectProject(dir)
	assertContains(t, suggestions, "gcp", "expected gcp suggestion for go.mod with cloud.google.com")
}

func TestDetectProject_GCPPython(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "requirements.txt"), []byte("google-cloud-storage==2.0.0"))

	suggestions := DetectProject(dir)
	assertContains(t, suggestions, "gcp", "expected gcp suggestion for requirements.txt with google-cloud")
}

func TestDetectProject_GCPNode(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "package.json"), []byte(`{"dependencies":{"@google-cloud/storage":"^6.0.0"}}`))

	suggestions := DetectProject(dir)
	assertContains(t, suggestions, "gcp", "expected gcp suggestion for package.json with @google-cloud")
}

func TestDetectProject_NPM(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "package.json"), []byte(`{"name":"myapp"}`))

	suggestions := DetectProject(dir)
	assertContains(t, suggestions, "npm", "expected npm suggestion for package.json")
}

func TestDetectProject_Vault(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, ".vault-token"), []byte("s.token123"))

	suggestions := DetectProject(dir)
	assertContains(t, suggestions, "vault", "expected vault suggestion for .vault-token")
}

func TestDetect_GoProject(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "go.mod"), []byte("module example"))
	suggestions := DetectProject(dir)
	assertContains(t, suggestions, "go", "expected go for go.mod")
}

func TestDetect_RustProject(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "Cargo.toml"), []byte("[package]\nname = \"test\""))
	suggestions := DetectProject(dir)
	assertContains(t, suggestions, "rust", "expected rust for Cargo.toml")
}

func TestDetect_PythonProject(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "pyproject.toml"), []byte("[project]\nname = \"test\""))
	suggestions := DetectProject(dir)
	assertContains(t, suggestions, "python", "expected python for pyproject.toml")
}

func TestDetect_RubyProject(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "Gemfile"), []byte("source 'https://rubygems.org'"))
	suggestions := DetectProject(dir)
	assertContains(t, suggestions, "ruby", "expected ruby for Gemfile")
}

func TestDetect_JavaMavenProject(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "pom.xml"), []byte("<project></project>"))
	suggestions := DetectProject(dir)
	assertContains(t, suggestions, "java", "expected java for pom.xml")
}

func TestDetect_JavaGradleProject(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "build.gradle"), []byte("apply plugin: 'java'"))
	suggestions := DetectProject(dir)
	assertContains(t, suggestions, "java", "expected java for build.gradle")
}

func TestDetect_GitHubProject(t *testing.T) {
	dir := t.TempDir()
	mustMkdirAll(t, filepath.Join(dir, ".github", "workflows"))
	suggestions := DetectProject(dir)
	assertContains(t, suggestions, "github", "expected github for .github/workflows/")
}

func TestDetect_HelmProject(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "Chart.yaml"), []byte("apiVersion: v2\nname: test"))
	suggestions := DetectProject(dir)
	assertContains(t, suggestions, "helm", "expected helm for Chart.yaml")
}

func TestDetect_KubernetesDirectory(t *testing.T) {
	dir := t.TempDir()
	mustMkdirAll(t, filepath.Join(dir, "kubernetes"))
	suggestions := DetectProject(dir)
	assertContains(t, suggestions, "k8s", "expected k8s for kubernetes/ directory")
}

func TestDetectProject_NoMarkers(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "main.go"), []byte("package main"))

	suggestions := DetectProject(dir)
	if len(suggestions) != 0 {
		t.Errorf("expected no suggestions, got %v", suggestions)
	}
}

func TestDetectProject_Multiple(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "Dockerfile"), []byte("FROM alpine"))
	mustWriteFile(t, filepath.Join(dir, "package.json"), []byte(`{"dependencies":{"aws-sdk":"^2.0.0"}}`))
	mustMkdirAll(t, filepath.Join(dir, "k8s"))

	suggestions := DetectProject(dir)
	sort.Strings(suggestions)

	expected := []string{"aws", "docker", "k8s", "npm"}
	sort.Strings(expected)

	if len(suggestions) != len(expected) {
		t.Fatalf("expected %v, got %v", expected, suggestions)
	}
	for i, s := range suggestions {
		if s != expected[i] {
			t.Errorf("expected %s at index %d, got %s", expected[i], i, s)
		}
	}
}

func TestDetectProject_NoDuplicates(t *testing.T) {
	dir := t.TempDir()
	// Both Dockerfile and docker-compose.yaml present — should only get one "docker"
	mustWriteFile(t, filepath.Join(dir, "Dockerfile"), []byte("FROM alpine"))
	mustWriteFile(t, filepath.Join(dir, "docker-compose.yaml"), []byte("version: '3'"))

	suggestions := DetectProject(dir)
	count := 0
	for _, s := range suggestions {
		if s == "docker" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 docker suggestion, got %d", count)
	}
}

func TestFileExists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	mustWriteFile(t, path, []byte("hello"))

	if !fileExists(path) {
		t.Error("expected fileExists to return true for existing file")
	}
	if fileExists(filepath.Join(dir, "nonexistent")) {
		t.Error("expected fileExists to return false for nonexistent file")
	}
}

func TestDirExists(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "sub")
	mustMkdirAll(t, subdir)

	if !dirExists(subdir) {
		t.Error("expected dirExists to return true for existing dir")
	}
	if dirExists(filepath.Join(dir, "nonexistent")) {
		t.Error("expected dirExists to return false for nonexistent dir")
	}
	// A file is not a directory
	file := filepath.Join(dir, "file.txt")
	mustWriteFile(t, file, []byte("hello"))
	if dirExists(file) {
		t.Error("expected dirExists to return false for a file")
	}
}

func TestContainsInFile(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "go.mod"), []byte("module example\nrequire cloud.google.com/go"))

	if !containsInFile(dir, "go.mod", "cloud.google.com") {
		t.Error("expected containsInFile to find cloud.google.com in go.mod")
	}
	if containsInFile(dir, "go.mod", "nonexistent-string") {
		t.Error("expected containsInFile to return false for missing string")
	}
	if containsInFile(dir, "nonexistent.txt", "anything") {
		t.Error("expected containsInFile to return false for missing file")
	}
}

func TestHasFileWithExtension(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "main.tf"), []byte("resource"))

	if !hasFileWithExtension(dir, ".tf") {
		t.Error("expected hasFileWithExtension to find .tf file")
	}
	if hasFileWithExtension(dir, ".py") {
		t.Error("expected hasFileWithExtension to return false for missing extension")
	}
}

func TestDetectProject_GitRemote(t *testing.T) {
	tmp := t.TempDir()
	gitDir := filepath.Join(tmp, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitConfig := "[remote \"origin\"]\n\turl = git@github.com:user/repo.git\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n"
	if err := os.WriteFile(filepath.Join(gitDir, "config"), []byte(gitConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	suggestions := DetectProject(tmp)
	if !slices.Contains(suggestions, "git-remote") {
		t.Errorf("expected git-remote in suggestions, got %v", suggestions)
	}
}

func TestDetectProject_GitRemote_NoRemotes(t *testing.T) {
	tmp := t.TempDir()
	gitDir := filepath.Join(tmp, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "config"), []byte("[core]\n\tbare = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	suggestions := DetectProject(tmp)
	for _, s := range suggestions {
		if s == "git-remote" {
			t.Error("should NOT suggest git-remote when no remotes configured")
		}
	}
}

func TestDetectProject_GitRemote_NoGitDir(t *testing.T) {
	tmp := t.TempDir()
	suggestions := DetectProject(tmp)
	for _, s := range suggestions {
		if s == "git-remote" {
			t.Error("should NOT suggest git-remote when no .git directory")
		}
	}
}

func TestDetectProject_MultipleDetections(t *testing.T) {
	tmp := t.TempDir()

	// Create markers for multiple capabilities
	if err := os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module test"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(tmp, ".github", "workflows"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitDir := filepath.Join(tmp, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "config"),
		[]byte("[remote \"origin\"]\n\turl = git@github.com:user/repo.git\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	suggestions := DetectProject(tmp)

	// Should detect go, github, and git-remote
	expected := map[string]bool{"go": false, "github": false, "git-remote": false}
	for _, s := range suggestions {
		if _, ok := expected[s]; ok {
			expected[s] = true
		}
	}
	for name, found := range expected {
		if !found {
			t.Errorf("expected %q to be detected, got suggestions: %v", name, suggestions)
		}
	}
}

func TestDetectProject_SuggestionFiltering(t *testing.T) {
	// Simulate the launcher's filtering: detected minus enabled = suggested
	detected := []string{"go", "github", "git-remote"}
	enabled := map[string]bool{"github": true}

	var suggested []string
	for _, name := range detected {
		if !enabled[name] {
			suggested = append(suggested, name)
		}
	}

	if len(suggested) != 2 {
		t.Errorf("expected 2 suggestions, got %d: %v", len(suggested), suggested)
	}

	// github should NOT be in suggested
	for _, s := range suggested {
		if s == "github" {
			t.Error("github should not be suggested when already enabled")
		}
	}
}

func assertContains(t *testing.T, suggestions []string, want string, msg string) {
	t.Helper()
	if !slices.Contains(suggestions, want) {
		t.Errorf("%s; got %v", msg, suggestions)
	}
}
