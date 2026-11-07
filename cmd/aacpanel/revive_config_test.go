package main

import (
	"testing"
)

func TestRecoveryLimitsAreValidated(t *testing.T) {
	for _, key := range []string{"AACP_REVIVE_RESERVE", "AACP_REVIVE_SESSION_MEMORY", "AACP_REVIVE_SLICE_PERCENT", "AACP_REVIVE_MEMORY_PSI", "AACP_REVIVE_WAIT", "AACP_REVIVE_ALIVE_WAIT", "AACP_REVIVE_POLL"} {
		t.Setenv(key, "")
	}
	c, err := readReviveConfig()
	if err != nil || c.Reserve != 2<<30 || c.Session != 768<<20 || c.SlicePercent != 90 || c.PSI != 5 {
		t.Fatalf("default recovery admission: %+v %v", c, err)
	}
	for _, bad := range []struct{ key, value string }{
		{"AACP_REVIVE_RESERVE", "0"}, {"AACP_REVIVE_SESSION_MEMORY", "-1G"}, {"AACP_REVIVE_SLICE_PERCENT", "101"},
		{"AACP_REVIVE_MEMORY_PSI", "NaN"}, {"AACP_REVIVE_MEMORY_PSI", "0"}, {"AACP_REVIVE_WAIT", "-1s"}, {"AACP_REVIVE_POLL", "never"},
	} {
		t.Run(bad.key+bad.value, func(t *testing.T) {
			t.Setenv(bad.key, bad.value)
			if _, err := readReviveConfig(); err == nil {
				t.Fatalf("accepted %s=%s", bad.key, bad.value)
			}
		})
	}
}
