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

// credentialDisplay names the injected credential bindings and their
// env vars, or the native-login mode. Values are never shown.
func credentialDisplay(data *BannerData) string {
	return CredentialSummary(data.Credentials, data.AuthMode)
}

// CredentialSummary renders credential bindings as "name (ENV, ...)",
// or the native-login mode. It never includes values.
func CredentialSummary(creds []CredentialInfo, authMode string) string {
	if authMode == "login" {
		return "native login (no API keys)"
	}
	parts := make([]string, 0, len(creds))
	for _, c := range creds {
		parts = append(parts, fmt.Sprintf("%s (%s)", c.Name, strings.Join(c.Env, ", ")))
	}
	return strings.Join(parts, ", ")
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
	return len(trustEntries(data)) > 0
}

// trustEntries is every untrusted or denied file to display: TrustFiles,
// or Trust alone when only it is set.
func trustEntries(data *BannerData) []*TrustInfo {
	if len(data.TrustFiles) > 0 {
		out := make([]*TrustInfo, len(data.TrustFiles))
		for i := range data.TrustFiles {
			out[i] = &data.TrustFiles[i]
		}
		return out
	}
	if data.Trust != nil {
		return []*TrustInfo{data.Trust}
	}
	return nil
}

// inheritedGrantLines renders InheritedGrants, each labeled with the
// file it comes from.
func inheritedGrantLines(data *BannerData) []string {
	lines := make([]string, 0, len(data.InheritedGrants))
	for _, g := range data.InheritedGrants {
		lines = append(lines, fmt.Sprintf("%-12s %s  ← %s", g.Kind, g.Value, g.Source))
	}
	return lines
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

// trustStatusLine returns the single-line trust status for data.Trust.
func trustStatusLine(data *BannerData) string {
	return trustFileStatusLine(data.Trust)
}

// trustFileStatusLine returns the status line for one untrusted or
// denied file; an inherited file says so and names the command that
// approves it from the worktree.
func trustFileStatusLine(ti *TrustInfo) string {
	if ti == nil {
		return ""
	}
	origin := ""
	if ti.Source == "inherited" {
		origin = " (inherited from main checkout)"
	}
	if ti.Target != "" {
		origin += " (symlink to " + ti.Target + ")"
	}
	// Name the file: a bare aide trust acts on ./.aide.yaml, which is
	// the wrong file from a subdirectory or for an inherited one.
	trustCmd := "aide trust " + ti.Path
	switch ti.Status {
	case "denied":
		return fmt.Sprintf("🚫 .aide.yaml%s denied at %s — run %s to undo", origin, ti.Path, trustCmd)
	default:
		return fmt.Sprintf("🚨 UNTRUSTED%s: %s — run %s to approve", origin, ti.Path, trustCmd)
	}
}

// trustWantsLine returns a compact summary of what data.Trust wants.
func trustWantsLine(data *BannerData) string {
	return trustFileWantsLine(data.Trust)
}

// trustFileWantsLine summarizes what one untrusted file requests.
func trustFileWantsLine(ti *TrustInfo) string {
	if ti == nil || ti.Status == "denied" {
		return ""
	}
	w := ti.Wants
	var parts []string
	if w.Agent != "" {
		parts = append(parts, w.Agent+" agent")
	}
	for _, c := range w.Capabilities {
		parts = append(parts, c+" capability")
	}
	if w.SandboxDisabled {
		parts = append(parts, "sandbox off")
	}
	if w.AutoApprove {
		parts = append(parts, "auto-approve")
	}
	if w.Network != "" {
		parts = append(parts, "network "+w.Network)
	}
	parts = append(parts, w.Writable...)
	parts = append(parts, w.Readable...)
	parts = append(parts, w.Unguard...)
	if len(w.Guards) > 0 {
		parts = append(parts, "guards "+strings.Join(w.Guards, ","))
	}
	parts = append(parts, w.Allow...)
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
