package handlers

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Default SSE resource bounds. They are generous enough that normal use
// (a handful of browser tabs) never trips them, but they cap the blast
// radius of an unauthenticated public deployment where anyone can open
// long-lived streams.
const (
	defaultSSEMaxGlobal   = 512
	defaultSSEMaxPerIP    = 32
	defaultSSEMaxLifetime = 30 * time.Minute
	// sseHeartbeatInterval is how often a keepalive comment is written. It
	// keeps proxies from idling the connection out and — more importantly —
	// surfaces dead clients via a failed write, so their slots are released
	// back to the limiter instead of leaking until max lifetime.
	sseHeartbeatInterval = 25 * time.Second
)

// SSELimits bounds concurrent Server-Sent Events connections. Zero fields
// fall back to the package defaults, so callers can set only what they want
// to override.
type SSELimits struct {
	// MaxGlobal caps concurrent SSE connections across all repos. 0 -> default.
	MaxGlobal int
	// MaxPerIP caps concurrent SSE connections from a single client IP.
	// 0 -> default.
	MaxPerIP int
	// MaxLifetime force-closes a connection after this duration. EventSource
	// clients reconnect transparently, so a bounded lifetime caps how long
	// any single connection can hold resources. 0 -> default.
	MaxLifetime time.Duration
}

func (l SSELimits) withDefaults() SSELimits {
	if l.MaxGlobal == 0 {
		l.MaxGlobal = defaultSSEMaxGlobal
	}
	if l.MaxPerIP == 0 {
		l.MaxPerIP = defaultSSEMaxPerIP
	}
	if l.MaxLifetime == 0 {
		l.MaxLifetime = defaultSSEMaxLifetime
	}
	return l
}

// sseLimiter tracks active SSE connection counts globally and per client IP.
// A negative or zero cap disables that dimension.
type sseLimiter struct {
	maxGlobal int
	maxPerIP  int

	mu     sync.Mutex
	global int
	perIP  map[string]int
}

func newSSELimiter(maxGlobal, maxPerIP int) *sseLimiter {
	return &sseLimiter{
		maxGlobal: maxGlobal,
		maxPerIP:  maxPerIP,
		perIP:     make(map[string]int),
	}
}

// acquire reserves a connection slot for ip. It returns false (reserving
// nothing) when the global or per-IP cap is already reached, so the caller
// must not release on a false result.
func (l *sseLimiter) acquire(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.maxGlobal > 0 && l.global >= l.maxGlobal {
		return false
	}
	if l.maxPerIP > 0 && l.perIP[ip] >= l.maxPerIP {
		return false
	}
	l.global++
	l.perIP[ip]++
	return true
}

// release returns a slot previously taken by acquire.
func (l *sseLimiter) release(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.global > 0 {
		l.global--
	}
	switch n := l.perIP[ip]; {
	case n <= 1:
		delete(l.perIP, ip)
	default:
		l.perIP[ip] = n - 1
	}
}

// ClientIP extracts the caller's IP for per-IP limiting. It honors the
// left-most X-Forwarded-For entry (set by a trusted proxy/CDN in front of a
// public deployment) and falls back to the connection's RemoteAddr. Behind a
// proxy that doesn't set the header, all callers collapse to the proxy IP —
// a deployment concern, not a code one.
func ClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if first, _, found := strings.Cut(xff, ","); found {
			xff = first
		}
		if ip := strings.TrimSpace(xff); ip != "" {
			return ip
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
