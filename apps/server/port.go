package main

import (
	"fmt"
	"strconv"
	"strings"
)

// resolvePort applies a $PORT fallback when -port wasn't passed explicitly.
// flagPort is the destination flag pointer. When portExplicit is true the env
// value is ignored (the operator wins). $PORT is the PaaS convention, so it is
// honored only in production (prod); locally it is ignored, so a stray $PORT
// exported by a dev shell (cmux, Heroku toolbelt, ...) can't move the listener.
// An empty env is a no-op.
func resolvePort(flagPort *int, portExplicit, prod bool, env string) error {
	if portExplicit {
		return nil
	}
	if !prod {
		return nil
	}
	env = strings.TrimSpace(env)
	if env == "" {
		return nil
	}
	p, err := strconv.Atoi(env)
	if err != nil {
		return fmt.Errorf("invalid PORT env %q: %w", env, err)
	}
	*flagPort = p
	return nil
}
