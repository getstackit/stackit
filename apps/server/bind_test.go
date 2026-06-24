package main

import "testing"

func TestResolveBind(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		bind     string
		explicit bool
		prod     bool
		want     string
	}{
		{name: "explicit operator value wins", bind: "10.0.0.5", explicit: true, want: "10.0.0.5"},
		{name: "explicit empty value wins too", bind: "", explicit: true, want: ""},
		{name: "production flips to all interfaces", prod: true, want: "0.0.0.0"},
		{name: "local default is loopback", want: "127.0.0.1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b := tc.bind
			resolveBind(&b, tc.explicit, tc.prod)
			if b != tc.want {
				t.Fatalf("resolveBind(%q, explicit=%v, prod=%v) = %q, want %q",
					tc.bind, tc.explicit, tc.prod, b, tc.want)
			}
		})
	}
}
