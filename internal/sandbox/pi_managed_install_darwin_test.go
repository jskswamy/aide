//go:build darwin && integration

package sandbox

import (
	"os/exec"
	"strings"
	"testing"
)

func piManagedBackends() []string { return []string{BackendSeatbelt} }
func piManagedApply(t *testing.T, cmd *exec.Cmd, policy Policy, _ string) {
	t.Helper()
	probe := exec.Command("/usr/bin/sandbox-exec", "-p", "(version 1)(allow default)", "/usr/bin/true")
	if out, err := probe.CombinedOutput(); err != nil {
		if strings.Contains(string(out), "sandbox_apply: Operation not permitted") {
			t.Skip("host forbids sandbox-exec; Seatbelt enforcement unverified")
		}
		t.Fatalf("Seatbelt preflight: %v %s", err, out)
	}
	if err := NewSandbox().Apply(cmd, policy, policy.RuntimeDir); err != nil {
		t.Fatal(err)
	}
}
