package main

import "testing"

func TestResolveEnv(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		raw     string
		want    serverEnv
		wantErr bool
	}{
		{name: "unset defaults to local", raw: "", want: envLocal},
		{name: "local", raw: "local", want: envLocal},
		{name: "local uppercase", raw: "LOCAL", want: envLocal},
		{name: "development alias", raw: "development", want: envLocal},
		{name: "dev alias", raw: "dev", want: envLocal},
		{name: "production", raw: "production", want: envProduction},
		{name: "production trimmed and cased", raw: "  Production  ", want: envProduction},
		{name: "prod alias", raw: "prod", want: envProduction},
		{name: "unknown is an error", raw: "staging", wantErr: true},
		{name: "typo is an error", raw: "prdouction", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolveEnv(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("resolveEnv(%q) = %v, want error", tc.raw, got)
				}
				// Even on error, the fail-safe fallback is local.
				if got != envLocal {
					t.Fatalf("resolveEnv(%q) error fallback = %v, want envLocal", tc.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveEnv(%q) unexpected error: %v", tc.raw, err)
			}
			if got != tc.want {
				t.Fatalf("resolveEnv(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestIsLoopbackBind(t *testing.T) {
	t.Parallel()

	cases := []struct {
		addr string
		want bool
	}{
		{addr: "127.0.0.1", want: true},
		{addr: "127.0.0.2", want: true}, // 127.0.0.0/8 is all loopback
		{addr: "::1", want: true},
		{addr: "localhost", want: true},
		{addr: " 127.0.0.1 ", want: true}, // trimmed
		{addr: "", want: false},           // empty = all interfaces = exposed
		{addr: "0.0.0.0", want: false},
		{addr: "::", want: false},
		{addr: "192.168.1.5", want: false},
		{addr: "10.0.0.5", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.addr, func(t *testing.T) {
			t.Parallel()
			if got := isLoopbackBind(tc.addr); got != tc.want {
				t.Fatalf("isLoopbackBind(%q) = %v, want %v", tc.addr, got, tc.want)
			}
		})
	}
}
