package main

import "testing"

func TestParseAdminUserIDs(t *testing.T) {
	for _, in := range []string{"", "1", " 1, 2,1 ", "9223372036854775807"} {
		if _, err := parseAdminUserIDs(in); err != nil {
			t.Errorf("valid %q: %v", in, err)
		}
	}
	for _, in := range []string{"0", "-1", "+1", "1,", "1,,2", "a", "1.0", "9223372036854775808", "１"} {
		if _, err := parseAdminUserIDs(in); err == nil {
			t.Errorf("invalid %q accepted", in)
		}
	}
}
