package launcher

import "testing"

func TestPreviewGetenv(t *testing.T) {
	t.Setenv("AIDE_PREVIEW_X", "process")
	get := PreviewGetenv(map[string]string{"A": "~/dir", "B": "{{ .secrets.k }}", "AIDE_PREVIEW_X": "profile"}, "/home/u")
	if got := get("A"); got != "/home/u/dir" {
		t.Errorf("A = %q, want the profile value with ~ expanded", got)
	}
	if got := get("AIDE_PREVIEW_X"); got != "profile" {
		t.Errorf("profile value should win over the process env, got %q", got)
	}
	t.Setenv("B", "process-b")
	if got := get("B"); got != "process-b" {
		t.Errorf("a templated value needs decryption, so the process env applies; got %q", got)
	}
}
