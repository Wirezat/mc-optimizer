package api

import "testing"

// If-None-Match is a comma-separated list, and a substring test would turn any
// header that merely contains the tag into a spurious 304.
func TestETagMatches(t *testing.T) {
	const tag = `"abc123"`
	for _, tc := range []struct {
		header string
		want   bool
	}{
		{"", false},
		{tag, true},
		{`"other", ` + tag, true},
		{`W/` + tag, true},
		{"*", true},
		{`"other"`, false},
		{`"abc1234"`, false},  // longer tag that contains ours as a prefix
		{`"xabc123x"`, false}, // ours as an infix
	} {
		if got := etagMatches(tc.header, tag); got != tc.want {
			t.Errorf("etagMatches(%q, %q) = %v, want %v", tc.header, tag, got, tc.want)
		}
	}
}
