package capability

// builtins holds all built-in capability definitions.
var builtins map[string]Capability

func init() {
	builtins = map[string]Capability{
		// Cloud providers
		"aws": {
			Name:        "aws",
			Description: "AWS CLI and credentials",
			Unguard:     []string{"cloud-aws"},
			Readable:    []string{"~/.aws"},
			EnvAllow: []string{
				"AWS_PROFILE", "AWS_REGION", "AWS_DEFAULT_REGION",
				"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN",
				"AWS_CONFIG_FILE", "AWS_SHARED_CREDENTIALS_FILE",
			},
		},
		"gcp": {
			Name:        "gcp",
			Description: "Google Cloud CLI and credentials",
			Unguard:     []string{"cloud-gcp"},
			Readable:    []string{"~/.config/gcloud"},
			EnvAllow:    []string{"CLOUDSDK_CONFIG", "GOOGLE_APPLICATION_CREDENTIALS", "GCLOUD_PROJECT"},
		},
		"azure": {
			Name:        "azure",
			Description: "Azure CLI and credentials",
			Unguard:     []string{"cloud-azure"},
			Readable:    []string{"~/.azure"},
			EnvAllow:    []string{"AZURE_CONFIG_DIR", "AZURE_SUBSCRIPTION_ID"},
		},
		"digitalocean": {
			Name:        "digitalocean",
			Description: "DigitalOcean CLI credentials",
			Unguard:     []string{"cloud-digitalocean"},
			Readable:    []string{"~/.config/doctl"},
			EnvAllow:    []string{"DIGITALOCEAN_ACCESS_TOKEN"},
		},
		"oci": {
			Name:        "oci",
			Description: "Oracle Cloud CLI credentials",
			Unguard:     []string{"cloud-oci"},
			Readable:    []string{"~/.oci"},
			EnvAllow:    []string{"OCI_CLI_CONFIG_FILE"},
		},

		// Containers
		"docker": {
			Name:        "docker",
			Description: "Docker daemon and registry credentials",
			Unguard:     []string{"docker"},
			Readable:    []string{"~/.docker"},
			EnvAllow:    []string{"DOCKER_CONFIG", "DOCKER_HOST"},
		},

		// Orchestration
		"k8s": {
			Name:        "k8s",
			Description: "Kubernetes cluster access",
			Unguard:     []string{"kubernetes"},
			Readable:    []string{"~/.kube"},
			EnvAllow:    []string{"KUBECONFIG"},
		},
		"helm": {
			Name:        "helm",
			Description: "Helm charts and releases",
			Unguard:     []string{"kubernetes"},
			Readable:    []string{"~/.kube", "~/.config/helm", "~/.cache/helm"},
			EnvAllow:    []string{"HELM_HOME", "KUBECONFIG"},
		},

		// Infrastructure as Code
		"terraform": {
			Name:        "terraform",
			Description: "Terraform state and providers",
			Unguard:     []string{"terraform"},
			Readable:    []string{"~/.terraform.d"},
			EnvAllow:    []string{"TF_CLI_CONFIG_FILE"},
		},
		"vault": {
			Name:        "vault",
			Description: "HashiCorp Vault access",
			Unguard:     []string{"vault"},
			Readable:    []string{"~/.vault-token"},
			EnvAllow:    []string{"VAULT_ADDR", "VAULT_TOKEN", "VAULT_TOKEN_FILE"},
		},

		// SSH
		"ssh": {
			Name:        "ssh",
			Description: "SSH keys and agent",
			Unguard:     []string{"ssh-keys"},
			Readable:    []string{"~/.ssh"},
			EnvAllow:    []string{"SSH_AUTH_SOCK"},
		},

		// Package registries
		"npm": {
			Name:        "npm",
			Description: "npm and yarn registry credentials",
			Unguard:     []string{"npm", "netrc"},
			Readable:    []string{"~/.npmrc", "~/.yarnrc"},
			EnvAllow:    []string{"NPM_TOKEN", "NODE_AUTH_TOKEN"},
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
