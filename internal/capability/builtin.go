package capability

// builtins holds all built-in capability definitions.
var builtins map[string]Capability

func init() {
	builtins = map[string]Capability{
		// Cloud providers
		"aws": {
			Name:        "aws",
			Description: "AWS CLI and credentials",
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
			Writable: []string{"~/.docker"},
			EnvAllow:    []string{"DOCKER_CONFIG", "DOCKER_HOST"},
		},

		// Orchestration
		"k8s": {
			Name:        "k8s",
			Description: "Kubernetes cluster access",
			Writable: []string{"~/.kube"},
			EnvAllow:    []string{"KUBECONFIG"},
		},
		"helm": {
			Name:        "helm",
			Description: "Helm charts and releases",
			Writable: []string{"~/.kube", "~/.config/helm", "~/.cache/helm"},
			EnvAllow:    []string{"HELM_HOME", "KUBECONFIG"},
		},

		// Infrastructure as Code
		"terraform": {
			Name:        "terraform",
			Description: "Terraform state and providers",
			Writable: []string{"~/.terraform.d"},
			EnvAllow:    []string{"TF_CLI_CONFIG_FILE"},
		},
		"vault": {
			Name:        "vault",
			Description: "HashiCorp Vault access",
			Writable: []string{"~/.vault-token"},
			EnvAllow:    []string{"VAULT_ADDR", "VAULT_TOKEN", "VAULT_TOKEN_FILE"},
		},

		// SSH
		"ssh": {
			Name:        "ssh",
			Description: "SSH keys and agent",
			Readable: []string{"~/.ssh"},
			EnvAllow:    []string{"SSH_AUTH_SOCK"},
		},

		// Package registries
		"npm": {
			Name:        "npm",
			Description: "npm and yarn registry credentials",
			Writable: []string{"~/.npmrc", "~/.yarnrc"},
			EnvAllow:    []string{"NPM_TOKEN", "NODE_AUTH_TOKEN"},
		},

		// Language runtimes
		"go": {
			Name:        "go",
			Description: "Go toolchain",
			Writable:    []string{"~/go"},
			EnvAllow:    []string{"GOPATH", "GOROOT", "GOBIN"},
		},
		"rust": {
			Name:        "rust",
			Description: "Rust toolchain",
			Writable:    []string{"~/.cargo", "~/.rustup"},
			EnvAllow:    []string{"CARGO_HOME", "RUSTUP_HOME"},
		},
		"python": {
			Name:        "python",
			Description: "Python toolchain",
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
			Writable:    []string{"~/.rbenv"},
			EnvAllow:    []string{"RBENV_ROOT", "GEM_HOME"},
		},
		"java": {
			Name:        "java",
			Description: "Java/JVM toolchain",
			Writable:    []string{"~/.sdkman", "~/.gradle", "~/.m2"},
			EnvAllow:    []string{"JAVA_HOME", "SDKMAN_DIR"},
		},

		// Dev tools
		"github": {
			Name:        "github",
			Description: "GitHub CLI and credentials",
			Writable:    []string{"~/.config/gh"},
			EnvAllow:    []string{"GITHUB_TOKEN", "GH_TOKEN"},
		},
		"git-remote": {
			Name:        "git-remote",
			Description: "Git remote operations (push, fetch, pull) via SSH and HTTPS",
			EnableGuard: []string{"git-remote"},
			EnvAllow:    []string{"SSH_AUTH_SOCK"},
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
// capabilities. User-defined capabilities override built-ins with the same name.
func MergedRegistry(userDefined map[string]Capability) map[string]Capability {
	merged := Builtins()
	for k, v := range userDefined {
		merged[k] = v
	}
	return merged
}
