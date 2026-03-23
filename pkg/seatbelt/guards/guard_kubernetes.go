// Kubernetes guard for macOS Seatbelt profiles.
//
// Protects kubeconfig files from credential leakage.

package guards

import "github.com/jskswamy/aide/pkg/seatbelt"

type kubernetesGuard struct{}

// KubernetesGuard returns a Guard that denies access to Kubernetes config files.
func KubernetesGuard() seatbelt.Guard { return &kubernetesGuard{} }

func (g *kubernetesGuard) Name() string        { return "kubernetes" }
func (g *kubernetesGuard) Type() string        { return "default" }
func (g *kubernetesGuard) Description() string { return "Kubernetes kubeconfig files" }

func (g *kubernetesGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	var rules []seatbelt.Rule
	rules = append(rules, seatbelt.Section("Kubernetes credentials"))

	// KUBECONFIG is colon-separated; each path is denied individually
	if kubeconfig, ok := ctx.EnvLookup("KUBECONFIG"); ok && kubeconfig != "" {
		for _, p := range splitColonPaths(kubeconfig) {
			if p != "" {
				rules = append(rules, denyLiteralRuleForPath(p)...)
			}
		}
		return rules
	}

	rules = append(rules, denyLiteralRuleForPath(ctx.HomePath(".kube/config"))...)
	return rules
}
