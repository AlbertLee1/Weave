package oms

import "testing"

func TestCheckMinWeaveVersion(t *testing.T) {
	cases := []struct {
		required, server string
		wantErr          bool
	}{
		{"", "0.99.0", false},          // empty required is always satisfied
		{"0.42.0", "0.99.0", false},    // server newer
		{"1.0.0", "1.0.0", false},      // exact match
		{"99.0.0", "0.99.0", true},     // server older
		{"v1.0.0", "1.0.0", false},     // tolerates "v" prefix
		{"1.0.0", "v1.0.0", false},     // server "v" prefix tolerated
		{"1.0.0-beta", "1.0.0", false}, // suffix stripped: 1.0.0 == 1.0.0
		{"2.10.0", "2.9.99", true},     // element-wise integer cmp, NOT lexical
		{"1.2", "1.2.0", false},        // "1.2" treated as 1.2.0
		{"1.2.3.4", "1.2.3", true},     // longer required wins when prefix matches
		{"abc", "1.0.0", false},        // non-numeric required normalises to 0
	}
	for _, tc := range cases {
		err := checkMinWeaveVersion(tc.required, tc.server)
		gotErr := err != nil
		if gotErr != tc.wantErr {
			t.Errorf("checkMinWeaveVersion(%q, %q) gotErr=%v want=%v err=%v",
				tc.required, tc.server, gotErr, tc.wantErr, err)
		}
	}
}

func TestCompareSemverPrefix(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "1.0.1", -1},
		{"1.0.1", "1.0.0", 1},
		{"2.10.0", "2.9.99", 1},
		{"v1.2.3", "1.2.3", 0},
		{"1.0", "1.0.0", 0},
		{"1.0.0", "1.0", 0},
		{"1.0.0.0", "1.0.0", 0},
	}
	for _, tc := range cases {
		got := compareSemverPrefix(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("compareSemverPrefix(%q, %q) = %d want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
