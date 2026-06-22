package api

import (
	"net/http"
	"sync"
	"time"

	"github.com/getstackit/stackit/internal/api/handlers"
)

// Default per-IP rate limit. Generous enough that a browser loading the
// dashboard (several parallel reads, plus a branch-diff per expanded
// branch) never trips it, but it bounds how fast a single client can hit a
// public deployment.
const (
	defaultRateLimitRPS   = 20
	defaultRateLimitBurst = 60
	// rateLimitSweepInterval is how often idle buckets are evicted (inline,
	// under the lock) so the per-IP map can't grow without bound on a public
	// server. idleTTL is how long a bucket must be untouched to be evicted.
	rateLimitSweepInterval = time.Minute
	rateLimitIdleTTL       = 10 * time.Minute
)

// RateLimitConfig configures the per-IP token-bucket limiter. Zero RPS/Burst
// take the defaults; a negative RPS disables limiting entirely.
type RateLimitConfig struct {
	RequestsPerSecond float64
	Burst             int
}

type tokenBucket struct {
	tokens float64
	last   time.Time
}

// rateLimiter is a per-IP token-bucket limiter. Refill is computed lazily
// from elapsed time on each call, so there is no background goroutine; idle
// buckets are swept inline on a fixed cadence to bound memory.
type rateLimiter struct {
	rate  float64 // tokens added per second
	burst float64 // bucket capacity

	mu        sync.Mutex
	buckets   map[string]*tokenBucket
	lastSweep time.Time
	now       func() time.Time // injectable for tests
}

func newRateLimiter(cfg RateLimitConfig) *rateLimiter {
	rate := cfg.RequestsPerSecond
	if rate == 0 {
		rate = defaultRateLimitRPS
	}
	burst := float64(cfg.Burst)
	if burst == 0 {
		burst = defaultRateLimitBurst
	}
	return &rateLimiter{
		rate:    rate,
		burst:   burst,
		buckets: make(map[string]*tokenBucket),
		now:     time.Now,
	}
}

// allow reports whether a request from ip may proceed, consuming a token
// when it can.
func (rl *rateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := rl.now()
	rl.sweep(now)

	b, ok := rl.buckets[ip]
	if !ok {
		// New clients start with a full bucket so a first request is never
		// rejected.
		rl.buckets[ip] = &tokenBucket{tokens: rl.burst - 1, last: now}
		return true
	}

	elapsed := now.Sub(b.last).Seconds()
	b.tokens = min(rl.burst, b.tokens+elapsed*rl.rate)
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// sweep evicts buckets untouched for longer than rateLimitIdleTTL. It runs at
// most once per rateLimitSweepInterval and assumes the caller holds rl.mu.
func (rl *rateLimiter) sweep(now time.Time) {
	if now.Sub(rl.lastSweep) < rateLimitSweepInterval {
		return
	}
	rl.lastSweep = now
	for ip, b := range rl.buckets {
		if now.Sub(b.last) > rateLimitIdleTTL {
			delete(rl.buckets, ip)
		}
	}
}

// rateLimitMiddleware rejects requests from an IP that exceeds its token
// budget with 429. A nil limiter is a no-op so callers can disable it.
func rateLimitMiddleware(rl *rateLimiter, next http.Handler) http.Handler {
	if rl == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !rl.allow(handlers.ClientIP(r)) {
			w.Header().Set("Retry-After", "1")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"rate limit exceeded"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}
