package doctor

import "testing"

func TestShellJoin(t *testing.T) {
	got := ShellJoin([]string{"aide", "sandbox", "allow", "--remove", "~/docs/api", "/a b/c", "it's"})
	want := `aide sandbox allow --remove '~/docs/api' '/a b/c' 'it'\''s'`
	if got != want {
		t.Errorf("ShellJoin = %s, want %s", got, want)
	}
}
