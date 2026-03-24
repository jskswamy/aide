// Kubernetes guard for macOS Seatbelt profiles.
//
// Protects kubeconfig files from credential leakage.

package guards

import (
	"fmt"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

type kubernetesGuard struct{}

// KubernetesGuard returns a Guard that denies access to Kubernetes config files.
func KubernetesGuard() seatbelt.Guard { return &kubernetesGuard{} }

func (g *kubernetesGuard) Name() string        { return "kubernetes" }
func (g *kubernetesGuard) Type() string        { return "default" }
func (g *kubernetesGuard) Description() string { return "Blocks access to Kubernetes config" }

func (g *kubernetesGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	result := seatbelt.GuardResult{}

	// KUBECONFIG is colon-separated; each path is denied individually
	if kubeconfig, ok := ctx.EnvLookup("KUBECONFIG"); ok && kubeconfig != "" {
		paths := SplitColonPaths(kubeconfig)
		var found []string
		for _, p := range paths {
			if pathExists(p) {
				found = append(found, p)
			} else {
				result.Skipped = append(result.Skipped, fmt.Sprintf("%s not found", p))
			}
		}
		if len(found) == 0 {
			return result
		}
		result.Rules = append(result.Rules, seatbelt.SectionDeny("Kubernetes credentials"))
		for _, p := range found {
			result.Rules = append(result.Rules, DenyFile(p)...)
			result.Protected = append(result.Protected, p)
		}
		result.Overrides = append(result.Overrides, seatbelt.Override{
			EnvVar:      "KUBECONFIG",
			Value:       kubeconfig,
			DefaultPath: ctx.HomePath(".kube/config"),
		})
		return result
	}

	defaultPath := ctx.HomePath(".kube/config")
	if !pathExists(defaultPath) {
		result.Skipped = append(result.Skipped, fmt.Sprintf("%s not found", defaultPath))
		return result
	}

	result.Rules = append(result.Rules, seatbelt.SectionDeny("Kubernetes credentials"))
	result.Rules = append(result.Rules, DenyFile(defaultPath)...)
	result.Protected = append(result.Protected, defaultPath)
	return result
}
