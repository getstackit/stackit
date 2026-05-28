package main

import (
	"fmt"
	"strconv"
	"strings"
)

// resolvePort applies a $PORT fallback when -port wasn't passed explicitly.
// flagPort is the destination flag pointer. When portExplicit is true the
// env value is ignored (the operator wins). An empty env is a no-op.
func resolvePort(flagPort *int, portExplicit bool, env string) error {
	if portExplicit {
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
