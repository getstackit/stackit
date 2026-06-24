package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestEnvSyncInterval(t *testing.T) {
	tests := []struct {
		name string
		set  bool
		val  string
		want time.Duration
	}{
		{name: "unset defaults to backstop", set: false, want: defaultSyncInterval},
		{name: "explicit value is honored", set: true, val: "60s", want: time.Minute},
		{name: "zero explicitly disables", set: true, val: "0", want: 0},
		{name: "unparseable falls back to default", set: true, val: "garbage", want: defaultSyncInterval},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.set {
				t.Setenv("STACKIT_SYNC_INTERVAL", tt.val)
			} else {
				// t.Setenv with an empty value still marks it set; to test the
				// unset path, clear it for this subtest.
				t.Setenv("STACKIT_SYNC_INTERVAL", "")
			}
			require.Equal(t, tt.want, envSyncInterval())
		})
	}
}
