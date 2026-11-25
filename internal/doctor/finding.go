// Package doctor holds aide's diagnostic findings: typed results of
// checks that validate, which, status and aide doctor share.
package doctor

// Severity grades a finding.
type Severity string

// Severities, worst first.
const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityTip     Severity = "tip"
)

// Finding is one problem or suggestion. ID names the check and Subject
// what it is about (a profile, agent, path); together they identify
// the finding.
type Finding struct {
	ID           string   `json:"id"`
	Subject      string   `json:"subject"`
	Severity     Severity `json:"severity"`
	WidensAccess bool     `json:"widens_access"`
	Layer        string   `json:"layer,omitempty"`
	Source       string   `json:"source,omitempty"`
	Profile      string   `json:"profile,omitempty"`
	Message      string   `json:"message"`
	Fix          Fix      `json:"fix"`
}

// Fix is either an aide command (Argv) or written Steps for the user.
type Fix struct {
	Argv        []string `json:"argv"`
	SideEffects []string `json:"side_effects"`
	Steps       []string `json:"steps"`
}
