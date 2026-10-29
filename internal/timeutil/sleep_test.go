package timeutil

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSleep(t *testing.T) {
	t.Parallel()

	t.Run("returns nil after the duration elapses", func(t *testing.T) {
		t.Parallel()
		if err := Sleep(t.Context(), time.Millisecond); err != nil {
			t.Fatalf("Sleep() = %v, want nil", err)
		}
	})

	t.Run("returns promptly when the context is already canceled", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		done := make(chan error, 1)
		go func() { done <- Sleep(ctx, time.Hour) }()

		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("Sleep() = %v, want context.Canceled", err)
			}
		case <-time.After(time.Second):
			t.Fatal("Sleep did not return promptly after cancellation")
		}
	})
}
