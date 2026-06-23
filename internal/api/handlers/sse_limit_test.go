package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSSELimiterGlobalCap(t *testing.T) {
	t.Parallel()

	l := newSSELimiter(2, 0) // global 2, per-IP unlimited
	require.True(t, l.acquire("a"))
	require.True(t, l.acquire("b"))
	require.False(t, l.acquire("c"), "third connection exceeds the global cap")

	l.release("a")
	require.True(t, l.acquire("c"), "releasing a slot lets a new connection in")
}

func TestSSELimiterPerIPCap(t *testing.T) {
	t.Parallel()

	l := newSSELimiter(0, 1) // global unlimited, per-IP 1
	require.True(t, l.acquire("1.1.1.1"))
	require.False(t, l.acquire("1.1.1.1"), "second connection from the same IP is capped")
	require.True(t, l.acquire("2.2.2.2"), "a different IP has its own budget")

	l.release("1.1.1.1")
	require.True(t, l.acquire("1.1.1.1"), "releasing frees the per-IP slot")
}

func TestSSELimiterReleaseIsBounded(t *testing.T) {
	t.Parallel()

	l := newSSELimiter(1, 1)
	// Over-releasing must not drive counters negative and grant phantom slots.
	l.release("x")
	require.True(t, l.acquire("x"))
	require.False(t, l.acquire("x"))
}

func TestClientIP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		remoteAddr string
		xff        string
		want       string
	}{
		{name: "remote addr host", remoteAddr: "203.0.113.5:54321", want: "203.0.113.5"},
		{name: "xff takes precedence", remoteAddr: "10.0.0.1:80", xff: "198.51.100.7", want: "198.51.100.7"},
		{name: "xff leftmost of chain", remoteAddr: "10.0.0.1:80", xff: "198.51.100.7, 10.0.0.1", want: "198.51.100.7"},
		{name: "xff trimmed", remoteAddr: "10.0.0.1:80", xff: "  198.51.100.7  ", want: "198.51.100.7"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tc.remoteAddr
			if tc.xff != "" {
				req.Header.Set("X-Forwarded-For", tc.xff)
			}
			require.Equal(t, tc.want, ClientIP(req))
		})
	}
}
