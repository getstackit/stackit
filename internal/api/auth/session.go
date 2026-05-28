package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"time"
)

// SessionTTL is how long a session lives from creation. Sessions don't slide
// — once expired the user re-authenticates. 30 days matches the default GH
// PAT-style cadence we'd want for a dev tool.
const SessionTTL = 30 * 24 * time.Hour

// sessionSweepInterval is how often the in-memory store walks itself to
// evict expired sessions. Slow enough not to thrash, fast enough that a
// memory leak from churning sessions never gets large.
const sessionSweepInterval = 5 * time.Minute

// Session is the per-user record the store holds. The GitHub access token
// is encrypted at rest with the server's Cipher; everything else is
// plaintext because it's needed for routing/auditing and isn't sensitive
// on its own.
type Session struct {
	ID             string
	GitHubLogin    string
	GitHubUserID   int64
	encryptedToken []byte
	CreatedAt      time.Time
	ExpiresAt      time.Time
}

// AccessToken decrypts the stored GitHub token for an outbound API call.
// Callers should not retain the result longer than the one request that
// needs it — re-decrypt each time so the plaintext lives on the stack.
func (s *Session) AccessToken(c *Cipher) (string, error) {
	if len(s.encryptedToken) == 0 {
		return "", errors.New("session has no access token")
	}
	plain, err := c.Open(s.encryptedToken)
	if err != nil {
		return "", fmt.Errorf("decrypt access token: %w", err)
	}
	return string(plain), nil
}

// Store is the surface used by handlers. The default implementation is
// MemoryStore; tests inject their own fakes against the same interface.
type Store interface {
	Create(login string, githubUserID int64, accessToken string) (*Session, error)
	Get(id string) (*Session, bool)
	Delete(id string)
	Close() error
}

// MemoryStore keeps sessions in a process-local map. Restarting the server
// drops every session — that's the documented behavior in docs/deploy.md.
// If you ever scale this horizontally, swap to a SQLite/Redis-backed Store
// behind the same interface.
type MemoryStore struct {
	cipher *Cipher
	now    func() time.Time
	ttl    time.Duration

	mu       sync.RWMutex
	sessions map[string]*Session

	stop chan struct{}
	done chan struct{}
}

// NewMemoryStore returns a Store backed by a map. The background sweeper
// goroutine is started immediately; Close() stops it.
func NewMemoryStore(c *Cipher) *MemoryStore {
	s := &MemoryStore{
		cipher:   c,
		now:      time.Now,
		ttl:      SessionTTL,
		sessions: make(map[string]*Session),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
	go s.sweepLoop(sessionSweepInterval)
	return s
}

// Create allocates an opaque 32-byte session ID, encrypts the supplied
// GitHub token, and stores the result. The caller sets a cookie with the
// returned Session.ID.
func (s *MemoryStore) Create(login string, githubUserID int64, accessToken string) (*Session, error) {
	id, err := newSessionID()
	if err != nil {
		return nil, err
	}
	var ct []byte
	if accessToken != "" {
		ct, err = s.cipher.Seal([]byte(accessToken))
		if err != nil {
			return nil, err
		}
	}
	now := s.now()
	sess := &Session{
		ID:             id,
		GitHubLogin:    login,
		GitHubUserID:   githubUserID,
		encryptedToken: ct,
		CreatedAt:      now,
		ExpiresAt:      now.Add(s.ttl),
	}
	s.mu.Lock()
	s.sessions[id] = sess
	s.mu.Unlock()
	return sess, nil
}

// Get returns the session if it exists and hasn't expired. Expired sessions
// are evicted lazily on read in addition to the periodic sweep, so a long
// gap between sweeps can't keep a session alive past its ExpiresAt.
func (s *MemoryStore) Get(id string) (*Session, bool) {
	if id == "" {
		return nil, false
	}
	s.mu.RLock()
	sess, ok := s.sessions[id]
	s.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if s.now().After(sess.ExpiresAt) {
		s.Delete(id)
		return nil, false
	}
	return sess, true
}

// Delete is a no-op for unknown IDs.
func (s *MemoryStore) Delete(id string) {
	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()
}

// Close stops the sweeper. Safe to call multiple times.
func (s *MemoryStore) Close() error {
	select {
	case <-s.stop:
		// already closed
	default:
		close(s.stop)
	}
	<-s.done
	return nil
}

func (s *MemoryStore) sweepLoop(interval time.Duration) {
	defer close(s.done)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-t.C:
			s.sweepExpired()
		}
	}
}

func (s *MemoryStore) sweepExpired() {
	now := s.now()
	s.mu.Lock()
	for id, sess := range s.sessions {
		if now.After(sess.ExpiresAt) {
			delete(s.sessions, id)
		}
	}
	s.mu.Unlock()
}

// SetClockForTest swaps the time source. Tests use this to advance past
// ExpiresAt without sleeping.
func (s *MemoryStore) SetClockForTest(now func() time.Time) {
	s.mu.Lock()
	s.now = now
	s.mu.Unlock()
}

func newSessionID() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("random session id: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

// sessionCtxKey isolates the session value on request contexts.
type sessionCtxKey struct{}

// SessionFromContext returns the session attached by RequireSession, or
// nil if no session is in scope. Handlers behind RequireSession can
// assume non-nil; unprotected handlers must nil-check.
func SessionFromContext(ctx context.Context) *Session {
	if v, ok := ctx.Value(sessionCtxKey{}).(*Session); ok {
		return v
	}
	return nil
}

func contextWithSession(ctx context.Context, sess *Session) context.Context {
	return context.WithValue(ctx, sessionCtxKey{}, sess)
}
