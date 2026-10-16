package ui

import (
	"encoding/json"
	"testing"
)

// context_name/context_icon are JSON contracts; profile_name and
// profile_icon carry the same values beside them.
func TestBannerDataJSONCarriesProfileKeys(t *testing.T) {
	data, err := json.Marshal(&BannerData{ContextName: "work", ContextIcon: "W"})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got["context_name"] != "work" || got["profile_name"] != "work" ||
		got["context_icon"] != "W" || got["profile_icon"] != "W" {
		t.Errorf("keys = %s", data)
	}
	var back BannerData
	if err := json.Unmarshal(data, &back); err != nil || back.ContextName != "work" {
		t.Errorf("round trip: %v %+v", err, back)
	}
}
