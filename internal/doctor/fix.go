package doctor

import (
	"fmt"
	"strings"
)

// SuggestedCapability reports a capability whose project marker is
// present but which the profile does not enable. Enabling it widens
// access. aide cap enable writes the local .aide.yaml, so it is offered
// as a command only when that file exists and is trusted: aide re-trusts
// a file it writes only if it was trusted before, and an untrusted
// .aide.yaml would not apply. Otherwise the steps include aide trust.
func SuggestedCapability(name, marker string, opens []string, profile string, local *ProjectFile) Finding {
	f := Finding{
		ID: "capability.suggested", Subject: name, Profile: profile, Severity: SeverityTip, WidensAccess: true,
		Message: fmt.Sprintf("%s found, but the %s capability is not enabled", marker, name),
	}
	if local != nil && local.State == "trusted" {
		f.Layer, f.Source = "local", local.Path
		f.Fix.Argv = []string{"aide", "cap", "enable", name}
		f.Fix.SideEffects = []string{"writes " + local.Path, "re-approves " + local.Path + " after writing it"}
		if len(opens) > 0 {
			f.Fix.SideEffects = append(f.Fix.SideEffects, "opens "+strings.Join(opens, ", "))
		}
		return f
	}
	f.Fix.Steps = []string{"Run: aide cap enable " + name, "Review the .aide.yaml it writes", "Run: aide trust"}
	return f
}
