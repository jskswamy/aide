package capability

import "github.com/jskswamy/aide/internal/sliceutil"

// builtins holds all built-in capability definitions.
var builtins map[string]Capability

func init() {
	builtins = map[string]Capability{
		// Cloud providers
		"aws": {
			Name:        "aws",
			Description: "AWS CLI and credentials",
			Markers: []Marker{
				{Contains: ContainsSpec{File: "go.mod", Pattern: "aws-sdk-go"}},
				{Contains: ContainsSpec{File: "requirements.txt", Pattern: "boto3"}},
				{Contains: ContainsSpec{File: "package.json", Pattern: "aws-sdk"}},
			},
			Writable: []string{"~/.aws"},
			EnvAllow: []string{
				"AWS_PROFILE", "AWS_REGION", "AWS_DEFAULT_REGION",
				"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN",
				"AWS_CONFIG_FILE", "AWS_SHARED_CREDENTIALS_FILE",
			},
		},
		"gcp": {
			Name:        "gcp",
			Description: "Google Cloud CLI and credentials",
			Markers: []Marker{
				{Contains: ContainsSpec{File: "go.mod", Pattern: "cloud.google.com"}},
				{Contains: ContainsSpec{File: "requirements.txt", Pattern: "google-cloud"}},
				{Contains: ContainsSpec{File: "package.json", Pattern: "@google-cloud"}},
			},
			Writable: []string{"~/.config/gcloud"},
			EnvAllow:    []string{"CLOUDSDK_CONFIG", "GOOGLE_APPLICATION_CREDENTIALS", "GCLOUD_PROJECT"},
		},
		"azure": {
			Name:        "azure",
			Description: "Azure CLI and credentials",
			Writable: []string{"~/.azure"},
			EnvAllow:    []string{"AZURE_CONFIG_DIR", "AZURE_SUBSCRIPTION_ID"},
		},
		"digitalocean": {
			Name:        "digitalocean",
			Description: "DigitalOcean CLI credentials",
			Writable: []string{"~/.config/doctl"},
			EnvAllow:    []string{"DIGITALOCEAN_ACCESS_TOKEN"},
		},
		"oci": {
			Name:        "oci",
			Description: "Oracle Cloud CLI credentials",
			Writable: []string{"~/.oci"},
			EnvAllow:    []string{"OCI_CLI_CONFIG_FILE"},
		},

		// Containers
		"docker": {
			Name:        "docker",
			Description: "Docker daemon and registry credentials",
			Markers: []Marker{
				{File: "Dockerfile"},
				{File: "docker-compose.yaml"},
				{File: "docker-compose.yml"},
			},
			Writable: []string{"~/.docker"},
			EnvAllow:    []string{"DOCKER_CONFIG", "DOCKER_HOST"},
		},

		// Orchestration
		"k8s": {
			Name:        "k8s",
			Description: "Kubernetes cluster access",
			Markers: []Marker{
				{DirExists: "k8s"},
				{DirExists: "kubernetes"},
				{DirExists: "manifests"},
				{GlobContains: GlobContainsSpec{Glob: "*.yaml", Pattern: "apiVersion:"}},
				{GlobContains: GlobContainsSpec{Glob: "*.yml", Pattern: "apiVersion:"}},
				{GlobContains: GlobContainsSpec{Glob: "*/*.yaml", Pattern: "apiVersion:"}},
				{GlobContains: GlobContainsSpec{Glob: "*/*.yml", Pattern: "apiVersion:"}},
			},
			Writable: []string{"~/.kube"},
			EnvAllow:    []string{"KUBECONFIG"},
		},
		"helm": {
			Name:        "helm",
			Description: "Helm charts and releases",
			Markers: []Marker{
				{File: "Chart.yaml"},
				{File: "helmfile.yaml"},
			},
			Writable: []string{"~/.kube", "~/.config/helm", "~/.cache/helm"},
			EnvAllow:    []string{"HELM_HOME", "KUBECONFIG"},
		},

		// Infrastructure as Code
		"terraform": {
			Name:        "terraform",
			Description: "Terraform state and providers",
			Markers: []Marker{
				{GlobPath: "*.tf"},
				{GlobPath: "*/*.tf"},
			},
			Writable: []string{"~/.terraform.d"},
			EnvAllow:    []string{"TF_CLI_CONFIG_FILE"},
		},
		"vault": {
			Name:        "vault",
			Description: "HashiCorp Vault access",
			Markers:     []Marker{{File: ".vault-token"}},
			Writable:    []string{"~/.vault-token"},
			EnvAllow:    []string{"VAULT_ADDR", "VAULT_TOKEN", "VAULT_TOKEN_FILE"},
		},

		// SSH — explicit opt-in. Required for git over SSH, ssh login, scp/rsync.
		"ssh": {
			Name:        "ssh",
			Description: "SSH keys, agent, and outbound SSH transport (port 22 + custom). Required for: git over SSH, ssh login, scp/rsync.",
			Readable:    []string{"~/.ssh"},
			EnableGuard: []string{"ssh"},
			EnvAllow:    []string{"SSH_AUTH_SOCK"},
		},

		// Package registries
		"npm": {
			Name:        "npm",
			Description: "npm and yarn registry credentials",
			Markers:     []Marker{{File: "package.json"}},
			Writable:    []string{"~/.npmrc", "~/.yarnrc"},
			EnvAllow:    []string{"NPM_TOKEN", "NODE_AUTH_TOKEN"},
		},

		// Language runtimes
		"go": {
			Name:        "go",
			Description: "Go toolchain",
			Markers: []Marker{
				{File: "go.mod"},
				{File: "go.sum"},
			},
			Writable:    []string{"~/go"},
			EnvAllow:    []string{"GOPATH", "GOROOT", "GOBIN"},
		},
		"rust": {
			Name:        "rust",
			Description: "Rust toolchain",
			Markers:     []Marker{{File: "Cargo.toml"}},
			Writable:    []string{"~/.cargo", "~/.rustup"},
			EnvAllow:    []string{"CARGO_HOME", "RUSTUP_HOME"},
		},
		"python": {
			Name:        "python",
			Description: "Python toolchain",
			Markers: []Marker{
				{File: "pyproject.toml"},
				{File: "requirements.txt"},
				{File: "Pipfile"},
				{File: "setup.py"},
			},
			Variants: []Variant{
				{
					Name:        "uv",
					Description: "uv — fast Python package/project manager",
					Markers: []Marker{
						{File: "uv.lock"},
					},
					Writable: []string{
						"~/.local/share/uv",
						"~/.cache/uv",
					},
					EnvAllow: []string{"UV_CACHE_DIR", "UV_PYTHON_INSTALL_DIR", "VIRTUAL_ENV"},
				},
				{
					Name:        "pyenv",
					Description: "pyenv — Simple Python version management",
					Markers: []Marker{
						{File: ".python-version"},
					},
					Writable: []string{"~/.pyenv"},
					EnvAllow: []string{"PYENV_ROOT", "VIRTUAL_ENV"},
				},
				{
					Name:        "conda",
					Description: "Conda / Mamba — scientific Python",
					Markers: []Marker{
						{File: "environment.yml"},
					},
					Writable: []string{"~/.conda", "~/miniconda3", "~/anaconda3"},
					EnvAllow: []string{"CONDA_PREFIX", "CONDA_DEFAULT_ENV"},
				},
				{
					Name:        "poetry",
					Description: "Poetry — dependency management and packaging",
					Markers: []Marker{
						{File: "poetry.lock"},
					},
					Writable: []string{"~/.cache/pypoetry", "~/Library/Caches/pypoetry"},
					EnvAllow: []string{"POETRY_HOME"},
				},
				{
					Name:        "venv",
					Description: "Standard library venv — no managed interpreter",
					// No markers → never auto-selected; used as safe default.
					EnvAllow: []string{"VIRTUAL_ENV"},
				},
			},
			DefaultVariants: []string{"venv"},
		},
		"ruby": {
			Name:        "ruby",
			Description: "Ruby toolchain",
			Markers: []Marker{
				{File: "Gemfile"},
				{GlobPath: "*.gemspec"},
			},
			Writable:    []string{"~/.rbenv"},
			EnvAllow:    []string{"RBENV_ROOT", "GEM_HOME"},
		},
		"java": {
			Name:        "java",
			Description: "Java/JVM toolchain",
			Markers: []Marker{
				{File: "pom.xml"},
				{File: "build.gradle"},
				{File: "build.gradle.kts"},
			},
			Writable:    []string{"~/.sdkman", "~/.gradle", "~/.m2"},
			EnvAllow:    []string{"JAVA_HOME", "SDKMAN_DIR"},
		},

		// Dev tools
		"github": {
			Name:        "github",
			Description: "GitHub CLI and credentials",
			Markers:     []Marker{{DirExists: ".github/workflows"}},
			Writable:    []string{"~/.config/gh"},
			EnvAllow:    []string{"GITHUB_TOKEN", "GH_TOKEN"},
		},
		"git-remote": {
			Name:        "git-remote",
			Description: "Git remote operations over HTTPS (port 443). For SSH-based remotes, also enable the 'ssh' capability.",
			Markers: []Marker{
				{Contains: ContainsSpec{File: ".git/config", Pattern: "[remote "}},
			},
			EnableGuard: []string{"git-remote"},
		},
		"gpg": {
			Name:        "gpg",
			Description: "GPG keys and signing",
			Writable:    []string{"~/.gnupg"},
			EnvAllow:    []string{"GNUPGHOME"},
		},

		// Network
		"network": {
			Name:        "network",
			Description: "Unrestricted network access (inbound and outbound)",
			NetworkMode: "unrestricted",
		},
	}
}

// Builtins returns a copy of the built-in capability registry.
func Builtins() map[string]Capability {
	out := make(map[string]Capability, len(builtins))
	for k, v := range builtins {
		out[k] = v
	}
	return out
}

// MergedRegistry returns a registry combining built-ins with user-defined
// capabilities. When a user-defined capability has the same name as a
// built-in, its non-empty fields are layered on top of the built-in (slice
// fields concatenate-and-dedup, scalar fields are taken from the user-def
// only when non-zero). New user-defined names are added as-is. This lets a
// user add (e.g.) capabilities.ssh.ports without losing the built-in's
// description, EnableGuard, or EnvAllow.
func MergedRegistry(userDefined map[string]Capability) map[string]Capability {
	merged := Builtins()
	for name, user := range userDefined {
		if base, ok := merged[name]; ok {
			merged[name] = mergeCapability(base, user)
		} else {
			merged[name] = user
		}
	}
	return merged
}

func mergeCapability(base, user Capability) Capability {
	out := base
	if user.Description != "" {
		out.Description = user.Description
	}
	if user.Extends != "" {
		out.Extends = user.Extends
	}
	if user.NetworkMode != "" {
		out.NetworkMode = user.NetworkMode
	}
	out.Combines = sliceutil.Dedup(append(base.Combines, user.Combines...))
	out.Unguard = sliceutil.Dedup(append(base.Unguard, user.Unguard...))
	out.Readable = sliceutil.Dedup(append(base.Readable, user.Readable...))
	out.Writable = sliceutil.Dedup(append(base.Writable, user.Writable...))
	out.Deny = sliceutil.Dedup(append(base.Deny, user.Deny...))
	out.EnvAllow = sliceutil.Dedup(append(base.EnvAllow, user.EnvAllow...))
	out.EnableGuard = sliceutil.Dedup(append(base.EnableGuard, user.EnableGuard...))
	out.Allow = sliceutil.Dedup(append(base.Allow, user.Allow...))
	out.Ports = sliceutil.Dedup(append(base.Ports, user.Ports...))
	return out
}
