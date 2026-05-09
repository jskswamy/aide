package main

import (
	"strings"
	"testing"
)

func TestValidateContextScope(t *testing.T) {
	cases := []struct {
		name        string
		global      bool
		contextName string
		wantErr     bool
	}{
		{"both unset is project scope", false, "", false},
		{"global only is global scope", true, "", false},
		{"global plus context targets that context", true, "myctx", false},
		{"context without global is rejected", false, "myctx", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateContextScope(tc.global, tc.contextName)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("validateContextScope(%v, %q) = nil, want error", tc.global, tc.contextName)
				}
				if !strings.Contains(err.Error(), "--context") || !strings.Contains(err.Error(), "--global") {
					t.Errorf("error %q should mention both flags", err)
				}
				return
			}
			if err != nil {
				t.Errorf("validateContextScope(%v, %q) = %v, want nil", tc.global, tc.contextName, err)
			}
		})
	}
}
