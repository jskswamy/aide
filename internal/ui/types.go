// Package ui provides terminal rendering for aide's startup banner and status output.
package ui

import (
	"time"

	"github.com/jskswamy/aide/internal/sandbox"
)

// CapabilityDisplay holds per-capability information for banner rendering.
type CapabilityDisplay struct {
	Name          string   `json:"name"`
	WritablePaths []string `json:"writable_paths,omitempty"` // always shown in full (security-critical)
	ReadablePaths []string `json:"readable_paths,omitempty"` // may be truncated (informational, lower risk)
	EnvVars       []string `json:"env_vars,omitempty"`       // env vars passed through
	Source        string   `json:"source,omitempty"`         // "context config", "--with", "--without"
	Disabled      bool     `json:"disabled,omitempty"`       // true if --without excluded this
	Suggested     bool     `json:"suggested,omitempty"`      // true if detected but not enabled

	// Variant + provenance (added for AIDE-j6m).
	// Variants: active variant names, e.g. []string{"uv"} or
	// []string{"pnpm", "corepack"}. nil for capabilities that do not
	// declare Variants.
	Variants []string `json:"variants,omitempty"`

	// ProvenanceTag is the short human-readable tag shown in Tier 2
	// (clean + boxed): "detected" | "pinned" | "--variant" | "default".
	// Empty string when the capability has no variant selection.
	ProvenanceTag string `json:"provenance_tag,omitempty"`

	// FreshGrant is true when a consent record for this capability was
	// written in the current launch (Provenance.Reason ==
	// "consent:granted"). Renders as a "🆕" marker.
	FreshGrant bool `json:"fresh_grant,omitempty"`

	// EvidenceSummary is the marker-evidence string for Tier 3 only,
	// e.g. "uv.lock, [tool.uv] in pyproject.toml". Empty when style is
	// not "boxed" or when no evidence was collected.
	EvidenceSummary string `json:"evidence_summary,omitempty"`

	// ConfirmedAt is the consent timestamp shown in Tier 3 only.
	// Zero-valued when style is not "boxed" or when no stored grant
	// exists.
	ConfirmedAt time.Time `json:"confirmed_at,omitempty"`

	// DetectionHint is for suggested-but-not-enabled caps in Tier 2+:
	// a short string describing the marker that fired
	// (e.g., "[remote in .git/config"). Empty when no hint available.
	DetectionHint string `json:"detection_hint,omitempty"`
}

// EnvItem represents a single env var entry in the banner env section.
type EnvItem struct {
	Key           string `json:"key"`                      // env var name
	Badge         string `json:"badge,omitempty"`          // "🔐", "📌", "🔧", "📁", "⚙", "📐", "⊘"
	Annotation    string `json:"annotation,omitempty"`     // "← secrets.api_key", "= claude-sonnet-4-6", "← aws", "never-allow"
	ResolvedValue string `json:"resolved_value,omitempty"` // redacted resolved value in detailed mode; empty in normal mode
	CredWarning   bool   `json:"cred_warning,omitempty"`   // true when this var is a known credential bearer
	Blocked       bool   `json:"blocked,omitempty"`        // true when blocked by never_allow_env
}

// TrustInfo carries trust display data populated when .aide.yaml is untrusted or denied.
type TrustInfo struct {
	Status string     `json:"status"` // "untrusted" | "denied"
	Path   string     `json:"path"`   // display path of .aide.yaml (tilde-contracted)
	Wants  TrustWants `json:"wants"`  // what the untrusted config is requesting
}

// TrustWants describes what an untrusted .aide.yaml is requesting.
// All fields are shown in full — no truncation for trust decisions.
type TrustWants struct {
	Agent        string   `json:"agent,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	Writable     []string `json:"writable,omitempty"`
	Unguard      []string `json:"unguard,omitempty"`
	EnvVars      []string `json:"env_vars,omitempty"` // var names (not a count — names are not sensitive)
}

// BannerData holds all information needed to render an aide banner.
type BannerData struct {
	ContextName   string              `json:"context_name"`
	MatchReason   string              `json:"match_reason,omitempty"`
	AgentName     string              `json:"agent_name"`
	AgentPath     string              `json:"agent_path,omitempty"`
	SecretName    string              `json:"secret_name,omitempty"`
	SecretKeys    []string            `json:"secret_keys,omitempty"`  // nil = normal (show count), populated = detailed (list names)
	ContextIcon   string              `json:"context_icon,omitempty"` // resolved context icon from config; empty if not set
	AgentIcon     string              `json:"agent_icon,omitempty"`   // resolved agent icon (config or built-in default); empty if none
	EnvItems      []EnvItem           `json:"env_items,omitempty"`    // replaces Env + EnvResolved + CredWarnings
	Trust         *TrustInfo          `json:"trust,omitempty"`        // nil when trusted or no project config
	Sandbox       *SandboxInfo        `json:"sandbox,omitempty"`
	Yolo          bool                `json:"yolo,omitempty"`
	Warnings      []string            `json:"warnings,omitempty"`
	Capabilities  []CapabilityDisplay `json:"capabilities,omitempty"`
	DisabledCaps  []CapabilityDisplay `json:"disabled_caps,omitempty"`  // --without caps
	SuggestedCaps []CapabilityDisplay `json:"suggested_caps,omitempty"` // detected but not enabled
	NeverAllow    []string            `json:"never_allow,omitempty"`
	CompWarnings  []string            `json:"comp_warnings,omitempty"` // composition warnings
	AutoApprove   bool                `json:"auto_approve,omitempty"`  // replaces Yolo for new banner display
	// Extra sandbox paths from config (not from capabilities)
	ExtraWritable []string `json:"extra_writable,omitempty"`
	ExtraReadable []string `json:"extra_readable,omitempty"`
	ExtraDenied   []string `json:"extra_denied,omitempty"`

	// IsolationTier is the OS-level sandbox strength for the current launch.
	// Nil means sandbox: false (user explicitly disabled sandboxing).
	// On macOS always primary; on Linux varies by kernel and policy.
	IsolationTier *sandbox.IsolationTier `json:"isolation_tier,omitempty"`
}

// SandboxInfo describes sandbox configuration for display.
type SandboxInfo struct {
	Disabled  bool           `json:"disabled,omitempty"`
	Network   string         `json:"network,omitempty"` // "outbound only", "unrestricted", "none"
	Ports     string         `json:"ports,omitempty"`   // "all" or "443, 53"
	Active    []GuardDisplay `json:"active,omitempty"`
	Skipped   []GuardDisplay `json:"skipped,omitempty"`
	Available []string       `json:"available,omitempty"` // opt-in guard names not enabled
	Hints     []string       `json:"hints,omitempty"`     // user-facing suggestions from guards
}

// GuardDisplay holds per-guard information for banner rendering.
type GuardDisplay struct {
	Name      string          `json:"name"`
	Protected []string        `json:"protected,omitempty"`
	Allowed   []string        `json:"allowed,omitempty"`
	Readable  []string        `json:"readable,omitempty"`
	Overrides []GuardOverride `json:"overrides,omitempty"`
	Reason    string          `json:"reason,omitempty"` // for skipped: "~/.kube not found"
}

// GuardOverride records an env var override for display.
type GuardOverride struct {
	EnvVar      string `json:"env_var"`
	Value       string `json:"value,omitempty"`
	DefaultPath string `json:"default_path,omitempty"`
}
