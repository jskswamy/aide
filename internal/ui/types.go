// Package ui provides terminal rendering for aide's startup banner and status output.
package ui

import "time"

// CapabilityDisplay holds per-capability information for banner rendering.
type CapabilityDisplay struct {
	Name      string
	Paths     []string // readable/writable paths granted
	EnvVars   []string // env vars passed through
	Source    string   // "context config", "--with", "--without"
	Disabled  bool     // true if --without excluded this
	Suggested bool     // true if detected but not enabled

	// Variant + provenance (added for AIDE-j6m).
	// Variants: active variant names, e.g. []string{"uv"} or
	// []string{"pnpm", "corepack"}. nil for capabilities that do not
	// declare Variants.
	Variants []string

	// ProvenanceTag is the short human-readable tag shown in Tier 2
	// (clean + boxed): "detected" | "pinned" | "--variant" | "default".
	// Empty string when the capability has no variant selection.
	ProvenanceTag string

	// FreshGrant is true when a consent record for this capability was
	// written in the current launch (Provenance.Reason ==
	// "consent:granted"). Renders as a "🆕" marker.
	FreshGrant bool

	// EvidenceSummary is the marker-evidence string for Tier 3 only,
	// e.g. "uv.lock, [tool.uv] in pyproject.toml". Empty when style is
	// not "boxed" or when no evidence was collected.
	EvidenceSummary string

	// ConfirmedAt is the consent timestamp shown in Tier 3 only.
	// Zero-valued when style is not "boxed" or when no stored grant
	// exists.
	ConfirmedAt time.Time

	// DetectionHint is for suggested-but-not-enabled caps in Tier 2+:
	// a short string describing the marker that fired
	// (e.g., "[remote in .git/config"). Empty when no hint available.
	DetectionHint string
}

// BannerData holds all information needed to render an aide banner.
type BannerData struct {
	ContextName string
	MatchReason string
	AgentName   string
	AgentPath   string
	SecretName  string
	SecretKeys  []string          // nil = normal (show count), populated = detailed (list names)
	Env         map[string]string // key → annotation (e.g. "← secrets.api_key" or "= literal")
	EnvResolved map[string]string // key → redacted value, nil in normal mode
	Sandbox       *SandboxInfo
	Yolo          bool
	Warnings      []string
	Capabilities  []CapabilityDisplay
	DisabledCaps  []CapabilityDisplay // --without caps
	SuggestedCaps []CapabilityDisplay // detected but not enabled
	NeverAllow    []string
	CredWarnings  []string // "AWS_SECRET_ACCESS_KEY (via aws)"
	CompWarnings  []string // composition warnings
	AutoApprove   bool     // replaces Yolo for new banner display
	// Extra sandbox paths from config (not from capabilities)
	ExtraWritable []string
	ExtraReadable []string
	ExtraDenied   []string
}

// SandboxInfo describes sandbox configuration for display.
type SandboxInfo struct {
	Disabled  bool
	Network   string           // "outbound only", "unrestricted", "none"
	Ports     string           // "all" or "443, 53"
	Active    []GuardDisplay
	Skipped   []GuardDisplay
	Available []string // opt-in guard names not enabled
}

// GuardDisplay holds per-guard information for banner rendering.
type GuardDisplay struct {
	Name      string
	Protected []string
	Allowed   []string
	Overrides []GuardOverride
	Reason    string // for skipped: "~/.kube not found"
}

// GuardOverride records an env var override for display.
type GuardOverride struct {
	EnvVar      string
	Value       string
	DefaultPath string
}
