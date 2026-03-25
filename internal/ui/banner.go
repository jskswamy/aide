// Package ui provides terminal rendering for aide's startup banner and status output.
package ui

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/fatih/color"
)

var (
	boldGreen = color.New(color.FgGreen, color.Bold)
	cyan      = color.New(color.FgCyan)
	yellow    = color.New(color.FgYellow)
	dim       = color.New(color.Faint)
	red       = color.New(color.FgRed, color.Bold)
)

// RenderBanner renders the banner using the given style. Valid styles are
// "compact" (default), "boxed", and "clean".
func RenderBanner(w io.Writer, style string, data *BannerData) {
	switch style {
	case "boxed":
		RenderBoxed(w, data)
	case "clean":
		RenderClean(w, data)
	default:
		RenderCompact(w, data)
	}
}

// agentDisplay returns the agent display string, including path when it differs
// from the name.
func agentDisplay(data *BannerData) string {
	if data.AgentPath != "" && data.AgentPath != data.AgentName {
		return fmt.Sprintf("%s → %s", data.AgentName, data.AgentPath)
	}
	return data.AgentName
}

// secretDisplay returns the secret display string.
func secretDisplay(data *BannerData) string {
	if data.SecretName == "" {
		return ""
	}
	if len(data.SecretKeys) > 0 {
		return fmt.Sprintf("%s (%d keys: %s)", data.SecretName, len(data.SecretKeys), strings.Join(data.SecretKeys, ", "))
	}
	return data.SecretName
}

// envLines returns formatted env variable lines sorted by key.
func envLines(data *BannerData) []string {
	if len(data.Env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(data.Env))
	for k := range data.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var lines []string
	for _, k := range keys {
		annotation := data.Env[k]
		if data.EnvResolved != nil {
			if rv, ok := data.EnvResolved[k]; ok {
				lines = append(lines, fmt.Sprintf("%s %s (%s)", k, annotation, rv))
				continue
			}
		}
		lines = append(lines, fmt.Sprintf("%s %s", k, annotation))
	}
	return lines
}

// truncateList caps a list at maxItems and appends "(+N more)" if truncated.
func truncateList(items []string, maxItems int) string {
	if len(items) == 0 {
		return ""
	}
	if len(items) <= maxItems {
		return strings.Join(items, ", ")
	}
	shown := strings.Join(items[:maxItems], ", ")
	return fmt.Sprintf("%s (+%d more)", shown, len(items)-maxItems)
}

// renderGuardSection is available for aide sandbox commands but no longer
// used in the banner. Guard details are internal — the banner shows
// capabilities only. Keeping the types (SandboxInfo, GuardDisplay) for
// the aide sandbox guards CLI command.
//
//nolint:unused // retained for aide sandbox guards command
func renderGuardSection(w io.Writer, info *SandboxInfo, prefix string) {
	for _, g := range info.Active {
		boldGreen.Fprintf(w, "%s✓ %s\n", prefix, g.Name)
		if len(g.Protected) > 0 {
			fmt.Fprintf(w, "%s    denied:  %s\n", prefix, truncateList(g.Protected, 3))
		}
		if len(g.Allowed) > 0 {
			fmt.Fprintf(w, "%s    allowed: %s\n", prefix, truncateList(g.Allowed, 3))
		}
		for _, o := range g.Overrides {
			fmt.Fprintf(w, "%s    override: %s → %s (default: %s)\n",
				prefix, o.EnvVar, o.Value, o.DefaultPath)
		}
	}
	if len(info.Active) > 0 && (len(info.Skipped) > 0 || len(info.Available) > 0) {
		fmt.Fprintln(w)
	}
	for _, g := range info.Skipped {
		yellow.Fprintf(w, "%s⊘ %s", prefix, g.Name)
		fmt.Fprintf(w, " — %s\n", g.Reason)
	}
	if len(info.Skipped) > 0 && len(info.Available) > 0 {
		fmt.Fprintln(w)
	}
	if len(info.Available) > 0 {
		dim.Fprintf(w, "%s○ %s — available (opt-in)\n",
			prefix, strings.Join(info.Available, ", "))
	}
	needsHint := len(info.Skipped) > 0 || len(info.Available) > 0
	for _, g := range info.Active {
		if len(g.Protected) > 3 || len(g.Allowed) > 3 {
			needsHint = true
		}
	}
	if needsHint {
		fmt.Fprintln(w)
		dim.Fprintf(w, "%srun `aide sandbox` for full details\n", prefix)
	}
}

// renderCapabilitySection renders the capability-oriented display for all banner styles.
func renderCapabilitySection(w io.Writer, data *BannerData, prefix string) {
	// Active capabilities (green checkmark)
	for _, cap := range data.Capabilities {
		paths := truncateList(cap.Paths, 3)
		if cap.Source != "" && cap.Source != "context config" {
			boldGreen.Fprintf(w, "%s\u2713 %-10s %s", prefix, cap.Name, paths)
			dim.Fprintf(w, "  \u2190 %s\n", cap.Source)
		} else {
			boldGreen.Fprintf(w, "%s\u2713 %-10s %s\n", prefix, cap.Name, paths)
		}
	}

	// Disabled capabilities (dim circle)
	for _, cap := range data.DisabledCaps {
		dim.Fprintf(w, "%s\u25CB %-10s disabled for this session", prefix, cap.Name)
		fmt.Fprintf(w, "  \u2190 --without\n")
	}

	// Never-allow (red X)
	for _, path := range data.NeverAllow {
		red.Fprintf(w, "%s\u2717 denied    %s (never-allow)\n", prefix, path)
	}

	// Credential warnings
	if len(data.CredWarnings) > 0 {
		fmt.Fprintln(w)
		yellow.Fprintf(w, "%s\u26A0 credentials exposed: %s\n", prefix,
			strings.Join(data.CredWarnings, ", "))
	}

	// Composition warnings
	for _, w2 := range data.CompWarnings {
		yellow.Fprintf(w, "%s\u26A0 %s\n", prefix, w2)
	}
}

// renderAutoApprove renders the auto-approve warning as the last line if enabled.
func renderAutoApprove(w io.Writer, prefix string, data *BannerData) {
	if data.AutoApprove {
		red.Fprintf(w, "%s\u26A1 AUTO-APPROVE \u2014 all agent actions execute without confirmation\n", prefix)
	}
}

// sandboxNetworkLabel returns the network mode for display.
func sandboxNetworkLabel(data *BannerData) string {
	if data.Sandbox != nil && data.Sandbox.Network != "" {
		return data.Sandbox.Network
	}
	return "outbound"
}

// hasCapabilities returns true if capability data is present.
func hasCapabilities(data *BannerData) bool {
	return len(data.Capabilities) > 0 || len(data.DisabledCaps) > 0
}

// RenderCompact renders the compact (default) banner style.
func RenderCompact(w io.Writer, data *BannerData) {
	agent := agentDisplay(data)
	if data.ContextName != "" {
		boldGreen.Fprintf(w, "🔧 aide · %s", data.ContextName)
		fmt.Fprintf(w, " (%s)\n", agent)
	} else {
		boldGreen.Fprintf(w, "🔧 aide")
		fmt.Fprintf(w, " (%s)\n", agent)
	}

	if data.MatchReason != "" {
		fmt.Fprintf(w, "   📁 %s\n", data.MatchReason)
	}

	if s := secretDisplay(data); s != "" {
		fmt.Fprintf(w, "   🔐 secret: %s\n", s)
	}

	if lines := envLines(data); len(lines) > 0 {
		fmt.Fprintf(w, "   📦 env: %s\n", lines[0])
		for _, l := range lines[1:] {
			fmt.Fprintf(w, "          %s\n", l)
		}
	}

	if data.Sandbox != nil && data.Sandbox.Disabled {
		fmt.Fprintf(w, "   🛡 sandbox: disabled\n")
	} else {
		network := sandboxNetworkLabel(data)
		if hasCapabilities(data) {
			fmt.Fprintf(w, "   🛡 sandbox: network %s\n", network)
			if data.Sandbox != nil && data.Sandbox.Ports != "" && data.Sandbox.Ports != "all" {
				fmt.Fprintf(w, "   🛡 ports: %s\n", data.Sandbox.Ports)
			}
			renderCapabilitySection(w, data, "     ")
		} else {
			fmt.Fprintf(w, "   🛡 sandbox: network %s, code-only\n", network)
			if data.Sandbox != nil && data.Sandbox.Ports != "" && data.Sandbox.Ports != "all" {
				fmt.Fprintf(w, "   🛡 ports: %s\n", data.Sandbox.Ports)
			}
		}
	}

	for _, w2 := range data.Warnings {
		yellow.Fprintf(w, "     ⚠ %s\n", w2)
	}

	renderAutoApprove(w, "   ", data)
}

// RenderBoxed renders the boxed banner style with box-drawing characters.
func RenderBoxed(w io.Writer, data *BannerData) {
	width := 50
	border := strings.Repeat("─", width)

	boldGreen.Fprintf(w, "┌─ aide %s\n", border[:width-7])

	if data.ContextName != "" {
		fmt.Fprintf(w, "│ 🎯 ")
		cyan.Fprintf(w, "Context   ")
		boldGreen.Fprintf(w, "%s\n", data.ContextName)
	}
	if data.MatchReason != "" {
		fmt.Fprintf(w, "│ 📁 ")
		cyan.Fprintf(w, "Matched   ")
		fmt.Fprintf(w, "%s\n", data.MatchReason)
	}

	fmt.Fprintf(w, "│ 🤖 ")
	cyan.Fprintf(w, "Agent     ")
	fmt.Fprintf(w, "%s\n", agentDisplay(data))

	if s := secretDisplay(data); s != "" {
		fmt.Fprintf(w, "│ 🔐 ")
		cyan.Fprintf(w, "Secret    ")
		fmt.Fprintf(w, "%s\n", s)
	}

	if lines := envLines(data); len(lines) > 0 {
		fmt.Fprintf(w, "│ 📦 ")
		cyan.Fprintf(w, "Env       ")
		fmt.Fprintf(w, "%s\n", lines[0])
		for _, l := range lines[1:] {
			fmt.Fprintf(w, "│              %s\n", l)
		}
	}

	network := sandboxNetworkLabel(data)
	if hasCapabilities(data) {
		fmt.Fprintf(w, "│ 🛡 sandbox: network %s\n", network)
		renderCapabilitySection(w, data, "│    ")
	} else {
		fmt.Fprintf(w, "│ 🛡 sandbox: network %s, code-only\n", network)
	}

	for _, w2 := range data.Warnings {
		fmt.Fprintf(w, "│ ")
		yellow.Fprintf(w, "⚠ %s\n", w2)
	}

	renderAutoApprove(w, "│ ", data)

	fmt.Fprintf(w, "└%s\n", border)
}

// RenderClean renders the clean banner style without emoji decorations.
func RenderClean(w io.Writer, data *BannerData) {
	if data.ContextName != "" {
		boldGreen.Fprintf(w, "aide")
		fmt.Fprintf(w, " · context: ")
		boldGreen.Fprintf(w, "%s\n", data.ContextName)
	} else {
		boldGreen.Fprintf(w, "aide\n")
	}

	fmt.Fprintf(w, "  ")
	cyan.Fprintf(w, "Agent     ")
	fmt.Fprintf(w, "%s\n", agentDisplay(data))

	if data.MatchReason != "" {
		fmt.Fprintf(w, "  ")
		cyan.Fprintf(w, "Matched   ")
		fmt.Fprintf(w, "%s\n", data.MatchReason)
	}

	if s := secretDisplay(data); s != "" {
		fmt.Fprintf(w, "  ")
		cyan.Fprintf(w, "Secret    ")
		fmt.Fprintf(w, "%s\n", s)
	}

	if lines := envLines(data); len(lines) > 0 {
		fmt.Fprintf(w, "  ")
		cyan.Fprintf(w, "Env       ")
		fmt.Fprintf(w, "%s\n", lines[0])
		for _, l := range lines[1:] {
			fmt.Fprintf(w, "            %s\n", l)
		}
	}

	network := sandboxNetworkLabel(data)
	if hasCapabilities(data) {
		fmt.Fprintf(w, "  ")
		cyan.Fprintf(w, "sandbox:  ")
		fmt.Fprintf(w, "network %s\n", network)
		renderCapabilitySection(w, data, "    ")
	} else {
		fmt.Fprintf(w, "  ")
		cyan.Fprintf(w, "sandbox:  ")
		fmt.Fprintf(w, "network %s, code-only\n", network)
	}

	for _, w2 := range data.Warnings {
		fmt.Fprintf(w, "  ")
		yellow.Fprintf(w, "⚠ %s\n", w2)
	}

	renderAutoApprove(w, "  ", data)
}
