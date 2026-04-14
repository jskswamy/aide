package capability

import (
	"testing"
)

func TestResolveOne_Builtin(t *testing.T) {
	registry := map[string]Capability{
		"k8s": {
			Name:        "k8s",
			Description: "Kubernetes cluster access",
			Readable:    []string{"~/.kube"},
			EnvAllow:    []string{"KUBECONFIG"},
		},
	}

	resolved, err := ResolveOne("k8s", registry)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved.Name != "k8s" {
		t.Errorf("expected name k8s, got %s", resolved.Name)
	}
	if len(resolved.Readable) != 1 || resolved.Readable[0] != "~/.kube" {
		t.Errorf("expected readable [~/.kube], got %v", resolved.Readable)
	}
}

func TestResolveOne_Extends(t *testing.T) {
	registry := map[string]Capability{
		"k8s": {
			Name:     "k8s",
			Readable: []string{"~/.kube"},
			EnvAllow: []string{"KUBECONFIG"},
		},
		"k8s-dev": {
			Name:     "k8s-dev",
			Extends:  "k8s",
			Readable: []string{"~/.kube/dev-config"},
			Deny:     []string{"~/.kube/prod-config"},
		},
	}

	resolved, err := ResolveOne("k8s-dev", registry)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved.Name != "k8s-dev" {
		t.Errorf("expected name k8s-dev, got %s", resolved.Name)
	}
	// Should merge readable (parent + child)
	if len(resolved.Readable) != 2 {
		t.Errorf("expected 2 readable paths, got %d: %v", len(resolved.Readable), resolved.Readable)
	}
	// Should have child's deny
	if len(resolved.Deny) != 1 || resolved.Deny[0] != "~/.kube/prod-config" {
		t.Errorf("expected deny [~/.kube/prod-config], got %v", resolved.Deny)
	}
	// Should have parent's env_allow
	if len(resolved.EnvAllow) != 1 || resolved.EnvAllow[0] != "KUBECONFIG" {
		t.Errorf("expected env_allow [KUBECONFIG], got %v", resolved.EnvAllow)
	}
}

func TestResolveOne_Combines(t *testing.T) {
	registry := map[string]Capability{
		"aws":    {Name: "aws", EnvAllow: []string{"AWS_PROFILE"}},
		"k8s":    {Name: "k8s", EnvAllow: []string{"KUBECONFIG"}},
		"docker": {Name: "docker"},
		"my-deploy": {
			Name:     "my-deploy",
			Combines: []string{"aws", "k8s", "docker"},
			Deny:     []string{"~/.kube/prod-config"},
		},
	}

	resolved, err := ResolveOne("my-deploy", registry)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolved.EnvAllow) != 2 {
		t.Errorf("expected 2 env_allow entries, got %d: %v", len(resolved.EnvAllow), resolved.EnvAllow)
	}
	if len(resolved.Deny) != 1 {
		t.Errorf("expected 1 deny entry, got %d: %v", len(resolved.Deny), resolved.Deny)
	}
}

func TestResolveOne_CircularReference(t *testing.T) {
	registry := map[string]Capability{
		"a": {Name: "a", Extends: "b"},
		"b": {Name: "b", Extends: "a"},
	}
	_, err := ResolveOne("a", registry)
	if err == nil {
		t.Fatal("expected circular reference error")
	}
}

func TestResolveOne_MutualExclusion(t *testing.T) {
	registry := map[string]Capability{
		"bad": {Name: "bad", Extends: "k8s", Combines: []string{"aws"}},
		"k8s": {Name: "k8s"},
		"aws": {Name: "aws"},
	}
	_, err := ResolveOne("bad", registry)
	if err == nil {
		t.Fatal("expected mutual exclusion error")
	}
}

func TestResolveOne_UnknownCapability(t *testing.T) {
	registry := map[string]Capability{}
	_, err := ResolveOne("nonexistent", registry)
	if err == nil {
		t.Fatal("expected unknown capability error")
	}
}

func TestResolveAll(t *testing.T) {
	registry := map[string]Capability{
		"aws":    {Name: "aws", Readable: []string{"~/.aws"}, EnvAllow: []string{"AWS_PROFILE"}},
		"k8s":    {Name: "k8s", Readable: []string{"~/.kube"}, EnvAllow: []string{"KUBECONFIG"}},
		"docker": {Name: "docker", Readable: []string{"~/.docker"}},
	}

	set, err := ResolveAll([]string{"aws", "k8s", "docker"}, registry, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(set.Capabilities) != 3 {
		t.Errorf("expected 3 capabilities, got %d", len(set.Capabilities))
	}

	overrides := set.ToSandboxOverrides()
	if len(overrides.ReadableExtra) != 3 {
		t.Errorf("expected 3 readable, got %d: %v", len(overrides.ReadableExtra), overrides.ReadableExtra)
	}
	if len(overrides.EnvAllow) != 2 {
		t.Errorf("expected 2 env_allow, got %d: %v", len(overrides.EnvAllow), overrides.EnvAllow)
	}
}

func TestResolveAll_NeverAllow(t *testing.T) {
	registry := map[string]Capability{
		"k8s": {Name: "k8s", Readable: []string{"~/.kube"}},
	}
	neverAllow := []string{"~/.kube/prod-config"}

	set, err := ResolveAll([]string{"k8s"}, registry, neverAllow, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	overrides := set.ToSandboxOverrides()
	if len(overrides.DeniedExtra) != 1 || overrides.DeniedExtra[0] != "~/.kube/prod-config" {
		t.Errorf("expected never_allow in denied, got %v", overrides.DeniedExtra)
	}
}

func TestResolveAll_NeverAllowEnv(t *testing.T) {
	registry := map[string]Capability{
		"aws": {Name: "aws", EnvAllow: []string{"AWS_PROFILE", "AWS_SECRET_ACCESS_KEY"}},
	}
	neverAllowEnv := []string{"AWS_SECRET_ACCESS_KEY"}

	set, err := ResolveAll([]string{"aws"}, registry, nil, neverAllowEnv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	overrides := set.ToSandboxOverrides()
	if len(overrides.EnvAllow) != 1 || overrides.EnvAllow[0] != "AWS_PROFILE" {
		t.Errorf("expected AWS_SECRET_ACCESS_KEY stripped, got %v", overrides.EnvAllow)
	}
}

func TestResolveAll_DuplicateNames(t *testing.T) {
	registry := map[string]Capability{
		"k8s": {Name: "k8s"},
	}
	set, err := ResolveAll([]string{"k8s", "k8s"}, registry, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(set.Capabilities) != 1 {
		t.Errorf("expected dedup to 1 capability, got %d", len(set.Capabilities))
	}
}

func TestResolveOne_EnableGuard(t *testing.T) {
	registry := map[string]Capability{
		"git-remote": {
			Name:        "git-remote",
			EnableGuard: []string{"git-remote"},
			EnvAllow:    []string{"SSH_AUTH_SOCK"},
		},
	}
	resolved, err := ResolveOne("git-remote", registry)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolved.EnableGuard) != 1 || resolved.EnableGuard[0] != "git-remote" {
		t.Errorf("expected EnableGuard [git-remote], got %v", resolved.EnableGuard)
	}
}

func TestResolveOne_EnableGuard_Inherits(t *testing.T) {
	registry := map[string]Capability{
		"base-remote": {Name: "base-remote", EnableGuard: []string{"git-remote"}},
		"my-remote":   {Name: "my-remote", Extends: "base-remote", EnvAllow: []string{"SSH_AUTH_SOCK"}},
	}
	resolved, err := ResolveOne("my-remote", registry)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolved.EnableGuard) != 1 || resolved.EnableGuard[0] != "git-remote" {
		t.Errorf("expected inherited EnableGuard [git-remote], got %v", resolved.EnableGuard)
	}
}

func TestToSandboxOverrides_EnableGuard(t *testing.T) {
	registry := map[string]Capability{
		"git-remote": {Name: "git-remote", EnableGuard: []string{"git-remote"}, EnvAllow: []string{"SSH_AUTH_SOCK"}},
	}
	set, err := ResolveAll([]string{"git-remote"}, registry, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	overrides := set.ToSandboxOverrides()
	if len(overrides.EnableGuard) != 1 || overrides.EnableGuard[0] != "git-remote" {
		t.Errorf("expected EnableGuard [git-remote] in overrides, got %v", overrides.EnableGuard)
	}
}

func TestCapability_NetworkMode_Resolves(t *testing.T) {
	registry := map[string]Capability{
		"network": {
			Name:        "network",
			NetworkMode: "unrestricted",
		},
	}
	resolved, err := ResolveOne("network", registry)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.NetworkMode != "unrestricted" {
		t.Errorf("expected NetworkMode unrestricted, got %q", resolved.NetworkMode)
	}
}

func TestCapability_NetworkMode_ToSandboxOverrides(t *testing.T) {
	set := &Set{
		Capabilities: []ResolvedCapability{
			{Name: "network", NetworkMode: "unrestricted"},
		},
	}
	o := set.ToSandboxOverrides()
	if o.NetworkMode != "unrestricted" {
		t.Errorf("expected NetworkMode unrestricted, got %q", o.NetworkMode)
	}
}

func TestCapability_NetworkMode_EmptyWhenNotSet(t *testing.T) {
	set := &Set{
		Capabilities: []ResolvedCapability{
			{Name: "k8s"},
		},
	}
	o := set.ToSandboxOverrides()
	if o.NetworkMode != "" {
		t.Errorf("expected empty NetworkMode, got %q", o.NetworkMode)
	}
}
