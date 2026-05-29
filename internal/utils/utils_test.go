package utils

import "testing"

func TestSupportsTerminalControl(t *testing.T) {
	tests := []struct {
		name string
		term string
		want bool
	}{
		{name: "empty", term: "", want: false},
		{name: "dumb", term: "dumb", want: false},
		{name: "dumb variant", term: "dumb-color", want: false},
		{name: "xterm", term: "xterm-256color", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TERM", tt.term)
			if got := supportsTerminalControl(); got != tt.want {
				t.Fatalf("supportsTerminalControl() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNonInteractiveEnv(t *testing.T) {
	tests := []struct {
		name string
		val  string
		set  bool
		want bool
	}{
		{name: "unset", set: false, want: false},
		{name: "empty", val: "", set: true, want: false},
		{name: "zero", val: "0", set: true, want: false},
		{name: "false", val: "false", set: true, want: false},
		{name: "no", val: "no", set: true, want: false},
		{name: "one", val: "1", set: true, want: true},
		{name: "true", val: "true", set: true, want: true},
		{name: "yes", val: "yes", set: true, want: true},
		{name: "arbitrary", val: "please", set: true, want: true},
		{name: "uppercase false", val: "FALSE", set: true, want: false},
		{name: "padded", val: "  1  ", set: true, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.set {
				t.Setenv("STACKIT_NO_INTERACTIVE", tt.val)
			} else {
				t.Setenv("STACKIT_NO_INTERACTIVE", "")
			}
			if got := NonInteractiveEnv(); got != tt.want {
				t.Fatalf("NonInteractiveEnv() = %v, want %v", got, tt.want)
			}
		})
	}
}
