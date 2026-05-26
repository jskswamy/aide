// Package ui provides terminal rendering for aide's startup banner and status output.
package ui

import (
	"time"

	"github.com/jskswamy/aide/internal/sandbox"
)

// CapabilityDisplay holds per-capability information for banner rendering.
type CapabilityDisplay struct {
	Name           string
	WritablePaths  []string // always shown in full (security-critical)
	ReadablePaths  []string // may be truncated (informational, lower risk)
	EnvVars        []string // env vars passed through
	Source         string   // "context config", "--with", "--without"
	Disabled       bool     // true if --without excluded this
	Suggested      bool     // true if detected but not enabled

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

// EnvItem represents a single env var entry in the banner env section.
type EnvItem struct {
	Key           string // env var name
	Badge         string // "🔐", "📌", "🔧", "📁", "⚙", "📐", "⊘"
	Annotation    string // "← secrets.api_key", "= claude-sonnet-4-6", "← aws", "never-allow"
	ResolvedValue string // redacted resolved value in detailed mode; empty in normal mode
	CredWarning   bool   // true when this var is a known credential bearer
	Blocked       bool   // true when blocked by never_allow_env
}

// TrustInfo carries trust display data populated when .aide.yaml is untrusted or denied.
type TrustInfo struct {
	Status string     // "untrusted" | "denied"
	Path   string     // display path of .aide.yaml (tilde-contracted)
	Wants  TrustWants // what the untrusted config is requesting
}

// TrustWants describes what an untrusted .aide.yaml is requesting.
// All fields are shown in full — no truncation for trust decisions.
type TrustWants struct {
	Agent        string
	Capabilities []string
	Writable     []string
	Unguard      []string
	EnvVars      []string // var names (not a count — names are not sensitive)
}

// BannerData holds all information needed to render an aide banner.
type BannerData struct {
	ContextName string
	MatchReason string
	AgentName   string
	AgentPath   string
	SecretName  string
	SecretKeys  []string  // nil = normal (show count), populated = detailed (list names)
	ContextIcon string    // resolved context icon from config; empty if not set
	AgentIcon   string    // resolved agent icon (config or built-in default); empty if none
	EnvItems    []EnvItem // replaces Env + EnvResolved + CredWarnings
	Trust       *TrustInfo // nil when trusted or no project config
	Sandbox       *SandboxInfo
	Yolo          bool
	Warnings      []string
	Capabilities  []CapabilityDisplay
	DisabledCaps  []CapabilityDisplay // --without caps
	SuggestedCaps []CapabilityDisplay // detected but not enabled
	NeverAllow    []string
	CompWarnings  []string // composition warnings
	AutoApprove   bool     // replaces Yolo for new banner display
	// Extra sandbox paths from config (not from capabilities)
	ExtraWritable []string
	ExtraReadable []string
	ExtraDenied   []string

	// IsolationTier is the OS-level sandbox strength for the current launch.
	// Nil means sandbox: false (user explicitly disabled sandboxing).
	// On macOS always primary; on Linux varies by kernel and policy.
	IsolationTier *sandbox.IsolationTier
}

// SandboxInfo describes sandbox configuration for display.
type SandboxInfo struct {
	Disabled  bool
	Network   string           // "outbound only", "unrestricted", "none"
	Ports     string           // "all" or "443, 53"
	Active    []GuardDisplay
	Skipped   []GuardDisplay
	Available []string // opt-in guard names not enabled
	Hints     []string // user-facing suggestions from guards
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
