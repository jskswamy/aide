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
	// Source is "inherited" for a linked worktree's main checkout
	// file and "local" for the checkout's own; empty when unknown.
	Source string `json:"source,omitempty"`
	// Target is where Path points when the .aide.yaml is a symlink.
	// Trust is given to Path itself, never to the target.
	Target string `json:"target,omitempty"`
}

// InheritedGrant is a security-sensitive grant a linked worktree
// receives from its main checkout's trusted .aide.yaml.
type InheritedGrant struct {
	Kind   string `json:"kind"`   // "writable", "readable", "network", "unguard", "capability", "sandbox", "auto_approve"
	Value  string `json:"value"`  // path, mode, guard or capability name
	Source string `json:"source"` // display path of the declaring file
}

// TrustWants describes what an untrusted .aide.yaml is requesting.
// All fields are shown in full — no truncation for trust decisions.
type TrustWants struct {
	Agent        string   `json:"agent,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	Writable     []string `json:"writable,omitempty"`
	Unguard      []string `json:"unguard,omitempty"`
	EnvVars      []string `json:"env_vars,omitempty"` // var names (not a count — names are not sensitive)
	Readable     []string `json:"readable,omitempty"`
	Network      string   `json:"network,omitempty"`
	// Guards is a replacement guard list; Allow extra sandbox rules.
	Guards          []string `json:"guards,omitempty"`
	Allow           []string `json:"allow,omitempty"`
	SandboxDisabled bool     `json:"sandbox_disabled,omitempty"`
	AutoApprove     bool     `json:"auto_approve,omitempty"`
}

// BannerData holds all information needed to render an aide banner.
type BannerData struct {
	ContextName string    `json:"context_name"`
	MatchReason string    `json:"match_reason,omitempty"`
	AgentName   string    `json:"agent_name"`
	AgentPath   string    `json:"agent_path,omitempty"`
	SecretName  string    `json:"secret_name,omitempty"`
	SecretKeys  []string  `json:"secret_keys,omitempty"`  // nil = normal (show count), populated = detailed (list names)
	ContextIcon string    `json:"context_icon,omitempty"` // resolved context icon from config; empty if not set
	AgentIcon   string    `json:"agent_icon,omitempty"`   // resolved agent icon (config or built-in default); empty if none
	EnvItems    []EnvItem `json:"env_items,omitempty"`    // replaces Env + EnvResolved + CredWarnings
	// Credentials are the credential bindings the launch injects, by
	// name and env var name only; never values. AuthMode is "api" or
	// "login" (native login, nothing injected); empty when unresolved.
	Credentials []CredentialInfo `json:"credentials,omitempty"`
	AuthMode    string           `json:"auth_mode,omitempty"`
	// Selection sources: how the profile, harness and credential
	// selection were chosen ("flag", "match", "default", "preference",
	// "repository", "profile", "auth"). CredentialSelection is the
	// selected binding names, never values; empty when none.
	ProfileSource       string     `json:"profile_source,omitempty"`
	HarnessSource       string     `json:"harness_source,omitempty"`
	CredentialSource    string     `json:"credential_source,omitempty"`
	CredentialSelection []string   `json:"credential_selection,omitempty"`
	Trust               *TrustInfo `json:"trust,omitempty"` // first untrusted/denied file; nil when all trusted or no project config
	// TrustFiles lists every untrusted or denied .aide.yaml (a linked
	// worktree can have two); Trust is its first entry.
	TrustFiles []TrustInfo `json:"trust_files,omitempty"`
	// InheritedGrants are security-sensitive grants applied from a
	// linked worktree's main checkout .aide.yaml.
	InheritedGrants []InheritedGrant    `json:"inherited_grants,omitempty"`
	Sandbox         *SandboxInfo        `json:"sandbox,omitempty"`
	Yolo            bool                `json:"yolo,omitempty"`
	Warnings        []string            `json:"warnings,omitempty"`
	Capabilities    []CapabilityDisplay `json:"capabilities,omitempty"`
	DisabledCaps    []CapabilityDisplay `json:"disabled_caps,omitempty"`  // --without caps
	SuggestedCaps   []CapabilityDisplay `json:"suggested_caps,omitempty"` // detected but not enabled
	NeverAllow      []string            `json:"never_allow,omitempty"`
	CompWarnings    []string            `json:"comp_warnings,omitempty"` // composition warnings
	AutoApprove     bool                `json:"auto_approve,omitempty"`  // replaces Yolo for new banner display
	// Extra sandbox paths from config (not from capabilities)
	ExtraWritable []string `json:"extra_writable,omitempty"`
	ExtraReadable []string `json:"extra_readable,omitempty"`
	ExtraDenied   []string `json:"extra_denied,omitempty"`

	// IsolationTier is the OS-level sandbox strength for the current launch.
	// Nil means sandbox: false (user explicitly disabled sandboxing).
	// On macOS always primary; on Linux varies by kernel and policy.
	IsolationTier *sandbox.IsolationTier `json:"isolation_tier,omitempty"`

	// HarnessConfigDir is the resolved profile config directory for the
	// active harness (e.g. ~/.claude-work).
	HarnessConfigDir string `json:"harness_config_dir,omitempty"`
	// AvailableHarnesses are the other installed harnesses available for
	// this profile, with their resolved profile config directories.
	AvailableHarnesses []HarnessOption `json:"available_harnesses,omitempty"`
	// AvailableGateways are the other credentials or gateways on the
	// profile that support the active harness.
	AvailableGateways []GatewayOption `json:"available_gateways,omitempty"`
}

// HarnessOption describes an alternate installed harness and its
// profile config directory.
type HarnessOption struct {
	Name      string `json:"name"`
	ConfigDir string `json:"config_dir,omitempty"`
}

// GatewayOption describes an alternate credential or gateway on the
// profile that supports the active harness.
type GatewayOption struct {
	Name string `json:"name"`
	URL  string `json:"url,omitempty"`
}

// CredentialInfo names one injected credential binding and the env
// vars it sets. It never carries a value.
type CredentialInfo struct {
	Name       string   `json:"name"`
	Env        []string `json:"env"`
	URL        string   `json:"url,omitempty"`
	Store      string   `json:"store,omitempty"`
	Key        string   `json:"key,omitempty"`
	IsGateway  bool     `json:"is_gateway,omitempty"`
	ModelCount int      `json:"model_count,omitempty"`
	ModelAge   string   `json:"model_age,omitempty"`
	TargetFile string   `json:"target_file,omitempty"`
	TargetSync string   `json:"target_sync,omitempty"`
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
