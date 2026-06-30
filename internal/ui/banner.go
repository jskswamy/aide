// Package ui provides terminal rendering for aide's startup banner and status output.
package ui

import (
	"embed"
	"fmt"
	"io"
	"strings"
	"text/template"
	"unicode"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

// EffectiveBannerStyle resolves which banner style to render given
// the user's configured preference, whether stdout is a terminal,
// and any explicit override (--info-style flag or AIDE_INFO_STYLE
// env). Explicit overrides always win; otherwise non-TTY output
// forces compact mode to keep CI logs quiet.
func EffectiveBannerStyle(preference string, isTTY bool, explicitOverride string) string {
	if explicitOverride != "" {
		return explicitOverride
	}
	if !isTTY {
		return "compact"
	}
	return preference
}

// RenderBanner renders the banner using the given style. Valid styles are
// "compact" (default), "boxed", and "clean".
func RenderBanner(w io.Writer, style string, data *BannerData) error {
	tmpl, err := template.New("").Funcs(colorFuncMap()).ParseFS(templateFS, "templates/*.tmpl")
	if err != nil {
		return fmt.Errorf("parsing banner templates: %w", err)
	}
	name := style + ".tmpl"
	// Fall back to compact for unknown styles
	if t := tmpl.Lookup(name); t == nil {
		name = "compact.tmpl"
	}
	if err := tmpl.ExecuteTemplate(w, name, data); err != nil {
		return fmt.Errorf("rendering banner style %q: %w", style, err)
	}
	return nil
}

// --- Data helper functions (used by FuncMap and retained for direct use) ---

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

// envItemLines returns formatted env variable lines from EnvItems.
// Credential warnings render inline as "⚠ credential".
// Blocked items render as "⊘ KEY  never-allow".
func envItemLines(data *BannerData) []string {
	if len(data.EnvItems) == 0 {
		return nil
	}
	maxKeyLen := 0
	for _, item := range data.EnvItems {
		if len(item.Key) > maxKeyLen {
			maxKeyLen = len(item.Key)
		}
	}
	var lines []string
	for _, item := range data.EnvItems {
		var line string
		if item.Blocked {
			line = fmt.Sprintf("⊘  %-*s %s", maxKeyLen, item.Key, item.Annotation)
		} else {
			line = fmt.Sprintf("%s %-*s %s", item.Badge, maxKeyLen, item.Key, item.Annotation)
			if item.ResolvedValue != "" {
				line += fmt.Sprintf(" (%s)", item.ResolvedValue)
			}
			if item.CredWarning {
				line += "  ⚠ credential"
			}
		}
		lines = append(lines, line)
	}
	return lines
}

// hasTrust reports whether BannerData has a trust warning to display.
func hasTrust(data *BannerData) bool {
	return data.Trust != nil
}

// SanitizeIcon removes Unicode control characters (category C) from an icon
// string and caps it at 4 runes. This prevents ANSI injection and newline
// injection from user-controlled .aide.yaml icon fields.
func SanitizeIcon(s string) string {
	var runes []rune
	for _, r := range s {
		if !unicode.Is(unicode.C, r) {
			runes = append(runes, r)
		}
	}
	s = strings.TrimSpace(string(runes))
	if r := []rune(s); len(r) > 4 {
		s = string(r[:4])
	}
	return s
}

// trustStatusLine returns the single-line trust status for compact mode.
func trustStatusLine(data *BannerData) string {
	if data.Trust == nil {
		return ""
	}
	switch data.Trust.Status {
	case "denied":
		return fmt.Sprintf("🚫 .aide.yaml denied at %s — run aide deny --remove to undo", data.Trust.Path)
	default:
		return fmt.Sprintf("🚨 UNTRUSTED: %s — run aide trust to approve", data.Trust.Path)
	}
}

// trustWantsLine returns a compact summary of what the untrusted config wants.
func trustWantsLine(data *BannerData) string {
	if data.Trust == nil || data.Trust.Status == "denied" {
		return ""
	}
	w := data.Trust.Wants
	var parts []string
	if w.Agent != "" {
		parts = append(parts, w.Agent+" agent")
	}
	for _, c := range w.Capabilities {
		parts = append(parts, c+" capability")
	}
	parts = append(parts, w.Writable...)
	parts = append(parts, w.Unguard...)
	if len(w.EnvVars) > 0 {
		parts = append(parts, truncateList(w.EnvVars, 3))
	}
	if len(parts) == 0 {
		return ""
	}
	return "wants: " + strings.Join(parts, " · ")
}

// contextIconDisplay returns "icon name" when icon is set, or just "name".
func contextIconDisplay(data *BannerData) string {
	if data.ContextIcon != "" {
		return SanitizeIcon(data.ContextIcon) + " " + data.ContextName
	}
	return data.ContextName
}

// agentIconPrefix returns "icon " when AgentIcon is set, or "".
func agentIconPrefix(data *BannerData) string {
	if data.AgentIcon != "" {
		return SanitizeIcon(data.AgentIcon) + " "
	}
	return ""
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

// sandboxNetworkLabel returns the network mode for display.
func sandboxNetworkLabel(data *BannerData) string {
	if data.Sandbox != nil && data.Sandbox.Network != "" {
		return data.Sandbox.Network
	}
	return "outbound"
}
