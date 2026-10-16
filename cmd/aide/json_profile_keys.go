package main

import "encoding/json"

// JSON keys are output contracts and change only additively (see the
// 2026-09-16 JSON output design). Each result below names its profile
// under the original "context" key and, since contexts became
// profiles, also under "profile" with the same value.

// MarshalJSON adds "profile" beside "context".
func (r statusResult) MarshalJSON() ([]byte, error) {
	type plain statusResult
	return json.Marshal(struct {
		plain
		Profile string `json:"profile"`
	}{plain(r), r.Context})
}

// MarshalJSON adds "profile" beside "context".
func (r hookListResult) MarshalJSON() ([]byte, error) {
	type plain hookListResult
	return json.Marshal(struct {
		plain
		Profile string `json:"profile"`
	}{plain(r), r.Context})
}

// MarshalJSON adds "profile" beside "context".
func (r pluginListResult) MarshalJSON() ([]byte, error) {
	type plain pluginListResult
	return json.Marshal(struct {
		plain
		Profile string `json:"profile"`
	}{plain(r), r.Context})
}

// MarshalJSON adds "profile" beside "context".
func (r mcpListResult) MarshalJSON() ([]byte, error) {
	type plain mcpListResult
	return json.Marshal(struct {
		plain
		Profile string `json:"profile"`
	}{plain(r), r.Context})
}

// MarshalJSON adds "profile" beside "context".
func (r sandboxShowResult) MarshalJSON() ([]byte, error) {
	type plain sandboxShowResult
	return json.Marshal(struct {
		plain
		Profile string `json:"profile"`
	}{plain(r), r.Context})
}

// MarshalJSON adds "profile" beside "context".
func (r envListResult) MarshalJSON() ([]byte, error) {
	type plain envListResult
	return json.Marshal(struct {
		plain
		Profile string `json:"profile"`
	}{plain(r), r.Context})
}

// MarshalJSON adds "profile" beside "context"; both are omitted when
// the report covers no single profile.
func (r capReportResult) MarshalJSON() ([]byte, error) {
	type plain capReportResult
	return json.Marshal(struct {
		plain
		Profile string `json:"profile,omitempty"`
	}{plain(r), r.Context})
}
