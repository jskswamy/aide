package ui

import "encoding/json"

// MarshalJSON adds profile_name and profile_icon beside the
// context_name and context_icon contract keys (additive only; see the
// 2026-09-16 JSON output design).
func (d BannerData) MarshalJSON() ([]byte, error) {
	type plain BannerData
	return json.Marshal(struct {
		plain
		ProfileName string `json:"profile_name"`
		ProfileIcon string `json:"profile_icon,omitempty"`
	}{plain(d), d.ContextName, d.ContextIcon})
}
