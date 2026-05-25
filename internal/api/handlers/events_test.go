package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/api/registry"
)

type signalingResponseWriter struct {
	*httptest.ResponseRecorder
	wrote chan struct{}
}

func newSignalingResponseWriter() *signalingResponseWriter {
	return &signalingResponseWriter{
		ResponseRecorder: httptest.NewRecorder(),
		wrote:            make(chan struct{}),
	}
}

func (w *signalingResponseWriter) Write(p []byte) (int, error) {
	select {
	case <-w.wrote:
	default:
		close(w.wrote)
	}
	return w.ResponseRecorder.Write(p)
}

func TestEventsHandlerReturnsWhenBroadcasterCloses(t *testing.T) {
	t.Parallel()

	reg := registry.New()
	broadcaster := registry.NewBroadcaster()
	require.NoError(t, reg.Add(&registry.RepoEntry{
		ID:          "default",
		Broadcaster: broadcaster,
	}))
	handler := NewEventsHandler(reg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/repos/default/events", nil)
	req.SetPathValue("repoID", "default")
	recorder := newSignalingResponseWriter()

	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(recorder, req)
		close(done)
	}()

	select {
	case <-recorder.wrote:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for initial SSE write")
	}

	broadcaster.Close()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not exit after broadcaster shutdown")
	}
}

func TestEventsHandlerReturns404ForUnknownRepo(t *testing.T) {
	t.Parallel()

	reg := registry.New()
	require.NoError(t, reg.Add(&registry.RepoEntry{
		ID:          "default",
		Broadcaster: registry.NewBroadcaster(),
	}))
	handler := NewEventsHandler(reg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/repos/missing/events", nil)
	req.SetPathValue("repoID", "missing")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusNotFound, rr.Code)
}
