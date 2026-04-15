package ui

import (
	"strings"
	"text/template"
	"time"

	"github.com/fatih/color"
)

// colorFuncMap returns the template.FuncMap for banner templates.
// Color helpers return plain strings (ANSI codes applied by fatih/color).
// Data helpers expose existing logic to templates declaratively.
func colorFuncMap() template.FuncMap {
	return template.FuncMap{
		// Color helpers
		"bold":      func(s string) string { return color.New(color.Bold).Sprint(s) },
		"green":     func(s string) string { return color.New(color.FgGreen).Sprint(s) },
		"boldGreen": func(s string) string { return color.New(color.FgGreen, color.Bold).Sprint(s) },
		"yellow":    func(s string) string { return color.New(color.FgYellow).Sprint(s) },
		"dim":       func(s string) string { return color.New(color.Faint).Sprint(s) },
		"red":       func(s string) string { return color.New(color.FgRed, color.Bold).Sprint(s) },
		"cyan":      func(s string) string { return color.New(color.FgCyan).Sprint(s) },

		// Data helpers (wrapping existing functions)
		"agentDisplay":  agentDisplay,
		"secretDisplay": secretDisplay,
		"envLines":      envLines,
		"networkLabel":  sandboxNetworkLabel,
		"truncate":      truncateList,

		// Variant + provenance helpers (Tier 1 + Tier 2)
		"variantSuffix":     variantSuffix,
		"freshMarker":       freshMarker,
		"provenanceTag":     provenanceTag,
		"formatConfirmedAt": formatConfirmedAt,
		"suggestedEvidence": suggestedEvidence,

		// Utility helpers
		"join":     strings.Join,
		"hasItems": func(s []string) bool { return len(s) > 0 },
		"slice": func(s []string, i int) []string {
			if i >= len(s) {
				return nil
			}
			return s[i:]
		},

		// Banner logic helpers (nil-safe)
		// IMPORTANT: Go text/template `and` does NOT short-circuit argument evaluation.
		// `{{if and .Sandbox .Sandbox.Ports}}` panics when .Sandbox is nil.
		// Use these nil-safe helpers instead.
		"sandboxDisabled": func(d *BannerData) bool {
			return d.Sandbox != nil && d.Sandbox.Disabled
		},
		"sandboxPorts": func(d *BannerData) string {
			if d.Sandbox == nil {
				return ""
			}
			if d.Sandbox.Ports == "all" {
				return ""
			}
			return d.Sandbox.Ports
		},
		"hasCapOrExtra": func(d *BannerData) bool {
			return len(d.Capabilities) > 0 ||
				len(d.DisabledCaps) > 0 ||
				len(d.SuggestedCaps) > 0 ||
				len(d.ExtraWritable) > 0 ||
				len(d.ExtraReadable) > 0 ||
				len(d.ExtraDenied) > 0
		},
	}
}

// variantSuffix returns "[uv]" or "[pnpm + corepack]" for a non-empty
// slice; "" for nil or empty. Multi-variant joins with " + ".
func variantSuffix(variants []string) string {
	if len(variants) == 0 {
		return ""
	}
	return "[" + strings.Join(variants, " + ") + "]"
}

// freshMarker returns " 🆕" when fresh is true; "" otherwise. Kept as
// a helper so the symbol is centralised (easy to swap for an ASCII
// fallback in a future NO_COLOR or !isatty pass).
func freshMarker(fresh bool) string {
	if fresh {
		return " 🆕"
	}
	return ""
}

// provenanceTag maps a capability.Provenance.Reason string to the
// short human-readable tag shown in Tier 2 (clean + boxed):
//
//	"detected" — consent:granted, consent:stable
//	"pinned"   — yaml-pin
//	"--variant" — cli-override
//	"default"  — any default:* reason
//
// Unknown reasons map to "".
func provenanceTag(reason string) string {
	switch reason {
	case "consent:granted", "consent:stable":
		return "detected"
	case "yaml-pin":
		return "pinned"
	case "cli-override":
		return "--variant"
	case "default:no-evidence", "default:declined",
		"default:skipped", "default:non-interactive":
		return "default"
	}
	return ""
}

// formatConfirmedAt formats a consent ConfirmedAt timestamp for the
// boxed banner. Returns "" for the zero time so templates can omit
// the line via {{with}}.
func formatConfirmedAt(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format("2006-01-02 · 15:04")
}

// suggestedEvidence returns " evidence: <hint>" when a DetectionHint is
// set, or "" otherwise. Templates write
// "detected{{suggestedEvidence .DetectionHint}}" and get either
// "detected" or "detected evidence: <hint>".
func suggestedEvidence(hint string) string {
	if hint == "" {
		return ""
	}
	return " evidence: " + hint
}
