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
	Warnings    []string
}

// SandboxInfo describes sandbox configuration for display.
type SandboxInfo struct {
	Disabled      bool
	Network       string
	Ports         string   // "all" or "443, 53"
	WritableCount int
	ReadableCount int
	Denied        []string // always listed
	Writable      []string // nil = show count, populated = list paths
	Readable      []string // nil = show count, populated = list paths
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

// sandboxSummary returns a short summary of sandbox mode.
func sandboxSummary(info *SandboxInfo) string {
	if info == nil || info.Disabled {
		return "disabled"
	}
	return info.Network
}

// sandboxDeniedLine returns the denied paths line.
func sandboxDeniedLine(info *SandboxInfo) string {
	if info == nil || len(info.Denied) == 0 {
		return ""
	}
	return "denied: " + strings.Join(info.Denied, ", ")
}

// sandboxCountsLine returns writable/readable path info.
func sandboxCountsLine(info *SandboxInfo) string {
	if info == nil {
		return ""
	}
	if info.Writable != nil {
		// detailed mode — list paths
		return fmt.Sprintf("writable: %s · readable: %s",
			strings.Join(info.Writable, ", "),
			strings.Join(info.Readable, ", "))
	}
	return fmt.Sprintf("writable: %d paths · readable: %d paths",
		info.WritableCount, info.ReadableCount)
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

	if data.Sandbox != nil {
		fmt.Fprintf(w, "   🛡️  sandbox: %s\n", sandboxSummary(data.Sandbox))
		if !data.Sandbox.Disabled {
			if dl := sandboxDeniedLine(data.Sandbox); dl != "" {
				fmt.Fprintf(w, "      %s\n", dl)
			}
			if cl := sandboxCountsLine(data.Sandbox); cl != "" {
				fmt.Fprintf(w, "      %s\n", cl)
			}
		}
	}

	for _, w2 := range data.Warnings {
		yellow.Fprintf(w, "      ⚠ %s\n", w2)
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
		fmt.Fprintf(w, "│ 🛡️  ")
		cyan.Fprintf(w, "Sandbox   ")
		fmt.Fprintf(w, "%s\n", sandboxSummary(data.Sandbox))
		if !data.Sandbox.Disabled {
			if dl := sandboxDeniedLine(data.Sandbox); dl != "" {
				fmt.Fprintf(w, "│              %s\n", dl)
			}
			if cl := sandboxCountsLine(data.Sandbox); cl != "" {
				fmt.Fprintf(w, "│              %s\n", cl)
			}
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
		cyan.Fprintf(w, "Sandbox   ")
		fmt.Fprintf(w, "%s\n", sandboxSummary(data.Sandbox))
		if !data.Sandbox.Disabled {
			if dl := sandboxDeniedLine(data.Sandbox); dl != "" {
				fmt.Fprintf(w, "            %s\n", dl)
			}
			if cl := sandboxCountsLine(data.Sandbox); cl != "" {
				fmt.Fprintf(w, "            %s\n", cl)
			}
		}
	}

	for _, w2 := range data.Warnings {
		fmt.Fprintf(w, "            ")
		yellow.Fprintf(w, "⚠ %s\n", w2)
	}
}

// ensure dim is used to avoid "declared and not used" compile error
var _ = dim
