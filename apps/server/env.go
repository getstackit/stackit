package main

import (
	"fmt"
	"net"
	"strings"
)

// serverEnv selects the deployment posture: a coherent bundle of defaults for
// the bind interface, log format, cookie security, and whether $PORT is
// honored. It is the single source of truth for "am I running locally or
// hosted", replacing the old heuristic that inferred this from $PORT — which
// collided with dev shells (cmux, Heroku-style toolchains) that export $PORT
// generically and silently flipped a local server into a public, auth-gated one.
type serverEnv int

const (
	envLocal serverEnv = iota
	envProduction
)

const (
	envNameLocal      = "local"
	envNameProduction = "production"
)

func (e serverEnv) isProduction() bool { return e == envProduction }

func (e serverEnv) String() string {
	if e == envProduction {
		return envNameProduction
	}
	return envNameLocal
}

// resolveEnv parses STACKIT_ENV. Unset (or "local"/"development") means local;
// "production" means hosted. Unrecognized values are a hard error so a typo on
// a deploy fails loudly rather than silently picking the wrong posture. The
// default is local — the fail-safe direction, since local binds loopback: a
// hosted deploy that forgets the var becomes unreachable, never exposed.
func resolveEnv(raw string) (serverEnv, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", envNameLocal, "development", "dev":
		return envLocal, nil
	case envNameProduction, "prod":
		return envProduction, nil
	default:
		return envLocal, fmt.Errorf("invalid STACKIT_ENV %q: want \"local\" or \"production\"", raw)
	}
}

// isLoopbackBind reports whether addr binds only the loopback interface, so an
// unauthenticated server on it is not reachable off-host. Empty ("all
// interfaces"), 0.0.0.0, and :: are treated as exposed, not loopback.
func isLoopbackBind(addr string) bool {
	addr = strings.TrimSpace(addr)
	if addr == "localhost" {
		return true
	}
	ip := net.ParseIP(addr)
	return ip != nil && ip.IsLoopback()
}
