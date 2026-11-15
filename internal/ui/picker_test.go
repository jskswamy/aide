package ui

import (
	"bufio"
	"bytes"
	"errors"
	"strings"
	"testing"
)

var pickerOptions = []PickerOption{
	{Name: "corp", URL: "https://corp.example.com", Store: "work", Key: "corp_key"},
	{Name: "local", URL: "http://127.0.0.1:11434"},
}

func pick(t *testing.T, answers string, canRepo bool) (string, Remember, string, error) {
	t.Helper()
	var out bytes.Buffer
	c, r, err := PickGateway(bufio.NewReader(strings.NewReader(answers)), &out, "claude", "work", pickerOptions, canRepo)
	return c, r, out.String(), err
}

func TestPickGateway(t *testing.T) {
	tests := []struct {
		name       string
		answers    string
		canRepo    bool
		wantChoice string
		wantScope  Remember
	}{
		{"gateway once", "1\n1\n", true, "corp", RememberOnce},
		{"second gateway for the repo", "2\n2\n", true, "local", RememberRepo},
		{"none for the profile", "3\n3\n", true, "none", RememberProfile},
		{"enter means once", "1\n\n", true, "corp", RememberOnce},
		{"profile is second without a repo", "1\n2\n", false, "corp", RememberProfile},
		{"bad answers ask again", "x\n9\n1\n1\n", true, "corp", RememberOnce},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, r, out, err := pick(t, tt.answers, tt.canRepo)
			if err != nil || c != tt.wantChoice || r != tt.wantScope {
				t.Fatalf("PickGateway() = %q, %q, %v; want %q, %q", c, r, err, tt.wantChoice, tt.wantScope)
			}
			if !strings.Contains(out, "work/corp_key") || !strings.Contains(out, "no key") || !strings.Contains(out, "Always in profile work") {
				t.Errorf("output missing the candidates or scopes:\n%s", out)
			}
			if tt.canRepo != strings.Contains(out, "Always in this repo") {
				t.Errorf("repo scope offered = %v, want %v", !tt.canRepo, tt.canRepo)
			}
		})
	}
}

func TestPickGateway_NoPreselectionOnEmptyOrEOF(t *testing.T) {
	if _, _, _, err := pick(t, "", true); !errors.Is(err, ErrPickerAborted) {
		t.Errorf("EOF error = %v, want ErrPickerAborted", err)
	}
	if _, _, _, err := pick(t, "\n", true); !errors.Is(err, ErrPickerAborted) {
		t.Errorf("empty first answer at EOF error = %v, want ErrPickerAborted", err)
	}
}
