// SelectVariants picks which variants of a capability activate at launch,
// following the five-state decision table from the design spec:
//
//	E. --variant CLI override (wins unconditionally)
//	D. .aide.yaml variant pin (explicit user intent)
//	B. stable consent — detection matches a prior grant at the same
//	   evidence digest (silent reuse)
//	A/C. evidence present but no current grant — prompt the user when
//	   interactive; fall through to DefaultVariants when not
//	default:no-evidence — no markers matched; DefaultVariants applies
//
// The returned Provenance names the chosen variants and the reason
// string so aide status and preflight can explain the selection.

package capability

import (
	"io/fs"
	"os"
	"time"

	"github.com/jskswamy/aide/internal/consent"
)

// PromptDecision is the user's answer to a consent prompt.
type PromptDecision int

const (
	PromptYes PromptDecision = iota
	PromptNo
	PromptSkip
)

// PromptInput carries the data a prompter needs to render a consent
// request.
type PromptInput struct {
	Capability       string
	DetectedVariants []Variant
	PreviousVariants []string // empty on first-time prompts
	Evidence         consent.Evidence
}

// PromptResult is the prompter's answer. When Decision is PromptYes,
// Variants is the subset the user approved (equal to DetectedVariants
// for a plain "yes", a subset from the Customize sub-flow).
type PromptResult struct {
	Decision PromptDecision
	Variants []string
}

// Prompter renders and collects a user's consent decision.
type Prompter interface {
	PromptVariantConsent(in PromptInput) PromptResult
}

// Provenance traces why a particular variant set was chosen.
type Provenance struct {
	Variants []string
	// Reason is one of: "cli-override", "yaml-pin", "consent:granted",
	// "consent:stable", "default:declined", "default:non-interactive",
	// "default:skipped", "default:no-evidence".
	Reason string
}

// SelectInput is the composite set of inputs to SelectVariants.
type SelectInput struct {
	Capability  Capability
	ProjectRoot string
	FS          fs.FS    // when nil, SelectVariants uses os.DirFS(ProjectRoot)
	Overrides   []string // from --variant flag
	YAMLPins    []string // from .aide.yaml capabilities.<cap>.variants
	Consent     *consent.Store
	Prompter    Prompter
	Interactive bool
	AutoYes     bool // treat prompts as an implicit Yes without calling Prompter
}

// SelectVariants runs the five-state decision table.
func SelectVariants(in SelectInput) ([]Variant, Provenance, error) {
	// State E — CLI override.
	if len(in.Overrides) > 0 {
		selected, err := variantsByName(in.Capability, in.Overrides)
		if err != nil {
			return nil, Provenance{}, err
		}
		return selected, Provenance{Variants: names(selected), Reason: "cli-override"}, nil
	}

	// State D — YAML pin.
	if len(in.YAMLPins) > 0 {
		selected, err := variantsByName(in.Capability, in.YAMLPins)
		if err != nil {
			return nil, Provenance{}, err
		}
		return selected, Provenance{Variants: names(selected), Reason: "yaml-pin"}, nil
	}

	fsys := in.FS
	if fsys == nil {
		fsys = os.DirFS(in.ProjectRoot)
	}
	evidence := DetectEvidence(fsys, in.Capability)

	// No evidence → DefaultVariants.
	if len(evidence.Variants) == 0 {
		defaults, err := variantsByName(in.Capability, in.Capability.DefaultVariants)
		if err != nil {
			return nil, Provenance{}, err
		}
		return defaults, Provenance{Variants: names(defaults), Reason: "default:no-evidence"}, nil
	}

	// State B — stable consent.
	if in.Consent != nil && in.Consent.Check(in.ProjectRoot, in.Capability.Name, evidence) == consent.Granted {
		selected, err := variantsByName(in.Capability, evidence.Variants)
		if err != nil {
			return nil, Provenance{}, err
		}
		return selected, Provenance{Variants: names(selected), Reason: "consent:stable"}, nil
	}

	// States A/C — need a decision. Non-interactive → fall through.
	if !in.Interactive || (in.Prompter == nil && !in.AutoYes) {
		defaults, err := variantsByName(in.Capability, in.Capability.DefaultVariants)
		if err != nil {
			return nil, Provenance{}, err
		}
		return defaults, Provenance{Variants: names(defaults), Reason: "default:non-interactive"}, nil
	}

	detected, err := variantsByName(in.Capability, evidence.Variants)
	if err != nil {
		return nil, Provenance{}, err
	}

	var result PromptResult
	if in.AutoYes {
		result = PromptResult{Decision: PromptYes, Variants: evidence.Variants}
	} else {
		result = in.Prompter.PromptVariantConsent(PromptInput{
			Capability:       in.Capability.Name,
			DetectedVariants: detected,
			PreviousVariants: previousVariants(in.Consent, in.ProjectRoot, in.Capability.Name),
			Evidence:         evidence,
		})
	}

	switch result.Decision {
	case PromptYes:
		approved, err := variantsByName(in.Capability, result.Variants)
		if err != nil {
			return nil, Provenance{}, err
		}
		if in.Consent != nil {
			_ = in.Consent.Grant(consent.Grant{
				ProjectRoot: in.ProjectRoot,
				Capability:  in.Capability.Name,
				Variants:    result.Variants,
				Evidence:    evidence,
				Summary:     summarizeEvidence(evidence),
				ConfirmedAt: time.Now().UTC(),
			})
		}
		return approved, Provenance{Variants: names(approved), Reason: "consent:granted"}, nil
	case PromptSkip:
		defaults, err := variantsByName(in.Capability, in.Capability.DefaultVariants)
		if err != nil {
			return nil, Provenance{}, err
		}
		return defaults, Provenance{Variants: names(defaults), Reason: "default:skipped"}, nil
	default: // PromptNo
		defaults, err := variantsByName(in.Capability, in.Capability.DefaultVariants)
		if err != nil {
			return nil, Provenance{}, err
		}
		return defaults, Provenance{Variants: names(defaults), Reason: "default:declined"}, nil
	}
}

func variantsByName(cap Capability, wanted []string) ([]Variant, error) {
	out := make([]Variant, 0, len(wanted))
	for _, n := range wanted {
		found := false
		for _, v := range cap.Variants {
			if v.Name == n {
				out = append(out, v)
				found = true
				break
			}
		}
		if !found {
			return nil, &UnknownVariantError{
				Capability: cap.Name,
				Variant:    n,
				Available:  allVariantNames(cap),
			}
		}
	}
	return out, nil
}

func names(vs []Variant) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = v.Name
	}
	return out
}

func allVariantNames(cap Capability) []string {
	out := make([]string, len(cap.Variants))
	for i, v := range cap.Variants {
		out[i] = v.Name
	}
	return out
}

// UnknownVariantError is returned when a caller names a variant the
// capability does not declare.
type UnknownVariantError struct {
	Capability string
	Variant    string
	Available  []string
}

func (e *UnknownVariantError) Error() string {
	return "unknown variant '" + e.Variant + "' for capability '" + e.Capability +
		"'. available: " + joinCSV(e.Available)
}

func joinCSV(s []string) string {
	out := ""
	for i, v := range s {
		if i > 0 {
			out += ", "
		}
		out += v
	}
	return out
}

func previousVariants(store *consent.Store, project, cap string) []string {
	if store == nil {
		return nil
	}
	grants, err := store.List(project)
	if err != nil {
		return nil
	}
	for _, g := range grants {
		if g.Capability == cap {
			return g.Variants
		}
	}
	return nil
}

func summarizeEvidence(e consent.Evidence) string {
	out := ""
	for _, m := range e.Matches {
		if !m.Matched {
			continue
		}
		if out != "" {
			out += ", "
		}
		out += m.Target
	}
	return out
}
