package auth

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newTestCipher(t *testing.T) *Cipher {
	t.Helper()
	key, err := GenerateSessionKey()
	require.NoError(t, err)
	c, err := NewCipher(key)
	require.NoError(t, err)
	return c
}

func TestMemoryStore_CreateGetDelete(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore(newTestCipher(t))
	t.Cleanup(func() { _ = store.Close() })

	sess, err := store.Create("jonnii", 42, "ghp_token")
	require.NoError(t, err)
	require.NotEmpty(t, sess.ID)
	require.Equal(t, "jonnii", sess.GitHubLogin)
	require.Equal(t, int64(42), sess.GitHubUserID)

	got, ok := store.Get(sess.ID)
	require.True(t, ok)
	require.Equal(t, sess.ID, got.ID)

	tok, err := got.AccessToken(store.cipher)
	require.NoError(t, err)
	require.Equal(t, "ghp_token", tok)

	store.Delete(sess.ID)
	_, ok = store.Get(sess.ID)
	require.False(t, ok)
}

func TestMemoryStore_ExpiresLazily(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore(newTestCipher(t))
	t.Cleanup(func() { _ = store.Close() })

	sess, err := store.Create("jonnii", 1, "tok")
	require.NoError(t, err)

	// Advance the store's clock past ExpiresAt; the next Get evicts.
	future := sess.ExpiresAt.Add(time.Second)
	store.SetClockForTest(func() time.Time { return future })

	_, ok := store.Get(sess.ID)
	require.False(t, ok, "expired session must not be returned")

	// And the entry should be gone from the underlying map (lazy evict).
	store.mu.RLock()
	_, present := store.sessions[sess.ID]
	store.mu.RUnlock()
	require.False(t, present)
}

func TestMemoryStore_SweepRemovesExpired(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore(newTestCipher(t))
	t.Cleanup(func() { _ = store.Close() })

	sess, _ := store.Create("jonnii", 1, "tok")

	store.SetClockForTest(func() time.Time { return sess.ExpiresAt.Add(time.Second) })
	store.sweepExpired()

	_, ok := store.Get(sess.ID)
	require.False(t, ok)
}

func TestMemoryStore_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore(newTestCipher(t))
	t.Cleanup(func() { _ = store.Close() })

	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s, err := store.Create("user", int64(i), "tok")
			require.NoError(t, err)
			_, ok := store.Get(s.ID)
			require.True(t, ok)
			store.Delete(s.ID)
		}(i)
	}
	wg.Wait()
}

func TestSessionFromContext(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	require.Nil(t, SessionFromContext(ctx))

	sess := &Session{ID: "x", GitHubLogin: "y"}
	ctx2 := contextWithSession(ctx, sess)
	require.Equal(t, sess, SessionFromContext(ctx2))
}

func TestMemoryStore_CloseIsIdempotent(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore(newTestCipher(t))
	require.NoError(t, store.Close())
	require.NoError(t, store.Close())
}
