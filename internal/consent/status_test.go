package consent

import "testing"

func TestStatusString(t *testing.T) {
	tests := []struct {
		s    Status
		want string
	}{
		{Granted, "granted"},
		{NotGranted, "not_granted"},
		{Status(99), "not_granted"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.s.String(); got != tt.want {
				t.Errorf("Status(%d).String() = %q, want %q", tt.s, got, tt.want)
			}
		})
	}
}
