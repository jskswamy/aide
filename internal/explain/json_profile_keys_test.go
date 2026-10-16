package explain

import (
	"encoding/json"
	"testing"
)

// default_context/contexts are JSON contracts; default_profile and
// profiles carry the same values beside them.
func TestConfigStateJSONCarriesProfileKeys(t *testing.T) {
	data, err := json.Marshal(ConfigState{Loaded: true, DefaultContext: "work", Contexts: []ContextState{{Name: "work"}}})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		DefaultContext string         `json:"default_context"`
		DefaultProfile string         `json:"default_profile"`
		Contexts       []ContextState `json:"contexts"`
		Profiles       []ContextState `json:"profiles"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.DefaultContext != "work" || got.DefaultProfile != "work" ||
		len(got.Contexts) != 1 || len(got.Profiles) != 1 || got.Profiles[0].Name != "work" {
		t.Errorf("keys = %s", data)
	}
}
