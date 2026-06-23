package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRateLimiterBurstThenRefill(t *testing.T) {
	t.Parallel()

	rl := newRateLimiter(RateLimitConfig{RequestsPerSecond: 1, Burst: 3})
	clock := time.Unix(1000, 0)
	rl.now = func() time.Time { return clock }

	// A full burst is allowed, then the bucket is empty.
	require.True(t, rl.allow("ip"))
	require.True(t, rl.allow("ip"))
	require.True(t, rl.allow("ip"))
	require.False(t, rl.allow("ip"), "fourth request exceeds the burst")

	// One second later, one token has refilled.
	clock = clock.Add(time.Second)
	require.True(t, rl.allow("ip"))
	require.False(t, rl.allow("ip"))
}

func TestRateLimiterIsPerIP(t *testing.T) {
	t.Parallel()

	rl := newRateLimiter(RateLimitConfig{RequestsPerSecond: 1, Burst: 1})
	clock := time.Unix(0, 0)
	rl.now = func() time.Time { return clock }

	require.True(t, rl.allow("a"))
	require.False(t, rl.allow("a"), "a is exhausted")
	require.True(t, rl.allow("b"), "b has its own bucket")
}

func TestRateLimitMiddlewareRejectsOverBudget(t *testing.T) {
	t.Parallel()

	rl := newRateLimiter(RateLimitConfig{RequestsPerSecond: 1, Burst: 1})
	clock := time.Unix(0, 0)
	rl.now = func() time.Time { return clock }

	handler := rateLimitMiddleware(rl, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/api/v1/repos", nil))
	require.Equal(t, http.StatusOK, first.Code)

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/api/v1/repos", nil))
	require.Equal(t, http.StatusTooManyRequests, second.Code)
	require.Equal(t, "1", second.Header().Get("Retry-After"))
}

func TestRateLimitMiddlewareNilIsPassthrough(t *testing.T) {
	t.Parallel()

	called := false
	handler := rateLimitMiddleware(nil, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	require.True(t, called)
}
