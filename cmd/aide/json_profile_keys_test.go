package main

import (
	"encoding/json"
	"testing"
)

// JSON keys are contracts: every result that names the profile under
// "context" also names it under "profile" (additive; "context" stays).
func TestResultsCarryProfileAlongsideContext(t *testing.T) {
	cases := map[string]any{
		"status":       statusResult{Context: "work"},
		"hook list":    hookListResult{Context: "work"},
		"plugin list":  pluginListResult{Context: "work"},
		"mcp list":     mcpListResult{Context: "work"},
		"sandbox show": sandboxShowResult{Context: "work"},
		"cap report":   capReportResult{Context: "work"},
		"env list":     envListResult{Context: "work"},
	}
	for name, v := range cases {
		t.Run(name, func(t *testing.T) {
			data, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			var got map[string]any
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			if got["context"] != "work" || got["profile"] != "work" {
				t.Errorf("context=%v profile=%v in %s", got["context"], got["profile"], data)
			}
		})
	}
}
