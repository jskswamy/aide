package ui

import (
	"fmt"
	"net/url"
	"strings"
)

// Gateway choice sources as the launcher reports them.
const (
	sourceFlag         = "flag"
	sourceRepo         = "repo"
	sourceProfile      = "profile"
	sourcePicker       = "picker"
	sourceNoCandidates = "no-candidates"
)

// SourceLabel words a gateway choice source for people.
func SourceLabel(source, profile string) string {
	switch source {
	case sourceFlag:
		return "--gateway"
	case sourceRepo:
		return "repo preference"
	case sourceProfile:
		return "profile " + profile
	case sourcePicker:
		return "picked at launch"
	case sourceNoCandidates:
		return "no gateways marked"
	}
	return source
}

// keyLocation is where a gateway's key comes from and how it is
// delivered, never the key.
func keyLocation(c CredentialInfo) string {
	if c.Kind == "keyless" {
		if u, err := url.Parse(c.URL); err == nil && u.Host != "" {
			return "keyless, " + u.Host
		}
		return "keyless"
	}
	loc := c.Key
	if c.Store != "" {
		loc = c.Store + "/" + c.Key
	}
	if c.KeyEnv != "" {
		loc += " → " + c.KeyEnv
	}
	return loc
}

// unsetText words an unset choice: multi harnesses (Others set) start on
// their own default, slot harnesses prompt or exit 78.
func unsetText(c CredentialInfo, harness string) string {
	if c.Others > 0 {
		return fmt.Sprintf("unset (starts on %s default)", harness)
	}
	return "unset (would prompt on a terminal, exit 78 in scripts)"
}

// activeChoice is the active harness's entry in GatewayChoices.
func activeChoice(data *BannerData) *CredentialInfo {
	for i := range data.GatewayChoices {
		if data.GatewayChoices[i].Harness == data.Harness {
			return &data.GatewayChoices[i]
		}
	}
	return nil
}

// gatewayLabel is the banner label of the gateway line: plural when
// the harness has several gateways in /model.
func gatewayLabel(data *BannerData) string {
	if ch := activeChoice(data); ch != nil && (ch.Startup || ch.Others > 0) {
		return fmt.Sprintf("%-10s", "Gateways")
	}
	return fmt.Sprintf("%-10s", "Gateway")
}

// gatewayLines is the active harness's gateway block: the choice with
// its source first, then key, models, file and alternatives. Empty when
// the harness has no gateways marked.
func gatewayLines(data *BannerData) []string {
	ch := activeChoice(data)
	if ch == nil || ch.Source == sourceNoCandidates {
		return nil
	}
	src := "   (" + SourceLabel(ch.Source, data.ContextName) + ")"
	var lines []string
	var gws []CredentialInfo
	for _, c := range data.Credentials {
		if c.IsGateway {
			gws = append(gws, c)
		}
	}
	switch ch.Choice {
	case "none":
		lines = append(lines, fmt.Sprintf("none — %s login%s", data.Harness, src))
	case "unset":
		lines = append(lines, unsetText(*ch, data.Harness)+src)
	default:
		head := ch.Choice
		if ch.Startup {
			head += " (startup)"
		}
		lines = append(lines, fmt.Sprintf("%s → %s%s", head, ch.URL, src))
		if ch.Kind == "keyless" && data.Harness == "claude" {
			lines = append(lines, "keyless (placeholder, login not sent)")
		} else {
			lines = append(lines, "key: "+keyLocation(*ch))
		}
	}
	var also []string
	for _, c := range gws {
		if c.Name == ch.Choice {
			switch {
			case c.ModelCount > 0 && c.ModelAge != "":
				lines = append(lines, fmt.Sprintf("models: %d cached (%s ago)", c.ModelCount, c.ModelAge))
			case c.ModelCount > 0:
				lines = append(lines, fmt.Sprintf("models: %d cached", c.ModelCount))
			default:
				lines = append(lines, "models: not synced yet")
			}
			if c.TargetFile != "" {
				file := "file: " + c.TargetFile
				if c.TargetSync != "" {
					file += " (" + c.TargetSync + ")"
				}
				lines = append(lines, file)
			}
			continue
		}
		also = append(also, fmt.Sprintf("%s (%s)", c.Name, keyLocation(c)))
	}
	if len(also) > 0 {
		label := "also in /model: "
		if ch.Choice == "unset" {
			label = "in /model: "
		}
		for i, a := range also {
			if i > 0 {
				label = strings.Repeat(" ", len(label))
			}
			lines = append(lines, label+a)
		}
	}
	if len(data.AvailableGateways) > 0 {
		opts := make([]string, 0, len(data.AvailableGateways)+1)
		for _, g := range data.AvailableGateways {
			opts = append(opts, g.Name)
		}
		if ch.Choice != "none" {
			opts = append(opts, fmt.Sprintf("none (%s login)", data.Harness))
		}
		label := "other"
		if ch.Choice == "none" || ch.Choice == "unset" {
			label = "available"
		}
		lines = append(lines, fmt.Sprintf("%s: %s — aide --gateway <name>", label, strings.Join(opts, " · ")))
	}
	for _, w := range ch.Warnings {
		lines = append(lines, "⚠ "+w)
	}
	return lines
}

// GatewaySummary is the per-harness summary `aide which` and `aide
// status` print: one line per gateway-capable harness with the choice,
// its source and the key location.
func GatewaySummary(profile string, views []CredentialInfo) []string {
	if len(views) == 0 {
		return nil
	}
	lines := []string{fmt.Sprintf("Gateways (profile %s)", profile)}
	hw, cw, sw := 0, 0, 0
	rows := make([][3]string, len(views))
	for i, v := range views {
		choice := v.Choice
		if v.Source == sourceNoCandidates {
			choice = "—"
		}
		rows[i] = [3]string{v.Harness, choice, SourceLabel(v.Source, profile)}
		hw, cw, sw = max(hw, len(v.Harness)), max(cw, len(choice)), max(sw, len(rows[i][2]))
	}
	for i, v := range views {
		var rest []string
		switch {
		case v.Source == sourceNoCandidates, v.Choice == "none":
		case v.Choice == "unset":
			text := unsetText(v, v.Harness)
			if v.Others > 0 {
				text += fmt.Sprintf(" · %d in /model", v.Others)
			}
			rest = append(rest, text)
		default:
			rest = append(rest, keyLocation(v))
			if v.Others > 0 {
				rest = append(rest, fmt.Sprintf("+%d in /model", v.Others))
			}
		}
		line := fmt.Sprintf("  %-*s  %-*s  %-*s", hw, rows[i][0], cw, rows[i][1], sw, rows[i][2])
		if len(rest) > 0 {
			line += "  " + strings.Join(rest, "  ")
		}
		lines = append(lines, strings.TrimRight(line, " "))
		for _, w := range v.Warnings {
			lines = append(lines, "    ⚠ "+w)
		}
	}
	return lines
}
