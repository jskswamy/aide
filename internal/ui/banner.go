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
)

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
	Sandbox     *SandboxInfo
	Yolo        bool
	Warnings    []string
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

// renderGuardSection renders the grouped guard display for all banner styles.
func renderGuardSection(w io.Writer, info *SandboxInfo, prefix string) {
	// Active guards (green)
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

	// Blank line between groups
	if len(info.Active) > 0 && (len(info.Skipped) > 0 || len(info.Available) > 0) {
		fmt.Fprintln(w)
	}

	// Skipped guards (yellow)
	for _, g := range info.Skipped {
		yellow.Fprintf(w, "%s⊘ %s", prefix, g.Name)
		fmt.Fprintf(w, " — %s\n", g.Reason)
	}

	// Blank line
	if len(info.Skipped) > 0 && len(info.Available) > 0 {
		fmt.Fprintln(w)
	}

	// Available guards (dim)
	if len(info.Available) > 0 {
		dim.Fprintf(w, "%s○ %s — available (opt-in)\n",
			prefix, strings.Join(info.Available, ", "))
	}

	// Hint line when truncated or guards skipped/available
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

	if data.Yolo {
		yellow.Fprintf(w, "   ⚡ yolo mode (agent permission checks disabled)\n")
	}

	if data.Sandbox != nil {
		fmt.Fprintf(w, "   🛡 Sandbox\n")
		if !data.Sandbox.Disabled {
			fmt.Fprintf(w, "         network: %s\n", data.Sandbox.Network)
			if data.Sandbox.Ports != "" && data.Sandbox.Ports != "all" {
				fmt.Fprintf(w, "         ports: %s\n", data.Sandbox.Ports)
			}
			fmt.Fprintln(w)
			renderGuardSection(w, data.Sandbox, "     ")
		} else {
			fmt.Fprintf(w, "         disabled\n")
		}
	}

	for _, w2 := range data.Warnings {
		yellow.Fprintf(w, "              ⚠ %s\n", w2)
	}
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

	if data.Yolo {
		fmt.Fprintf(w, "│ ")
		yellow.Fprintf(w, "⚡ yolo mode (agent permission checks disabled)\n")
	}

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

	if data.Sandbox != nil {
		fmt.Fprintf(w, "│ 🛡 ")
		cyan.Fprintf(w, "Sandbox\n")
		if !data.Sandbox.Disabled {
			fmt.Fprintf(w, "│    network: %s\n", data.Sandbox.Network)
			if data.Sandbox.Ports != "" && data.Sandbox.Ports != "all" {
				fmt.Fprintf(w, "│    ports: %s\n", data.Sandbox.Ports)
			}
			fmt.Fprintln(w)
			renderGuardSection(w, data.Sandbox, "│    ")
		} else {
			fmt.Fprintf(w, "│    disabled\n")
		}
	}

	for _, w2 := range data.Warnings {
		fmt.Fprintf(w, "│              ")
		yellow.Fprintf(w, "⚠ %s\n", w2)
	}

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

	if data.Yolo {
		fmt.Fprintf(w, "  ")
		yellow.Fprintf(w, "yolo mode (agent permission checks disabled)\n")
	}

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

	if data.Sandbox != nil {
		fmt.Fprintf(w, "  ")
		cyan.Fprintf(w, "Sandbox\n")
		if !data.Sandbox.Disabled {
			fmt.Fprintf(w, "    network: %s\n", data.Sandbox.Network)
			if data.Sandbox.Ports != "" && data.Sandbox.Ports != "all" {
				fmt.Fprintf(w, "    ports: %s\n", data.Sandbox.Ports)
			}
			fmt.Fprintln(w)
			renderGuardSection(w, data.Sandbox, "    ")
		} else {
			fmt.Fprintf(w, "    disabled\n")
		}
	}

	for _, w2 := range data.Warnings {
		fmt.Fprintf(w, "            ")
		yellow.Fprintf(w, "⚠ %s\n", w2)
	}
}
