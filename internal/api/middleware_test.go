package api

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStatusWriterImplementsFlusherWhenWrappedWriterDoes(t *testing.T) {
	t.Parallel()

	sw := &statusWriter{
		ResponseWriter: httptest.NewRecorder(),
		status:         http.StatusOK,
	}

	if _, ok := any(sw).(http.Flusher); !ok {
		t.Fatal("statusWriter must implement http.Flusher for SSE endpoints")
	}
}

func TestCORSMiddlewareAllowsConfiguredOrigin(t *testing.T) {
	t.Parallel()

	handler := corsMiddleware([]string{"http://example.com"}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/view", nil)
	req.Header.Set("Origin", "http://example.com")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, "http://example.com", rr.Header().Get("Access-Control-Allow-Origin"))
	require.Equal(t, "GET, POST, OPTIONS", rr.Header().Get("Access-Control-Allow-Methods"))
	require.Equal(t, "true", rr.Header().Get("Access-Control-Allow-Credentials"))
	require.Contains(t, rr.Header().Get("Access-Control-Allow-Headers"), "Authorization")
	require.Equal(t, "Origin", rr.Header().Get("Vary"))
}

func TestCORSMiddlewareRejectsLoopbackOriginByDefault(t *testing.T) {
	t.Parallel()

	// Previously the middleware allowed any loopback origin automatically.
	// On a public deploy that opens an attack surface for anything else
	// running on the host, so the rule was removed; localhost must be in
	// -cors to be accepted.
	for _, origin := range []string{"http://localhost:5100", "http://127.0.0.1:3000", "http://[::1]:5173"} {
		handler := corsMiddleware(nil, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "/api/view", nil)
		req.Header.Set("Origin", origin)

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		require.Emptyf(t, rr.Header().Get("Access-Control-Allow-Origin"), "origin %q should not be auto-allowed", origin)
	}
}

func TestCORSMiddlewareRejectsUnconfiguredNonLocalOrigin(t *testing.T) {
	t.Parallel()

	handler := corsMiddleware(nil, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/view", nil)
	req.Header.Set("Origin", "http://example.com")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Empty(t, rr.Header().Get("Access-Control-Allow-Origin"))
}

func TestRecoverMiddlewareConvertsPanicTo500(t *testing.T) {
	t.Parallel()

	handler := recoverMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic("boom")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	require.NotPanics(t, func() { handler.ServeHTTP(rr, req) })
	require.Equal(t, http.StatusInternalServerError, rr.Code)
}

func TestRequestIDMiddlewareGeneratesAndEchoes(t *testing.T) {
	t.Parallel()

	var observed string
	handler := requestIDMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		observed = RequestIDFromContext(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.NotEmpty(t, observed)
	require.Equal(t, observed, rr.Header().Get(requestIDHeader))
}

func TestRequestIDMiddlewareAdoptsSafeIncomingID(t *testing.T) {
	t.Parallel()

	handler := requestIDMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(requestIDHeader, "trace-abc-123")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, "trace-abc-123", rr.Header().Get(requestIDHeader))
}

func TestRequestIDMiddlewareRejectsUnsafeIncomingID(t *testing.T) {
	t.Parallel()

	cases := []string{
		strings.Repeat("a", 65), // too long
		"contains\nnewline",
		"contains\x00null",
		"unicode-😀",
	}
	for _, in := range cases {
		handler := requestIDMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set(requestIDHeader, in)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		got := rr.Header().Get(requestIDHeader)
		require.NotEqualf(t, in, got, "should not adopt unsafe id %q", in)
		require.NotEmpty(t, got, "should still emit a generated id")
	}
}

func TestSecurityHeadersMiddlewareSetsBaseline(t *testing.T) {
	t.Parallel()

	handler := securityHeadersMiddleware("default-src 'self'; frame-ancestors 'none'", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, "nosniff", rr.Header().Get("X-Content-Type-Options"))
	require.Equal(t, "DENY", rr.Header().Get("X-Frame-Options"))
	require.Equal(t, "no-referrer", rr.Header().Get("Referrer-Policy"))
	require.Contains(t, rr.Header().Get("Strict-Transport-Security"), "max-age=")
	require.Contains(t, rr.Header().Get("Content-Security-Policy"), "default-src 'self'")
	require.Contains(t, rr.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'")
}

func TestMaxBodyMiddlewareCapsRequestBody(t *testing.T) {
	t.Parallel()

	var readErr error
	handler := maxBodyMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
		if readErr != nil {
			http.Error(w, readErr.Error(), http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	body := bytes.Repeat([]byte("a"), maxRequestBodyBytes+1)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Error(t, readErr)
	require.Equal(t, http.StatusRequestEntityTooLarge, rr.Code)
}

func TestMaxBodyMiddlewareAllowsSmallBody(t *testing.T) {
	t.Parallel()

	handler := maxBodyMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Equal(t, []byte("ok"), b)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("ok"))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
}

func TestSanitizeIncomingRequestID(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"trace-1", "trace-1"},
		{strings.Repeat("x", 64), strings.Repeat("x", 64)},
		{strings.Repeat("x", 65), ""},
		{"a\tb", ""},   // tab is < 0x20
		{"a\x7fb", ""}, // DEL is > 0x7e
	}
	for _, tc := range cases {
		require.Equalf(t, tc.want, sanitizeIncomingRequestID(tc.in), "input=%q", tc.in)
	}
}
