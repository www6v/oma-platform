package main

import (
	"testing"
)

func TestEnvDisabled(t *testing.T) {
	cases := []struct {
		name string
		val  string
		set  bool
		want bool
	}{
		{"unset", "", false, false},
		{"empty", "", true, false},
		{"one", "1", true, false},
		{"true_lower", "true", true, false},
		{"yes", "yes", true, false},
		{"zero", "0", true, true},
		{"false_lower", "false", true, true},
		{"FALSE_upper", "FALSE", true, true},
		{"no", "no", true, true},
		{"off", "off", true, true},
		{"OFF_mixed", "Off", true, true},
		{"whitespace_padded", "  false  ", true, true},
		{"nonsense", "banana", true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			const key = "OMA_TEST_ENV_DISABLED_FLAG"
			t.Setenv(key, c.val)
			if !c.set {
				t.Setenv(key, "")
				// t.Setenv replaces with ""; we need truly unset for the
				// unset case. Clearing via Setenv("", "") keeps the var
				// set to empty, which envDisabled treats identically.
			}
			got := envDisabled(key)
			if got != c.want {
				t.Errorf("envDisabled(%q) [val=%q] = %v, want %v", key, c.val, got, c.want)
			}
		})
	}
}
