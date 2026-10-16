package explain

import "encoding/json"

// MarshalJSON adds default_profile and profiles beside the
// default_context and contexts contract keys (additive only; see the
// 2026-09-16 JSON output design).
func (s ConfigState) MarshalJSON() ([]byte, error) {
	type plain ConfigState
	return json.Marshal(struct {
		plain
		DefaultProfile string         `json:"default_profile,omitempty"`
		Profiles       []ContextState `json:"profiles,omitempty"`
	}{plain(s), s.DefaultContext, s.Contexts})
}
