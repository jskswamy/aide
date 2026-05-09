package sliceutil_test

import (
	"reflect"
	"testing"

	"github.com/jskswamy/aide/internal/sliceutil"
)

func TestDedupString(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"empty returns nil", []string{}, nil},
		{"nil returns nil", nil, nil},
		{"unique unchanged", []string{"a", "b", "c"}, []string{"a", "b", "c"}},
		{"adjacent duplicates", []string{"a", "a", "b"}, []string{"a", "b"}},
		{"non-adjacent duplicates preserve first-seen order", []string{"a", "b", "a", "c", "b"}, []string{"a", "b", "c"}},
		{"all duplicates collapse", []string{"x", "x", "x"}, []string{"x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sliceutil.Dedup(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Dedup(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestDedupInt(t *testing.T) {
	got := sliceutil.Dedup([]int{1, 2, 1, 3, 2})
	want := []int{1, 2, 3}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Dedup ints = %v, want %v", got, want)
	}
	if got := sliceutil.Dedup([]int(nil)); got != nil {
		t.Errorf("Dedup(nil ints) = %v, want nil", got)
	}
}
