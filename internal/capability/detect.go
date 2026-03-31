package capability

import (
	"io"
	"os"
	"path/filepath"
	"strings"
)

// maxReadSize is the maximum number of bytes to read when checking file contents.
const maxReadSize = 64 * 1024 // 64KB

// DetectProject scans the project root for tool markers and returns
// suggested capability names. Does not auto-enable — suggestions only.
//
// Most markers are checked at the top level of projectRoot only.
// For *.tf files and YAML files with apiVersion:, scanning goes one level deep.
func DetectProject(projectRoot string) []string {
	var suggestions []string

	// Docker
	if fileExists(filepath.Join(projectRoot, "Dockerfile")) ||
		fileExists(filepath.Join(projectRoot, "docker-compose.yaml")) ||
		fileExists(filepath.Join(projectRoot, "docker-compose.yml")) {
		suggestions = append(suggestions, "docker")
	}

	// Terraform — scan top level and one level deep
	if hasFileWithExtension(projectRoot, ".tf") || hasFileWithExtensionOneLevelDeep(projectRoot, ".tf") {
		suggestions = append(suggestions, "terraform")
	}

	// Go
	if fileExists(filepath.Join(projectRoot, "go.mod")) ||
		fileExists(filepath.Join(projectRoot, "go.sum")) {
		suggestions = append(suggestions, "go")
	}

	// Rust
	if fileExists(filepath.Join(projectRoot, "Cargo.toml")) {
		suggestions = append(suggestions, "rust")
	}

	// Python
	if fileExists(filepath.Join(projectRoot, "pyproject.toml")) ||
		fileExists(filepath.Join(projectRoot, "requirements.txt")) ||
		fileExists(filepath.Join(projectRoot, "Pipfile")) ||
		fileExists(filepath.Join(projectRoot, "setup.py")) {
		suggestions = append(suggestions, "python")
	}

	// Ruby
	if fileExists(filepath.Join(projectRoot, "Gemfile")) ||
		hasFileWithExtension(projectRoot, ".gemspec") {
		suggestions = append(suggestions, "ruby")
	}

	// Java/JVM
	if fileExists(filepath.Join(projectRoot, "pom.xml")) ||
		fileExists(filepath.Join(projectRoot, "build.gradle")) ||
		fileExists(filepath.Join(projectRoot, "build.gradle.kts")) {
		suggestions = append(suggestions, "java")
	}

	// Kubernetes
	if dirExists(filepath.Join(projectRoot, "k8s")) ||
		dirExists(filepath.Join(projectRoot, "kubernetes")) ||
		dirExists(filepath.Join(projectRoot, "manifests")) ||
		hasYAMLWithAPIVersion(projectRoot) {
		suggestions = append(suggestions, "k8s")
	}

	// GitHub
	if dirExists(filepath.Join(projectRoot, ".github", "workflows")) {
		suggestions = append(suggestions, "github")
	}

	// Helm
	if fileExists(filepath.Join(projectRoot, "Chart.yaml")) ||
		fileExists(filepath.Join(projectRoot, "helmfile.yaml")) {
		suggestions = append(suggestions, "helm")
	}

	// AWS SDK detection
	if containsInFile(projectRoot, "go.mod", "aws-sdk-go") ||
		containsInFile(projectRoot, "requirements.txt", "boto3") ||
		containsInFile(projectRoot, "package.json", "aws-sdk") {
		suggestions = append(suggestions, "aws")
	}

	// GCP SDK detection
	if containsInFile(projectRoot, "go.mod", "cloud.google.com") ||
		containsInFile(projectRoot, "requirements.txt", "google-cloud") ||
		containsInFile(projectRoot, "package.json", "@google-cloud") {
		suggestions = append(suggestions, "gcp")
	}

	// npm
	if fileExists(filepath.Join(projectRoot, "package.json")) {
		suggestions = append(suggestions, "npm")
	}

	// Vault
	if fileExists(filepath.Join(projectRoot, ".vault-token")) {
		suggestions = append(suggestions, "vault")
	}

	// Git remote operations
	gitConfigPath := filepath.Join(projectRoot, ".git", "config")
	if containsInFileByPath(gitConfigPath, "[remote ") {
		suggestions = append(suggestions, "git-remote")
	}

	// Xcode — .xcodeproj, .xcworkspace, or Package.swift with platform targets
	if hasDirWithExtension(projectRoot, ".xcodeproj") ||
		hasDirWithExtension(projectRoot, ".xcworkspace") ||
		containsInFile(projectRoot, "Package.swift", ".iOS") ||
		containsInFile(projectRoot, "Package.swift", ".macOS") ||
		containsInFile(projectRoot, "Package.swift", ".watchOS") ||
		containsInFile(projectRoot, "Package.swift", ".tvOS") ||
		containsInFile(projectRoot, "Package.swift", ".visionOS") {
		suggestions = append(suggestions, "xcode")
	}

	return suggestions
}

// fileExists returns true if path exists and is a regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// dirExists returns true if path exists and is a directory.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// hasFileWithExtension returns true if any file in dir has the given extension.
// Only checks the top level of dir (not recursive).
func hasFileWithExtension(dir string, ext string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ext {
			return true
		}
	}
	return false
}

// hasFileWithExtensionOneLevelDeep returns true if any file one level deep
// in dir has the given extension (i.e., dir/subdir/*.ext).
func hasFileWithExtensionOneLevelDeep(dir string, ext string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if hasFileWithExtension(filepath.Join(dir, e.Name()), ext) {
			return true
		}
	}
	return false
}

// containsInFile reads the file at dir/filename (up to maxReadSize bytes)
// and returns true if it contains the given substring.
func containsInFile(dir string, filename string, substring string) bool {
	path := filepath.Join(dir, filename)
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()

	buf := make([]byte, maxReadSize)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return false
	}
	return strings.Contains(string(buf[:n]), substring)
}

// hasYAMLWithAPIVersion checks for YAML files containing "apiVersion:" at the
// top level and one level deep within projectRoot.
func hasYAMLWithAPIVersion(projectRoot string) bool {
	// Check top level
	if checkYAMLsForAPIVersion(projectRoot) {
		return true
	}

	// Check one level deep
	entries, err := os.ReadDir(projectRoot)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			if checkYAMLsForAPIVersion(filepath.Join(projectRoot, e.Name())) {
				return true
			}
		}
	}
	return false
}

// checkYAMLsForAPIVersion checks if any .yaml or .yml file in dir contains "apiVersion:".
func checkYAMLsForAPIVersion(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := filepath.Ext(e.Name())
		if ext == ".yaml" || ext == ".yml" {
			path := filepath.Join(dir, e.Name())
			if containsInFileByPath(path, "apiVersion:") {
				return true
			}
		}
	}
	return false
}

// hasDirWithExtension returns true if any directory entry in dir has
// the given extension (e.g., ".xcodeproj", ".xcworkspace").
func hasDirWithExtension(dir string, ext string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() && filepath.Ext(e.Name()) == ext {
			return true
		}
	}
	return false
}

// containsInFileByPath reads a file (up to maxReadSize bytes) and returns
// true if it contains the given substring.
func containsInFileByPath(path string, substring string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()

	buf := make([]byte, maxReadSize)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return false
	}
	return strings.Contains(string(buf[:n]), substring)
}
